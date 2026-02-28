package mesh

import (
	"strings"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/discovery"
)

// RemoteAgent represents a discovered agent with its full descriptor.
type RemoteAgent struct {
	AgentID      string    `json:"agent_id"`
	Name         string    `json:"name,omitempty"`
	Description  string    `json:"description,omitempty"`
	Capabilities []string  `json:"capabilities,omitempty"`
	Host         string    `json:"host"`
	Port         int       `json:"port"`
	Features     []string  `json:"features,omitempty"`
	LastSeen     time.Time `json:"last_seen"`
}

// RemoteAgentRegistry stores discovered remote agents and allows querying
// them by ID or capability.
type RemoteAgentRegistry struct {
	agents map[string]*RemoteAgent
	mu     sync.RWMutex
}

// NewRemoteAgentRegistry creates an empty registry.
func NewRemoteAgentRegistry() *RemoteAgentRegistry {
	return &RemoteAgentRegistry{
		agents: make(map[string]*RemoteAgent),
	}
}

// Update adds or updates a remote agent from a discovered peer.
func (r *RemoteAgentRegistry) Update(peer discovery.PeerInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents[peer.AgentID] = &RemoteAgent{
		AgentID:      peer.AgentID,
		Name:         peer.Name,
		Description:  peer.Description,
		Capabilities: peer.Capabilities,
		Host:         peer.Host,
		Port:         peer.Port,
		Features:     peer.Features,
		LastSeen:     peer.LastSeen,
	}
}

// Remove deletes a remote agent from the registry.
func (r *RemoteAgentRegistry) Remove(agentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.agents, agentID)
}

// Get returns a specific remote agent by ID.
func (r *RemoteAgentRegistry) Get(agentID string) (RemoteAgent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.agents[agentID]
	if !ok {
		return RemoteAgent{}, false
	}
	return *a, true
}

// List returns all known remote agents.
func (r *RemoteAgentRegistry) List() []RemoteAgent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]RemoteAgent, 0, len(r.agents))
	for _, a := range r.agents {
		result = append(result, *a)
	}
	return result
}

// FindByCapability returns agents that have the given capability (case-insensitive).
func (r *RemoteAgentRegistry) FindByCapability(capability string) []RemoteAgent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	capLower := strings.ToLower(capability)
	var result []RemoteAgent
	for _, a := range r.agents {
		for _, c := range a.Capabilities {
			if strings.ToLower(c) == capLower {
				result = append(result, *a)
				break
			}
		}
	}
	return result
}
