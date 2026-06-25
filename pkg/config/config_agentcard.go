package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sipeed/picoclaw/pkg/logger"
)

const (
	// AgentCardProtocolVersion is the Google A2A protocol version this
	// implementation targets (v0.3.0 stable spec).
	AgentCardProtocolVersion = "0.3.0"
	// AgentCardSidecarFile is the sidecar filename holding the Agent Card,
	// placed next to the main config file (mirrors .security.yml).
	AgentCardSidecarFile = "agent.json"
)

// agentCardPath returns the path to agent.json relative to the config file,
// mirroring securityPath for the security sidecar.
func agentCardPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), AgentCardSidecarFile)
}

// AgentCard is a Google A2A (v0.3.0) Agent Card describing the agent's
// identity, capabilities and skills. It is loaded from the agent.json sidecar
// and served at /.well-known/agent.json. The card is card-level compatible
// with A2A; picoclaw's task protocol stays its custom WS/HTTP-ask (the card's
// `url`/`preferredTransport`/`protocolVersion` are injected at serve time via
// ApplyRuntime, so values written in agent.json for those fields are hints).
type AgentCard struct {
	Name                              string                 `json:"name"`
	Description                       string                 `json:"description"`
	Version                           string                 `json:"version"`
	ProtocolVersion                   string                 `json:"protocolVersion"`
	URL                               string                 `json:"url"`
	PreferredTransport                string                 `json:"preferredTransport,omitempty"`
	AdditionalInterfaces              []AgentInterface       `json:"additionalInterfaces,omitempty"`
	Capabilities                      *AgentCapabilities     `json:"capabilities,omitempty"`
	DefaultInputModes                 []string               `json:"defaultInputModes,omitempty"`
	DefaultOutputModes                []string               `json:"defaultOutputModes,omitempty"`
	Skills                            []AgentSkill           `json:"skills"`
	Provider                          *AgentProvider         `json:"provider,omitempty"`
	DocumentationURL                  string                 `json:"documentationUrl,omitempty"`
	IconURL                           string                 `json:"iconUrl,omitempty"`
	Authentication                    *AgentAuthentication   `json:"authentication,omitempty"`
	SupportsAuthenticatedExtendedCard bool                   `json:"supportsAuthenticatedExtendedCard,omitempty"`
}

// AgentCapabilities mirrors the A2A capabilities object.
type AgentCapabilities struct {
	Streaming              bool              `json:"streaming,omitempty"`
	PushNotifications      bool              `json:"pushNotifications,omitempty"`
	StateTransitionHistory bool              `json:"stateTransitionHistory,omitempty"`
	Extensions             []AgentExtension  `json:"extensions,omitempty"`
}

// AgentSkill is one entry in the Agent Card skills array.
type AgentSkill struct {
	ID              string                          `json:"id"`
	Name            string                          `json:"name"`
	Description     string                          `json:"description"`
	Tags            []string                        `json:"tags,omitempty"`
	Examples        []string                        `json:"examples,omitempty"`
	InputModes      []string                        `json:"inputModes,omitempty"`
	OutputModes     []string                        `json:"outputModes,omitempty"`
	SecuritySchemes map[string]AgentSecurityScheme  `json:"securitySchemes,omitempty"`
}

// AgentInterface describes an additional transport/URL combo.
type AgentInterface struct {
	URL             string `json:"url"`
	Transport       string `json:"transport"`
	ProtocolVersion string `json:"protocolVersion,omitempty"`
}

// AgentProvider is the organization publishing the agent.
type AgentProvider struct {
	Organization string `json:"organization,omitempty"`
	URL          string `json:"url,omitempty"`
}

// AgentAuthentication declares security schemes (OpenAPI-style).
type AgentAuthentication struct {
	Schemes     map[string]AgentSecurityScheme `json:"schemes,omitempty"`
	Credentials string                         `json:"credentials,omitempty"`
}

