package a2a

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hashicorp/mdns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sipeed/picoclaw/pkg/config"
)

func testCard() *config.AgentCard {
	return &config.AgentCard{
		Name:            "alice",
		Description:     "d",
		Version:         "0.1.0",
		ProtocolVersion: config.AgentCardProtocolVersion,
		URL:             "http://placeholder/a2a/v1/ask",
		Skills:          []config.AgentSkill{{ID: "ask", Name: "Ask", Description: "ask"}},
	}
}

func TestServeAgentCard_GET(t *testing.T) {
	s := &wsServer{ch: &A2AChannel{card: testCard(), cfg: &config.A2ASettings{}, port: 18791}}
	req := httptest.NewRequest(http.MethodGet, "/.well-known/agent.json", nil)
	rec := httptest.NewRecorder()
	s.serveAgentCard(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")

	var got config.AgentCard
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, config.AgentCardProtocolVersion, got.ProtocolVersion)
	assert.Equal(t, "HTTP+JSON", got.PreferredTransport) // forced at serve time
	assert.Contains(t, got.URL, "/a2a/v1/ask")
	assert.Contains(t, got.URL, "18791") // runtime port injected
}

func TestServeAgentCard_PostNotAllowed(t *testing.T) {
	s := &wsServer{ch: &A2AChannel{card: testCard(), cfg: &config.A2ASettings{}, port: 18791}}
	req := httptest.NewRequest(http.MethodPost, "/.well-known/agent.json", nil)
	rec := httptest.NewRecorder()
	s.serveAgentCard(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestServeAgentCard_NoCard404(t *testing.T) {
	s := &wsServer{ch: &A2AChannel{card: nil, cfg: &config.A2ASettings{}, port: 18791}}
	req := httptest.NewRequest(http.MethodGet, "/.well-known/agent.json", nil)
	rec := httptest.NewRecorder()
	s.serveAgentCard(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestDiscovery_CardURL(t *testing.T) {
	d := &discovery{ch: &A2AChannel{cfg: &config.A2ASettings{}, port: 18791}}
	got := d.cardURL()
	assert.Contains(t, got, ".well-known/agent.json")
	assert.Contains(t, got, "18791")
}

func TestDiscovery_HandleEntry_ParsesCardURL(t *testing.T) {
	ch := &A2AChannel{agentID: "alice", peers: newPeerTable(time.Hour)}
	d := &discovery{ch: ch}
	entry := &mdns.ServiceEntry{
		Name:       "bob._picoclaw-a2a._tcp.local.",
		AddrV4:     net.ParseIP("10.0.0.5"),
		Port:       18792,
		InfoFields: []string{"v=1", "path=/a2a/v1/ws", "agent=bob", "desc=hi", "card=http://10.0.0.5:18792/.well-known/agent.json"},
	}
	d.handleEntry(entry)

	peer, ok := ch.peers.Get("bob")
	require.True(t, ok)
	assert.Equal(t, "http://10.0.0.5:18792/.well-known/agent.json", peer.CardURL)
}

func TestPeerTable_UpsertPreservesCardURL(t *testing.T) {
	pt := newPeerTable(time.Hour)
	pt.Upsert(&PeerInfo{AgentID: "bob", Host: "h", Port: 1, CardURL: "http://x/.well-known/agent.json"})
	pt.Upsert(&PeerInfo{AgentID: "bob", Host: "h2", Port: 2, CardURL: "http://y/.well-known/agent.json"})

	got, ok := pt.Get("bob")
	require.True(t, ok)
	assert.Equal(t, "http://y/.well-known/agent.json", got.CardURL)
	assert.Equal(t, "h2", got.Host)
}
