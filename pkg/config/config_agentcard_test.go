package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validCard() *AgentCard {
	return &AgentCard{
		Name:            "alice",
		Description:     "does things",
		Version:         "0.1.0",
		ProtocolVersion: AgentCardProtocolVersion,
		URL:             "http://localhost:8080/a2a/v1/ask",
		Skills:          []AgentSkill{{ID: "ask", Name: "Ask", Description: "ask the agent"}},
	}
}

func TestAgentCard_RoundTrip(t *testing.T) {
	c := validCard()
	c.Provider = &AgentProvider{Organization: "acme"}
	c.Capabilities = &AgentCapabilities{Streaming: false}
	data, err := json.Marshal(c)
	require.NoError(t, err)
	var got AgentCard
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, c.Name, got.Name)
	assert.Equal(t, c.URL, got.URL)
	require.Len(t, got.Skills, 1)
	assert.Equal(t, "ask", got.Skills[0].ID)
	assert.NotNil(t, got.Provider)
	assert.Equal(t, "acme", got.Provider.Organization)
}

func TestAgentCard_Validate(t *testing.T) {
	assert.NoError(t, validCard().Validate())

	for _, field := range []string{"Name", "Description", "Version", "URL", "ProtocolVersion"} {
		c := validCard()
		switch field {
		case "Name":
			c.Name = ""
		case "Description":
			c.Description = ""
		case "Version":
			c.Version = ""
		case "URL":
			c.URL = ""
		case "ProtocolVersion":
			c.ProtocolVersion = ""
		}
		require.Error(t, c.Validate(), field)
	}

	// no skills
	c := validCard()
	c.Skills = nil
	assert.ErrorContains(t, c.Validate(), "skill")

	// skill missing id
	c = validCard()
	c.Skills[0].ID = ""
	assert.ErrorContains(t, c.Validate(), "id")
}

func TestAgentCard_ApplyRuntime(t *testing.T) {
	c := validCard()
	c.URL = "http://placeholder/a2a/v1/ask"
	c.PreferredTransport = "JSONRPC"
	out := c.ApplyRuntime("10.0.0.5", 18791)
	assert.Equal(t, "http://10.0.0.5:18791/a2a/v1/ask", out.URL)
	assert.Equal(t, AgentCardProtocolVersion, out.ProtocolVersion)
	assert.Equal(t, "HTTP+JSON", out.PreferredTransport)
	// receiver is not mutated
	assert.Equal(t, "http://placeholder/a2a/v1/ask", c.URL)
	assert.Equal(t, "JSONRPC", c.PreferredTransport)
}

func TestLoadAgentCard_MissingFile(t *testing.T) {
	cfg := &Config{}
	require.NoError(t, loadAgentCard(cfg, filepath.Join(t.TempDir(), "agent.json")))
	assert.Nil(t, cfg.AgentCard)
}

func TestLoadAgentCard_Malformed(t *testing.T) {
	p := filepath.Join(t.TempDir(), "agent.json")
	require.NoError(t, os.WriteFile(p, []byte("{not json"), 0o644))
	cfg := &Config{}
	require.NoError(t, loadAgentCard(cfg, p)) // non-fatal
	assert.Nil(t, cfg.AgentCard)
}

func TestLoadAgentCard_Invalid(t *testing.T) {
	p := filepath.Join(t.TempDir(), "agent.json")
	require.NoError(t, os.WriteFile(p, []byte(`{"name":"x"}`), 0o644)) // missing required fields
	cfg := &Config{}
	require.NoError(t, loadAgentCard(cfg, p)) // non-fatal
	assert.Nil(t, cfg.AgentCard)
}

func TestLoadAgentCard_Valid(t *testing.T) {
	p := filepath.Join(t.TempDir(), "agent.json")
	data, err := json.Marshal(validCard())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(p, data, 0o644))
	cfg := &Config{}
	require.NoError(t, loadAgentCard(cfg, p))
	require.NotNil(t, cfg.AgentCard)
	assert.Equal(t, "alice", cfg.AgentCard.Name)
}
