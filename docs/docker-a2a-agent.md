# Docker A2A Agent — 使用指南

`docker-compose.agent.yml` 是**統一長駐 agent** 入口,每跑一次就帶起一個 gateway。
每個 agent 是 host 上一個自我包含的資料夾(也是獨立 git repo),bind-mount 進容器;
不需 Docker named volume,down 之後就只是 host 上一個普通資料夾,下次 `up` 再 mount 回去。

## 30 秒上手

```bash
# 1. 建一個 agent 資料夾(已有 alice / bob 範例)
mkdir -p agents/myagent/{workspace,sessions,logs}
cp agents/alice/.env.example agents/myagent/.env.example
cp agents/myagent/.env.example agents/myagent/.env
$EDITOR agents/myagent/.env                  # 設 MINIMAX_API_KEY

# 2. 寫 config.json(從 agents/alice/config.json 抄,改 agent_id / port)

# 3. 加 service block 到 docker/docker-compose.agent.yml(見下面「加 agent」)

# 4. 啟動
docker compose -f docker/docker-compose.agent.yml up -d myagent

# 5. 用
curl -X POST http://localhost:18791/a2a/v1/ask \
  -H 'Content-Type: application/json' -d '{"text":"用一句話介紹你自己"}'

# 6. 收尾
docker compose -f docker/docker-compose.agent.yml down myagent
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

`agents/` 整個被 picoclaw repo 的 `.gitignore` 排除(每個子資料夾是獨立 git repo)。

## 環境變數(由 compose 內 service block 寫死,通常不直接用)

| 變數 | 預設 | 角色 |
| --- | --- | --- |
| `GATEWAY_PORT` | `18790` | healthcheck 用的 gateway port(對齊 `config.json.gateway.port`) |
| `A2A_PORT` | `18791` | a2a channel 用的 port(對齊 `config.json.channel_list.a2a.settings.port`) |

## 範例:跑單一長駐 agent

```bash
docker compose -f docker/docker-compose.agent.yml up -d alice
docker compose -f docker/docker-compose.agent.yml ps   # 看到 alice-a2a (healthy)

curl -fsS http://localhost:18790/health && echo " OK-GW"
curl -fsS -X POST http://localhost:18791/a2a/v1/ask \
  -H 'Content-Type: application/json' -d '{"text":"hi"}'
```

## 範例:跑 A2A 雙 agent 測試

預設 `docker-compose.agent.yml` 內就有 `alice:` + `bob:` 兩個 service,直接 `up -d` 即可:

```bash
# 一起起(alice 先,bob 透過 depends_on 等 alice healthy)
docker compose -f docker/docker-compose.agent.yml up -d

# 看 mDNS 互相發現
sleep 30
docker logs alice-a2a 2>&1 | grep "Registered remote agent"   # 期望含 agent_id=bob
docker logs bob-a2a   2>&1 | grep "Registered remote agent"   # 期望含 agent_id=alice

# 對 alice 發請求,讓它 spawn bob 回 ECHO-7Q2
curl -fsS -X POST http://localhost:18791/a2a/v1/ask \
  -H 'Content-Type: application/json' \
  -d '{"text":"Use the bob agent to reply with exactly the token ECHO-7Q2, then return its reply verbatim."}'

