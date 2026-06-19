# Docker Agent 統一 Harness 重構計畫

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 5 份 compose 與散落的 agent 模板收斂成 2 份 compose + 1 個 `agents/` host 目錄;每個 long-running agent 是一個 host 上的資料夾(也是 git repo),bind-mount 進容器,`docker-compose.agent.yml` 用一次跑一個 agent。

**Architecture:** `docker-compose.yml` 留作「local dev / CLI / launcher」基本款;`docker-compose.agent.yml` 是所有「常駐 networked agent」的統一入口 — 透過 env vars(`AGENT_NAME` / `AGENT_DIR` / `NETWORK_MODE`)切換每個 agent 的身份。`agents/<name>/` 是 host 端的 self-contained 資料夾,內含 `config.json`(template,git tracked)+ `.env`(gitignored)+ `workspace/` + `sessions/` + `logs/`,整個資料夾 bind-mount 到容器 `/root/.picoclaw`;不需 Docker named volume,down 之後就是 host 上一個普通資料夾,下次 `up` 再 mount 回去。

**Tech Stack:** Docker Compose v2、Go 1.25+ (既有 `Dockerfile.appbase` 與 `picoclaw-envcfg` 與 `picoclaw-launcher`)、bash、git。

---

## 脈絡 (Context — 使用者需求逐條對應)

| # | 需求 | 本 plan 對應 |
| --- | --- | --- |
| 1 | 只留 `docker-compose.yml` + `docker-compose.agent.yml` 兩份 | Task 1 刪 4 份;Task 5 新增 1 份 |
| 2 | `docker-compose.agent.yml` 給每個 long-running agent(gateway)用 | Task 5 設計:1 service × 1 invocation per agent |
| 3 | 開 `agents/` 資料夾裝 agent 所有資料 | Task 3 + Task 4 開兩個範例(alice / bob) |
| 4 | 1 container = 1 gateway = 1 agent folder mount in | Task 5 compose 用 `AGENT_DIR` env 切;Task 6 entrypoint 只讀自己的 folder |
| 5 | 不需 persistent volume,down 之後就只是 host 資料夾,up 再 mount | Task 5 用 bind mount 而非 named volume;Task 1 刪除舊 named volume 宣告 |
| 6 | 每個 agent 資料夾是 git repo | Task 3 + Task 4 在 `agents/alice/` 與 `agents/bob/` 各跑 `git init`;提供 `.gitignore` 排除 `sessions/` / `logs/` / `config.active.json` / `.env` |

## 檔案影響 (File Impact)

### 刪除 (DELETE)

| 檔案 | 為何刪 |
| --- | --- |
| `docker/docker-compose.full.yml` | 已被 `docker-compose.yml` 的 profile 機制涵蓋(agent / gateway / launcher 三個 profile) |
| `docker/docker-compose.app.yml` | 唯一 consumer 是被棄用的 `config.app.json` |
| `docker/docker-compose.a2a.yml` | 被新的 `docker-compose.agent.yml` + `agents/<name>/config.json` 取代 |
| `docker/entrypoint-a2a.sh` | 配合 `docker-compose.a2a.yml` 刪除;由新的 `docker/entrypoint-agent.sh` 取代 |
| `docker/Dockerfile.full` | 唯一 consumer 是 `docker-compose.full.yml` |
| `docker/Dockerfile.heavy` | 目前沒有 compose 使用(grep 驗證) |
| `config/config.a2a.json` | 配合 `docker-compose.a2a.yml` 刪除;範本下沉到 `agents/<name>/config.json` |
| `config/config.app.json` | 配合 `docker-compose.app.yml` 刪除 |

### 保留 (KEEP)

| 檔案 | 為何留 |
| --- | --- |
| `docker/docker-compose.yml` | 基本款,涵蓋 agent / gateway / launcher 三 profile |
| `docker/Dockerfile` | `docker-compose.yml` 的 `picoclaw-agent` / `picoclaw-gateway` 用 |
| `docker/Dockerfile.launcher` | `docker-compose.yml` 的 `picoclaw-launcher` profile 用 |
| `docker/Dockerfile.appbase` | 新 `docker-compose.agent.yml` 用 |
| `docker/Dockerfile.goreleaser*` | release 用 |
| `docker/entrypoint.sh` | `docker/Dockerfile` 預設入口 |

### 新增 (ADD)

| 檔案 | 角色 |
| --- | --- |
| `docker/docker-compose.agent.yml` | 統一長駐 agent 入口(1 service × 1 invocation) |
| `docker/entrypoint-agent.sh` | 通用 entrypoint;從 `/root/.picoclaw` 讀 `config.json` + `.env`,envcfg 渲染到 `config.active.json`,exec launcher |
| `agents/README.md` | 說明 `agents/` 是 host 端 git repo 集合,不進版控 |
| `agents/alice/README.md` | alice 的 agent 說明 |
| `agents/alice/config.json` | alice 的 picoclaw config 範本(已注入 agent_id=alice, port 18791/18790) |
| `agents/alice/.gitignore` | 排除 `sessions/` `logs/` `config.active.json` `.env` `workspace/` |
| `agents/alice/.env.example` | `MINIMAX_API_KEY=...` 範本(讓使用者複製成 `.env`) |
| `agents/bob/README.md` | bob 的 agent 說明 |
| `agents/bob/config.json` | bob 的 picoclaw config 範本(已注入 agent_id=bob, port 28791/28790) |
| `agents/bob/.gitignore` | 同 alice |
| `agents/bob/.env.example` | 同 alice |

### 更新 (UPDATE)

| 檔案 | 改動 |
| --- | --- |
| `scripts/a2a-test/gen-configs.py` | 改寫成在 `agents/alice/` 與 `agents/bob/` 內建立並初始化 git repo(若使用者已有 repo 則跳過 init) |
| `scripts/a2a-test/run-test1.sh` | 改用 `docker-compose.agent.yml` 啟動 alice + bob(各自一次 `up -d`),用 `NETWORK_MODE=service:alice` 共用 netns |
| `scripts/a2a-test/run-test2.sh` | 同上,並改用 `curl http://localhost:18791/...` |
| `scripts/a2a-test/verify-docker.sh` | 同上,並在 trap cleanup 裡對 alice / bob 各 `down` 一次 |
| `docs/docker-a2a-agent.md` | 重寫成引用 `docker-compose.agent.yml` + `agents/<name>/` 模式 |
| `docs/a2a-two-agent-test.md` | 保留(native 流程仍可用);在「手動版」段落補一句「Docker 版見 `docs/docker-a2a-agent.md`」 |
| `.gitignore` (repo root) | 加上 `agents/`(host 資料夾不進版控;每個子資料夾的 `.git` 是獨立的 git repo) |

> **本 plan 把上一份 `plans/2026-06-19-docker-a2a-pair-harness.md` 取代。** 若那份已 commit 進 git,實作完成後在 commit message 標 `BREAKING CHANGE: supersede 2026-06-19-docker-a2a-pair-harness.md`。

## 設計決策 (Design Decisions)

