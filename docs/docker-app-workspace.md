# Docker App-Workspace Agent — 使用說明

> **本指南已被 `docs/docker-a2a-agent.md` 取代。** `docker-compose.agent.yml` 把原本分開的
> a2a 與 app-workspace 兩種模式合併成同一個 compose 檔,概念上「一個 agent 資料夾」就
> 同時包含 config + 你的 app(在 `workspace/` 內)+ session/log 狀態。

## 速查對應

| 舊(`docker-compose.app.yml`) | 新(`docker-compose.agent.yml`) |
| --- | --- |
| `APP_DIR=/path/to/app` | `agents/<name>/workspace/`(把你的 app 放在這) |
| `config/config.app.json` 範本 | `agents/<name>/config.json` |
| 單一 service,`APP_DIR` mount 為 workspace | 同一個 service 模式(`workspace:` subpath mount 改在 agent 資料夾內) |

## 新的最短路徑(把 app 掛成 agent workspace)

```bash
# 1. 建 agent 資料夾,把你的 app 放在 workspace/
mkdir -p agents/myapp/{workspace,sessions,logs}
cp -r /path/to/your/app/* agents/myapp/workspace/

# 2. 寫 config.json(從 agents/alice/config.json 抄,改 agent_id/port)
# 3. 寫 .env(從 .env.example 抄,設 MINIMAX_API_KEY)
cp agents/alice/.env.example agents/myapp/.env
$EDITOR agents/myapp/.env

# 4. 在 docker/docker-compose.agent.yml 加 service block(見 docs/docker-a2a-agent.md「加一個新 agent」)
# 5. 啟動
docker compose -f docker/docker-compose.agent.yml up -d myapp

# 6. 用 WebUI 或 pico ws
# WebUI: http://localhost:18800(若 service 內 expose port)
# Pico:  ws://localhost:18790/pico/ws?token=local-dev
```

完整指南見 `docs/docker-a2a-agent.md`。
