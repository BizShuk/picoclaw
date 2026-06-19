#!/bin/bash
# scripts/a2a-test/verify-docker.sh
#
# End-to-end Docker test for the A2A pair (alice + bob) using the unified
# docker-compose.agent.yml. Each agent is a host folder (its own git repo)
# bind-mounted into its container; no Docker named volume.
#
# Run from repo root:
#   ./scripts/a2a-test/verify-docker.sh
#
# Prereq: agents/alice and agents/bob populated by gen-configs.py, with .env
#         in each containing MINIMAX_API_KEY.
set -u
cd "$(dirname "$0")/../.." || exit 1

COMPOSE_FILE="docker/docker-compose.agent.yml"
TOKEN="ECHO-7Q2"
PASS=0; FAIL=0

cleanup() {
  echo "[cleanup] docker compose down..."
  docker compose -f "$COMPOSE_FILE" down --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "[1/4] bring up alice + bob..."
docker compose -f "$COMPOSE_FILE" up -d --build

echo "[2/4] wait 30s for healthy + mDNS discovery..."
sleep 30

# Test 1: mutual discovery
A=$(docker logs alice-a2a 2>&1 | grep "Registered remote agent" | grep -ci bob   || true)
B=$(docker logs bob-a2a   2>&1 | grep "Registered remote agent" | grep -ci alice || true)
echo "alice sees bob : $A    bob sees alice : $B"
if [ "$A" -ge 1 ] && [ "$B" -ge 1 ]; then
  echo "TEST1: PASS (mutual mDNS discovery)"; PASS=$((PASS+1))
else
  echo "TEST1: FAIL"; FAIL=$((FAIL+1))
  echo "--- alice a2a ---"; docker logs alice-a2a 2>&1 | grep -iE "a2a|mdns|discov" | tail -20
  echo "--- bob a2a ---";   docker logs bob-a2a   2>&1 | grep -iE "a2a|mdns|discov" | tail -20
fi

# Test 2: alice asks bob
PROMPT="A remote agent named \"bob\" is available to you as a sub-agent. Use the spawn tool with agent_id set to \"bob\" and task set to exactly: reply with the single token ${TOKEN} and nothing else. After bob replies, output bob's reply verbatim as your final answer."
BODY=$(python3 -c 'import json,sys; print(json.dumps({"text": sys.argv[1]}))' "$PROMPT")
echo "[3/4] POST http://localhost:18791/a2a/v1/ask ..."
HTTP=$(curl -sS --max-time 150 -X POST "http://127.0.0.1:18791/a2a/v1/ask" \
  -H 'Content-Type: application/json' -d "$BODY")
echo "HTTP RESPONSE: $HTTP"
echo "--- data flow ---"
docker logs alice-a2a 2>&1 | grep -E "Tool call: spawn|Dialing peer WS" | head -2
docker logs bob-a2a   2>&1 | grep -E "Processing message from a2a|Response: $TOKEN" | head -2

RESP_HAS=$(echo "$HTTP" | grep -c "$TOKEN")
BOB_HANDLED=$(docker logs bob-a2a 2>&1 | grep -icE "a2a:alice")
if [ "$RESP_HAS" -ge 1 ] && [ "$BOB_HANDLED" -ge 1 ]; then
  echo "TEST2: PASS (A asked B, B processed, data round-tripped)"; PASS=$((PASS+1))
else
  echo "TEST2: FAIL"; FAIL=$((FAIL+1))
fi

echo "[4/4] summary: PASS=$PASS FAIL=$FAIL"
[ "$FAIL" = "0" ]
