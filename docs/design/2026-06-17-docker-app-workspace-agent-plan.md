# Docker App-Workspace Agent Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 打包一個 Docker 容器,執行時 mount 使用者的 app 原始碼資料夾當 agent 的 workspace,讓 gateway 或 webui 的請求以該資料夾為依據,並能完整 read/write/execute (Go/Node/Python)。

**Architecture:** 方案 A — app folder 直接 mount 成 `/root/.picoclaw/workspace`。新增一個多階段 Dockerfile (frontend build → static Go binary build → Go+Node+Python runtime),一個 compose,一份 config 模板,以及 backlog 與用法文件。不修改任何 picoclaw Go 程式碼。

**Tech Stack:** Docker multi-stage、`golang:1.25-bookworm`、Node 24 (NodeSource)、Python3 + `uv`、picoclaw `gateway` + `pico` channel + `picoclaw-launcher` webui。

---

## File Structure

| 檔案 (File) | 動作 | 責任 (Responsibility) |
| --- | --- | --- |
| `config/config.app.json` | Create | 容器預設 config 模板:workspace=mount、全工具、`pico` channel、`gateway.host=0.0.0.0`、placeholder model key |
| `docker/Dockerfile.appbase` | Create | 多階段映像:webui 資產 + 靜態 binary + Go/Node/Python runtime |
| `docker/docker-compose.app.yml` | Create | 單一 service:mount app→workspace、mount config、env、ports 18800/18790 |
| `docs/backlog.md` | Create | 記錄方案 B (狀態獨立 + allow_read/write_paths) 待辦 |
| `docs/docker-app-workspace.md` | Create | 使用說明 (啟動指令、sessions/.gitignore 提示、觸發端點) |

說明:這些都是新增的部署 / 設定檔,彼此獨立。real API key 不進 repo — `config/config.app.json` 只放 placeholder,使用者在本機填入或改 mount 自己的 config (見 Task 1 與 Task 6)。

---

## Task 1: 預設 config 模板

**Files:**
- Create: `config/config.app.json`

- [ ] **Step 1: 寫 config.app.json**

以 `config/config.example.json` 為藍本,只保留容器需要的部分。完整內容:

```json
{
  "version": 3,
  "agents": {
    "defaults": {
      "workspace": "/root/.picoclaw/workspace",
      "restrict_to_workspace": true,
      "model_name": "claude-sonnet-4.6",
      "max_tokens": 8192,
      "context_window": 131072,
      "temperature": 0.7,
      "max_tool_iterations": 40,
      "summarize_message_threshold": 20,
      "summarize_token_percent": 75,
      "system_prompt": "You are an assistant operating directly inside the application source code mounted at your workspace root (/root/.picoclaw/workspace). Treat that folder as the single source of truth. When asked a question, inspect the real files there with read_file / list_dir, and when changes are requested use write_file / edit_file and run commands via the shell (go / node / npm / python / pytest are available). Always ground answers in the actual folder contents rather than assumptions."
    }
  },
  "model_list": [
    {
      "model_name": "claude-sonnet-4.6",
      "model": "anthropic/claude-sonnet-4.6",
      "api_keys": ["sk-ant-REPLACE-ME"],
      "api_base": "https://api.anthropic.com/v1",
      "thinking_level": "high"
    }
  ],
  "channel_list": {
    "pico": {
      "enabled": true,
      "type": "pico",
      "allow_from": [],
      "settings": {
        "token": "",
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
    "exec": {
      "enabled": true,
      "enable_deny_patterns": true,
      "custom_deny_patterns": null,
      "custom_allow_patterns": null
    },
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
    "log_level": "info"
  }
}
```

備註:`system_prompt` 取代了原本「persona 寫進 repo」的需求,讓資料夾保持乾淨。`gateway.host` 在 config 與 compose env 都設 `0.0.0.0` 作雙重保險。`api_keys` 為 placeholder。

- [ ] **Step 2: 驗證 JSON 合法**

Run: `python3 -c "import json,sys; json.load(open('config/config.app.json')); print('ok')"`
Expected: 印出 `ok`,無例外。

- [ ] **Step 3: 驗證關鍵欄位存在**

Run: `python3 -c "import json; c=json.load(open('config/config.app.json')); assert c['agents']['defaults']['workspace']=='/root/.picoclaw/workspace'; assert c['gateway']['host']=='0.0.0.0'; assert c['channel_list']['pico']['enabled'] is True; assert c['tools']['exec']['enabled'] is True; print('fields ok')"`
Expected: 印出 `fields ok`。

- [ ] **Step 4: Commit**

```bash
git add config/config.app.json
git commit -m "feat(docker): add app-workspace agent config template"
```

