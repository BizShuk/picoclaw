# Docker A2A Networked Agent — 使用與測試指南

一個持續運行的 PicoClaw 容器,會在區網以 mDNS 廣播自己、探索其他 agent,
並接受 HTTP 或 WebSocket 請求來跑 LLM (MiniMax)。

金鑰流程:`MINIMAX_API_KEY` 放在 `.env`,由 `gosdk/config.Default()` 載入,
再注入 `config.a2a.json` 的 minimax model_list 條目 (該欄位無 env binding,
必須在啟動時寫進 config)。

## 入口一覽 (Endpoints)

| 用途 | 位址 | 協定 |
| --- | --- | --- |
| 跑 LLM (最簡單) | `http://HOST:18791/a2a/v1/ask` | HTTP `POST` JSON |
| Agent 互連 | `ws://HOST:18791/a2a/v1/ws` | WebSocket (`picoclaw-a2a.v1`) |
| mDNS 服務 | `_picoclaw-a2a._tcp.local` | 多播 (host networking) |
| WebUI | `http://HOST:18800` | 瀏覽器 |
| Pico 聊天 | `ws://HOST:18790/pico/ws` | WebSocket |

## 元件 (Components)

| 檔案 | 職責 |
| --- | --- |
| `.env` (gitignored) | 放 `MINIMAX_API_KEY` (從 `.env.example` 複製) |
| `cmd/picoclaw-envcfg` | 用 `gosdk/config.Default()` 載 `.env`,把金鑰注入 config |
| `config/config.a2a.json` | 模板:MiniMax model + a2a & pico channels + gateway 0.0.0.0 |
| `docker/entrypoint-a2a.sh` | 容器啟動時跑 `picoclaw-envcfg` render config,再啟動 launcher |
| `docker/docker-compose.a2a.yml` | host networking、mount `.env`、mounts |
| `pkg/channels/a2a/` | mDNS 廣播 / 探索、WS peer 協定、`POST /a2a/v1/ask` HTTP 入口 |

金鑰解析順序 (第一個非空者勝):`os.Getenv(MINIMAX_API_KEY)` → `.env` (gosdk/viper)。
所以 `docker run -e` / compose env 仍可覆蓋 `.env`。

---

## Part A — 本機:用 gosdk 載 `.env` 並 render config

驗證 `.env` → gosdk → config 注入是否正確 (不需 Docker)。

1. 建立 `.env` (此檔已被 gitignore):

   ```bash
   cp .env.example .env
   # 編輯 .env,把 MINIMAX_API_KEY 換成真實金鑰
   ```

2. 跑 renderer (它會用 `gosdk/config.Default()` 從目前目錄載入 `.env`):

   ```bash
   go run ./cmd/picoclaw-envcfg \
     -template config/config.a2a.json \
     -out /tmp/config.a2a.local.json
   # => [envcfg] rendered ... (injected MINIMAX_API_KEY into 1 "minimax" model(s))
   ```

3. 確認金鑰已注入 (不會印出完整金鑰):

   ```bash
   python3 -c "import json; k=json.load(open('/tmp/config.a2a.local.json'))['model_list'][0]['api_keys'][0]; print('len=', len(k), 'prefix=', k[:6])"
   ```

   預期:`len=` 大於 0、`prefix=` 是你金鑰的開頭。

（選用）本機直接跑 picoclaw — 注意 `config.a2a.json` 的 `workspace` 指向容器路徑
`/root/.picoclaw/workspace`,本機跑請先把 render 出來的檔內 workspace 改成本機可寫目錄,
再 `go run -tags goolm,stdjson ./web/backend -console -public -no-browser /tmp/config.a2a.local.json`。

---

## Part B — 容器:mount `.env` 並啟動

1. 確認 repo 根目錄有 `.env` (Part A 已建立),內含 `MINIMAX_API_KEY=...`。

2. 啟動 (從 repo 根目錄執行):

   ```bash
   docker compose -f docker/docker-compose.a2a.yml up --build
   ```

   `docker-compose.a2a.yml` 會:
   - 以 `network_mode: host` 啟動 (mDNS 多播需要,見下)。
   - 把 `../.env` mount 到 `/root/.picoclaw/.env`。
   - entrypoint 跑 `picoclaw-envcfg` (gosdk 載 `.env`) → render `config.json` → 啟動 launcher。

3. 等容器起來後,確認兩個入口都活著:

   ```bash
   curl -fsS http://localhost:18790/health && echo " OK-GW"
   curl -fsS -o /dev/null -w "webui:%{http_code}\n" http://localhost:18800
   ```

4. 跑 LLM:純 HTTP POST

   ```bash
   curl -fsS -X POST http://localhost:18791/a2a/v1/ask \
     -H 'Content-Type: application/json' \
     -d '{"text":"用一句話介紹你自己"}'
   # => {"answer":"...","session":"http-...."}
   ```

   可帶 `session` 維持多輪;回應只含最終答案 (中間思考/工具訊息不回傳);
   超過 `ask_timeout` (預設 120 秒) 回 `504`。

5. (選用) WebSocket 互連入口:`ws://localhost:18791/a2a/v1/ws`,subprotocol `picoclaw-a2a.v1`。
   或 WebUI 直接開 `http://localhost:18800`。

6. 收尾:

   ```bash
   docker compose -f docker/docker-compose.a2a.yml down
   ```

---

## mDNS 與網路 (Discovery & Networking)

mDNS 走多播 `224.0.0.251`,無法穿過 Docker 預設 bridge 的 NAT,所以
compose 用 `network_mode: host`:

- Linux:host networking 下,同網段其他 PicoClaw 會自動發現本 agent,
  並把它登錄成可 spawn 的遠端 agent (`a2a-mdns`)。
- macOS / Windows Docker Desktop:host networking 無法觸及實體 LAN,
  mDNS 僅限容器本機;HTTP / WS 入口仍可用 `localhost` 存取。

## 掛載 app 當 workspace (選用)

```bash
APP_DIR=/path/to/your/app \
  docker compose -f docker/docker-compose.a2a.yml up --build
```

未指定 `APP_DIR` 時使用具名 volume `picoclaw-a2a-workspace`,容器自包含。
掛載真實 app 時,`sessions/` 會寫進該資料夾 (方案 A 已知副作用,見 `docs/backlog.md`)。
