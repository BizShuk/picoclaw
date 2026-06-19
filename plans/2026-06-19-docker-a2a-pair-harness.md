# Docker A2A 雙 Agent 測試 Harness 實作計畫

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> ⚠️ **SUPERSEDED by `plans/2026-06-19-docker-agent-unification.md` (2026-06-20).**
> 原本計畫建立的 `docker-compose.a2a-pair.yml` 與本檔描述的兩份 config 範本
> (`config.a2a.alice.json` / `config.a2a.bob.json`) 都不再需要;統一入口在
> `docker/docker-compose.agent.yml`(Pattern B,YAML anchor + `alice:` / `bob:` 兩個 service)
> 與 host 端 `agents/{alice,bob}/config.json`。實作完成於 2026-06-20。

**Goal:** 建立 Docker-based 雙 agent (`alice` + `bob`) 測試 harness,讓 `docs/a2a-two-agent-test.md` 的 Test 1 (mDNS 互相發現) 與 Test 2 (A 請 B 回覆) 都能在容器內一次跑完,跨平台 (Linux / macOS Docker Desktop) 都能運作。

**Architecture:** 沿用 `docker/Dockerfile.appbase` 同一映像,新增兩份 config 模板 (`config/config.a2a.alice.json` / `config/config.a2a.bob.json`) 與一份 `docker/docker-compose.a2a-pair.yml`。兩個 service 共用 alice 的 network namespace (`network_mode: "service:alice"`),讓 mDNS 多播 (224.0.0.251) 留在同一個 loopback,跨平台可運作;`bob` 透過不同 port (a2a: 28791 / gateway: 28790) 避開衝突。

**Tech Stack:** Docker Compose v2、Go 1.25+ (既有 `Dockerfile.appbase`)、`picoclaw-envcfg` 渲染 config + 注入 `MINIMAX_API_KEY`、bash + curl 驗證腳本。

---

## 脈絡 (Context — 影響這個 plan 的既有資產)

| 既有資產 | 角色 | 本計畫如何用 |
| --- | --- | --- |
| `docker/Dockerfile.appbase` | 3-stage build,產出 `picoclaw` / `picoclaw-launcher` / `picoclaw-envcfg`;build 時跑 `picoclaw onboard` 建立 `/root/.picoclaw` skeleton | 不修改;`alice` / `bob` 共用同一 image |
| `docker/entrypoint-a2a.sh` | `picoclaw-envcfg -template $PICOCLAW_CONFIG_TEMPLATE -env-key MINIMAX_API_KEY` 渲染後 `exec picoclaw-launcher` | 不修改;每個 service 透過 `PICOCLAW_CONFIG_TEMPLATE` env 指向自己的 config |
| `config/config.a2a.json` | 單 agent 模板 (agent_id=picoclaw, port=18791) | 不修改;新增 alice / bob 兩份,agent_id 與 port 各異 |
| `cmd/picoclaw-envcfg` | 載 `.env` 並把 `MINIMAX_API_KEY` 注入 `minimax-i18n` provider 的 `api_keys` | 沿用;`api_keys: ["MINIMAX-REPLACE-ME"]` 會被替換 |
| `plans/2026-06-19-a2a-two-agent-verification.md` | 既有驗證計畫 (Test 1 / Test 2 + 觀測矩陣) | 本 plan 把其中「共用 netns」的 harness 建議實作成檔案 |
| `docs/a2a-two-agent-test.md` | 原生 process 測試指南 | 本 plan 是它的 Docker 平行版,輸出結果與之對齊 |
| mDNS service type 常數 | `pkg/channels/a2a/discovery.go:46` = `_picoclaw-a2a._tcp` | alice / bob 共用同 service type,只差 agent_id |

## 檔案結構 (File Map)

| 檔案 | 角色 | 動作 |
| --- | --- | --- |
| `config/config.a2a.alice.json` | alice 模板 (agent_id=alice, a2a:18791, gw:18790) | 新增 |
| `config/config.a2a.bob.json` | bob 模板 (agent_id=bob, a2a:28791, gw:28790) | 新增 |
| `docker/docker-compose.a2a-pair.yml` | 雙 service compose,共用 netns | 新增 |
| `scripts/a2a-test/verify-docker.sh` | 端到端跑 Test 1 + Test 2,印 PASS/FAIL | 新增 |
| `docs/docker-a2a-pair.md` | 使用指南 (含 macOS 限制) | 新增 |