---

## Task 2: 多階段 Dockerfile

**Files:**
- Create: `docker/Dockerfile.appbase`

- [ ] **Step 1: 寫 Dockerfile.appbase**

```dockerfile
# ============================================================
# Stage 1: Build frontend assets (for launcher webui)
# ============================================================
FROM node:24-alpine3.23 AS frontend

RUN corepack enable && corepack prepare pnpm@latest --activate
WORKDIR /src/web/frontend
COPY web/frontend/package.json web/frontend/pnpm-lock.yaml ./
RUN CI=true pnpm install --frozen-lockfile
COPY web/frontend/ ./
RUN pnpm build:backend

# ============================================================
# Stage 2: Build static Go binaries (picoclaw + launcher)
# ============================================================
FROM golang:1.25-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends git make ca-certificates \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/web/backend/dist web/backend/dist

# make build defaults to CGO_ENABLED=0 -> static binary, portable to runtime stage
RUN make build

RUN CONFIG_PKG=github.com/sipeed/picoclaw/pkg/config && \
    VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo dev) && \
    GIT_COMMIT=$(git rev-parse --short=8 HEAD 2>/dev/null || echo dev) && \
    BUILD_TIME=$(date +%FT%T%z) && \
    GO_VERSION=$(go env GOVERSION) && \
    CGO_ENABLED=0 go build -v -tags goolm,stdjson \
      -ldflags "-X ${CONFIG_PKG}.Version=${VERSION} -X ${CONFIG_PKG}.GitCommit=${GIT_COMMIT} -X ${CONFIG_PKG}.BuildTime=${BUILD_TIME} -X ${CONFIG_PKG}.GoVersion=${GO_VERSION} -s -w" \
      -o build/picoclaw-launcher ./web/backend/

# ============================================================
# Stage 3: Runtime with Go + Node + Python toolchains
#   (the agent uses these to build/test/run the mounted app)
# ============================================================
FROM golang:1.25-bookworm

RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl git python3 python3-pip python3-venv \
  && curl -fsSL https://deb.nodesource.com/setup_24.x | bash - \
  && apt-get install -y --no-install-recommends nodejs \
  && rm -rf /var/lib/apt/lists/*

# uv for fast Python dependency management
RUN curl -LsSf https://astral.sh/uv/install.sh | sh && \
    ln -s /root/.local/bin/uv /usr/local/bin/uv && \
    ln -s /root/.local/bin/uvx /usr/local/bin/uvx && \
    uv --version

COPY --from=builder /src/build/picoclaw /usr/local/bin/picoclaw
COPY --from=builder /src/build/picoclaw-launcher /usr/local/bin/picoclaw-launcher

# Create the baseline ~/.picoclaw skeleton; runtime mounts override config.json + workspace
RUN /usr/local/bin/picoclaw onboard

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD curl -fsS http://localhost:18790/health || exit 1

EXPOSE 18790 18800
ENTRYPOINT ["picoclaw-launcher"]
CMD ["-console", "-public", "-no-browser"]
```

- [ ] **Step 2: Build the image**

Run: `docker build -f docker/Dockerfile.appbase -t picoclaw-appbase:dev .`
Expected: build 成功,結尾 `naming to docker.io/library/picoclaw-appbase:dev`。

- [ ] **Step 3: 驗證三個 runtime 與兩個 binary 都在**

Run: `docker run --rm --entrypoint sh picoclaw-appbase:dev -c "go version && node --version && python3 --version && uv --version && which picoclaw picoclaw-launcher"`
Expected: 印出 `go version go1.25...`、`v24...`、`Python 3...`、`uv ...`,以及兩個 binary 路徑。

- [ ] **Step 4: Commit**

```bash
git add docker/Dockerfile.appbase
git commit -m "feat(docker): add multi-lang appbase image (go+node+python+launcher)"
```

---

## Task 3: docker-compose

**Files:**
- Create: `docker/docker-compose.app.yml`

- [ ] **Step 1: 寫 docker-compose.app.yml**

