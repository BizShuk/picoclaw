package config

import (
	"github.com/bizshuk/gosdk/config"
	"github.com/spf13/viper"
)

// apiKeySentinelToEnv maps a sentinel literal string in api_keys[] to the
// env var name to read when the sentinel is encountered. Add new providers
// here when their config template uses a placeholder that should be
// resolved from process env at runtime.
var apiKeySentinelToEnv = map[string]string{
	"MINIMAX_API_KEY":   "MINIMAX_API_KEY",
	"ANTHROPIC_API_KEY": "ANTHROPIC_API_KEY",
}

func init() {
	// Initialize gosdk config with appName="picoclaw". This loads .env,
	// .env.local, config.yaml, and settings.json from the standard search
	// paths into viper's global store (set automatically by gosdk).
	// After that, viper.AutomaticEnv() is already active, so all
	// ModelConfig.APIKey() sentinel lookups resolve via viper.GetString().
	config.Default(config.WithAppName("picoclaw"))
}

// APIKey returns the first API key from apiKeys, resolving sentinels via
// viper (process env + .env file). If the literal value is a known sentinel
// (see apiKeySentinelToEnv), the corresponding env var is read; if the env var
// is unset, the sentinel is returned (caller surfaces a clear 401).
func (c *ModelConfig) APIKey() string {
	if len(c.APIKeys) == 0 {
		return ""
	}
	literal := c.APIKeys[0].String()
	if envName, ok := apiKeySentinelToEnv[literal]; ok {
		if v := viper.GetString(envName); v != "" {
			return v
		}
	}
	return literal
}