| 決策 | 選擇 | 替代與否決原因 |
| --- | --- | --- |
| 每個 agent config 從哪讀 | `agents/<name>/config.json`(bind mount 進 `/root/.picoclaw/config.json`) | 不用 sub-path mount,避免新增檔案(如 custom skill)要逐一加 volume |
| 渲染後 config 寫哪 | `/root/.picoclaw/config.active.json`(獨立檔,加進 agent folder `.gitignore`) | 寫回 `config.json` 會污染 git tracked 範本,讓 `git diff` 一直有幻覺 diff |
| network_mode 預設 | `host`(讓 mDNS 觸實體 LAN) | 預設 `bridge` 會壞 mDNS;`service:alice` 太特殊,只測試用 |
| 兩個 agent 共用 netns 怎麼做 | 第二個 `up` 加 `NETWORK_MODE=service:alice` | compose v2 不支援 service 引用 service(`service:xxx` 只能引用啟動中的 service 名),且舊 `network_mode: "service:alice"` 仍可用 |
| agents 資料夾要進版控嗎 | 不進(`agents/` 加進 `.gitignore`) | 子資料夾是獨立 git repo,父層追蹤會混亂 |
| agent folder 的 .env 進版控嗎 | 不進(`.gitignore` 排除,提供 `.env.example`) | 與 `.env.example` pattern 一致;API key 屬 secrets |
| 刪除 `Dockerfile.heavy` 嗎 | 刪(目前無 consumer) | 留著會是 dead code |
| 刪除 `entrypoint.sh` 嗎 | **不刪** | 是 `Dockerfile` 的預設入口(`docker-compose.yml` 會用到) |

---

## Task 1: 清理舊 compose / Dockerfile / 範本

**Files:**
- Delete: `docker/docker-compose.full.yml`
- Delete: `docker/docker-compose.app.yml`
- Delete: `docker/docker-compose.a2a.yml`
- Delete: `docker/entrypoint-a2a.sh`
- Delete: `docker/Dockerfile.full`
- Delete: `docker/Dockerfile.heavy`
- Delete: `config/config.a2a.json`
- Delete: `config/config.app.json`

- [ ] **Step 1: 確認刪除前無其他引用**

```bash
cd /Users/shuk/projects/tmp/picoclaw
grep -rnE "docker-compose\.(a2a|app|full)\.yml|Dockerfile\.(full|heavy)|entrypoint-a2a\.sh|config\.(a2a|app)\.json" \
  --include="*.yml" --include="*.yaml" --include="*.md" --include="*.go" --include="*.sh" \
  --exclude-dir=node_modules --exclude-dir=.git 2>/dev/null
```

預期:除了這 8 個檔案本身,沒有其他參照;若出現其他檔,先記下來再評估。

- [ ] **Step 2: 刪檔**

```bash
cd /Users/shuk/projects/tmp/picoclaw
rm -v \
  docker/docker-compose.full.yml \
  docker/docker-compose.app.yml \
  docker/docker-compose.a2a.yml \
  docker/entrypoint-a2a.sh \
  docker/Dockerfile.full \
  docker/Dockerfile.heavy \
  config/config.a2a.json \
  config/config.app.json
```

預期:8 行 `removed` 訊息。

- [ ] **Step 3: 確認 docker/ 與 config/ 剩餘檔案**

```bash
ls docker/Dockerfile* docker/docker-compose* docker/entrypoint* config/*.json
```

預期:

```txt
docker/Dockerfile
docker/Dockerfile.appbase
docker/Dockerfile.goreleaser
docker/Dockerfile.goreleaser.launcher
docker/Dockerfile.launcher
docker/docker-compose.agent.yml  (尚未建立,屬正常)
docker/docker-compose.yml
docker/entrypoint.sh
config/config.example.json
```

- [ ] **Step 4: Commit**

```bash
git add -A docker/ config/
git status   # 確認只列上述 8 個刪除 + 無其他意外
git commit -m "refactor(docker): remove obsolete compose / Dockerfile / templates

- docker-compose.full.yml / app.yml / a2a.yml: replaced by docker-compose.agent.yml
- entrypoint-a2a.sh: replaced by entrypoint-agent.sh (next task)
- Dockerfile.full / heavy: unused after compose removal
- config.a2a.json / app.json: templates moved to agents/<name>/config.json"
```

---

## Task 2: 新增 entrypoint-agent.sh

**Files:**
- Create: `docker/entrypoint-agent.sh`

- [ ] **Step 1: 寫入新 entrypoint**

寫入以下內容。設計重點:
- `/root/.picoclaw` 是 bind mount 的 agent folder;裡面有 `config.json`(template)+ `.env` + `workspace/` + `sessions/` + `logs/`
- `picoclaw-envcfg` 把 template + `.env` 渲染到 `config.active.json`(獨立檔,gitignored),**不覆蓋** `config.json`
- `exec picoclaw-launcher` 讀 `config.active.json`

```sh
#!/bin/sh
# ---------------------------------------------------------------------------
# PicoClaw generic agent entrypoint.
#
# Mount layout (set by docker-compose.agent.yml):
#   /root/.picoclaw/
#     config.json          template (git tracked, NEVER overwritten)
#     .env                 MINIMAX_API_KEY etc. (gitignored)
#     workspace/           agent's working files
#     sessions/            chat history
#     logs/                runtime logs (file mirror of stdout/stderr)
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

# picoclaw-envcfg uses gosdk/config.Default() to load .env from the cwd.
# A real environment variable (docker run -e / compose env) still wins.
picoclaw-envcfg \
  -template "$TEMPLATE" \
  -out "$TARGET" \
  -provider minimax-i18n \
  -env-key MINIMAX_API_KEY \
  -app-name picoclaw

exec picoclaw-launcher -console -public -no-browser "$TARGET"
```

- [ ] **Step 2: 設可執行**

```bash
chmod +x /Users/shuk/projects/tmp/picoclaw/docker/entrypoint-agent.sh
ls -l /Users/shuk/projects/tmp/picoclaw/docker/entrypoint-agent.sh
```

預期:第一欄是 `-rwxr-xr-x`。

- [ ] **Step 3: shellcheck(若有)**

```bash
command -v shellcheck >/dev/null && shellcheck /Users/shuk/projects/tmp/picoclaw/docker/entrypoint-agent.sh && echo OK || echo "shellcheck: skipped (not installed)"
```

預期:有裝就跑無錯;沒裝就 `skipped`。

- [ ] **Step 4: Commit**

```bash
cd /Users/shuk/projects/tmp/picoclaw
git add docker/entrypoint-agent.sh
git commit -m "feat(docker): add generic entrypoint-agent.sh for docker-compose.agent.yml"
```

---

## Task 3: 建立 `agents/` 與 `agents/alice/`(含 git init)

**Files:**
- Create: `agents/README.md`
- Create: `agents/alice/README.md`
- Create: `agents/alice/config.json`
- Create: `agents/alice/.gitignore`
- Create: `agents/alice/.env.example`
- Modify: `.gitignore` (repo root)— 加上 `agents/`

- [ ] **Step 1: 在 repo root 的 `.gitignore` 加上 `agents/`**

讀 `.gitignore` 最後一段,接在 secrets 那節後面加:

```gitignore
# Per-agent folders (each is its own git repo; do not track at the parent level)
agents/
```

