package mesh

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/channels"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/discovery"
	"github.com/sipeed/picoclaw/pkg/identity"
	"github.com/sipeed/picoclaw/pkg/logger"
)

// meshConn represents a persistent WebSocket connection to or from a peer agent.
type meshConn struct {
	id        string
	agentID   string
	conn      *websocket.Conn
	writeMu   sync.Mutex
	closed    atomic.Bool
	direction string // "inbound" or "outbound"
}

func (mc *meshConn) writeJSON(v any) error {
	if mc.closed.Load() {
		return fmt.Errorf("connection closed")
	}
	mc.writeMu.Lock()
	defer mc.writeMu.Unlock()
	return mc.conn.WriteJSON(v)
}

func (mc *meshConn) close() {
	if mc.closed.CompareAndSwap(false, true) {
		mc.conn.Close()
	}
}

// MeshChannel implements inter-agent communication via HTTP and WebSocket,
// with UDP-broadcast-based LAN discovery.
type MeshChannel struct {
	*channels.BaseChannel
	config    config.MeshConfig
	discovery *discovery.Service
	registry  *RemoteAgentRegistry
	upgrader  websocket.Upgrader
	conns     sync.Map // connID -> *meshConn
	connCount atomic.Int32
	agentID   string
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewMeshChannel creates a new mesh channel for agent-to-agent communication.
func NewMeshChannel(cfg config.MeshConfig, messageBus *bus.MessageBus) (*MeshChannel, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("mesh token is required")
	}

	agentID := cfg.AgentID
	if agentID == "" {
		agentID = "agent-" + uuid.New().String()[:8]
	}

	base := channels.NewBaseChannel("mesh", cfg, messageBus, cfg.AllowFrom)

	host := cfg.Host
	if host == "" {
		host = detectLocalIP()
	}

	port := cfg.Port
	if port == 0 {
		port = 9100
	}

	broadcastPort := cfg.BroadcastPort
	if broadcastPort == 0 {
		broadcastPort = 9101
	}

	interval := time.Duration(cfg.BroadcastInterval) * time.Second
	if interval == 0 {
		interval = 60 * time.Second
	}

	peerTTL := time.Duration(cfg.PeerTTL) * time.Second
	if peerTTL == 0 {
		peerTTL = 3 * interval
	}

	disc := discovery.NewService(discovery.Config{
		AgentID:           agentID,
		AgentName:         cfg.AgentName,
		Description:       cfg.Description,
		Capabilities:      cfg.Capabilities,
		Host:              host,
		Port:              port,
		BroadcastPort:     broadcastPort,
		BroadcastInterval: interval,
		PeerTTL:           peerTTL,
		Token:             cfg.Token,
		Features:          []string{"http", "websocket"},
	})

	allowOrigins := cfg.AllowOrigins
	checkOrigin := func(r *http.Request) bool {
		if len(allowOrigins) == 0 {
			return true
		}
		origin := r.Header.Get("Origin")
		for _, allowed := range allowOrigins {
			if allowed == "*" || allowed == origin {
				return true
			}
		}
		return false
	}

	return &MeshChannel{
		BaseChannel: base,
		config:      cfg,
		discovery:   disc,
		registry:    NewRemoteAgentRegistry(),
		agentID:     agentID,
		upgrader: websocket.Upgrader{
			CheckOrigin:     checkOrigin,
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
		},
	}, nil
}

// Start implements Channel.
func (c *MeshChannel) Start(ctx context.Context) error {
	logger.InfoCF("mesh", "Starting mesh channel", map[string]any{
		"agent_id": c.agentID,
	})

	c.ctx, c.cancel = context.WithCancel(ctx)
	c.SetRunning(true)

	if err := c.discovery.Start(c.ctx); err != nil {
		return fmt.Errorf("failed to start discovery: %w", err)
	}

	c.discovery.OnPeerJoined(func(peer discovery.PeerInfo) {
		c.registry.Update(peer)
		logger.InfoCF("mesh", "Peer joined", map[string]any{
			"agent_id":     peer.AgentID,
			"host":         peer.Host,
			"port":         peer.Port,
			"description":  peer.Description,
			"capabilities": peer.Capabilities,
		})
	})
	c.discovery.OnPeerLeft(func(peer discovery.PeerInfo) {
		c.registry.Remove(peer.AgentID)
		logger.InfoCF("mesh", "Peer left", map[string]any{
			"agent_id": peer.AgentID,
		})
		c.closeConnectionsForAgent(peer.AgentID)
	})

	logger.InfoCF("mesh", "Mesh channel started", map[string]any{
		"agent_id": c.agentID,
	})
	return nil
}