`docker/Dockerfile.appbase` 與 `docker/entrypoint-a2a.sh` 不修改。

---

## Task 1: alice config 模板

**Files:**
- Create: `config/config.a2a.alice.json`

- [ ] **Step 1: 寫入 alice config 模板**

寫入以下內容 (基於 `config/config.a2a.json`,改 `agent_id=alice`、加 `agents.list` 允許 `*` spawn、加 `announce_interval=5s` 加速測試、開 `log_level: debug`):

```json
{
  "version": 3,
  "agents": {
    "defaults": {
      "workspace": "/root/.picoclaw/workspace",
      "restrict_to_workspace": true,
      "model_name": "MiniMax-M2.5-i18n",
      "max_tokens": 8192,
      "context_window": 131072,
      "temperature": 0.7,
      "max_tool_iterations": 40,
      "summarize_message_threshold": 20,
      "summarize_token_percent": 75
    },
    "list": [
      { "id": "main", "default": true, "subagents": { "allow_agents": ["*"] } }
    ]
  },
  "model_list": [
    {
      "model_name": "MiniMax-M2.5-i18n",
      "provider": "minimax-i18n",
      "model": "MiniMax-M2.5",
      "api_base": "https://api.minimax.io/v1",
      "api_keys": ["MINIMAX-REPLACE-ME"],
      "extra_body": { "reasoning_split": true }
    }
  ],
  "channel_list": {
    "a2a": {
      "enabled": true,
      "type": "a2a",
      "allow_from": [],
      "settings": {
        "agent_id": "alice",
        "port": 18791,
        "description": "PicoClaw alice (Docker test pair)",
        "announce_interval": 5000000000,
        "ask_timeout": 120000000000,
        "max_turn_default": 6
      }
    },
    "pico": {
      "enabled": true,
      "type": "pico",
      "allow_from": [],
      "settings": {
        "token": "local-dev",
        "allow_token_query": true,
        "allow_origins": ["*"],
        "ping_interval": 30,
        "read_timeout": 60,
        "max_connections": 100
      }
    }
  },
  "tools": {
    "allow_read_paths": null,
    "allow_write_paths": null,
    "exec": { "enabled": true, "enable_deny_patterns": true },
    "read_file": { "enabled": true, "mode": "bytes" },
    "write_file": { "enabled": true },
    "edit_file": { "enabled": true },
    "append_file": { "enabled": true },
    "list_dir": { "enabled": true },
    "message": { "enabled": true },
    "spawn": { "enabled": true },
    "subagent": { "enabled": true },
    "skills": { "enabled": false },
    "find_skills": { "enabled": false },
    "install_skill": { "enabled": false },
    "mcp": { "enabled": false },
    "web": { "enabled": false }
  },
  "gateway": {
    "host": "0.0.0.0",
    "port": 18790,
    "hot_reload": false,
    "log_level": "debug"
  }
}
```

- [ ] **Step 2: 驗證 JSON 合法**

```bash
python3 -c "import json; json.load(open('config/config.a2a.alice.json'))" && echo OK
```

預期:`OK`。

- [ ] **Step 3: Commit**

```bash
git add config/config.a2a.alice.json
git commit -m "feat(docker): add alice config template for A2A pair test"
```

---

## Task 2: bob config 模板

**Files:**
- Create: `config/config.a2a.bob.json`

- [ ] **Step 1: 寫入 bob config 模板**

寫入以下內容 (agent_id=bob、port=28791、gateway=28790,關閉 spawn / subagent 工具,bob 純回答):

```json
{
  "version": 3,
  "agents": {
    "defaults": {
      "workspace": "/root/.picoclaw/workspace",
      "restrict_to_workspace": true,
      "model_name": "MiniMax-M2.5-i18n",
      "max_tokens": 4096,
      "context_window": 131072,
      "temperature": 0.7,
      "max_tool_iterations": 12,
      "summarize_message_threshold": 20,
      "summarize_token_percent": 75
    }
  },
  "model_list": [
    {
      "model_name": "MiniMax-M2.5-i18n",
      "provider": "minimax-i18n",
      "model": "MiniMax-M2.5",
      "api_base": "https://api.minimax.io/v1",
      "api_keys": ["MINIMAX-REPLACE-ME"],
      "extra_body": { "reasoning_split": true }
    }
  ],
  "channel_list": {
    "a2a": {
      "enabled": true,
      "type": "a2a",
      "allow_from": [],
      "settings": {
        "agent_id": "bob",
        "port": 28791,
        "description": "PicoClaw bob (Docker test pair)",
        "announce_interval": 5000000000,
        "ask_timeout": 120000000000,
        "max_turn_default": 6
      }
    }
  },
  "tools": {
    "message": { "enabled": true }
  },
  "gateway": {
    "host": "0.0.0.0",
    "port": 28790,
    "hot_reload": false,
    "log_level": "debug"
  }
}
```