實作(在 shell 內):

```bash
cd /Users/shuk/projects/tmp/picoclaw
grep -q "^agents/$" .gitignore || echo "

# Per-agent folders (each is its own git repo; do not track at the parent level)
agents/" >> .gitignore
tail -5 .gitignore
```

預期:最後 5 行包含 `agents/` 與說明。

- [ ] **Step 2: 建立 `agents/` 與 `agents/alice/` 骨架**

```bash
cd /Users/shuk/projects/tmp/picoclaw
mkdir -p agents/alice/{workspace,sessions,logs}
ls -la agents/alice
```

預期:`workspace/`, `sessions/`, `logs/` 三個子目錄 + 隱含的 `.` 與 `..`。

- [ ] **Step 3: 寫入 `agents/alice/config.json`**

`agent_id=alice`、a2a port `18791`、gateway port `18790`、允許 spawn 任何 agent:

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
        "description": "PicoClaw alice (agents/alice)",
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

寫入(用 Write 工具,路徑 `/Users/shuk/projects/tmp/picoclaw/agents/alice/config.json`)。

- [ ] **Step 4: 寫入 `agents/alice/.gitignore`**

```gitignore
# Secrets
.env

# Rendered artifacts (regenerated by entrypoint each start)
config.active.json

# Runtime data (large, churn-heavy; don't track)
sessions/
logs/
workspace/
```

- [ ] **Step 5: 寫入 `agents/alice/.env.example`**

```bash
# International MiniMax key (api.minimax.io). Test 2 needs a real key.
MINIMAX_API_KEY=sk-replace-me
```

- [ ] **Step 6: 寫入 `agents/alice/README.md`**

````markdown
# alice — PicoClaw agent

A2A peer for the two-agent test harness. Runs as a long-running gateway that
spawns `bob` over A2A when asked.

## Layout

| Path | Tracked? | Role |
| --- | --- | --- |
| `config.json` | ✓ (git) | picoclaw config template |
| `.env` | ✗ | `MINIMAX_API_KEY` (copy from `.env.example`) |
| `config.active.json` | ✗ | rendered config (regenerated by entrypoint) |
| `workspace/` | ✗ | agent's working files |
| `sessions/` | ✗ | chat history |
| `logs/` | ✗ | runtime logs |

## Bring up

```bash
cp .env.example .env
# edit .env and set MINIMAX_API_KEY

AGENT_DIR="$(pwd)" AGENT_NAME=alice \
  docker compose -f docker/docker-compose.agent.yml up -d --build
```

Ports: a2a ws `localhost:18791/a2a/v1/ws`, HTTP `localhost:18791/a2a/v1/ask`,
gateway `localhost:18790/health`.

## Tear down

```bash
AGENT_NAME=alice docker compose -f docker/docker-compose.agent.yml down
```

The folder stays on disk — re-running `up` re-mounts it.
````

- [ ] **Step 7: 驗證 JSON 合法 + git init**

```bash
cd /Users/shuk/projects/tmp/picoclaw/agents/alice
python3 -c "import json; json.load(open('config.json'))" && echo "config.json: OK"
git init -q
git add config.json .gitignore .env.example README.md
git -c user.email="alice@local" -c user.name="alice" commit -q -m "init: alice agent"
git log --oneline
```

預期:`config.json: OK` + 一行 `init: alice agent`。

- [ ] **Step 8: Commit(repo root,只加 `.gitignore` 那行)**

```bash
cd /Users/shuk/projects/tmp/picoclaw
git add .gitignore
git commit -m "chore: gitignore agents/ (each subdir is its own git repo)"
```

> `agents/alice/` 本身不被 picoclaw repo 追蹤;它內部的 git repo 是獨立的。

---

## Task 4: 建立 `agents/bob/`(含 git init)

**Files:**
- Create: `agents/bob/README.md`
- Create: `agents/bob/config.json`
- Create: `agents/bob/.gitignore`
- Create: `agents/bob/.env.example`

- [ ] **Step 1: 建立目錄骨架**

```bash
cd /Users/shuk/projects/tmp/picoclaw
mkdir -p agents/bob/{workspace,sessions,logs}
ls -la agents/bob
```

- [ ] **Step 2: 寫入 `agents/bob/config.json`**

`agent_id=bob`、a2a port `28791`、gateway port `28790`、**關閉** spawn / subagent 工具(bob 純回答):

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
        "description": "PicoClaw bob (agents/bob)",
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

- [ ] **Step 3: 寫入 `agents/bob/.gitignore`**(同 alice)

```gitignore
.env
config.active.json
sessions/
logs/
workspace/
```

- [ ] **Step 4: 寫入 `agents/bob/.env.example`**(同 alice)

```bash
MINIMAX_API_KEY=sk-replace-me
```

- [ ] **Step 5: 寫入 `agents/bob/README.md`**

````markdown
# bob — PicoClaw agent

A2A peer for the two-agent test harness. Pure answerer; does not spawn or
reach out to other agents.

## Layout

| Path | Tracked? | Role |
| --- | --- | --- |
| `config.json` | ✓ (git) | picoclaw config template |
| `.env` | ✗ | `MINIMAX_API_KEY` |
| `config.active.json` | ✗ | rendered config |
| `workspace/` `sessions/` `logs/` | ✗ | runtime data |

## Bring up

```bash
cp .env.example .env
# edit .env and set MINIMAX_API_KEY

AGENT_DIR="$(pwd)" AGENT_NAME=bob \
  docker compose -f docker/docker-compose.agent.yml up -d --build
```

To share alice's network namespace (for same-host mDNS test):

```bash
# Start alice first, then bob with NETWORK_MODE pointing at alice
NETWORK_MODE="container:alice" AGENT_NAME=bob \
  docker compose -f docker/docker-compose.agent.yml up -d
```

Ports: a2a `localhost:28791`, gateway `localhost:28790`.

## Tear down

```bash
AGENT_NAME=bob docker compose -f docker/docker-compose.agent.yml down
```
````

- [ ] **Step 6: 驗證 JSON + git init**

```bash
cd /Users/shuk/projects/tmp/picoclaw/agents/bob
python3 -c "import json; json.load(open('config.json'))" && echo "config.json: OK"
git init -q
git add config.json .gitignore .env.example README.md
git -c user.email="bob@local" -c user.name="bob" commit -q -m "init: bob agent"
git log --oneline
```

- [ ] **Step 7: 寫入 `agents/README.md`**

````markdown
# agents/ — Per-Agent Folders

Each subfolder here is a self-contained, bind-mounted PicoClaw agent. Each is
its **own git repository** so config-as-code lives alongside the runtime
state without polluting the parent `picoclaw` repo.

## Layout of one agent

```
agents/<name>/
├── .git/              independent git repo
├── README.md
├── config.json        picoclaw config (git tracked, source of truth)
├── .env.example       copy → .env, set MINIMAX_API_KEY
├── .env               gitignored
├── config.active.json rendered config (gitignored, regenerated each start)
├── workspace/         gitignored
├── sessions/          gitignored
└── logs/              gitignored
```

## Bring up one agent

