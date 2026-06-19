package a2a

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sipeed/picoclaw/pkg/bus"
)

func TestHTTPWaiterTable_ResolveDelivers(t *testing.T) {
	tbl := newHTTPWaiterTable()
	ch := tbl.register("s1")

	ok := tbl.resolve("s1", "hello")
	assert.True(t, ok, "resolve should report a waiter was present")

	select {
	case got := <-ch:
		assert.Equal(t, "hello", got)
	case <-time.After(time.Second):
		t.Fatal("waiter channel never delivered")
	}
}

func TestHTTPWaiterTable_ResolveMissing(t *testing.T) {
	tbl := newHTTPWaiterTable()
	assert.False(t, tbl.resolve("nope", "x"), "resolve on an unknown session is a no-op")
}

func TestHTTPWaiterTable_DropPreventsDelivery(t *testing.T) {
	tbl := newHTTPWaiterTable()
	_ = tbl.register("s1")
	tbl.drop("s1")
	assert.False(t, tbl.resolve("s1", "x"), "dropped waiter must not resolve")
}

// Send must resolve the HTTP waiter only on the final outbound, ignoring
// intermediate thought/tool messages, so the caller gets one clean answer.
func TestSend_HTTPPeer_ResolvesOnFinalOnly(t *testing.T) {
	c := &A2AChannel{httpAsks: newHTTPWaiterTable(), routes: newSessionRouteTable(time.Hour)}
	waiter := c.httpAsks.register("s1")

	raw := func(kind string) map[string]string {
		m := map[string]string{
			"a2a_frame_id":   "f1",
			"a2a_session_id": "s1",
			"a2a_peer_id":    "http",
		}
		if kind != "" {
			m["outbound_kind"] = kind
		}
		return m
	}

	// Intermediate (non-final) message: must not resolve the waiter.
	_, err := c.Send(context.Background(), bus.OutboundMessage{
		Content: "thinking...",
		Context: bus.InboundContext{Raw: raw("")},
	})
	require.NoError(t, err)
	select {
	case <-waiter:
		t.Fatal("waiter resolved on a non-final message")
	default:
	}

	// Final message: resolves the waiter with its content.
	_, err = c.Send(context.Background(), bus.OutboundMessage{
		Content: "the answer",
		Context: bus.InboundContext{Raw: raw("final")},
	})
	require.NoError(t, err)
	select {
	case got := <-waiter:
		assert.Equal(t, "the answer", got)
	case <-time.After(time.Second):
		t.Fatal("waiter never resolved on final message")
	}
}
