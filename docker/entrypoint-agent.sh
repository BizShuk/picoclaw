#!/bin/sh
# docker/entrypoint-agent.sh
# ---------------------------------------------------------------------------
# PicoClaw generic agent entrypoint (replaces entrypoint.sh + entrypoint-a2a.sh).
#
# Mount layout (set by docker-compose.agent.yml):
#   /root/.picoclaw/
#     config.json          template (git tracked, NEVER overwritten)
#     .env                 MINIMAX_API_KEY etc. (gitignored)
#     workspace/           agent's working files
#     sessions/            chat history
#     logs/                runtime logs
#     config.active.json   rendered config (gitignored, regenerated each start)
#
# We render template -> config.active.json via picoclaw-envcfg so the
# source-of-truth config.json stays clean for git diffs.
# ---------------------------------------------------------------------------
set -e

CONFIG_DIR="${PICOCLAW_CONFIG_DIR:-/root/.picoclaw}"
TEMPLATE="$CONFIG_DIR/config.json"
TARGET="$CONFIG_DIR/config.active.json"

cd "$CONFIG_DIR"

if [ ! -f "$TEMPLATE" ]; then
  echo "[entrypoint] missing $TEMPLATE" >&2
  echo "[entrypoint] expected an agent folder bind-mounted here" >&2
  exit 1
fi

# picoclaw-envcfg uses gosdk/config.Default() to load .env from cwd.
# A real environment variable (docker run -e / compose env) still wins.
picoclaw-envcfg \
  -template "$TEMPLATE" \
  -out "$TARGET" \
  -provider minimax-i18n \
  -env-key MINIMAX_API_KEY \
  -app-name picoclaw

exec picoclaw-launcher -console -public -no-browser "$TARGET"