```bash
cd agents/alice
cp .env.example .env && $EDITOR .env

AGENT_DIR="$(pwd)" AGENT_NAME=alice \
  docker compose -f docker/docker-compose.agent.yml up -d --build
```

`AGENT_DIR` and `AGENT_NAME` are the only two env vars you usually need.
`NETWORK_MODE` defaults to `host` (mDNS reaches the LAN); override for tests
that need shared netns:

```bash
NETWORK_MODE="container:alice" AGENT_NAME=bob AGENT_DIR="$PWD" \
  docker compose -f docker/docker-compose.agent.yml up -d
```

## Tear down

```bash
AGENT_NAME=alice docker compose -f docker/docker-compose.agent.yml down
```

The folder stays on disk. Next `up` re-mounts it. Nothing in `agents/`
is tracked by the parent `picoclaw` repo.
````

---

## Task 5: 新增 `docker-compose.agent.yml`

**Files:**
- Create: `docker/docker-compose.agent.yml`

- [ ] **Step 1: 寫入 compose**

```yaml
services:
  # ─────────────────────────────────────────────────────────────────────────
  # PicoClaw generic long-running agent.
  #
  # Each invocation = one agent = one host folder mounted as /root/.picoclaw.
  #
  #   AGENT_DIR=./agents/alice AGENT_NAME=alice \
  #     docker compose -f docker/docker-compose.agent.yml up -d --build
  #
  # Required env:
  #   AGENT_DIR   absolute path to the agent folder on the host
  #   AGENT_NAME  container name (also used by the second agent when it does
  #               NETWORK_MODE=container:<AGENT_NAME> to share netns)
  #
  # Optional env:
  #   NETWORK_MODE   defaults to "host" (mDNS reaches LAN). For same-host
  #                  two-agent tests, run the second one with
  #                  NETWORK_MODE="container:<first AGENT_NAME>".
  # ─────────────────────────────────────────────────────────────────────────
  agent:
    build:
      context: ..
      dockerfile: docker/Dockerfile.appbase
    image: picoclaw-appbase:dev
    container_name: ${AGENT_NAME:?AGENT_NAME is required}
    restart: unless-stopped
    network_mode: ${NETWORK_MODE:-host}
    environment:
      - PICOCLAW_GATEWAY_HOST=0.0.0.0
      - PICOCLAW_CONFIG_DIR=/root/.picoclaw
    entrypoint: ["/bin/sh", "/opt/picoclaw/entrypoint-agent.sh"]
    volumes:
      - ./entrypoint-agent.sh:/opt/picoclaw/entrypoint-agent.sh:ro
      - ${AGENT_DIR:?AGENT_DIR is required}:/root/.picoclaw
    healthcheck:
      test: ["CMD", "curl", "-fsS", "http://localhost:${GATEWAY_PORT:-18790}/health"]
      interval: 10s
      timeout: 3s
      start_period: 15s
      retries: 5
    stop_grace_period: 5s
```

- [ ] **Step 2: 驗證 compose 合法**

```bash
cd /Users/shuk/projects/tmp/picoclaw
AGENT_NAME=alice AGENT_DIR=./agents/alice \
  docker compose -f docker/docker-compose.agent.yml config --quiet && echo OK
```

預期:`OK`(YAML / interpolation 無錯)。

- [ ] **Step 3: 確認 netns 預期 + 必填檢查**

```bash
cd /Users/shuk/projects/tmp/picoclaw
AGENT_NAME=alice AGENT_DIR=./agents/alice \
  docker compose -f docker/docker-compose.agent.yml config | grep -E "container_name|network_mode"
```

預期:

```txt
    container_name: alice
    network_mode: host
```

- [ ] **Step 4: 確認缺 env 會 fail**

```bash
cd /Users/shuk/projects/tmp/picoclaw
docker compose -f docker/docker-compose.agent.yml config 2>&1 | head -5
```

預期:出現 `AGENT_NAME is required` 或 `AGENT_DIR is required` 的錯誤訊息(compose 對 `${VAR:?msg}` 會 fail)。

- [ ] **Step 5: Commit**

```bash
cd /Users/shuk/projects/tmp/picoclaw
git add docker/docker-compose.agent.yml
git commit -m "feat(docker): add docker-compose.agent.yml (1 container = 1 agent folder)"
```

---

## Task 6: 重寫 `gen-configs.py`

**Files:**
- Modify: `scripts/a2a-test/gen-configs.py`

- [ ] **Step 1: 完整重寫腳本**

設計:
- 不再寫到 `$WORKDIR/alice/config.json`;改寫到 `agents/alice/config.json` 與 `agents/bob/config.json`
- 若資料夾不存在,自動 `mkdir -p` + `git init`(只在 init 失敗時警告,不打斷)
- 若 `.env` 不存在,自動 `cp .env.example .env` 骨架
- 使用者提供 `MINIMAX_API_KEY`;若沒給,印警告但仍產出 config(`api_keys: ["MINIMAX-REPLACE-ME"]` 保留)

寫入以下內容到 `/Users/shuk/projects/tmp/picoclaw/scripts/a2a-test/gen-configs.py`:

```python
#!/usr/bin/env python3
"""Seed agents/alice and agents/bob with API-key-injected configs.

Each folder is a self-contained PicoClaw agent directory and its own git
repository. Run from the repo root:

    MINIMAX_API_KEY=sk-... python3 scripts/a2a-test/gen-configs.py

What it does:
  1. Ensures agents/alice and agents/bob exist (mkdir -p workspace, sessions, logs).
  2. Ensures each has a git repo (git init if missing). Skips silently if it
     already has one or the user has staged uncommitted work.
  3. Writes config.json with agent_id / ports set per agent.
  4. Injects MINIMAX_API_KEY into the model_list entry. If the env var is
     missing, the placeholder MINIMAX-REPLACE-ME is left in place and a
     warning is printed.
  5. Creates .env (from .env.example) and .gitignore if missing.

This script is idempotent: re-running it with the same key updates config.json
in place. Existing git history is preserved.
"""
import json
import os
import shutil
import subprocess
import sys
from pathlib import Path

KEY = os.environ.get("MINIMAX_API_KEY", "")
API_BASE = "https://api.minimax.io/v1"
REPO_ROOT = Path(__file__).resolve().parent.parent.parent
AGENTS_DIR = REPO_ROOT / "agents"


def warn(msg):
    print(f"[warn] {msg}", file=sys.stderr)


def ensure_agent(name, a2a_port, gw_port, can_spawn):
    agent_dir = AGENTS_DIR / name
    agent_dir.mkdir(parents=True, exist_ok=True)
    for sub in ("workspace", "sessions", "logs"):
        (agent_dir / sub).mkdir(exist_ok=True)

    # .gitignore
    gi = agent_dir / ".gitignore"
    if not gi.exists():
        gi.write_text(".env\nconfig.active.json\nsessions/\nlogs/\nworkspace/\n")

    # .env.example
    env_example = agent_dir / ".env.example"
    if not env_example.exists():
        env_example.write_text("MINIMAX_API_KEY=sk-replace-me\n")

    # .env (skeleton copy if missing; user fills the key in)
    env_file = agent_dir / ".env"
    if not env_file.exists():
        shutil.copy(env_example, env_file)
        env_file.chmod(0o600)

    # git init (skip if .git already present or git unavailable)
    if not (agent_dir / ".git").exists():
        try:
            subprocess.run(
                ["git", "init", "-q"], cwd=agent_dir, check=True,
                env={**os.environ, "GIT_AUTHOR_NAME": name, "GIT_AUTHOR_EMAIL": f"{name}@local",
                     "GIT_COMMITTER_NAME": name, "GIT_COMMITTER_EMAIL": f"{name}@local"},
            )
            print(f"[{name}] git init done")
        except (FileNotFoundError, subprocess.CalledProcessError) as e:
            warn(f"{name}: git init skipped ({e})")

    # config.json
    cfg = {
        "version": 3,
        "agents": {"defaults": {
            "workspace": "/root/.picoclaw/workspace",
            "restrict_to_workspace": True,
            "model_name": "MiniMax-M2.5-i18n",
            "max_tokens": 4096 if not can_spawn else 8192,
            "context_window": 131072,
            "max_tool_iterations": 12 if not can_spawn else 40,
            "summarize_message_threshold": 20,
            "summarize_token_percent": 75,
        }},
        "model_list": [{
            "model_name": "MiniMax-M2.5-i18n",
            "provider": "minimax-i18n",
            "model": "MiniMax-M2.5",
            "api_base": API_BASE,
            "api_keys": [KEY if KEY else "MINIMAX-REPLACE-ME"],
            "extra_body": {"reasoning_split": True},
        }],
        "channel_list": {"a2a": {
            "enabled": True, "type": "a2a", "allow_from": [],
            "settings": {
                "agent_id": name, "port": a2a_port,
                "description": f"PicoClaw {name} (agents/{name})",
                "announce_interval": 5000000000,
                "ask_timeout": 120000000000,
                "max_turn_default": 6,
            },
        }},
        "gateway": {"host": "0.0.0.0", "port": gw_port, "hot_reload": False, "log_level": "debug"},
    }
    if can_spawn:
        cfg["agents"]["list"] = [{"id": "main", "default": True, "subagents": {"allow_agents": ["*"]}}]
        cfg["tools"] = {
            "exec": {"enabled": True, "enable_deny_patterns": True},
            "read_file": {"enabled": True, "mode": "bytes"},
            "write_file": {"enabled": True},
            "edit_file": {"enabled": True},
            "append_file": {"enabled": True},
            "list_dir": {"enabled": True},
            "message": {"enabled": True},
            "spawn": {"enabled": True},
            "subagent": {"enabled": True},
            "skills": {"enabled": False},
            "find_skills": {"enabled": False},
            "install_skill": {"enabled": False},
            "mcp": {"enabled": False},
            "web": {"enabled": False},
        }
        cfg["channel_list"]["pico"] = {
            "enabled": True, "type": "pico", "allow_from": [],
            "settings": {
                "token": "local-dev", "allow_token_query": True,
                "allow_origins": ["*"], "ping_interval": 30,
                "read_timeout": 60, "max_connections": 100,
            },
        }
    else:
        cfg["tools"] = {"message": {"enabled": True}}

    cfg_path = agent_dir / "config.json"
    cfg_path.write_text(json.dumps(cfg, indent=2) + "\n")

    # README
    readme = agent_dir / "README.md"
    if not readme.exists():
        readme.write_text(
            f"# {name} — PicoClaw agent\n\n"
            f"See `../../agents/README.md` for the full convention.\n\n"
            f"- a2a port: {a2a_port}\n- gateway port: {gw_port}\n- can_spawn: {can_spawn}\n"
        )

    print(f"[{name}] {cfg_path}  a2a={a2a_port} gw={gw_port}  "
          f"{'key-injected' if KEY else 'placeholder-key (set MINIMAX_API_KEY)'}")


def main():
    if not KEY:
        warn("MINIMAX_API_KEY not set; config.json will keep MINIMAX-REPLACE-ME placeholder.")
    ensure_agent("alice", 18791, 18790, can_spawn=True)
    ensure_agent("bob",   28791, 28790, can_spawn=False)


if __name__ == "__main__":
    main()
```

- [ ] **Step 2: 跑一次驗證**

```bash
cd /Users/shuk/projects/tmp/picoclaw
MINIMAX_API_KEY=sk-fake python3 scripts/a2a-test/gen-configs.py
ls -la agents/alice agents/bob
python3 -c "import json; print('alice agent_id =', json.load(open('agents/alice/config.json'))['channel_list']['a2a']['settings']['agent_id'])"
python3 -c "import json; print('bob   agent_id =', json.load(open('agents/bob/config.json'))['channel_list']['a2a']['settings']['agent_id'])"
```

預期:

- 兩個資料夾都有 `config.json` `.env.example` `.gitignore` `README.md` `workspace/` `sessions/` `logs/`
- alice agent_id = alice
- bob   agent_id = bob
- 都印 `[<name>] ... key-injected`(若 KEY 有給)

- [ ] **Step 3: Commit**

```bash
cd /Users/shuk/projects/tmp/picoclaw
git add scripts/a2a-test/gen-configs.py
git commit -m "refactor(a2a-test): gen-configs.py now seeds agents/{alice,bob} with git init"
```

---

## Task 7: 重寫 `run-test1.sh`、`run-test2.sh`、`verify-docker.sh`

**Files:**
- Modify: `scripts/a2a-test/run-test1.sh`
- Modify: `scripts/a2a-test/run-test2.sh`
- Modify: `scripts/a2a-test/verify-docker.sh`

- [ ] **Step 1: 重寫 `run-test1.sh`**

```bash
#!/bin/bash
# Test 1 — mutual mDNS discovery between two PicoClaw agents (Docker harness).
# Run from repo root:
#   ./scripts/a2a-test/run-test1.sh
#
# Prereq: agents/alice and agents/bob exist with config.json (run gen-configs.py).
#         Each has .env with MINIMAX_API_KEY set.
set -u
cd "$(dirname "$0")/../.."

COMPOSE="docker compose -f docker/docker-compose.agent.yml"

# Start alice (host networking; standard mDNS on LAN / loopback)
AGENT_NAME=alice AGENT_DIR="$PWD/agents/alice" \
  $COMPOSE up -d --build

# Start bob in alice's netns (same-host mDNS test)
NETWORK_MODE="container:alice" AGENT_NAME=bob AGENT_DIR="$PWD/agents/bob" \
  $COMPOSE up -d

echo "[t1] waiting 20s for mDNS discovery..."
sleep 20

A=$(docker logs alice 2>&1 | grep "Registered remote agent" | grep -ci bob   || true)
B=$(docker logs bob   2>&1 | grep "Registered remote agent" | grep -ci alice || true)
echo "alice sees bob : $A    bob sees alice : $B"

if [ "$A" -ge 1 ] && [ "$B" -ge 1 ]; then
  echo "TEST1: PASS (mutual mDNS discovery)"
else
  echo "TEST1: FAIL"
  echo "--- alice a2a ---"; docker logs alice 2>&1 | grep -iE "a2a|mdns|discov" | tail -20
  echo "--- bob a2a ---";   docker logs bob   2>&1 | grep -iE "a2a|mdns|discov" | tail -20
fi

echo "[t1] leaving containers UP for further tests; tear down with:"
echo "    AGENT_NAME=alice $COMPOSE down"
echo "    AGENT_NAME=bob   $COMPOSE down"
```

