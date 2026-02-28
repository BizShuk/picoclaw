package mesh

import "time"

// Protocol message types for agent-to-agent communication.
const (
	TypeAgentMessage = "agent.message"
	TypeAgentAck     = "agent.ack"
	TypePeerQuery    = "peer.query"
	TypePeerList     = "peer.list"
	TypePing         = "ping"
	TypePong         = "pong"
	TypeError        = "error"
)

// MeshMessage is the wire format for all mesh protocol messages.
type MeshMessage struct {
	Type      string         `json:"type"`
	ID        string         `json:"id,omitempty"`
	FromAgent string         `json:"from_agent"`
	ToAgent   string         `json:"to_agent,omitempty"`
	Timestamp int64          `json:"timestamp,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
}

func newMessage(msgType, fromAgent string, payload map[string]any) MeshMessage {
	return MeshMessage{
		Type:      msgType,
		FromAgent: fromAgent,
		Timestamp: time.Now().UnixMilli(),
		Payload:   payload,
	}
}

func newError(code, message string) MeshMessage {
	return MeshMessage{
		Type:      TypeError,
		Timestamp: time.Now().UnixMilli(),
		Payload: map[string]any{
			"code":    code,
			"message": message,
		},
	}
}