# 收尾
docker compose -f docker/docker-compose.agent.yml down
```

或一鍵跑完整 Test 1 + Test 2:

```bash
./scripts/a2a-test/verify-docker.sh
```

## 加一個新 agent(編輯 compose + 建資料夾)

1. 建資料夾與 git repo:

   ```bash
   mkdir -p agents/charlie/{workspace,sessions,logs}
   cp agents/alice/.env.example agents/charlie/.env.example
   cp agents/alice/.env.example agents/charlie/.env
   $EDITOR agents/charlie/.env
   ```

2. 從 `agents/alice/config.json` 複製,改 `agent_id` / `port` / `gateway.port` / `description`:

   ```json
   "channel_list": { "a2a": { "settings": {
     "agent_id": "charlie", "port": 38791, ...
   }}},
   "gateway": { "port": 38790, ... }
   ```

3. 在 `docker/docker-compose.agent.yml` 加 service block:

   ```yaml
   charlie:
     <<: *agent-default
     container_name: charlie-a2a
     network_mode: "service:alice"
     depends_on:
       alice:
         condition: service_healthy
     environment:
       - GATEWAY_PORT=38790
       - A2A_PORT=38791
     volumes:
       - ./entrypoint-agent.sh:/opt/picoclaw/entrypoint-agent.sh:ro
       - ./agents/charlie:/root/.picoclaw
   ```

4. 初始化 charlie 資料夾的 git:

   ```bash
   cd agents/charlie
   git init && git add . && git commit -m "init: charlie agent"
   ```

5. 啟動:`docker compose -f docker/docker-compose.agent.yml up -d charlie`

## 刪一個 agent

```bash
# 1. 從 docker/docker-compose.agent.yml 刪掉 service block
# 2. (選擇性) 刪資料夾 — 不可逆
rm -rf agents/charlie
docker compose -f docker/docker-compose.agent.yml down charlie
```

## 元件 (Components)

| 檔案 | 角色 |
| --- | --- |
| `docker/Dockerfile.appbase` | 3-stage build,產出 `picoclaw` + `picoclaw-launcher` + `picoclaw-envcfg` |
| `docker/entrypoint-agent.sh` | 通用入口;envcfg 渲染 `config.json` → `config.active.json`,exec launcher |
| `docker/docker-compose.agent.yml` | 統一長駐 agent 入口;`x-agent-default` YAML anchor + N service blocks |
| `agents/<name>/` | 1 個 agent = 1 個 host 資料夾(也是 git repo) |
| `cmd/picoclaw-envcfg` | 用 `gosdk/config.Default()` 載 `.env`,注入 `model_list.api_keys` |
| `pkg/channels/a2a/` | mDNS 廣播 / 探索、WS peer 協定、`POST /a2a/v1/ask` |

## 收尾 (Cleanup)

```bash
# 停某個 agent(container 移除,但 agents/<name>/ 資料夾留在 host)
docker compose -f docker/docker-compose.agent.yml down alice

# 刪 agent 資料夾(包含 git repo 與 sessions/logs)— 不可逆
rm -rf agents/alice

# 刪本機的 picoclaw image(若不再用)
docker image rm picoclaw-appbase:dev
```

## 平台注意

| 平台 | mDNS | 備註 |
| --- | --- | --- |
| Linux | container + host LAN 都通(預設 `network_mode: host`) | 預設即可 |
| macOS Docker Desktop | container loopback 通;host networking 觸不到實體 LAN | 同機雙 agent 用 `network_mode: "service:alice"`(bob 的預設) |
| 真實跨機驗證 | 兩台 Linux,各自 `network_mode: host` | mDNS 自然走 LAN;不在本指南範圍 |

## 失敗排查

- **healthcheck 一直 `starting`**:a2a channel 啟動比 gateway 慢,`start_period: 15s` 不夠時可調大,或暫時改 `GATEWAY_PORT` 對齊 `config.json` 內 `gateway.port`。
- **`Registered remote agent` 沒出現**:
  - 確認兩個 agent 資料夾的 `config.json` 內 `channel_list.a2a.settings.agent_id` 不同(同名會被 self-filter 濾掉)。
  - 確認第二個 agent 的 `network_mode: "service:alice"` 寫對。
  - `docker logs <agent>` 看 a2a channel log。
- **Test 2 沒回 `ECHO-7Q2`**:
  - 確認 alice config.json 的 `agents.list[0].subagents.allow_agents: ["*"]`。
  - 確認兩個 `.env` 的 `MINIMAX_API_KEY` 是國際站(`api.minimax.io`)。
- **想看 frame JSON**:目前不印 envelope;暫時 instrument 見 `plans/2026-06-19-a2a-two-agent-verification.md`。