- [ ] **Step 2: 重寫 `run-test2.sh`**

```bash
#!/bin/bash
# Test 2 — alice asks bob (via spawn) to return a token; verify the round-trip.
# Run from repo root AFTER run-test1.sh has left containers up.
#   ./scripts/a2a-test/run-test2.sh
set -u
cd "$(dirname "$0")/../.."

COMPOSE="docker compose -f docker/docker-compose.agent.yml"
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
docker logs alice 2>&1 | grep -E "Tool call: spawn|Dialing peer WS" | head -2
docker logs bob   2>&1 | grep -E "Processing message from a2a|Response: $TOKEN" | head -2

RESP_HAS=$(echo "$HTTP" | grep -c "$TOKEN")
BOB_HANDLED=$(docker logs bob 2>&1 | grep -icE "a2a:alice")

if [ "$RESP_HAS" -ge 1 ] && [ "$BOB_HANDLED" -ge 1 ]; then
  echo "TEST2: PASS (A asked B, B processed, data round-tripped)"
else
  echo "TEST2: FAIL"
  echo "--- alice tail ---"; docker logs alice 2>&1 | tail -30
  echo "--- bob tail ---";   docker logs bob   2>&1 | tail -30
fi
```

- [ ] **Step 3: 重寫 `verify-docker.sh`**

```bash
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
cd "$(dirname "$0")/../.."

COMPOSE="docker compose -f docker/docker-compose.agent.yml"
TOKEN="ECHO-7Q2"
PASS=0; FAIL=0

cleanup() {
  echo "[cleanup] docker compose down (alice + bob)..."
  AGENT_NAME=alice $COMPOSE down --remove-orphans >/dev/null 2>&1 || true
  AGENT_NAME=bob   $COMPOSE down --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "[1/4] bring up alice (host networking)..."
AGENT_NAME=alice AGENT_DIR="$PWD/agents/alice" $COMPOSE up -d --build

echo "[2/4] bring up bob (shared netns with alice)..."
NETWORK_MODE="container:alice" AGENT_NAME=bob AGENT_DIR="$PWD/agents/bob" \
  $COMPOSE up -d --build

echo "[3/4] wait 20s for healthy + mDNS discovery..."
sleep 20

# Test 1: mutual discovery
A=$(docker logs alice 2>&1 | grep "Registered remote agent" | grep -ci bob   || true)
B=$(docker logs bob   2>&1 | grep "Registered remote agent" | grep -ci alice || true)
echo "alice sees bob : $A    bob sees alice : $B"
if [ "$A" -ge 1 ] && [ "$B" -ge 1 ]; then
  echo "TEST1: PASS (mutual mDNS discovery)"; PASS=$((PASS+1))
else
  echo "TEST1: FAIL"; FAIL=$((FAIL+1))
  echo "--- alice a2a ---"; docker logs alice 2>&1 | grep -iE "a2a|mdns|discov" | tail -20
  echo "--- bob a2a ---";   docker logs bob   2>&1 | grep -iE "a2a|mdns|discov" | tail -20
fi

# Test 2: alice asks bob
PROMPT="A remote agent named \"bob\" is available to you as a sub-agent. Use the spawn tool with agent_id set to \"bob\" and task set to exactly: reply with the single token ${TOKEN} and nothing else. After bob replies, output bob's reply verbatim as your final answer."
BODY=$(python3 -c 'import json,sys; print(json.dumps({"text": sys.argv[1]}))' "$PROMPT")
echo "[4/4] POST http://localhost:18791/a2a/v1/ask ..."
HTTP=$(curl -sS --max-time 150 -X POST "http://127.0.0.1:18791/a2a/v1/ask" \
  -H 'Content-Type: application/json' -d "$BODY")
echo "HTTP RESPONSE: $HTTP"
echo "--- data flow ---"
docker logs alice 2>&1 | grep -E "Tool call: spawn|Dialing peer WS" | head -2
docker logs bob   2>&1 | grep -E "Processing message from a2a|Response: $TOKEN" | head -2

RESP_HAS=$(echo "$HTTP" | grep -c "$TOKEN")
BOB_HANDLED=$(docker logs bob 2>&1 | grep -icE "a2a:alice")
if [ "$RESP_HAS" -ge 1 ] && [ "$BOB_HANDLED" -ge 1 ]; then
  echo "TEST2: PASS (A asked B, B processed, data round-tripped)"; PASS=$((PASS+1))
else
  echo "TEST2: FAIL"; FAIL=$((FAIL+1))
fi

echo "summary: PASS=$PASS FAIL=$FAIL"
[ "$FAIL" = "0" ]
```

- [ ] **Step 4: 設可執行**

```bash
cd /Users/shuk/projects/tmp/picoclaw
chmod +x scripts/a2a-test/run-test1.sh scripts/a2a-test/run-test2.sh scripts/a2a-test/verify-docker.sh
ls -l scripts/a2a-test/
```

預期:三個 `.sh` 都有 `-rwxr-xr-x`。

- [ ] **Step 5: shellcheck + Commit**

```bash
cd /Users/shuk/projects/tmp/picoclaw
command -v shellcheck >/dev/null && shellcheck scripts/a2a-test/*.sh && echo "shellcheck: OK" || echo "shellcheck: skipped"
git add scripts/a2a-test/
git commit -m "refactor(a2a-test): use docker-compose.agent.yml + agents/ folders"
```

---

## Task 8: 重寫 `docs/docker-a2a-agent.md`

**Files:**
- Modify: `docs/docker-a2a-agent.md`(整篇重寫成新模式)

- [ ] **Step 1: 完整重寫**

寫入以下內容到 `/Users/shuk/projects/tmp/picoclaw/docs/docker-a2a-agent.md`:

````markdown
# Docker Agent — 使用指南

`docker-compose.agent.yml` 是一個**通用長駐 agent** 的入口,每跑一次就帶起一個 gateway。
每個 agent 是 host 上一個自我包含的資料夾(也是獨立 git repo),bind-mount 進容器;
不需 Docker named volume,down 之後就只是 host 上一個普通資料夾,下次 `up` 再 mount 回去。

## 30 秒上手

```bash
# 1. 建一個 agent 資料夾
mkdir -p agents/myagent/{workspace,sessions,logs}
cp .env.example agents/myagent/.env.example   # 或從 agents/alice/.env.example 抄
cp agents/myagent/.env.example agents/myagent/.env
$EDITOR agents/myagent/.env                   # 設 MINIMAX_API_KEY

# 2. 寫 config.json(可從 agents/alice/config.json 抄,把 agent_id/port 改掉)

# 3. 啟動
AGENT_NAME=myagent AGENT_DIR="$PWD/agents/myagent" \
  docker compose -f docker/docker-compose.agent.yml up -d --build

# 4. 用
curl -X POST http://localhost:18791/a2a/v1/ask \
  -H 'Content-Type: application/json' -d '{"text":"用一句話介紹你自己"}'

# 5. 收尾
AGENT_NAME=myagent docker compose -f docker/docker-compose.agent.yml down
```