```yaml
services:
  # ─────────────────────────────────────────────
  # PicoClaw App-Workspace Agent
  #   APP_DIR=/path/to/app docker compose -f docker/docker-compose.app.yml up
  #   WebUI:   http://localhost:18800
  #   Gateway: http://localhost:18790/pico/   (programmatic HTTP/WS)
  # ─────────────────────────────────────────────
  picoclaw-app:
    build:
      context: ..
      dockerfile: docker/Dockerfile.appbase
    image: picoclaw-appbase:dev
    container_name: picoclaw-app
    restart: unless-stopped
    environment:
      # Required: bind gateway shared HTTP server on all interfaces,
      # otherwise external requests to :18790 cannot reach it.
      - PICOCLAW_GATEWAY_HOST=0.0.0.0
      # Optional: fix the launcher dashboard token (else a random one is printed).
      #- PICOCLAW_LAUNCHER_TOKEN=your-secret-token
    ports:
      - "18800:18800"
      - "18790:18790"
    volumes:
      # The user's application source folder becomes the agent workspace (rw).
      - ${APP_DIR:-./app}:/root/.picoclaw/workspace
      # Config template (read-only). Edit a local copy to add your real API key.
      - ../config/config.app.json:/root/.picoclaw/config.json:ro
```

- [ ] **Step 2: 驗證 compose 可解析**

Run: `APP_DIR=./app docker compose -f docker/docker-compose.app.yml config`
Expected: 印出展開後的 YAML,無錯誤;`volumes` 中可見 `/root/.picoclaw/workspace` 與 `config.json` 對應。

- [ ] **Step 3: Commit**

```bash
git add docker/docker-compose.app.yml
git commit -m "feat(docker): add app-workspace compose service"
```

---

## Task 4: 端到端驗證 (sample app)

**Files:**
- Create: `examples/sample-app/main.go` (僅供本機測試,commit 與否見 Step 5)

- [ ] **Step 1: 建一個最小 sample app**

```go
// examples/sample-app/main.go
package main

import "fmt"

func main() {
	fmt.Println("hello from sample app")
}
```

- [ ] **Step 2: 在本機 config 副本填入真實 key**

Run:
```bash
cp config/config.app.json /tmp/config.app.local.json
# 用編輯器把 /tmp/config.app.local.json 內 "sk-ant-REPLACE-ME" 換成真實金鑰
```
Expected: `/tmp/config.app.local.json` 內 `api_keys` 為有效金鑰。(此檔不進 repo)

- [ ] **Step 3: 啟動容器 (以 sample app 為 workspace、本機 config)**

Run:
```bash
docker run --rm -d --name picoclaw-app-test \
  -e PICOCLAW_GATEWAY_HOST=0.0.0.0 \
  -p 18800:18800 -p 18790:18790 \
  -v "$PWD/examples/sample-app":/root/.picoclaw/workspace \
  -v /tmp/config.app.local.json:/root/.picoclaw/config.json:ro \
  picoclaw-appbase:dev
```
Expected: 印出 container id。

- [ ] **Step 4: 驗證兩個入口都活著**

Run: `sleep 8; curl -fsS http://localhost:18790/health && echo OK-GW; curl -fsS -o /dev/null -w "%{http_code}\n" http://localhost:18800`
Expected: gateway health 回應 + `OK-GW`;webui 回 `200`。

- [ ] **Step 5: 驗證 agent 以 workspace 內容回答 (pico channel)**

Run:
```bash
curl -fsS -X POST "http://localhost:18790/pico/?token=" \
  -H 'Content-Type: application/json' \
  -d '{"text":"List the files in your workspace and tell me what this app prints."}' ; echo
```
Expected: 回應提及 `main.go` 與輸出字串 `hello from sample app`(證明 agent 讀到 mount 進來的資料夾)。

> 注意:`/pico/` 的確切 HTTP/WS 收訊格式以 `pkg/channels/pico/pico.go` 的 `ServeHTTP` 為準。實作此步時先讀該檔確認 endpoint 與 payload;若為 WebSocket-only,改用 `websocat` 或一段 Node/Python WS client 送同樣文字並讀回應。先讀程式碼再定指令,不要猜。

- [ ] **Step 6: 驗證可觀察性 (host 端即時看到變更)**

Run:
```bash
# 透過同一入口請 agent 在 workspace 新增一個檔
curl -fsS -X POST "http://localhost:18790/pico/?token=" -H 'Content-Type: application/json' \
  -d '{"text":"Create a file NOTES.md in the workspace with the line: touched by agent."}'; echo
ls -l examples/sample-app/NOTES.md && cat examples/sample-app/NOTES.md
```
Expected: host 端 `examples/sample-app/NOTES.md` 出現且內容為 `touched by agent.`。

- [ ] **Step 7: 清理**

Run: `docker rm -f picoclaw-app-test; rm -f examples/sample-app/NOTES.md`
Expected: 容器移除、暫存檔清掉。

- [ ] **Step 8: Commit sample app**

```bash
git add examples/sample-app/main.go
git commit -m "test(docker): add sample app for app-workspace e2e verification"
```

---

## Task 5: 方案 B backlog

**Files:**
- Create: `docs/backlog.md`

- [ ] **Step 1: 寫 backlog.md**

