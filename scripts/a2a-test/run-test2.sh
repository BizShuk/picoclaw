#!/bin/bash
# Test 2 — alice asks bob (via spawn) to return a token; verify the round-trip.
# Run from repo root AFTER run-test1.sh has left containers up.
#   ./scripts/a2a-test/run-test2.sh
set -u
cd "$(dirname "$0")/../.." || exit 1

TOKEN="ECHO-7Q2"
PROMPT="A remote agent named \"bob\" is available to you as a sub-agent. Use the spawn tool with agent_id set to \"bob\" and task set to exactly: reply with the single token ${TOKEN} and nothing else. After bob replies, output bob's reply verbatim as your final answer."
BODY=$(python3 -c 'import json,sys; print(json.dumps({"text": sys.argv[1]}))' "$PROMPT")

echo "[t2] waiting 5s for any leftover discovery..."
sleep 5

echo "[t2] POST http://localhost:18791/a2a/v1/ask ..."
HTTP=$(curl -sS --max-time 150 -X POST "http://127.0.0.1:18791/a2a/v1/ask" \
  -H 'Content-Type: application/json' -d "$BODY")
echo "HTTP RESPONSE: $HTTP"

echo "--- data flow ---"
docker logs alice-a2a 2>&1 | grep -E "Tool call: spawn|Dialing peer WS" | head -2
docker logs bob-a2a   2>&1 | grep -E "Processing message from a2a|Response: $TOKEN" | head -2

RESP_HAS=$(echo "$HTTP" | grep -c "$TOKEN")
BOB_HANDLED=$(docker logs bob-a2a 2>&1 | grep -icE "a2a:alice")

if [ "$RESP_HAS" -ge 1 ] && [ "$BOB_HANDLED" -ge 1 ]; then
  echo "TEST2: PASS (A asked B, B processed, data round-tripped)"
else
  echo "TEST2: FAIL"
  echo "--- alice tail ---"; docker logs alice-a2a 2>&1 | tail -30
  echo "--- bob tail ---";   docker logs bob-a2a   2>&1 | tail -30
fi
