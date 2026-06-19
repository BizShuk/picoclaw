#!/bin/sh
# ---------------------------------------------------------------------------
# PicoClaw A2A agent entrypoint.
#
# model_list.api_keys is the one PicoClaw secret with no env binding (it loads
# only from config.json / .security.yml), so we render config.json from a
# template at container start, injecting the API key.
#
# The render is done by `picoclaw-envcfg`, which uses gosdk/config.Default()
# to load a mounted .env (and .env.local). A real environment variable
# (docker run -e / compose env) still takes precedence over the .env file.
#
#   .env (mounted)  ->  picoclaw-envcfg  ->  config.json  ->  launcher
# ---------------------------------------------------------------------------
set -e

TEMPLATE="${PICOCLAW_CONFIG_TEMPLATE:-/root/.picoclaw/config.a2a.template.json}"
TARGET="${PICOCLAW_CONFIG:-/root/.picoclaw/config.json}"

mkdir -p /root/.picoclaw/workspace

# gosdk/config.Default() searches the working directory for .env, so run the
# renderer from where the .env is mounted (/root/.picoclaw/.env).
cd /root/.picoclaw

picoclaw-envcfg \
  -template "$TEMPLATE" \
  -out "$TARGET" \
  -provider minimax-i18n \
  -env-key MINIMAX_API_KEY \
  -app-name picoclaw

exec picoclaw-launcher -console -public -no-browser "$TARGET"
