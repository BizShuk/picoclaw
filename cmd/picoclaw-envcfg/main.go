// Command picoclaw-envcfg renders a PicoClaw config from a template, injecting
// secrets loaded from a .env file via gosdk/config.Default().
//
// model_list.api_keys is the one PicoClaw secret with no env binding, so it must
// be written into config.json before launch. This tool unifies that step for both
// local runs and the container: gosdk loads .env (and .env.local), and the chosen
// provider's api_keys is replaced with the value of the named env key.
//
// Resolution order for the key (first non-empty wins):
//  1. process environment (os.Getenv) — honours `docker run -e` / compose env
//  2. gosdk/viper (.env / .env.local loaded by config.Default)
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	gconfig "github.com/bizshuk/gosdk/config"
	"github.com/spf13/viper"
)

func main() {
	template := flag.String("template", "/root/.picoclaw/config.a2a.template.json", "path to the config template")
	out := flag.String("out", "/root/.picoclaw/config.json", "path to write the rendered config")
	provider := flag.String("provider", "minimax", "model_list provider whose api_keys is injected")
	envKey := flag.String("env-key", "MINIMAX_API_KEY", "env/.env key holding the API key")
	appName := flag.String("app-name", "picoclaw", "gosdk app name (adds ~/.config/<app> to the .env search path)")
	flag.Parse()

	// gosdk loads .env / .env.local from ".", "./conf", and ~/.config/<appName>
	// into viper's global registry.
	gconfig.Default(gconfig.WithAppName(*appName))

	key := os.Getenv(*envKey)
	if key == "" {
		key = viper.GetString(*envKey)
	}
	if key == "" {
		fmt.Fprintf(os.Stderr, "[envcfg] WARNING: %s not found in environment or .env; rendering template as-is\n", *envKey)
	}

	raw, err := os.ReadFile(*template)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[envcfg] ERROR: read template %s: %v\n", *template, err)
		os.Exit(1)
	}

	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "[envcfg] ERROR: parse template: %v\n", err)
		os.Exit(1)
	}

	injected := 0
	if key != "" {
		injected = injectAPIKey(cfg, *provider, key)
	}

	rendered, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "[envcfg] ERROR: marshal config: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, rendered, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "[envcfg] ERROR: write %s: %v\n", *out, err)
		os.Exit(1)
	}

	fmt.Printf("[envcfg] rendered %s -> %s (injected %s into %d %q model(s))\n",
		*template, *out, *envKey, injected, *provider)
}

// injectAPIKey replaces api_keys on every model_list entry matching provider.
// It returns the number of entries updated.
func injectAPIKey(cfg map[string]any, provider, key string) int {
	list, ok := cfg["model_list"].([]any)
	if !ok {
		return 0
	}
	injected := 0
	for _, item := range list {
		model, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if p, _ := model["provider"].(string); p == provider {
			model["api_keys"] = []any{key}
			injected++
		}
	}
	return injected
}
