package config

import "github.com/spf13/viper"

// apiKeySentinelToEnv maps a sentinel literal string in api_keys[] to the
// env var name to read when the sentinel is encountered. Add new providers
// here when their config template uses a placeholder that should be
// resolved from process env at runtime.
var apiKeySentinelToEnv = map[string]string{
	"MINIMAX_API_KEY": "MINIMAX_API_KEY",
}

func init() {
	// Enable viper to read from process env without per-call BindEnv.
	// This makes ModelConfig.APIKey()'s sentinel resolution work as long
	// as the host shell / docker compose injects the env var.
	viper.AutomaticEnv()
}

// APIKey returns the first API key from apiKeys, resolving sentinels via
// viper (process env). If the literal value is a known sentinel (see
// apiKeySentinelToEnv), the corresponding env var is read; if the env var
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