## 檔案配置 (Layout)

每個 agent 資料夾**自我包含**:

```
agents/<name>/
├── .git/              獨立 git repo
├── README.md
├── config.json        picoclaw config(已 git tracked,source of truth)
├── .env.example       範本
├── .env               gitignored,放 MINIMAX_API_KEY
├── config.active.json 渲染後的 config(每次 entrypoint 重生;gitignored)
├── workspace/         agent 工作目錄
├── sessions/          chat history
└── logs/              runtime logs
```

## 環境變數

| 變數 | 必填? | 預設 | 角色 |
| --- | --- | --- | --- |
| `AGENT_NAME` | ✓ | — | container 名稱;第二個 agent 用 `NETWORK_MODE=container:<AGENT_NAME>` 共享 netns |
| `AGENT_DIR` | ✓ | — | host 端 agent 資料夾絕對路徑;bind-mount 到 `/root/.picoclaw` |
| `NETWORK_MODE` | ✗ | `host` | 同 `docker run --network`;測試雙 agent 共用 netns 設 `container:<第一個 AGENT_NAME>` |
| `GATEWAY_PORT` | ✗ | `18790` | healthcheck 用的 gateway port(要對齊 config.json 內 `gateway.port`) |

`AGENT_NAME` / `AGENT_DIR` 缺一會 fail(compose 用 `${VAR:?msg}`)。

## 範例:跑單一長駐 agent

```bash
AGENT_NAME=alice AGENT_DIR="$PWD/agents/alice" \
  docker compose -f docker/docker-compose.agent.yml up -d --build

docker compose -f docker/docker-compose.agent.yml ps   # 看到 alice (healthy)
curl -fsS http://localhost:18790/health && echo " OK-GW"
curl -fsS -X POST http://localhost:18791/a2a/v1/ask \
  -H 'Content-Type: application/json' -d '{"text":"hi"}'
```

## 範例:跑 A2A 雙 agent 測試

```bash
# 1. 先起 alice(host networking,預設)
AGENT_NAME=alice AGENT_DIR="$PWD/agents/alice" \
  docker compose -f docker/docker-compose.agent.yml up -d --build

# 2. 再起 bob,共用 alice 的 netns(同 host mDNS loopback)
NETWORK_MODE="container:alice" AGENT_NAME=bob AGENT_DIR="$PWD/agents/bob" \
  docker compose -f docker/docker-compose.agent.yml up -d --build

# 3. 看 mDNS 互相發現
sleep 20
docker logs alice 2>&1 | grep "Registered remote agent"   # 期望含 agent_id=bob
docker logs bob   2>&1 | grep "Registered remote agent"   # 期望含 agent_id=alice

# 4. 對 alice 發請求,讓它 spawn bob 回 ECHO-7Q2
curl -fsS -X POST http://localhost:18791/a2a/v1/ask \
  -H 'Content-Type: application/json' \
  -d '{"text":"Use the bob agent to reply with exactly the token ECHO-7Q2, then return its reply verbatim."}'

# 5. 收尾(各 down 一次,因為是不同 compose 實例)
AGENT_NAME=alice docker compose -f docker/docker-compose.agent.yml down
AGENT_NAME=bob   docker compose -f docker/docker-compose.agent.yml down
```

或一鍵跑完整 Test 1 + Test 2:

```bash
./scripts/a2a-test/verify-docker.sh
```

## 元件 (Components)

| 檔案 | 角色 |
| --- | --- |
| `docker/Dockerfile.appbase` | 3-stage build,產出 `picoclaw` + `picoclaw-launcher` + `picoclaw-envcfg` |
| `docker/entrypoint-agent.sh` | 通用入口;envcfg 渲染 `config.json` → `config.active.json`,exec launcher |
| `docker/docker-compose.agent.yml` | 統一長駐 agent 入口;1 service × 1 invocation |
| `agents/<name>/` | 1 個 agent = 1 個 host 資料夾(也是 git repo) |
| `cmd/picoclaw-envcfg` | 用 `gosdk/config.Default()` 載 `.env`,注入 `model_list.api_keys` |
| `pkg/channels/a2a/` | mDNS 廣播 / 探索、WS peer 協定、`POST /a2a/v1/ask` |

## 收尾 (Cleanup)

```bash
# 停某個 agent(container 移除,但 agents/<name>/ 資料夾留在 host)
AGENT_NAME=alice docker compose -f docker/docker-compose.agent.yml down

# 刪 agent 資料夾(包含 git repo 與 sessions/logs)— 不可逆
rm -rf agents/alice

# 刪本機的 picoclaw image(若不再用)
docker image rm picoclaw-appbase:dev
```

## 平台注意

| 平台 | mDNS | 備註 |
| --- | --- | --- |
| Linux | container + host LAN 都通 | `NETWORK_MODE=host` 預設即可 |
| macOS Docker Desktop | container loopback 通;host networking 觸不到實體 LAN | 雙 agent 同機測試用 `NETWORK_MODE=container:<first>` |
| 真實跨機驗證 | 兩台 Linux,各自 `NETWORK_MODE=host` | mDNS 自然走 LAN;不在本指南範圍 |

## 失敗排查

- **`AGENT_NAME is required`**:忘了設 env;兩個必填之一缺一就 fail。
- **healthcheck 一直 `starting`**:a2a channel 啟動比 gateway 慢,`start_period: 15s` 不夠時可調大,或暫時改 `GATEWAY_PORT` 對齊。
- **`Registered remote agent` 沒出現**:
  - 確認兩個 agent 資料夾的 `config.json` 內 `channel_list.a2a.settings.agent_id` 不同(同名會被 self-filter 濾掉)。
  - 確認第二個 agent 的 `NETWORK_MODE=container:<第一個 AGENT_NAME>` 寫對。
  - `docker logs <agent>` 看 a2a channel log。
- **Test 2 沒回 `ECHO-7Q2`**:
  - 確認 alice config.json 的 `agents.list[0].subagents.allow_agents: ["*"]`。
  - 確認兩個 `.env` 的 `MINIMAX_API_KEY` 是國際站(`api.minimax.io`)。
- **想看 frame JSON**:目前不印 envelope;暫時 instrument 見 `plans/2026-06-19-a2a-two-agent-verification.md`。
````

- [ ] **Step 2: 確認結構**

```bash
cd /Users/shuk/projects/tmp/picoclaw
grep -E "^##" docs/docker-a2a-agent.md
```

預期:列出 `## 30 秒上手` / `## 檔案配置` / `## 環境變數` / `## 範例:跑單一長駐 agent` / `## 範例:跑 A2A 雙 agent 測試` / `## 元件` / `## 收尾` / `## 平台注意` / `## 失敗排查`。

- [ ] **Step 3: Commit**

```bash
cd /Users/shuk/projects/tmp/picoclaw
git add docs/docker-a2a-agent.md
git commit -m "docs: rewrite docker-a2a-agent.md for docker-compose.agent.yml pattern"
```

---

## Task 9: 更新 `docs/a2a-two-agent-test.md`(加一句 Docker 引用)