> 為何 bob 不啟 `pico` channel + 關 `spawn` / `subagent`:測試只需要「能被 alice spawn 進來回訊息」,越少工具越能排除 noise。

- [ ] **Step 2: 驗證 JSON 合法**

```bash
python3 -c "import json; json.load(open('config/config.a2a.bob.json'))" && echo OK
```

預期:`OK`。

- [ ] **Step 3: Commit**

```bash
git add config/config.a2a.bob.json
git commit -m "feat(docker): add bob config template for A2A pair test"
```

---

## Task 3: docker-compose.a2a-pair.yml

**Files:**
- Create: `docker/docker-compose.a2a-pair.yml`

- [ ] **Step 1: 寫入 compose 檔**

寫入以下內容。`alice` 起 `picoclaw-appbase:dev` 映像,`bob` 透過 `network_mode: "service:alice"` 共用 netns (mDNS 多播在 loopback 通),`depends_on: alice healthy` 確保 alice 先就緒;每個 service 透過 `PICOCLAW_CONFIG_TEMPLATE` 指向自己的 config 模板。`bob` 容器內 `localhost:28790` 仍可達 (port 不衝突)。

```yaml
services:
  # ─────────────────────────────────────────────────────────────────────────
  # PicoClaw A2A pair test harness (alice + bob)
  #
  #   docker compose -f docker/docker-compose.a2a-pair.yml up --build
  #
  # alice  a2a ws: ws://localhost:18791/a2a/v1/ws   POST: http://localhost:18791/a2a/v1/ask
  # bob    shares alice's network namespace; reachable via ws://localhost:28791/a2a/v1/ws
  #
  # Why shared netns: mDNS multicast (224.0.0.251) does not cross Docker's
  # bridge NAT, and macOS Docker Desktop's "host" networking can't reach the
  # physical LAN. Sharing alice's netns keeps multicast on loopback — works
  # on Linux and macOS alike.
  # ─────────────────────────────────────────────────────────────────────────
  alice:
    build:
      context: ..
      dockerfile: docker/Dockerfile.appbase
    image: picoclaw-appbase:dev
    container_name: alice-a2a
    environment:
      - PICOCLAW_GATEWAY_HOST=0.0.0.0
      - PICOCLAW_CONFIG_TEMPLATE=/root/.picoclaw/config.a2a.alice.template.json
    entrypoint: ["/bin/sh", "/opt/picoclaw/entrypoint-a2a.sh"]
    volumes:
      - ./entrypoint-a2a.sh:/opt/picoclaw/entrypoint-a2a.sh:ro
      - ../config/config.a2a.alice.json:/root/.picoclaw/config.a2a.alice.template.json:ro
      - ${ENV_FILE:-../.env}:/root/.picoclaw/.env:ro
    healthcheck:
      test: ["CMD", "curl", "-fsS", "http://localhost:18790/health"]
      interval: 10s
      timeout: 3s
      start_period: 15s
      retries: 5
    # bob 會在 alice 跑健康後才啟動,確保探索週期一致
    stop_grace_period: 5s

  bob:
    image: picoclaw-appbase:dev
    container_name: bob-a2a
    depends_on:
      alice:
        condition: service_healthy
    network_mode: "service:alice"
    environment:
      - PICOCLAW_GATEWAY_HOST=0.0.0.0
      - PICOCLAW_CONFIG_TEMPLATE=/root/.picoclaw/config.a2a.bob.template.json
    entrypoint: ["/bin/sh", "/opt/picoclaw/entrypoint-a2a.sh"]
    volumes:
      - ./entrypoint-a2a.sh:/opt/picoclaw/entrypoint-a2a.sh:ro
      - ../config/config.a2a.bob.json:/root/.picoclaw/config.a2a.bob.template.json:ro
      - ${ENV_FILE:-../.env}:/root/.picoclaw/.env:ro
    healthcheck:
      test: ["CMD", "curl", "-fsS", "http://localhost:28790/health"]
      interval: 10s
      timeout: 3s
      start_period: 15s
      retries: 5
    stop_grace_period: 5s
```

