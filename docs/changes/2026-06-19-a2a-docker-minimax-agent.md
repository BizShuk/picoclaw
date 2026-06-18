# Changes — A2A Docker Networked Agent (MiniMax)

日期 (Date): 2026-06-19
分支 (Branch): `feat/docker-app-workspace-agent`

## 目標 (Goal)

打包一個持續運行的 Docker 容器:在區網以 mDNS 廣播自己、探索其他 agent,
並接受 HTTP / WebSocket 請求來跑 LLM。LLM 用 MiniMax,金鑰從 `.env` 載入
(經 `gosdk/config.Default()`) 並在啟動時注入 config。

## 變更總覽 (Change Summary)

| 範疇 | 檔案 | 動作 | 說明 |
| --- | --- | --- | --- |
| HTTP 入口 | `pkg/channels/a2a/http_ask.go` | 新增 | 同步 `POST /a2a/v1/ask`:publish inbound → 等 final 回覆 → 回 JSON |
| HTTP 入口 | `pkg/channels/a2a/http_ask_test.go` | 新增 | waiter table + Send() final-only 關聯邏輯單元測試 |
| HTTP 入口 | `pkg/channels/a2a/a2a.go` | 修改 | 加 `httpAsks` 欄位 + `Send()` 對 http peer 的 final 分支 |
| HTTP 入口 | `pkg/channels/a2a/server.go` | 修改 | 掛 `/a2a/v1/ask` route |
| Config 載入 | `cmd/picoclaw-envcfg/main.go` | 新增 | 用 `gosdk/config.Default()` 載 `.env`,注入 API key 到 config |
| Config 模板 | `config/config.a2a.json` | 新增 | MiniMax model + a2a & pico channels + gateway 0.0.0.0 |
| Docker | `docker/Dockerfile.appbase` | 修改 | builder 多編 `picoclaw-envcfg` 並 COPY 進 runtime |
| Docker | `docker/entrypoint-a2a.sh` | 新增 | 啟動跑 `picoclaw-envcfg` render config → 啟動 launcher |
| Docker | `docker/docker-compose.a2a.yml` | 新增 | host networking、mount `.env`、mounts、named volume workspace |
| 依賴 | `go.mod` / `go.sum` | 修改 | `github.com/bizshuk/gosdk@master` 變成直接依賴 |
| 文件 | `docs/docker-a2a-agent.md` | 新增 | 使用與手動測試指南 (Part A 本機 / Part B 容器) |

## 既有 (平行 session) 已 staged 的 A2A 程式碼

mDNS 廣播 / 探索、WS peer 協定、registry bridge 等核心在本次之前已存在並 staged:
`pkg/channels/a2a/{discovery,server,client,bridge,protocol,peer_table,session_map,turn_counter,ask_client,init}.go`
與 `pkg/agent/{discovery,registry}.go`、`pkg/config/config_channel.go`、
`pkg/gateway/gateway.go`、`pkg/events/kind.go` 的接線。本次在其上補 HTTP 入口與 Docker 打包。

## 金鑰流程 (Secret Flow)

```txt
.env (gitignored, mounted)
  └─(gosdk/config.Default 載入)→ picoclaw-envcfg
        └─(注入 minimax model_list.api_keys)→ config.json
              └→ picoclaw-launcher
```

解析順序 (第一個非空者勝):`os.Getenv(MINIMAX_API_KEY)` → `.env` (gosdk/viper)。
原因:`model_list.api_keys` 是 PicoClaw 唯一無 env binding 的 secret,只能在啟動時寫進 config。

## 入口 (Endpoints)

| 用途 | 位址 | 協定 |
| --- | --- | --- |
| 跑 LLM (最簡單) | `http://HOST:18791/a2a/v1/ask` | HTTP POST JSON |
| Agent 互連 | `ws://HOST:18791/a2a/v1/ws` | WebSocket (`picoclaw-a2a.v1`) |
| mDNS 服務 | `_picoclaw-a2a._tcp.local` | 多播 (host networking) |
| WebUI | `http://HOST:18800` | 瀏覽器 |
| Pico 聊天 | `ws://HOST:18790/pico/ws` | WebSocket |

## 驗證狀態 (Verification)

各層獨立驗過 (實機 end-to-end 容器跑由使用者手動進行):

| 項目 | 結果 |
| --- | --- |
| `go build` (goolm,stdjson) launcher + a2a + envcfg | 通過 |
| `go test` a2a + config | 通過 |
| `go vet` a2a + envcfg、`gofmt` | 乾淨 |
| `config.a2a.json` 經 picoclaw `LoadConfig` 實載 | 通過 (a2a:18791 / pico / minimax 都在) |
| `picoclaw-envcfg` 載 `.env` 注入 (含 os-env 覆蓋) | 通過 |
| `docker compose config` | 解析正常 |

## 待辦 / 注意 (Follow-ups)

- 實機驗證:`docker compose -f docker/docker-compose.a2a.yml up --build` + curl `/a2a/v1/ask`。
- `.env.example`:本次曾被覆寫 (原有多 provider 範例),已留作未 staged 變更待整併,
  建議把 `MINIMAX_API_KEY=` 併入原檔而非取代。
- macOS/Windows Docker Desktop:host networking 無法觸及實體 LAN,mDNS 僅限容器本機。
