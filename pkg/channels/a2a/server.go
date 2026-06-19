package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"nhooyr.io/websocket"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/logger"
)

type peerWriter struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

func (w *peerWriter) WriteFrame(ctx context.Context, env *Envelope) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	raw, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal frame: %w", err)
	}
	return w.conn.Write(ctx, websocket.MessageText, raw)
}

type wsServer struct {
	addr    string
	httpSrv *http.Server
	ch      *A2AChannel
	mu      sync.Mutex
	conns   map[string]*peerWriter // session_id -> peerWriter
}

func newWSServer(addr string, ch *A2AChannel) *wsServer {
	s := &wsServer{
		addr:  addr,
		ch:    ch,
		conns: make(map[string]*peerWriter),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/a2a/v1/ws", s.serveHTTP)
	mux.HandleFunc("/a2a/v1/ask", s.serveHTTPAsk)
	s.httpSrv = &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	return s
}

func (s *wsServer) Start() error {
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("ws server listen failed: %w", err)
	}
	if tcpAddr, ok := listener.Addr().(*net.TCPAddr); ok {
		s.ch.port = tcpAddr.Port
	}

	logger.InfoCF("a2a", "Starting WS server", map[string]any{"addr": listener.Addr().String(), "port": s.ch.port})
	go func() {
		if err := s.httpSrv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.ErrorCF("a2a", "WS server Serve failed", map[string]any{"error": err.Error()})
		}
	}()
	return nil
}

func (s *wsServer) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	// Close all active connections in the server
	for _, writer := range s.conns {
		_ = writer.conn.Close(websocket.StatusNormalClosure, "server shutting down")
	}
	s.conns = make(map[string]*peerWriter)
	s.mu.Unlock()

	return s.httpSrv.Shutdown(ctx)
}

func (s *wsServer) registerConn(sessionID string, w *peerWriter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conns[sessionID] = w
}

func (s *wsServer) unregisterConn(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, sessionID)
}

func (s *wsServer) getWriter(sessionID string) (*peerWriter, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.conns[sessionID]
	return w, ok
}

func (s *wsServer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	opts := &websocket.AcceptOptions{
		Subprotocols: []string{Subprotocol},
	}
	conn, err := websocket.Accept(w, r, opts)
	if err != nil {
		logger.WarnCF("a2a", "WS upgrade failed", map[string]any{"error": err.Error()})
		return
	}

	if conn.Subprotocol() != Subprotocol {
		conn.Close(websocket.StatusPolicyViolation, "unsupported subprotocol")
		return
	}

	s.serveConn(conn)
}

func (s *wsServer) serveConn(conn *websocket.Conn) {
	ctx := context.Background()
	writer := &peerWriter{conn: conn}
	var registeredSessions []string

	defer func() {
		conn.Close(websocket.StatusNormalClosure, "")
		for _, sid := range registeredSessions {
			s.unregisterConn(sid)
		}
	}()

	for {
		_, msgBytes, err := conn.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) != -1 {
				break // normal close
			}
			logger.WarnCF("a2a", "Read frame failed", map[string]any{"error": err.Error()})
			break
		}

		env, err := ParseEnvelope(msgBytes)
		if err != nil {
			logger.WarnCF("a2a", "Parse envelope failed", map[string]any{"error": err.Error()})
			continue
		}

		switch env.Type {
		case TypeAsk:
			ask, ok := env.Payload.(AskPayload)
			if !ok {
				s.sendError(ctx, writer, env.FrameID, "invalid_payload", "expected ask payload", false)
				continue
			}

			// Check turn counter limits
			_, err = s.ch.turns.CheckAndIncrement(env.SessionID, ask.MaxTurn)
			if err != nil {
				code := "internal_error"
				if errors.Is(err, ErrMaxTurnExceeded) {
					code = "max_turn_exceeded"
				} else if errors.Is(err, ErrSessionEnded) {
					code = "session_ended"
				}
				s.sendError(ctx, writer, env.FrameID, code, err.Error(), false)
				continue
			}

			// Resolve local session key and bind
			localKey, _ := s.ch.sessions.Resolve(env.SessionID, env.From)
			s.registerConn(env.SessionID, writer)
			registeredSessions = append(registeredSessions, env.SessionID)

			// Remember how to address the reply: the agent's outbound is built
			// with a fresh context, so Send() recovers this route by ChatID.
			s.ch.routes.put("a2a:"+env.From, env.SessionID, env.From, env.FrameID)

			// Publish to bus
			inbound := bus.InboundMessage{
				Channel: "a2a",
				ChatID:  "a2a:" + env.From,
				Sender: bus.SenderInfo{
					Platform:    "a2a",
					PlatformID:  env.From,
					CanonicalID: "a2a:" + env.From,
				},
				SessionKey: localKey,
				Content:    ask.Question,
				Context: bus.InboundContext{
					Raw: map[string]string{
						"a2a_frame_id":   env.FrameID,
						"a2a_peer_id":    env.From,
						"a2a_session_id": env.SessionID,
						"a2a_max_turn":   fmt.Sprintf("%d", ask.MaxTurn),
						"a2a_turn":       fmt.Sprintf("%d", ask.Turn),
					},
				},
			}
			if pubErr := s.ch.bus.PublishInbound(ctx, inbound); pubErr != nil {
				logger.ErrorCF("a2a", "Failed to publish inbound A2A message", map[string]any{"error": pubErr.Error()})
				s.sendError(ctx, writer, env.FrameID, "bus_error", "failed to process message internally", true)
			}

		case TypePing:
			pong := &Envelope{
				V:       ProtocolVersion,
				Type:    TypePong,
				FrameID: env.FrameID,
				Ts:      time.Now().Unix(),
			}
			_ = writer.WriteFrame(ctx, pong)

		case TypeBye:
			break
		}
	}
}

func (s *wsServer) sendError(ctx context.Context, w *peerWriter, replyTo, code, msg string, retryable bool) {
	env := &Envelope{
		V:         ProtocolVersion,
		Type:      TypeError,
		InReplyTo: replyTo,
		Ts:        time.Now().Unix(),
		Payload: ErrorPayload{
			Code:      code,
			Message:   msg,
			Retryable: retryable,
		},
	}
	_ = w.WriteFrame(ctx, env)
}
