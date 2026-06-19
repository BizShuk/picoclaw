#!/bin/bash
# Test 1 — mutual mDNS discovery between two PicoClaw agents (Docker harness).
# Run from repo root:
#   ./scripts/a2a-test/run-test1.sh
#
# Prereq: agents/alice and agents/bob exist with config.json (run gen-configs.py).
#         Each has .env with MINIMAX_API_KEY set.
set -u
cd "$(dirname "$0")/../.." || exit 1

COMPOSE_FILE="docker/docker-compose.agent.yml"
COMPOSE=(docker compose -f "$COMPOSE_FILE")

# alice first (host networking; standard mDNS)
"${COMPOSE[@]}" up -d alice

# bob shares alice's netns (same-host mDNS test)
"${COMPOSE[@]}" up -d bob

echo "[t1] waiting 20s for mDNS discovery..."
sleep 20

A=$(docker logs alice-a2a 2>&1 | grep "Registered remote agent" | grep -ci bob   || true)
B=$(docker logs bob-a2a   2>&1 | grep "Registered remote agent" | grep -ci alice || true)
echo "alice sees bob : $A    bob sees alice : $B"

if [ "$A" -ge 1 ] && [ "$B" -ge 1 ]; then
  echo "TEST1: PASS (mutual mDNS discovery)"
else
  echo "TEST1: FAIL"
  echo "--- alice a2a ---"; docker logs alice-a2a 2>&1 | grep -iE "a2a|mdns|discov" | tail -20
  echo "--- bob a2a ---";   docker logs bob-a2a   2>&1 | grep -iE "a2a|mdns|discov" | tail -20
fi

echo "[t1] leaving containers UP for further tests; tear down with:"
echo "    docker compose -f $COMPOSE_FILE down"
