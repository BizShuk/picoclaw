package agent

import (
	"context"
	"fmt"

	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/routing"
	"github.com/sipeed/picoclaw/pkg/tools"
)

// RemoteAgentDescriptor represents a virtual agent discovered over the network (e.g. A2A).
type RemoteAgentDescriptor struct {
	ID          string          // e.g. "bob" (mDNS instance name)
	Name        string          // human-readable
	Description string          // peer capability description
	Source      string          // source channel identifier, e.g. "a2a-mdns"
	RemoteHook  RemoteSpawnHook // callback that handles the RPC AskPeer call
}

// RemoteSpawnHook is a callback that executes a sub-turn on a remote peer.
type RemoteSpawnHook func(ctx context.Context, cfg SubTurnConfig) (*tools.ToolResult, error)

// RegisterDynamic registers a remote peer as a spawnable virtual agent.
func (r *AgentRegistry) RegisterDynamic(d *RemoteAgentDescriptor) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := routing.NormalizeAgentID(d.ID)
	if _, exists := r.agents[id]; exists {
		return fmt.Errorf("agent id %q conflicts with local agent", id)
	}
	r.remotes[id] = d
	logger.InfoCF("agent", "Registered remote agent", map[string]any{
		"agent_id": id, "source": d.Source,
	})
	return nil
}

// UnregisterDynamic unregisters a remote peer.
func (r *AgentRegistry) UnregisterDynamic(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id = routing.NormalizeAgentID(id)
	delete(r.remotes, id)
	logger.InfoCF("agent", "Unregistered remote agent", map[string]any{
		"agent_id": id,
	})
}

// ResolveRemote returns the remote peer descriptor, or nil if not a remote.
func (r *AgentRegistry) ResolveRemote(id string) *RemoteAgentDescriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.remotes[routing.NormalizeAgentID(id)]
}
