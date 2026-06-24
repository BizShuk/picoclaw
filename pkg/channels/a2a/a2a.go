package a2a

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/sipeed/picoclaw/pkg/agent"
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/channels"
	"github.com/sipeed/picoclaw/pkg/config"
)

type A2AChannel struct {
	*channels.BaseChannel
	bc       *config.Channel
	cfg      *config.A2ASettings
	bus      *bus.MessageBus

	agentID  string
	port     int

	server    *wsServer
	clients   *wsClientPool
	peers     *peerTable
	discovery *discovery

	sessions *sessionMap
	turns    *turnCounter
	httpAsks *httpWaiterTable
	routes   *sessionRouteTable

	bridge *registryBridge

	ctx    context.Context
	cancel context.CancelFunc
}

// RegistryAware interface is used by the gateway to inject the agent registry into the channel.
type RegistryAware interface {
	SetAgentRegistry(r *agent.AgentRegistry)
}

func NewA2AChannel(
	bc *config.Channel,
	cfg *config.A2ASettings,
	bus *bus.MessageBus,
	agentID string,
) *A2AChannel {
	ch := &A2AChannel{
		bc:       bc,
		cfg:      cfg,
		bus:      bus,
		agentID:  agentID,
		port:     cfg.Port,
		sessions: newSessionMap(),
		turns:    newTurnCounter(),
		httpAsks: newHTTPWaiterTable(),
		routes:   newSessionRouteTable(24 * time.Hour),
	}
	ch.BaseChannel = channels.NewBaseChannel(
		string(config.ChannelA2A),
		cfg,
		bus,
		bc.AllowFrom,
	)
	ch.BaseChannel.SetOwner(ch)

	peerTTL := cfg.PeerTTL
	if peerTTL <= 0 {
		peerTTL = 60 * time.Second
	}
	ch.peers = newPeerTable(peerTTL)
	ch.clients = newWSClientPool(60 * time.Second, ch)

	return ch
}

func (c *A2AChannel) SetAgentRegistry(r *agent.AgentRegistry) {
	c.bridge = &registryBridge{registry: r, ch: c}
	c.peers.onAdd = c.bridge.onPeerAdded
	c.peers.onRemove = c.bridge.onPeerRemoved
}

func (c *A2AChannel) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(context.Background())

	// 0. Start the reply-route janitor (TTL eviction backstop).
	c.routes.startJanitor(c.ctx)

	// 1. Start Server
	c.server = newWSServer(fmt.Sprintf(":%d", c.port), c)
	if err := c.server.Start(); err != nil {
		return fmt.Errorf("start A2A server: %w", err)
	}

	// 2. Start Discovery (if enabled)
	if c.cfg.AnnounceInterval >= 0 {
		c.discovery = newDiscovery(c)
		if err := c.discovery.Start(); err != nil {
			_ = c.server.Shutdown(ctx)
			return fmt.Errorf("start A2A discovery: %w", err)
		}
	}

	c.SetRunning(true)
	return nil
}

func (c *A2AChannel) Stop(ctx context.Context) error {
	if c.cancel != nil {
		c.cancel()
	}

	c.routes.stop()

	if c.discovery != nil {
		c.discovery.Stop()
	}

	if c.server != nil {
		_ = c.server.Shutdown(ctx)
	}

	if c.clients != nil {
		c.clients.Close()
	}

	if c.bridge != nil {
		peers := c.peers.List()
		for _, p := range peers {
			c.bridge.onPeerRemoved(p.AgentID)
		}
	}

	c.SetRunning(false)
	return nil
}

func (c *A2AChannel) Send(ctx context.Context, msg bus.OutboundMessage) ([]string, error) {
	frameID := msg.Context.Raw["a2a_frame_id"]
	sessionID := msg.Context.Raw["a2a_session_id"]
	peerID := msg.Context.Raw["a2a_peer_id"]

	// The agent's response path rebuilds the outbound with a fresh context that
	// drops the inbound Raw, so recover the A2A routing by ChatID when missing.
	if frameID == "" || sessionID == "" || peerID == "" {
		if r := c.routes.get(msg.ChatID); r != nil {
			if frameID == "" {
				frameID = r.frameID
			}
			if sessionID == "" {
				sessionID = r.sessionID
			}
			if peerID == "" {
				peerID = r.peerID
			}
		}
	}

	if frameID == "" || sessionID == "" || peerID == "" {
		return nil, fmt.Errorf("missing A2A routing context in outbound message")
	}

	done := msg.Context.Raw["done"] == "true" || msg.Context.Raw["outbound_kind"] == "final"

	// HTTP ask path: the plain POST /a2a/v1/ask handler is blocked on a waiter.
	// Only the final answer resolves it; intermediate thought/tool messages are
	// dropped so the HTTP caller receives a single clean reply.
	if peerID == "http" {
		if done {
			c.httpAsks.resolve(sessionID, msg.Content)
			c.routes.remove(msg.ChatID)
		}
		return []string{frameID}, nil
	}

	writer, ok := c.server.getWriter(sessionID)
	if !ok {
		return nil, fmt.Errorf("no active server connection for session %s", sessionID)
	}

	replyEnv := &Envelope{
		V:         ProtocolVersion,
		Type:      TypeReply,
		SessionID: sessionID,
		FrameID:   uuid.New().String(),
		InReplyTo: frameID,
		From:      c.agentID,
		Ts:        time.Now().Unix(),
		Payload: ReplyPayload{
			Answer: msg.Content,
			Done:   done,
		},
	}

	if err := writer.WriteFrame(ctx, replyEnv); err != nil {
		return nil, fmt.Errorf("send reply frame: %w", err)
	}

	if done {
		c.turns.MarkDone(sessionID)
		c.routes.remove(msg.ChatID)
	}

	return []string{replyEnv.FrameID}, nil
}
