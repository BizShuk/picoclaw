# Docker App-Workspace Agent — 使用說明

把任意 app 原始碼資料夾 mount 進容器當 agent 的 base,從 webui 或 gateway 發問,
agent 以該資料夾內容回答,並可讀寫 / 執行 (Go / Node / Python)。

## 啟動

1. 在 `config/config.app.json` 填入真實的模型金鑰 (把 `sk-ant-REPLACE-ME` 換成你的金鑰)。
   此改動屬本機機密,請勿 commit。若不想動到追蹤中的檔案,可改 mount 自己的副本
   (見下方「用本機 config 副本」)。

2. 啟動 (用 compose,`APP_DIR` 指向你的 app 資料夾):

   ```bash
   APP_DIR=/path/to/your/app docker compose -f docker/docker-compose.app.yml up --build
   ```

   - WebUI:    `http://localhost:18800` (launcher 主控台,啟動時 log 會印出 dashboard token)
   - Gateway:  `ws://localhost:18790/pico/ws` (程式化 WebSocket 觸發)

## 觸發方式

### WebUI

開 `http://localhost:18800`,用 log 印出的 dashboard token 登入,進聊天頁直接發問。
聊天前端會連到 `/pico/ws` 把訊息送進同一個 agent。

### 程式化 (gateway, WebSocket)

pico channel 是 WebSocket-only,掛在 `ws://localhost:18790/pico/ws`。
token 預設為 `local-dev` (config 內 `channel_list.pico.settings.token`),
`allow_token_query: true` 允許用查詢參數帶 token。

送出的訊息格式 (`PicoMessage`):

    {"type": "message.send", "payload": {"content": "你的問題"}}

回覆會以 `message.create` (完整訊息) 與 `message.update` (串流) 等 frame 推回同一條連線。

Python 範例 (`pip install websockets`):

    ```python
    import asyncio, json, websockets

    async def main():
        uri = "ws://localhost:18790/pico/ws?token=local-dev"
        async with websockets.connect(uri) as ws:
            await ws.send(json.dumps({
                "type": "message.send",
                "payload": {"content": "List the files in your workspace and tell me what this app prints."},
            }))
            async for raw in ws:
                msg = json.loads(raw)
                if msg.get("type") == "message.create":
                    print(msg["payload"].get("content", ""))
                    break

    asyncio.run(main())
    ```

## 用本機 config 副本 (不動到追蹤檔)

    ```bash
    cp config/config.app.json config/config.app.local.json
    # 編輯 config/config.app.local.json 填入真實金鑰
    ```

然後啟動時用 compose override 或直接 docker run 覆蓋 config 掛載,例如:

    ```bash
    docker run --rm -e PICOCLAW_GATEWAY_HOST=0.0.0.0 \
      -p 18800:18800 -p 18790:18790 \
      -v /path/to/your/app:/root/.picoclaw/workspace \
      -v "$PWD/config/config.app.local.json":/root/.picoclaw/config.json:ro \
      picoclaw-appbase:dev
    ```

## 自訂 agent persona (選用)

在你的 app 資料夾根目錄放一個 `AGENT.md` (或舊版 `AGENTS.md`),picoclaw 會自動載入
當作 agent 的人格 / 系統提示 (`pkg/agent/definition.go`)。不放則用 picoclaw 預設人格。

## 已知副作用 (方案 A)

agent 的 workspace 就是你的資料夾,picoclaw 會在其中寫出 `sessions/`。
請把它加進該 app repo 的 `.gitignore`:

    ```bash
    echo "sessions/" >> /path/to/your/app/.gitignore
    ```

乾淨隔離 (零污染) 的版本見 `docs/backlog.md` 的方案 B。