```markdown
# Backlog

## 方案 B:狀態獨立的 app-workspace agent (clean isolation)

來源:`docs/design/2026-06-17-docker-app-workspace-agent-design.md` (附錄方案 B)。

現況 (方案 A) 把 app folder 直接 mount 成 workspace,picoclaw 會在使用者 repo 內寫出
`sessions/` (`pkg/agent/instance.go:129`)。方案 B 消除此污染:

- picoclaw 狀態 (含 `sessions/`) 放獨立 volume,例如 workspace=`/var/lib/picoclaw/workspace`。
- 使用者 app 改 mount 在 `/app`,透過 `tools.allow_read_paths` + `tools.allow_write_paths`
  (config `pkg/config/config.go:1021-1022`,env `PICOCLAW_TOOLS_ALLOW_READ_PATHS` /
  `_WRITE_PATHS`) 授權 fs 工具與 shell 存取 `/app`。
- shell cwd 可經 `allowedPathPatterns` 放行 `/app` (`pkg/tools/shell.go:358`)。
- persona/`system_prompt` 改指向 `/app`;image 內固定,可跨不同 app 重用。

優點:app 資料夾零污染、persona 不必動使用者 repo。
代價:agent 的「base」是設定面的 (allow paths) 而非 workspace 根,設定略多。
```

- [ ] **Step 2: 驗證連結指向存在的檔**

Run: `test -f docs/design/2026-06-17-docker-app-workspace-agent-design.md && grep -q "allow_read_paths" pkg/config/config.go && echo refs-ok`
Expected: 印出 `refs-ok`。

- [ ] **Step 3: Commit**

```bash
git add docs/backlog.md
git commit -m "docs: backlog approach B (clean-isolation app-workspace agent)"
```

---

## Task 6: 使用說明

**Files:**
- Create: `docs/docker-app-workspace.md`

- [ ] **Step 1: 寫使用說明**

```markdown
# Docker App-Workspace Agent — 使用說明

把任意 app 原始碼資料夾 mount 進容器當 agent 的 base,從 webui 或 gateway 發問,
agent 以該資料夾內容回答,並可讀寫 / 執行 (Go / Node / Python)。

## 啟動

1. 複製 config 模板並填入真實金鑰 (此副本請勿進版控):

   ```bash
   cp config/config.app.json config/config.app.local.json
   # 編輯 config/config.app.local.json,把 api_keys 換成真實金鑰
   ```

2. 啟動 (用 compose,並覆蓋 config 掛載為你的本機副本):

   ```bash
   APP_DIR=/path/to/your/app docker compose -f docker/docker-compose.app.yml up --build
   ```

   - WebUI:    http://localhost:18800
   - Gateway:  http://localhost:18790/pico/  (programmatic HTTP/WS 觸發)

## 已知副作用 (方案 A)

agent 的 workspace 就是你的資料夾,picoclaw 會在其中寫出 `sessions/`。
請把它加進該 app repo 的 `.gitignore`:

```bash
echo "sessions/" >> /path/to/your/app/.gitignore
```

乾淨隔離 (零污染) 的版本見 `docs/backlog.md` 的方案 B。
```

- [ ] **Step 2: 驗證指令片段與實際檔案一致**

Run: `grep -q 'docker/docker-compose.app.yml' docs/docker-app-workspace.md && test -f docker/docker-compose.app.yml && test -f config/config.app.json && echo usage-ok`
Expected: 印出 `usage-ok`。

- [ ] **Step 3: Commit**

```bash
git add docs/docker-app-workspace.md
git commit -m "docs: usage guide for docker app-workspace agent"
```

---

## Self-Review Notes

- Spec coverage:Dockerfile (Task 2)、compose (Task 3)、config 全工具+pico (Task 1)、Go+Node+Python runtime (Task 2)、雙入口 webui+gateway (Task 3/4)、可觀察性 (Task 4 Step 6)、backlog 方案 B (Task 5)、README+gitignore (Task 6) — spec 各節皆有對應 task。
- 金鑰處理:spec 原寫「金鑰走 env」,但 `model_list.api_keys` 無 env 覆寫 (僅 `.security.yml` 機制)。計畫改為「模板放 placeholder + 本機副本填真實金鑰」,real key 不進 repo,符合 spec 不落地進 (committed) config 的意圖。
- 開放確認點:`pico` channel 的 `/pico/` 實際 payload 格式 (HTTP vs WS-only) 需在 Task 4 Step 5 實作時先讀 `pkg/channels/pico/pico.go:280` 的 `ServeHTTP` 確認;計畫已明確要求「先讀程式碼再定指令」。
```