**Files:**
- Modify: `docs/a2a-two-agent-test.md`

- [ ] **Step 1: 在「手動版」段落後加一句**

在文件最後 `## 注意` 段之前(或任意合理位置),插入:

```markdown
## Docker 版

雙 agent 測試可用 `docker-compose.agent.yml` 跑(每個 agent 是 host 端 `agents/<name>/` 資料夾,各自 bind-mount 進容器)。詳見 `docs/docker-a2a-agent.md` 的「範例:跑 A2A 雙 agent 測試」與一鍵腳本 `scripts/a2a-test/verify-docker.sh`。
```

實作(用 Edit 工具,在 `## 注意` 之前插入)。

- [ ] **Step 2: Commit**

```bash
cd /Users/shuk/projects/tmp/picoclaw
git add docs/a2a-two-agent-test.md
git commit -m "docs: add Docker 版 pointer to a2a-two-agent-test.md"
```

---

## Task 10: 標記舊 plan 為 superseded

**Files:**
- Modify: `plans/2026-06-19-docker-a2a-pair-harness.md`(在頂端加 deprecation 標頭)

- [ ] **Step 1: 插入 deprecation 標頭**

在檔案最頂端(標題之前)插入:

```markdown
> ⚠️ **SUPERSEDED by `plans/2026-06-19-docker-agent-unification.md` (2026-06-19).**
> The `docker-compose.a2a-pair.yml` described here is no longer needed;
> `docker-compose.agent.yml` now handles both single-agent and pair tests.
```

實作:用 Edit 工具,在第一行 `# Docker A2A 雙 Agent 測試 Harness 實作計畫` 之前插入這段。

- [ ] **Step 2: Commit**

```bash
cd /Users/shuk/projects/tmp/picoclaw
git add plans/2026-06-19-docker-a2a-pair-harness.md
git commit -m "docs(plans): mark docker-a2a-pair-harness.md as superseded"
```

---

## Task 11: 端到端手動驗證

**Files:**(無新增)

- [ ] **Step 1: 確認 .gitignore + 資料夾結構**

```bash
cd /Users/shuk/projects/tmp/picoclaw
grep "agents/" .gitignore
ls agents/ && ls agents/alice agents/bob
```

預期:`agents/` 在 .gitignore;`agents/` 下有 `alice` 與 `bob`(以及 `README.md`);每個子資料夾內有 `config.json` `.env` `.env.example` `.gitignore` `workspace/` `sessions/` `logs/`。

- [ ] **Step 2: 在每個 agent folder 設 API key**

```bash
cd /Users/shuk/projects/tmp/picoclaw
$EDITOR agents/alice/.env   # 設 MINIMAX_API_KEY=sk-...
$EDITOR agents/bob/.env
grep -E "^MINIMAX_API_KEY" agents/alice/.env agents/bob/.env
```

預期:兩行都顯示非空 key。

- [ ] **Step 3: 跑一鍵驗證**

```bash
cd /Users/shuk/projects/tmp/picoclaw
./scripts/a2a-test/verify-docker.sh
```

預期結尾:

```txt
TEST1: PASS (mutual mDNS discovery)
TEST2: PASS (A asked B, B processed, data round-tripped)
summary: PASS=2 FAIL=0
```

- [ ] **Step 4: 確認 cleanup 跑了**

```bash
cd /Users/shuk/projects/tmp/picoclaw
docker ps -a --filter "name=alice\|name=bob" --format "{{.Names}} {{.Status}}"
```

預期:無輸出(或 `Up` 之外的狀態,但預期是空)。

- [ ] **Step 5: 確認 agents 資料夾仍存在(沒被刪)**

```bash
cd /Users/shuk/projects/tmp/picoclaw
ls -d agents/alice agents/bob
```

預期:兩個資料夾都還在,內含 git repo。

---

## 風險與注意 (Risks & Caveats)

- **刪除是不可逆的**:Task 1 刪除的 8 個檔案涵蓋 compose / Dockerfile / 範本;若有人在外部依賴(`docker-compose.full.yml` 仍在 CI、release script 引 `Dockerfile.full`),要先確認。
- **`.env` 含明文 API key**:`agents/<name>/.env` 不進版控但仍以明文存在於 host。`gen-configs.py` 會自動 `chmod 600`;提醒使用者測完刪資料夾或清 key。
- **`config.active.json` 會被 git 看到嗎**:`.gitignore` 已排除;但若使用者 `git add -f` 強加,仍會進版控 — 提醒在 `agents/README.md` 內標示。
- **mDNS 跨容器**:`NETWORK_MODE=container:<first>` 在新版 compose (v2.20+) 仍可用;若升級後失效,可改用 `network_mode: "service:alice"`(行為相同但語法較舊)。
- **與 `docker-compose.yml` 的差別**:`docker-compose.yml` 是** local dev / CLI / 單機測試**,`docker-compose.agent.yml` 是** production-style 長駐 agent**;兩者 image / entrypoint / config 機制完全不同,**不要混用**。
- **Task 11 端到端要真實 API key**:Test 2 會打國際站 MiniMax,有費用;Test 1 只要假 key(因為 alice / bob 啟動會試 ping LLM 嗎? — 若只有 Test 1 不打 LLM 就不需要 key,但目前 entrypoint 沒做「無 key skip LLM init」,所以 alice / bob 仍會在啟動時建立 provider;若 key 假則 a2a channel 可能 warn 但仍 listen;若要嚴格無 LLM 跑 Test 1,可加 env `PICOCLAW_ALLOW_EMPTY_STARTUP=true` 給 gateway)。

## Self-Review

| 檢查項 | 結論 |
| --- | --- |
| Spec coverage:使用者 5 項需求 | 1.「只留 2 compose」→ Task 1 + Task 5;2.「agent compose 給長駐 agent」→ Task 5 設計;3.「agents/ 資料夾」→ Task 3 + Task 4;4.「1 container = 1 gateway = 1 agent folder」→ Task 5 `AGENT_DIR` bind mount;5.「down 之後就只是 host 資料夾」→ Task 5 不用 named volume;6.「每個 agent folder 是 git repo」→ Task 3 + Task 4 + Task 6 `git init` |
| Placeholder scan | 無 `TBD` / `TODO` / `fill in`;所有 code block 完整 |
| Name/path 一致性 | `AGENT_NAME` / `AGENT_DIR` / `NETWORK_MODE` / `GATEWAY_PORT` 在 compose / 腳本 / 文件一致;`alice` / `bob` / `18791` / `18790` / `28791` / `28790` / `picoclaw-appbase:dev` / `entrypoint-agent.sh` 全部對齊 |
| 刪除與現有引用清乾淨 | Task 1 Step 1 先 grep 驗證再刪 |
| 跨平台 | Linux + macOS Docker Desktop + 真實跨機三種情境都有對應的 `NETWORK_MODE` 指引 |
| 與舊 plan 的相容性 | Task 10 明確標 superseded;commit message 標 `BREAKING CHANGE` |
| Commit hygiene | 每個 Task 一個 commit,前綴(feat / refactor / docs / chore)一致 |
| 端到端驗證 | Task 11 含 `verify-docker.sh` + cleanup 確認 + agents 資料夾存活確認 |
