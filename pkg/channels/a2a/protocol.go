package a2a

import (
	"encoding/json"
	"fmt"
)

const (
	ProtocolVersion = 1
	Subprotocol     = "picoclaw-a2a.v1"
)

type FrameType string

const (
	TypeAsk   FrameType = "ask"
	TypeReply FrameType = "reply"
	TypeError FrameType = "error"
	TypePing  FrameType = "ping"
	TypePong  FrameType = "pong"
	TypeBye   FrameType = "bye"
)

// Envelope is the outer container for every frame on the wire.
type Envelope struct {
	V         int       `json:"v"`
	Type      FrameType `json:"type"`
	FrameID   string    `json:"frame_id"`
	InReplyTo string    `json:"in_reply_to,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
	From      string    `json:"from,omitempty"`
	To        string    `json:"to,omitempty"`
	Ts        int64     `json:"ts,omitempty"`
	Payload   any       `json:"payload,omitempty"`
}

type AskPayload struct {
	Question string `json:"question"`
	MaxTurn  int    `json:"max_turn"`
	Turn     int    `json:"turn"`
}

type ReplyPayload struct {
	Answer string `json:"answer"`
	Turn   int    `json:"turn"`
	Done   bool   `json:"done,omitempty"`
}

type ErrorPayload struct {
	Code      string `json:"code"`
	Message   string `json:"message,omitempty"`
	Retryable bool   `json:"retryable,omitempty"`
}

type ByePayload struct {
	Reason string `json:"reason,omitempty"`
}

// ParseEnvelope deserializes raw bytes into an Envelope, validating
// version and known frame types, and decoding the payload to its typed form.
func ParseEnvelope(raw []byte) (*Envelope, error) {
	var stub struct {
		V       int             `json:"v"`
		Type    FrameType       `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(raw, &stub); err != nil {
		return nil, fmt.Errorf("decode envelope: %w", err)
	}
	if stub.V != ProtocolVersion {
		return nil, fmt.Errorf("unsupported protocol version %d", stub.V)
	}

	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	env.Payload = nil // re-decode below into typed form

	switch stub.Type {
	case TypeAsk:
		var p AskPayload
		if err := json.Unmarshal(stub.Payload, &p); err != nil {
			return nil, fmt.Errorf("decode ask payload: %w", err)
		}
		env.Payload = p
	case TypeReply:
		var p ReplyPayload
		if err := json.Unmarshal(stub.Payload, &p); err != nil {
			return nil, fmt.Errorf("decode reply payload: %w", err)
		}
		env.Payload = p
	case TypeError:
		var p ErrorPayload
		if err := json.Unmarshal(stub.Payload, &p); err != nil {
			return nil, fmt.Errorf("decode error payload: %w", err)
		}
		env.Payload = p
	case TypePing, TypePong:
		env.Payload = nil
	case TypeBye:
		var p ByePayload
		_ = json.Unmarshal(stub.Payload, &p)
		env.Payload = p
	default:
		return nil, fmt.Errorf("unknown frame type %q", stub.Type)
	}
	return &env, nil
}