// AgentSecurityScheme is an OpenAPI 3.x security scheme.
type AgentSecurityScheme struct {
	Type              string `json:"type"`
	Description       string `json:"description,omitempty"`
	Name              string `json:"name,omitempty"`
	In                string `json:"in,omitempty"`
	Scheme            string `json:"scheme,omitempty"`
	BearerFormat      string `json:"bearerFormat,omitempty"`
	Flows             any    `json:"flows,omitempty"`
	OpenIDConnectURL  string `json:"openIdConnectUrl,omitempty"`
}

// AgentExtension is a declared A2A protocol extension.
type AgentExtension struct {
	URI         string         `json:"uri"`
	Description string         `json:"description,omitempty"`
	Required    bool           `json:"required,omitempty"`
	Params      map[string]any `json:"params,omitempty"`
}

// Validate enforces the A2A required fields. It does NOT fail on a
// protocolVersion mismatch (only warns) so slightly older/newer cards still
// load. A non-empty url is required in the file even though ApplyRuntime
// overrides it at serve time — operators should put a placeholder URL.
func (c *AgentCard) Validate() error {
	if c == nil {
		return fmt.Errorf("agent card: nil")
	}
	if c.Name == "" {
		return fmt.Errorf("agent card: name required")
	}
	if c.Description == "" {
		return fmt.Errorf("agent card: description required")
	}
	if c.Version == "" {
		return fmt.Errorf("agent card: version required")
	}
	if c.URL == "" {
		return fmt.Errorf("agent card: url required")
	}
	if c.ProtocolVersion == "" {
		return fmt.Errorf("agent card: protocolVersion required")
	}
	if c.ProtocolVersion != AgentCardProtocolVersion {
		logger.WarnCF("config", "agent card protocolVersion differs from expected",
			map[string]any{"got": c.ProtocolVersion, "expected": AgentCardProtocolVersion})
	}
	if len(c.Skills) < 1 {
		return fmt.Errorf("agent card: at least one skill required")
	}
	for i, s := range c.Skills {
		if s.ID == "" {
			return fmt.Errorf("agent card: skill[%d] id required", i)
		}
		if s.Name == "" {
			return fmt.Errorf("agent card: skill[%d] name required", i)
		}
		if s.Description == "" {
			return fmt.Errorf("agent card: skill[%d] description required", i)
		}
	}
	return nil
}

// ApplyRuntime returns a shallow copy of the card with the runtime-dependent
// fields overridden: url is set to the agent's live /a2a/v1/ask endpoint,
// protocolVersion is forced to the supported version, and preferredTransport
// is forced to HTTP+JSON (picoclaw's closest match). The receiver is not
// mutated.
func (c *AgentCard) ApplyRuntime(host string, port int) *AgentCard {
	if c == nil {
		return nil
	}
	out := *c
	out.URL = fmt.Sprintf("http://%s:%d/a2a/v1/ask", host, port)
	out.ProtocolVersion = AgentCardProtocolVersion
	out.PreferredTransport = "HTTP+JSON"
	return &out
}

// loadAgentCard loads the A2A Agent Card from the agent.json sidecar next to
// the config file. A missing file is not an error (the agent is simply
// card-less). A malformed or invalid file is logged and ignored (card left
// nil) so a bad sidecar never takes down startup. A non-IsNotExist read error
// is returned (mirrors loadSecurityConfig).
func loadAgentCard(cfg *Config, cardPath string) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	data, err := os.ReadFile(cardPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read agent card: %w", err)
	}

	var card AgentCard
	if err := json.Unmarshal(data, &card); err != nil {
		logger.WarnCF("config", "agent.json parse failed; ignoring agent card",
			map[string]any{"path": cardPath, "error": err.Error()})
		return nil
	}
	if err := card.Validate(); err != nil {
		logger.WarnCF("config", "agent.json invalid; ignoring agent card",
			map[string]any{"path": cardPath, "error": err.Error()})
		return nil
	}
	cfg.AgentCard = &card
	return nil
}