// Stop implements Channel.
func (c *MeshChannel) Stop(ctx context.Context) error {
	logger.InfoC("mesh", "Stopping mesh channel")
	c.SetRunning(false)

	c.conns.Range(func(key, value any) bool {
		if mc, ok := value.(*meshConn); ok {
			mc.close()
		}
		c.conns.Delete(key)
		return true
	})

	c.discovery.Stop()

	if c.cancel != nil {
		c.cancel()
	}

	logger.InfoC("mesh", "Mesh channel stopped")
	return nil
}

// WebhookPath implements channels.WebhookHandler.
func (c *MeshChannel) WebhookPath() string { return "/mesh/" }

// ServeHTTP implements http.Handler for the shared HTTP server.
func (c *MeshChannel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/mesh")

	switch {
	case path == "/ws" || path == "/ws/":
		c.handleWebSocket(w, r)
	case path == "/message" || path == "/message/":
		c.handleHTTPMessage(w, r)
	case path == "/peers" || path == "/peers/":
		c.handlePeers(w, r)
	case path == "/info" || path == "/info/":
		c.handleInfo(w, r)
	default:
		http.NotFound(w, r)
	}
}

// Send implements Channel -- routes an outbound message to a target peer agent.
// chatID format: "mesh:<target_agent_id>"
func (c *MeshChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return channels.ErrNotRunning
	}

	targetAgentID := strings.TrimPrefix(msg.ChatID, "mesh:")

	// Try WebSocket first (already connected peers).
	if c.sendViaWebSocket(targetAgentID, msg.Content) {
		return nil
	}

	// Fall back to HTTP POST.
	return c.sendViaHTTP(ctx, targetAgentID, msg.Content)
}

// GetDiscovery returns the discovery service for external access.
func (c *MeshChannel) GetDiscovery() *discovery.Service {
	return c.discovery
}

// GetRegistry returns the remote agent registry.
func (c *MeshChannel) GetRegistry() *RemoteAgentRegistry {
	return c.registry
}

// GetAgentID returns this channel's agent ID.
func (c *MeshChannel) GetAgentID() string {
	return c.agentID
}

// DialPeer establishes an outbound WebSocket connection to a discovered peer.
// Returns the connection if successful; the caller should not manage the read loop
// as it is started automatically.
func (c *MeshChannel) DialPeer(agentID string) (*meshConn, error) {
	// Check if we already have a connection.
	var existing *meshConn
	c.conns.Range(func(key, value any) bool {
		mc, ok := value.(*meshConn)
		if ok && mc.agentID == agentID && !mc.closed.Load() {
			existing = mc
			return false
		}
		return true
	})
	if existing != nil {
		return existing, nil
	}

	peer, ok := c.discovery.GetPeer(agentID)
	if !ok {
		return nil, fmt.Errorf("peer %q not found in discovery", agentID)
	}

	u := url.URL{
		Scheme:   "ws",
		Host:     fmt.Sprintf("%s:%d", peer.Host, peer.Port),
		Path:     "/mesh/ws",
		RawQuery: fmt.Sprintf("token=%s&agent_id=%s", url.QueryEscape(c.config.Token), url.QueryEscape(c.agentID)),
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	conn, _, err := dialer.DialContext(c.ctx, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("WebSocket dial to %s failed: %w", agentID, err)
	}

	mc := &meshConn{
		id:        uuid.New().String(),
		agentID:   agentID,
		conn:      conn,
		direction: "outbound",
	}

	c.conns.Store(mc.id, mc)
	c.connCount.Add(1)

	logger.InfoCF("mesh", "Outbound WebSocket connected", map[string]any{
		"conn_id":  mc.id,
		"agent_id": agentID,
		"host":     peer.Host,
		"port":     peer.Port,
	})

	go c.wsReadLoop(mc)

	return mc, nil
}

// SendAndWait sends a message to a target agent and waits for an ack or response
// with a timeout. It tries WebSocket first (dialing if needed), then falls back to HTTP.
func (c *MeshChannel) SendAndWait(ctx context.Context, targetAgentID string, content string, timeout time.Duration) (string, error) {
	if !c.IsRunning() {
		return "", channels.ErrNotRunning
	}

	// Try via existing WebSocket or dial a new one.
	mc, err := c.DialPeer(targetAgentID)
	if err == nil {
		msg := newMessage(TypeAgentMessage, c.agentID, map[string]any{
			"content": content,
		})
		msg.ID = uuid.New().String()
		msg.ToAgent = targetAgentID

		if writeErr := mc.writeJSON(msg); writeErr == nil {
			return fmt.Sprintf("Message sent to %s via WebSocket (msg_id: %s)", targetAgentID, msg.ID), nil
		}
	}

	// Fall back to HTTP.
	if httpErr := c.sendViaHTTP(ctx, targetAgentID, content); httpErr != nil {
		return "", httpErr
	}
	return fmt.Sprintf("Message sent to %s via HTTP", targetAgentID), nil
}

// --- WebSocket handling ---

func (c *MeshChannel) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if !c.IsRunning() {
		http.Error(w, "channel not running", http.StatusServiceUnavailable)
		return
	}

	if !c.authenticate(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	maxConns := c.config.MaxConnections
	if maxConns <= 0 {
		maxConns = 100
	}
	if int(c.connCount.Load()) >= maxConns {
		http.Error(w, "too many connections", http.StatusServiceUnavailable)
		return
	}

	conn, err := c.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.ErrorCF("mesh", "WebSocket upgrade failed", map[string]any{"error": err.Error()})
		return
	}

	remoteAgentID := r.URL.Query().Get("agent_id")
	if remoteAgentID == "" {
		remoteAgentID = "unknown-" + uuid.New().String()[:8]
	}

	mc := &meshConn{
		id:        uuid.New().String(),
		agentID:   remoteAgentID,
		conn:      conn,
		direction: "inbound",
	}

	c.conns.Store(mc.id, mc)
	c.connCount.Add(1)

	logger.InfoCF("mesh", "Peer WebSocket connected", map[string]any{
		"conn_id":   mc.id,
		"agent_id":  remoteAgentID,
		"direction": "inbound",
	})

	go c.wsReadLoop(mc)
}

