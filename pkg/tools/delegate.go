package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// MeshDelegator abstracts the mesh channel's ability to list remote agents
// and send messages, so the tool doesn't depend on the mesh package directly.
type MeshDelegator interface {
	ListRemoteAgents() []RemoteAgentInfo
	SendToAgent(ctx context.Context, agentID string, content string, timeout time.Duration) (string, error)
}

// RemoteAgentInfo is a serializable snapshot of a discovered remote agent.
type RemoteAgentInfo struct {
	AgentID      string   `json:"agent_id"`
	Name         string   `json:"name,omitempty"`
	Description  string   `json:"description,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	Host         string   `json:"host"`
	Port         int      `json:"port"`
}

// DelegateTool allows the LLM to discover and communicate with remote agents
// on the mesh network.
type DelegateTool struct {
	delegator MeshDelegator
}

func NewDelegateTool(delegator MeshDelegator) *DelegateTool {
	return &DelegateTool{delegator: delegator}
}

func (t *DelegateTool) Name() string {
	return "delegate_to_agent"
}

func (t *DelegateTool) Description() string {
	return `Discover and communicate with remote agents on the local mesh network.
When called without agent_id, lists all available remote agents with their descriptions and capabilities.
When called with agent_id and message, sends the message to that specific agent via WebSocket (preferred) or HTTP.`
}

func (t *DelegateTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"agent_id": map[string]any{
				"type":        "string",
				"description": "Target agent ID. Omit to list all available agents.",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "The message or prompt to send to the remote agent. Required when agent_id is provided.",
			},
		},
		"required": []string{},
	}
}

func (t *DelegateTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	agentID, _ := args["agent_id"].(string)
	message, _ := args["message"].(string)

	agentID = strings.TrimSpace(agentID)
	message = strings.TrimSpace(message)

	if agentID == "" {
		return t.listAgents()
	}

	if message == "" {
		return ErrorResult("message is required when agent_id is provided")
	}

	return t.sendToAgent(ctx, agentID, message)
}

func (t *DelegateTool) listAgents() *ToolResult {
	agents := t.delegator.ListRemoteAgents()
	if len(agents) == 0 {
		return SilentResult("No remote agents discovered on the mesh network.")
	}

	data, err := json.MarshalIndent(agents, "", "  ")
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to marshal agent list: %v", err))
	}

	return SilentResult(fmt.Sprintf("Discovered %d remote agent(s):\n%s", len(agents), string(data)))
}

func (t *DelegateTool) sendToAgent(ctx context.Context, agentID, message string) *ToolResult {
	result, err := t.delegator.SendToAgent(ctx, agentID, message, 30*time.Second)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to send to agent %q: %v", agentID, err))
	}
	return SilentResult(result)
}