- [ ] **Step 2: 驗證 compose 合法**

```bash
docker compose -f docker/docker-compose.a2a-pair.yml config --quiet && echo OK
```

預期:`OK`(沒有 YAML 錯誤)。

- [ ] **Step 3: 確認 netns 設定預期**

```bash
docker compose -f docker/docker-compose.a2a-pair.yml config | grep -E "network_mode|container_name"
```

預期:出現 `container_name: alice-a2a` 與 `network_mode: service:alice`(各一行)。

- [ ] **Step 4: Commit**

```bash
git add docker/docker-compose.a2a-pair.yml
git commit -m "feat(docker): add compose for A2A pair harness with shared netns"
```

---

## Task 4: verify-docker.sh 端到端腳本

**Files:**
- Create: `scripts/a2a-test/verify-docker.sh`

- [ ] **Step 1: 寫入驗證腳本**

寫入以下內容,從 repo 根目錄跑。`cleanup` trap 確保測完一定 `down`;`docker logs` 取代 `tail -f` 抓 `Registered remote agent`;Test 2 走原生 `run-test2.sh` 同樣的 prompt 與 token `ECHO-7Q2`,輸出格式對齊原生腳本。

```bash
#!/bin/bash
# scripts/a2a-test/verify-docker.sh
#
# End-to-end Docker harness test for the A2A pair (alice + bob).
# Mirrors docs/a2a-two-agent-test.md's Test 1 + Test 2 using containers.
#
# Run from repo root:
#   MINIMAX_API_KEY=sk-... ./scripts/a2a-test/verify-docker.sh
#
# Requires:
#   - Docker Compose v2
#   - .env with MINIMAX_API_KEY (or MINIMAX_API_KEY exported in shell)
set -u
cd "$(dirname "$0")/../.."   # repo root

COMPOSE="docker compose -f docker/docker-compose.a2a-pair.yml"
TOKEN="ECHO-7Q2"
ALICE_LOG="$(mktemp -t alice.log.XXXXXX)"
BOB_LOG="$(mktemp -t bob.log.XXXXXX)"
PASS=0; FAIL=0

cleanup() {
  echo "[cleanup] docker compose down..."
  $COMPOSE down --remove-orphans >/dev/null 2>&1 || true
  rm -f "$ALICE_LOG" "$BOB_LOG"
}
trap cleanup EXIT

echo "[1/5] docker compose up -d --build ..."
$COMPOSE up -d --build --remove-orphans

echo "[2/5] wait for both containers to be healthy..."
for i in $(seq 1 30); do
  AH=$($COMPOSE ps alice | awk 'NR>1 {print $4}')
  BH=$($COMPOSE ps bob   | awk 'NR>1 {print $4}')
  echo "  [t+${i}] alice=$AH bob=$BH"
  [ "$AH" = "(healthy)" ] && [ "$BH" = "(healthy)" ] && break
  sleep 2
done
if [ "$AH" != "(healthy)" ] || [ "$BH" != "(healthy)" ]; then
  echo "FAIL: containers not healthy (alice=$AH bob=$BH)"
  $COMPOSE logs --no-color alice bob | tail -80
  exit 1
fi

echo "[3/5] wait 15s for mDNS mutual discovery..."
sleep 15
docker logs --no-color alice-a2a > "$ALICE_LOG" 2>&1
docker logs --no-color bob-a2a   > "$BOB_LOG"   2>&1

# Test 1: mutual discovery
A=$(grep "Registered remote agent" "$ALICE_LOG" | grep -ci bob   || true)
B=$(grep "Registered remote agent" "$BOB_LOG"   | grep -ci alice || true)
echo "alice sees bob : $A    bob sees alice : $B"
if [ "$A" -ge 1 ] && [ "$B" -ge 1 ]; then
  echo "TEST1: PASS (mutual mDNS discovery)"; PASS=$((PASS+1))
else
  echo "TEST1: FAIL"
  echo "--- alice a2a ---"; grep -iE "a2a|mdns|discov" "$ALICE_LOG" | tail -20
  echo "--- bob a2a ---";   grep -iE "a2a|mdns|discov" "$BOB_LOG"   | tail -20
  FAIL=$((FAIL+1))
fi

# Test 2: alice asks bob to echo a token
PROMPT="A remote agent named \"bob\" is available to you as a sub-agent. Use the spawn tool with agent_id set to \"bob\" and task set to exactly: reply with the single token ${TOKEN} and nothing else. After bob replies, output bob's reply verbatim as your final answer."
BODY=$(python3 -c 'import json,sys; print(json.dumps({"text": sys.argv[1]}))' "$PROMPT")
echo "[4/5] POST http://localhost:18791/a2a/v1/ask ..."
HTTP=$(curl -sS --max-time 150 -X POST "http://127.0.0.1:18791/a2a/v1/ask" \
  -H 'Content-Type: application/json' -d "$BODY")
echo "HTTP RESPONSE: $HTTP"

# re-pull logs after Test 2 to capture late writes
docker logs --no-color alice-a2a > "$ALICE_LOG" 2>&1
docker logs --no-color bob-a2a   > "$BOB_LOG"   2>&1

echo "--- data flow ---"
grep -E "Tool call: spawn|Dialing peer WS" "$ALICE_LOG" | head -2
grep -E "Processing message from a2a|Response: $TOKEN" "$BOB_LOG" | head -2

RESP_HAS=$(echo "$HTTP" | grep -c "$TOKEN")
BOB_HANDLED=$(grep -icE "a2a:alice" "$BOB_LOG")
if [ "$RESP_HAS" -ge 1 ] && [ "$BOB_HANDLED" -ge 1 ]; then
  echo "TEST2: PASS (A asked B, B processed, data round-tripped)"; PASS=$((PASS+1))
else
  echo "TEST2: FAIL"; FAIL=$((FAIL+1))
  echo "--- alice tail ---"; tail -30 "$ALICE_LOG"
  echo "--- bob tail ---";   tail -30 "$BOB_LOG"
fi

echo "[5/5] summary: PASS=$PASS FAIL=$FAIL"
[ "$FAIL" = "0" ]
```