func (c *MeshChannel) wsReadLoop(mc *meshConn) {
	defer func() {
		mc.close()
		c.conns.Delete(mc.id)
		c.connCount.Add(-1)
		logger.InfoCF("mesh", "Peer WebSocket disconnected", map[string]any{
			"conn_id":  mc.id,
			"agent_id": mc.agentID,
		})
	}()

	readTimeout := 60 * time.Second
	_ = mc.conn.SetReadDeadline(time.Now().Add(readTimeout))
	mc.conn.SetPongHandler(func(string) error {
		_ = mc.conn.SetReadDeadline(time.Now().Add(readTimeout))
		return nil
	})

	go c.wsPingLoop(mc, 30*time.Second)

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		_, rawMsg, err := mc.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				logger.DebugCF("mesh", "WebSocket read error", map[string]any{
					"conn_id": mc.id,
					"error":   err.Error(),
				})
			}
			return
		}

		_ = mc.conn.SetReadDeadline(time.Now().Add(readTimeout))

		var msg MeshMessage
		if err := json.Unmarshal(rawMsg, &msg); err != nil {
			mc.writeJSON(newError("invalid_message", "failed to parse message"))
			continue
		}

		c.handleMeshMessage(mc, msg)
	}
}

func (c *MeshChannel) wsPingLoop(mc *meshConn, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if mc.closed.Load() {
				return
			}
			mc.writeMu.Lock()
			err := mc.conn.WriteMessage(websocket.PingMessage, nil)
			mc.writeMu.Unlock()
			if err != nil {
				return
			}
		}
	}
}

func (c *MeshChannel) handleMeshMessage(mc *meshConn, msg MeshMessage) {
	switch msg.Type {
	case TypePing:
		pong := newMessage(TypePong, c.agentID, nil)
		pong.ID = msg.ID
		mc.writeJSON(pong)

	case TypeAgentMessage:
		c.processInboundAgentMessage(msg)
		ack := newMessage(TypeAgentAck, c.agentID, map[string]any{"ack_id": msg.ID})
		mc.writeJSON(ack)

	case TypePeerQuery:
		peers := c.discovery.GetPeers()
		peerData := make([]map[string]any, 0, len(peers))
		for _, p := range peers {
			peerData = append(peerData, map[string]any{
				"agent_id":     p.AgentID,
				"name":         p.Name,
				"description":  p.Description,
				"capabilities": p.Capabilities,
				"host":         p.Host,
				"port":         p.Port,
				"features":     p.Features,
			})
		}
		reply := newMessage(TypePeerList, c.agentID, map[string]any{"peers": peerData})
		reply.ID = msg.ID
		mc.writeJSON(reply)

	default:
		mc.writeJSON(newError("unknown_type", fmt.Sprintf("unknown message type: %s", msg.Type)))
	}
}

