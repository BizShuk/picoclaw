package mesh

import (
	"context"
	"time"

	"github.com/sipeed/picoclaw/pkg/tools"
)

// meshDelegator adapts MeshChannel to the tools.MeshDelegator interface.
type meshDelegator struct {
	channel *MeshChannel
}

// NewDelegator creates a MeshDelegator backed by the given MeshChannel.
func NewDelegator(ch *MeshChannel) tools.MeshDelegator {
	return &meshDelegator{channel: ch}
}

func (d *meshDelegator) ListRemoteAgents() []tools.RemoteAgentInfo {
	agents := d.channel.registry.List()
	result := make([]tools.RemoteAgentInfo, 0, len(agents))
	for _, a := range agents {
		result = append(result, tools.RemoteAgentInfo{
			AgentID:      a.AgentID,
			Name:         a.Name,
			Description:  a.Description,
			Capabilities: a.Capabilities,
			Host:         a.Host,
			Port:         a.Port,
		})
	}
	return result
}

func (d *meshDelegator) SendToAgent(ctx context.Context, agentID string, content string, timeout time.Duration) (string, error) {
	return d.channel.SendAndWait(ctx, agentID, content, timeout)
}