- [ ] **Step 2: 設可執行 + shellcheck**

```bash
chmod +x scripts/a2a-test/verify-docker.sh
command -v shellcheck >/dev/null && shellcheck scripts/a2a-test/verify-docker.sh && echo "shellcheck: OK" || echo "shellcheck: skipped (not installed)"
```

預期:腳本可執行;若有 `shellcheck` 跑無錯。

- [ ] **Step 3: Commit**

```bash
git add scripts/a2a-test/verify-docker.sh
git commit -m "feat(docker): add verify-docker.sh end-to-end A2A pair script"
```

---

## Task 5: 使用者文件 `docs/docker-a2a-pair.md`

**Files:**
- Create: `docs/docker-a2a-pair.md`

- [ ] **Step 1: 寫入使用指南**

寫入以下內容:

````markdown
# Docker A2A 雙 Agent 測試 Harness

`docker-compose.a2a-pair.yml` 啟兩個容器 (`alice` + `bob`),把 `docs/a2a-two-agent-test.md` 的
Test 1 (mDNS 互相發現) 與 Test 2 (A 請 B 回覆) 整包跑在 Docker 內。架構上是原 `docker-compose.a2a.yml`
(單 agent) 的雙 agent 延伸,差別在 `bob` 用 `network_mode: "service:alice"` 共用 alice 的 netns,
讓 mDNS 多播留在 loopback,跨平台 (Linux + macOS Docker Desktop) 都能通。

## 入口 (Endpoints)

| Agent | a2a ws              | a2a HTTP                   | gateway health    |
| ----- | ------------------- | -------------------------- | ----------------- |
| alice | `localhost:18791/a2a/v1/ws` | `POST localhost:18791/a2a/v1/ask` | `localhost:18790/health` |
| bob   | `localhost:28791/a2a/v1/ws` | (同 netns;curl 從容器內 `localhost:28791/a2a/v1/ask`) | `localhost:28790/health` |

## 前置 (Prerequisites)

| 需求            | 說明                                          |
| --------------- | --------------------------------------------- |
| Docker Compose v2 | 確認 `docker compose version` 有輸出        |
| `.env` (在 repo 根目錄) | 含 `MINIMAX_API_KEY=sk-...` (國際站,Test 2 必用;Test 1 假值即可) |

```bash
cp .env.example .env
# 編輯 .env,把 MINIMAX_API_KEY 換成真實金鑰
```

## 快速跑 (Quick Run)

從 repo 根目錄:

```bash
# 一鍵:build → up → Test 1 → Test 2 → down
./scripts/a2a-test/verify-docker.sh
```

