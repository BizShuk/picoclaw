package a2a

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sipeed/picoclaw/pkg/bus"
)

func TestSessionRouteTable_PutGetRemove(t *testing.T) {
	tbl := newSessionRouteTable(time.Hour)
	tbl.put("a2a:bob", "sess-1", "bob", "frame-1")

	r := tbl.get("a2a:bob")
	require.NotNil(t, r)
	assert.Equal(t, "sess-1", r.sessionID)
	assert.Equal(t, "bob", r.peerID)
	assert.Equal(t, "frame-1", r.frameID)

	tbl.remove("a2a:bob")
	assert.Nil(t, tbl.get("a2a:bob"))
	assert.Nil(t, tbl.get(""))
}

func TestSessionRouteTable_ReapStale(t *testing.T) {
	tbl := newSessionRouteTable(time.Hour)
	tbl.put("a2a:bob", "sess-1", "bob", "frame-1")
	// Force the entry to look old, then reap.
	tbl.m["a2a:bob"].lastSeen = time.Now().Add(-2 * time.Hour)
	tbl.reapStale()
	assert.Nil(t, tbl.get("a2a:bob"), "stale entry should be reaped past TTL")
}

// The core fix: an outbound whose fresh context lacks the a2a_* routing keys is
// addressed back to the HTTP waiter by recovering the route via ChatID.
func TestSend_HTTPPeer_RecoversRoutingFromTable(t *testing.T) {
	c := &A2AChannel{
		httpAsks: newHTTPWaiterTable(),
		routes:   newSessionRouteTable(time.Hour),
	}
	chatID := "a2a:http:http-xyz"
	waiter := c.httpAsks.register("http-xyz")
	c.routes.put(chatID, "http-xyz", "http", "frame-9")

	// Outbound as built by the agent's response path: ChatID set, but Raw has no
	// a2a routing keys — only the final marker.
	_, err := c.Send(context.Background(), bus.OutboundMessage{
		ChatID:  chatID,
		Content: "the answer",
		Context: bus.InboundContext{Raw: map[string]string{"outbound_kind": "final"}},
	})
	require.NoError(t, err)

	select {
	case got := <-waiter:
		assert.Equal(t, "the answer", got)
	case <-time.After(time.Second):
		t.Fatal("waiter not resolved after routing recovery")
	}
	// Route is evicted once the reply is delivered.
	assert.Nil(t, c.routes.get(chatID), "route should be removed after final reply")
}