func (c *MeshChannel) processInboundAgentMessage(msg MeshMessage) {
	content, _ := msg.Payload["content"].(string)
	if strings.TrimSpace(content) == "" {
		return
	}

	chatID := "mesh:" + msg.FromAgent
	senderID := msg.FromAgent

	peer := bus.Peer{Kind: "direct", ID: chatID}

	metadata := map[string]string{
		"platform":       "mesh",
		"from_agent":     msg.FromAgent,
		"mesh_message_id": msg.ID,
	}

	sender := bus.SenderInfo{
		Platform:    "mesh",
		PlatformID:  senderID,
		CanonicalID: identity.BuildCanonicalID("mesh", senderID),
		DisplayName: msg.FromAgent,
	}

	c.HandleMessage(c.ctx, peer, msg.ID, senderID, chatID, content, nil, metadata, sender)
}

// --- HTTP message handling ---

func (c *MeshChannel) handleHTTPMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !c.authenticate(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var msg MeshMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if msg.Type == "" {
		msg.Type = TypeAgentMessage
	}

	if msg.ID == "" {
		msg.ID = uuid.New().String()
	}

	c.processInboundAgentMessage(msg)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"status":   "ok",
		"agent_id": c.agentID,
		"ack_id":   msg.ID,
	})
}

func (c *MeshChannel) handlePeers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !c.authenticate(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	peers := c.discovery.GetPeers()
	result := make([]map[string]any, 0, len(peers))
	for _, p := range peers {
		result = append(result, map[string]any{
			"agent_id":     p.AgentID,
			"name":         p.Name,
			"description":  p.Description,
			"capabilities": p.Capabilities,
			"host":         p.Host,
			"port":         p.Port,
			"features":     p.Features,
			"last_seen":    p.LastSeen.Format(time.RFC3339),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"agent_id": c.agentID,
		"peers":    result,
	})
}

func (c *MeshChannel) handleInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"agent_id":     c.agentID,
		"agent_name":   c.config.AgentName,
		"description":  c.config.Description,
		"capabilities": c.config.Capabilities,
		"features":     []string{"http", "websocket", "discovery"},
		"connections":  c.connCount.Load(),
		"peers":        len(c.discovery.GetPeers()),
	})
}

// --- Outbound message delivery ---

func (c *MeshChannel) sendViaWebSocket(targetAgentID string, content string) bool {
	msg := newMessage(TypeAgentMessage, c.agentID, map[string]any{
		"content": content,
	})
	msg.ID = uuid.New().String()
	msg.ToAgent = targetAgentID

	var sent bool
	c.conns.Range(func(key, value any) bool {
		mc, ok := value.(*meshConn)
		if !ok || mc.agentID != targetAgentID {
			return true
		}
		if err := mc.writeJSON(msg); err != nil {
			logger.DebugCF("mesh", "WS send failed", map[string]any{
				"conn_id": mc.id,
				"error":   err.Error(),
			})
			return true
		}
		sent = true
		return false
	})
	return sent
}

func (c *MeshChannel) sendViaHTTP(ctx context.Context, targetAgentID string, content string) error {
	peer, ok := c.discovery.GetPeer(targetAgentID)
	if !ok {
		return fmt.Errorf("peer %q not found in discovery: %w", targetAgentID, channels.ErrSendFailed)
	}

	msg := newMessage(TypeAgentMessage, c.agentID, map[string]any{
		"content": content,
	})
	msg.ID = uuid.New().String()
	msg.ToAgent = targetAgentID

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}

	url := fmt.Sprintf("http://%s:%d/mesh/message", peer.Host, peer.Port)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.config.Token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP send to %s failed: %w", targetAgentID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("HTTP send to %s returned %d: %s", targetAgentID, resp.StatusCode, string(body))
	}

	logger.DebugCF("mesh", "HTTP message sent", map[string]any{
		"target":      targetAgentID,
		"content_len": len(content),
	})
	return nil
}

// --- Helpers ---

func (c *MeshChannel) authenticate(r *http.Request) bool {
	token := c.config.Token
	if token == "" {
		return false
	}

	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		if strings.TrimPrefix(auth, "Bearer ") == token {
			return true
		}
	}

	if r.URL.Query().Get("token") == token {
		return true
	}

	return false
}

func (c *MeshChannel) closeConnectionsForAgent(agentID string) {
	c.conns.Range(func(key, value any) bool {
		mc, ok := value.(*meshConn)
		if !ok {
			return true
		}
		if mc.agentID == agentID {
			mc.close()
			c.conns.Delete(key)
			c.connCount.Add(-1)
		}
		return true
	})
}

// detectLocalIP returns the preferred outbound local IPv4 address.
func detectLocalIP() string {
	conn, err := net.DialTimeout("udp4", "8.8.8.8:80", time.Second)
	if err != nil {
		return "0.0.0.0"
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}
