package config

import (
	"testing"
)

// TestModelConfig_APIKey_SentinelResolution verifies that APIKey() resolves
// a sentinel literal (e.g. "MINIMAX_API_KEY") to the corresponding env var
// via viper, while leaving real keys untouched. Empty literal is a clear
// "no key configured" signal and does NOT fall through to env lookup.
func TestModelConfig_APIKey_SentinelResolution(t *testing.T) {
	cases := []struct {
		name     string
		literal  string
		envValue string
		want     string
	}{
		{
			name:     "literal_key_wins",
			literal:  "sk-real-key",
			envValue: "sk-env-key",
			want:     "sk-real-key",
		},
		{
			name:     "sentinel_resolves_via_viper",
			literal:  "MINIMAX_API_KEY",
			envValue: "sk-env-key",
			want:     "sk-env-key",
		},
		{
			name:     "empty_literal_stays_empty",
			literal:  "",
			envValue: "sk-env-key",
			want:     "",
		},
		{
			name:     "sentinel_no_env_returns_sentinel",
			literal:  "MINIMAX_API_KEY",
			envValue: "",
			want:     "MINIMAX_API_KEY",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MINIMAX_API_KEY", tc.envValue)
			cfg := &ModelConfig{Provider: "minimax-i18n"}
			if tc.literal != "" {
				cfg.SetAPIKey(tc.literal)
			}
			if got := cfg.APIKey(); got != tc.want {
				t.Errorf("APIKey() = %q, want %q (env=%q)", got, tc.want, tc.envValue)
			}
		})
	}
}