預期結尾:

```txt
TEST1: PASS (mutual mDNS discovery)
TEST2: PASS (A asked B, B processed, data round-tripped)
[5/5] summary: PASS=2 FAIL=0
```

## 手動跑 (Step by Step)

```bash
# 1. 啟動
docker compose -f docker/docker-compose.a2a-pair.yml up -d --build

# 2. 等健康 (大約 30–60 秒)
docker compose -f docker/docker-compose.a2a-pair.yml ps
# 期望 alice 與 bob 都是 (healthy)

# 3. Test 1:看 mDNS 互相登錄
sleep 15
docker logs alice-a2a 2>&1 | grep "Registered remote agent"   # 期望含 agent_id=bob
docker logs bob-a2a   2>&1 | grep "Registered remote agent"   # 期望含 agent_id=alice

# 4. Test 2:對 alice 發請求,讓它 spawn bob 回 ECHO-7Q2
curl -sS -X POST http://127.0.0.1:18791/a2a/v1/ask \
  -H 'Content-Type: application/json' \
  -d '{"text":"Use the bob agent to reply with exactly the token ECHO-7Q2, then return its reply verbatim."}'

# 5. 收尾
docker compose -f docker/docker-compose.a2a-pair.yml down
```

## 收尾 (Cleanup)

```bash
docker compose -f docker/docker-compose.a2a-pair.yml down --remove-orphans
# 把含金鑰的 .env 一併刪掉,若不再用
rm -f .env
```

## 平台注意 (Platform Notes)

| 平台 | mDNS | 備註 |
| ---- | ---- | ---- |
| Linux | 容器內 loopback 互通;若改用 `network_mode: host` 可觸實體 LAN | 預設共用 netns 即可 |
| macOS Docker Desktop | 容器內 loopback 互通;host networking 觸不到實體 LAN,故必須用共用 netns | 預設 OK |
| 真實跨機驗證 | 改用 `network_mode: host` + 兩台 Linux,自然走 LAN mDNS | 需另外寫 LAN 用的 compose |

## 失敗排查 (Troubleshooting)

- **兩個容器都起得來但沒看到 `Registered remote agent`**:
  - 確認 `network_mode: "service:alice"` 生效 (`docker compose config | grep network_mode`)。
  - 確認 `agent_id` 兩邊不同 (同名會被 self-filter 濾掉)。
  - 確認 mDNS service type 仍是 `_picoclaw-a2a._tcp` (`pkg/channels/a2a/discovery.go:46`)。
- **Test 2 沒回 `ECHO-7Q2`**:
  - 確認 `MINIMAX_API_KEY` 是國際站 (`api.minimax.io`)。
  - 確認 `agents.list` 在 alice 的 config 內有 `subagents.allow_agents: ["*"]`。
  - `docker logs bob-a2a 2>&1 | tail -50` 看是否 inbound 進來。
- **healthcheck 一直 `starting`**:a2a channel 啟動比 gateway 慢,`start_period: 15s` 不夠時可暫時調大;`Dockerfile.appbase` 也有 `HEALTHCHECK ... start-period=10s`,但 compose 覆蓋之。

## 元件 (Components)

| 檔案                                | 角色                                                |
| ----------------------------------- | --------------------------------------------------- |
| `config/config.a2a.alice.json`      | alice 模板,agent_id=alice,port 18791/18790         |
| `config/config.a2a.bob.json`        | bob 模板,agent_id=bob,port 28791/28790             |
| `docker/docker-compose.a2a-pair.yml`| 雙 service 共用 netns,共用 `Dockerfile.appbase`     |
| `scripts/a2a-test/verify-docker.sh` | build + 跑 Test 1 + Test 2 + down,trap 一定 cleanup |
| `docker/Dockerfile.appbase`         | 共用映像 (既有)                                    |
| `docker/entrypoint-a2a.sh`          | 共用入口 (既有,`PICOCLAW_CONFIG_TEMPLATE` 切換)    |
````

- [ ] **Step 2: 渲染文件結構一致**

```bash
grep -E "^##" docs/docker-a2a-pair.md
```

預期:列出 `## 入口` / `## 前置` / `## 快速跑` / `## 手動跑` / `## 收尾` / `## 平台注意` / `## 失敗排查` / `## 元件` 等標題。

- [ ] **Step 3: Commit**

