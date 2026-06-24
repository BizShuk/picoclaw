package a2a

import (
	"encoding/json"
	"testing"
)

func TestEnvelopeRoundTrip_Ask(t *testing.T) {
	src := Envelope{
		V: 1, Type: TypeAsk,
		FrameID: "f1", SessionID: "s1",
		From: "alice", To: "bob", Ts: 100,
		Payload: AskPayload{Question: "q?", MaxTurn: 3, Turn: 1},
	}
	raw, err := json.Marshal(&src)
	if err != nil {
		t.Fatal(err)
	}

	got, err := ParseEnvelope(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != TypeAsk {
		t.Fatalf("type = %s", got.Type)
	}
	ask, ok := got.Payload.(AskPayload)
	if !ok {
		t.Fatalf("payload not AskPayload: %T", got.Payload)
	}
	if ask.Question != "q?" || ask.MaxTurn != 3 || ask.Turn != 1 {
		t.Fatalf("payload mismatch: %+v", ask)
	}
}

func TestEnvelopeRoundTrip_Reply(t *testing.T) {
	src := Envelope{
		V: 1, Type: TypeReply,
		FrameID: "f2", InReplyTo: "f1",
		SessionID: "s1", From: "bob", To: "alice", Ts: 200,
		Payload: ReplyPayload{Answer: "a!", Turn: 1, Done: false},
	}
	raw, _ := json.Marshal(&src)
	got, err := ParseEnvelope(raw)
	if err != nil {
		t.Fatal(err)
	}
	rp, ok := got.Payload.(ReplyPayload)
	if !ok {
		t.Fatalf("payload not ReplyPayload: %T", got.Payload)
	}
	if rp.Answer != "a!" || got.InReplyTo != "f1" {
		t.Fatalf("mismatch: %+v inreply=%s", rp, got.InReplyTo)
	}
}

func TestEnvelopeRejectsUnknownVersion(t *testing.T) {
	raw := []byte(`{"v":99,"type":"ask","frame_id":"x","payload":{}}`)
	_, err := ParseEnvelope(raw)
	if err == nil {
		t.Fatal("expected error for unknown version")
	}
}

func TestEnvelopeRejectsUnknownType(t *testing.T) {
	raw := []byte(`{"v":1,"type":"nuke","frame_id":"x","payload":{}}`)
	_, err := ParseEnvelope(raw)
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}
