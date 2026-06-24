package a2a

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"nhooyr.io/websocket"

	"github.com/sipeed/picoclaw/pkg/logger"
)

type peerConn struct {
	peer     *PeerInfo
	ws       *websocket.Conn
	writer   *peerWriter
	pending  *pendingMap
	lastUsed atomic.Int64
	closed   atomic.Bool
}

func (pc *peerConn) Close() {
	if pc.closed.CompareAndSwap(false, true) {
		_ = pc.ws.Close(websocket.StatusNormalClosure, "closing connection")
		// Cancel all pending calls
		pc.pending.CancelAll()
	}
}

type wsClientPool struct {
	mu       sync.Mutex
	conns    map[string]*peerConn
	idleTTL  time.Duration
	ch       *A2AChannel
	cancel   context.CancelFunc
	ctx      context.Context
}

func newWSClientPool(idleTTL time.Duration, ch *A2AChannel) *wsClientPool {
	ctx, cancel := context.WithCancel(context.Background())
	p := &wsClientPool{
		conns:   make(map[string]*peerConn),
		idleTTL: idleTTL,
		ch:      ch,
		ctx:     ctx,
		cancel:  cancel,
	}
	go p.reapLoop()
	return p
}

func (p *wsClientPool) Close() {
	p.cancel()
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, pc := range p.conns {
		pc.Close()
	}
	p.conns = make(map[string]*peerConn)
}

func (p *wsClientPool) GetOrDial(ctx context.Context, peerID string) (*peerConn, error) {
	p.mu.Lock()
	pc, ok := p.conns[peerID]
	if ok && !pc.closed.Load() {
		pc.lastUsed.Store(time.Now().Unix())
		p.mu.Unlock()
		return pc, nil
	}
	p.mu.Unlock()

	// Dial peer
	info, ok := p.ch.peers.Get(peerID)
	if !ok {
		return nil, fmt.Errorf("peer %q not found in discovery table", peerID)
	}

	addr := net.JoinHostPort(info.Host, fmt.Sprintf("%d", info.Port))
	wsURL := fmt.Sprintf("ws://%s/a2a/v1/ws", addr)

	opts := &websocket.DialOptions{
		Subprotocols: []string{Subprotocol},
	}
	
	dialCtx, dialCancel := context.WithTimeout(ctx, 5*time.Second)
	defer dialCancel()

	logger.InfoCF("a2a", "Dialing peer WS", map[string]any{"url": wsURL})
	conn, _, err := websocket.Dial(dialCtx, wsURL, opts)
	if err != nil {
		return nil, fmt.Errorf("dial peer %s: %w", peerID, err)
	}

	pc = &peerConn{
		peer:    info,
		ws:      conn,
		writer:  &peerWriter{conn: conn},
		pending: newPendingMap(),
	}
	pc.lastUsed.Store(time.Now().Unix())

	p.mu.Lock()
	// Check if a connection was established in parallel
	if existing, ok := p.conns[peerID]; ok && !existing.closed.Load() {
		pc.Close()
		p.mu.Unlock()
		return existing, nil
	}
	p.conns[peerID] = pc
	p.mu.Unlock()

	go p.serveClientRead(pc)

	return pc, nil
}

func (p *wsClientPool) serveClientRead(pc *peerConn) {
	ctx := context.Background()
	defer func() {
		pc.Close()
		p.mu.Lock()
		if current, ok := p.conns[pc.peer.AgentID]; ok && current == pc {
			delete(p.conns, pc.peer.AgentID)
		}
		p.mu.Unlock()
	}()

	for {
		_, msgBytes, err := pc.ws.Read(ctx)
		if err != nil {
			break // connection lost
		}

		env, err := ParseEnvelope(msgBytes)
		if err != nil {
			logger.WarnCF("a2a", "Client failed to parse reply envelope", map[string]any{"error": err.Error()})
			continue
		}

		if env.Type == TypeReply || env.Type == TypeError {
			pc.pending.Deliver(env.InReplyTo, env)
		} else if env.Type == TypePing {
			pong := &Envelope{
				V:       ProtocolVersion,
				Type:    TypePong,
				FrameID: env.FrameID,
				Ts:      time.Now().Unix(),
			}
			_ = pc.writer.WriteFrame(ctx, pong)
		}
	}
}

func (p *wsClientPool) reapLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.mu.Lock()
			now := time.Now().Unix()
			for peerID, pc := range p.conns {
				if now-pc.lastUsed.Load() > int64(p.idleTTL.Seconds()) {
					logger.InfoCF("a2a", "Closing idle client connection", map[string]any{"peer": peerID})
					pc.Close()
					delete(p.conns, peerID)
				}
			}
			p.mu.Unlock()
		}
	}
}