```bash
git add docs/docker-a2a-pair.md
git commit -m "docs: usage guide for docker A2A pair harness"
```

---

## Task 6: 端到端手動驗證 (Manual End-to-End Run)

**Files:** (無新增;只跑既有檔案)

- [ ] **Step 1: 確認 `.env` 有國際站金鑰**

```bash
test -f .env && grep -q "^MINIMAX_API_KEY=sk-" .env && echo "env: OK" || echo "env: missing MINIMAX_API_KEY=sk-..."
```

預期:`env: OK`。

- [ ] **Step 2: 跑驗證腳本**

```bash
./scripts/a2a-test/verify-docker.sh
```

預期結尾 (約 1–3 分鐘):

```txt
TEST1: PASS (mutual mDNS discovery)
TEST2: PASS (A asked B, B processed, data round-tripped)
[5/5] summary: PASS=2 FAIL=0
```

- [ ] **Step 3: 失敗時排查 (僅在 Step 2 FAIL 時執行)**

```bash
# 容器仍活著,看 mDNS log
docker logs alice-a2a 2>&1 | grep -iE "a2a|mdns|discov|register" | tail -30
docker logs bob-a2a   2>&1 | grep -iE "a2a|mdns|discov|register" | tail -30

# 確認 bob 的 healthcheck 端口正確
docker compose -f docker/docker-compose.a2a-pair.yml exec -T bob \
  curl -fsS http://localhost:28790/health && echo " bob-gw OK"

# 確認 mDNS 真的在 loopback 通 (從 alice netns 內)
docker compose -f docker/docker-compose.a2a-pair.yml exec -T alice \
  sh -c 'apk add --no-cache avahi-tools 2>/dev/null; getent hosts bob || true'
```

> 上面的 `apk add` 在 Debian-based image 不一定 work;若失敗就靠 `docker logs` 判定。

- [ ] **Step 4: 確認 cleanup 跑了**

```bash
docker compose -f docker/docker-compose.a2a-pair.yml ps
```

預期:無輸出 (容器已 down)。若還在,手動 `docker compose -f docker/docker-compose.a2a-pair.yml down --remove-orphans`。

---

## 風險與注意 (Risks & Caveats)

- **共用 netns 的代價**:bob 容器的 hostname、network interface 跟 alice 相同,某些日誌/工具若依賴 hostname 會混淆。本計畫不依賴 hostname 判斷身份,改以 `container_name` 與 log 內容。
- **測試是 LLM 驅動**:Test 2 會打國際站 MiniMax,有 API 費用且回應可能因 prompt 抖動;`ECHO-7Q2` 是固定 token 方便驗證。
- **`.env` 含明文金鑰**:測完建議 `rm -f .env`,且 `.env` 已在 `.gitignore`。
- **build 時間**:第一次跑約 3–5 分鐘 (3-stage build,含 frontend `pnpm install`)。之後若沒改 frontend 與 Go 程式碼,會用 cache 加速。
- **多工取樣**:若同時跑兩份 harness,`picoclaw-appbase:dev` 與 container_name 會撞;目前只有一份,沒問題。

## Self-Review

| 檢查項 | 結論 |
| --- | --- |
| Spec coverage:`docs/a2a-two-agent-test.md` 的 Test 1 / Test 2 / 前置 / 端口 / 注意 | Task 1–6 全部覆蓋 (Test 1 對應 Task 6 Step 2 + `verify-docker.sh` 的 TEST1 區塊;Test 2 對應 TEST2 區塊;端口寫進 Task 1/2/3 config;前置寫進 Task 5) |
| Placeholder scan | 無 `TBD` / `TODO` / `fill in`;每個 code block 都是完整內容 |
| Type / name consistency | agent_id / port / container_name / image tag / compose service name 在所有檔案一致 (`alice` / `bob` / 18791 / 28791 / 18790 / 28790 / `picoclaw-appbase:dev` / `alice-a2a` / `bob-a2a`) |
| 路徑與既有資產對齊 | `Dockerfile.appbase`、`entrypoint-a2a.sh`、`picoclaw-envcfg`、`_picoclaw-a2a._tcp` 都與 codebase 實際檔案/常數一致 |
| 跨平台 | Linux + macOS Docker Desktop 都用同一份 compose (共用 netns),真實跨機需另寫 LAN 版,於 Task 5「平台注意」明示 |
| Commit hygiene | 每個 Task 一個 commit,訊息型別前綴 (feat / docs) 一致 |
