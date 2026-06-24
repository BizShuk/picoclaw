package a2a

import (
	"context"
	"fmt"

	"github.com/sipeed/picoclaw/pkg/agent"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/tools"
)

type registryBridge struct {
	registry *agent.AgentRegistry
	ch       *A2AChannel
}

func (b *registryBridge) onPeerAdded(p *PeerInfo) {
	desc := &agent.RemoteAgentDescriptor{
		ID:          p.AgentID,
		Name:        p.AgentID,
		Description: p.Description,
		Source:      "a2a-mdns",
		RemoteHook:  b.makeHook(p.AgentID),
	}
	if desc.Description == "" {
		desc.Description = fmt.Sprintf("Remote A2A peer at %s:%d", p.Host, p.Port)
	}

	if err := b.registry.RegisterDynamic(desc); err != nil {
		logger.WarnCF("a2a", "Failed to register peer in registry", map[string]any{
			"peer":  p.AgentID,
			"error": err.Error(),
		})
	}
}

func (b *registryBridge) onPeerRemoved(peerID string) {
	b.registry.UnregisterDynamic(peerID)
}

func (b *registryBridge) makeHook(peerID string) agent.RemoteSpawnHook {
	return func(ctx context.Context, cfg agent.SubTurnConfig) (*tools.ToolResult, error) {
		// 1. Resolve parent session key from context turnState
		parentSessionKey := "default-session"
		if ts := agent.TurnStateFromContext(ctx); ts != nil && ts.ParentTurnState() != nil {
			parentSessionKey = ts.ParentTurnState().SessionKey()
		}

		// 2. Resolve external session ID
		sessionID := b.ch.sessions.GetOrCreate(parentSessionKey, peerID)

		// 3. Resolve max turn limit
		maxTurn := b.ch.cfg.MaxTurnDefault
		if maxTurn <= 0 {
			maxTurn = 6
		}

		question := cfg.SystemPrompt
		if cfg.ActualSystemPrompt != "" {
			question = cfg.ActualSystemPrompt + "\n\n" + cfg.SystemPrompt
		}

		// 4. Run AskPeer RPC Call
		answer, err := b.ch.AskPeer(ctx, peerID, sessionID, maxTurn, question)
		if err != nil {
			return nil, err
		}

		return &tools.ToolResult{
			ForLLM:  answer,
			ForUser: answer,
		}, nil
	}
}
