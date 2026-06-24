package a2a

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/logger"
)

// httpAskRequest is the JSON body accepted by POST /a2a/v1/ask.
// It offers a plain HTTP entry point to run the agent, complementing the
// native WebSocket peer protocol on /a2a/v1/ws.
type httpAskRequest struct {
	Text    string `json:"text"`
	Session string `json:"session,omitempty"`
}

// httpAskResponse is the JSON body returned by POST /a2a/v1/ask.
type httpAskResponse struct {
	Answer  string `json:"answer"`
	Session string `json:"session"`
}

// httpWaiterTable correlates a synchronous HTTP ask with the asynchronous
// final outbound message the agent later produces for that session. The HTTP
// handler registers a channel keyed by session id and blocks on it; the
// channel's Send path resolves it when the final answer arrives over the bus.
type httpWaiterTable struct {
	mu sync.Mutex
	m  map[string]chan string
}

func newHTTPWaiterTable() *httpWaiterTable {
	return &httpWaiterTable{m: make(map[string]chan string)}
}

func (t *httpWaiterTable) register(sessionID string) chan string {
	ch := make(chan string, 1)
	t.mu.Lock()
	t.m[sessionID] = ch
	t.mu.Unlock()
	return ch
}

// resolve delivers the answer to a waiting handler and removes the entry.
// It reports whether a waiter was present.
func (t *httpWaiterTable) resolve(sessionID, answer string) bool {
	t.mu.Lock()
	ch, ok := t.m[sessionID]
	if ok {
		delete(t.m, sessionID)
	}
	t.mu.Unlock()
	if ok {
		ch <- answer
	}
	return ok
}

func (t *httpWaiterTable) drop(sessionID string) {
	t.mu.Lock()
	delete(t.m, sessionID)
	t.mu.Unlock()
}

// serveHTTPAsk handles POST /a2a/v1/ask: publish the prompt as an inbound
// message and block until the agent's final reply (or a timeout).
func (s *wsServer) serveHTTPAsk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req httpAskRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" {
		http.Error(w, "missing 'text'", http.StatusBadRequest)
		return
	}

	sessionID := strings.TrimSpace(req.Session)
	if sessionID == "" {
		sessionID = "http-" + uuid.New().String()
	}

	waiter := s.ch.httpAsks.register(sessionID)
	defer s.ch.httpAsks.drop(sessionID)

	frameID := uuid.New().String()
	chatID := "a2a:http:" + sessionID

	// Remember how to address the reply: the agent's final outbound is built
	// with a fresh context, so Send() recovers this route by ChatID to resolve
	// the waiter below.
	s.ch.routes.put(chatID, sessionID, "http", frameID)

	inbound := bus.InboundMessage{
		Channel: "a2a",
		ChatID:  chatID,
		Sender: bus.SenderInfo{
			Platform:    "a2a",
			PlatformID:  "http",
			CanonicalID: "a2a:http",
		},
		SessionKey: sessionID,
		Content:    req.Text,
		Context: bus.InboundContext{
			Raw: map[string]string{
				"a2a_frame_id":   frameID,
				"a2a_peer_id":    "http",
				"a2a_session_id": sessionID,
			},
		},
	}

	if err := s.ch.bus.PublishInbound(r.Context(), inbound); err != nil {
		logger.ErrorCF("a2a", "Failed to publish HTTP ask", map[string]any{"error": err.Error()})
		http.Error(w, "failed to dispatch message", http.StatusInternalServerError)
		return
	}

	timeout := s.ch.cfg.AskTimeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}

	select {
	case answer := <-waiter:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(httpAskResponse{Answer: answer, Session: sessionID})
	case <-time.After(timeout):
		http.Error(w, "timed out waiting for agent reply", http.StatusGatewayTimeout)
	case <-r.Context().Done():
		http.Error(w, "client cancelled", http.StatusRequestTimeout)
	}
}
