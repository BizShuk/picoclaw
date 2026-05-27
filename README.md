# PicoClaw

PicoClaw 是一個以 Go 語言從頭打造的超輕量級個人 AI 助理，旨在以極低的記憶體與硬體成本運行於嵌入式 RISC-V 或 ARM 設備，並在極短的時間內啟動，為用戶提供高效且靈活的智能代理服務。

## 業務領域 (Business Domains)

### 多平台對話通道與網關 (Chat Channels & Gateway)

負責連接與管理多達 19 種以上的外部聊天與通訊平台，將各平台的私有訊息結構標準化並過濾速率，透過傳輸通道進行 inbound 與 outbound 的訊息收發。

`領域流程 (Domain Flow):`

1. 外部通訊平台（例如 Telegram）產生使用者事件，由 `pkg/channels/telegram` 等通道接收器捕獲。
2. 通道處理器將訊息結構轉化為標準的 `bus.InboundMessage`，並調用 `MessageBus.PublishInbound` 發布。
3. `pkg/channels/manager.go` 中的通道管理器進行速率限制 (Rate Limiting) 與狀態控制（如顯示輸入中狀態），最後交由 Agent 處理。

`核心實體 (Key Entities):` `InboundMessage`, `OutboundMessage`, `MessageBus`, `ChannelManager`

`相關處理器 (Related Handlers):` `pkg/channels/manager.go` 中的 `Manager`、`pkg/gateway/router.go` 中的 `Router`

---

### 智能代理循環與管線執行 (Agent Loop & Pipeline)

負責協調單次對話回合 (Turn) 的完整生命週期。透過專利的五階段管線控制大語言模型 (LLM) 推理與工具執行的相互轉換與併發。

`領域流程 (Domain Flow):`

1. `AgentLoop.Run` 自 `MessageBus` 的通道讀取 `InboundMessage`。
2. 啟動 `Pipeline` 執行，經歷 `Setup`（載入 Prompt 與歷史記錄）與 `LLM Call`（呼叫大語言模型）階段。
3. 若 LLM 請求調用工具，進入 `Execute Tools` 階段執行，之後透過 `Streaming` 與 `Finalize` 完成該回合對話，並將 `OutboundMessage` 發送回 `MessageBus`。

`核心實體 (Key Entities):` `AgentLoop`, `Pipeline`, `TurnState`, `TurnContext`

`相關處理器 (Related Handlers):` `pkg/agent/agent.go` 中的 `AgentLoop`、`pkg/agent/pipeline.go` 中的 `Pipeline`

---

### 動態路由與多模態推理 (Routing & Multimodal Reasoning)

負責解析使用者訊息之複雜度以動態路由至輕量或主要模型，並對語音、圖片與檔案 (Vision Pipeline) 進行解碼與文字轉錄，以配合多模態推理。

`領域流程 (Domain Flow):`

1. `AgentLoop` 呼叫 `routing/Router` 評估該 turn 之輸入訊息複雜度。
2. `Router.ResolvedRoute` 決定使用 Light Model (輕量模型) 或 Primary Model (主要模型)。
3. 若訊息包含多媒體，調用 `asr.Transcriber` 將語音轉換為文字，或由 `media.MediaStore` 把圖片 Base64 編碼，再傳入 LLM 提供商 (LLM Provider) 進行推理。

`核心實體 (Key Entities):` `ResolvedRoute`, `LLMProvider`, `MediaStore`, `Transcriber`

`相關處理器 (Related Handlers):` `pkg/routing/router.go` 中的 `Router`、`pkg/providers/factory.go` 中的 `Factory`

---

### 擴充工具、協定與自我進化 (Tools, MCP & Self-Evolution)

提供本地與遠端工具整合能力（包括本地文件、Shell 執行、定時任務 Cron、MCP 協定伺服器、Skills 腳本）以及 Agent 自我修改與代碼修補之進化機制。

`領域流程 (Domain Flow):`

1. LLM 呼叫後返回 Tool Calls，在 `Pipeline` 執行階段中透過 `ToolRegistry` 比對執行權限。
2. 工具管理器動態派發給本地工具（如 `fs`、`shell`、`cron`）或遠端 `MCP` 伺服器與載入的 `SKILL.md`。
3. 在特定情況下，由 `pkg/evolution` 模組對 Agent 的內部代碼或配置進行打補丁。

`核心實體 (Key Entities):` `ToolRegistry`, `MCPServer`, `Skill`, `EvolutionBridge`

`相關處理器 (Related Handlers):` `pkg/tools/registry.go` 中的 `Registry`、`pkg/mcp/manager.go` 中的 `Manager`、`pkg/evolution/evolution.go` 中的 `Evolution`

---

### 會話狀態與持久化 (Session & Persistence)

管理多用戶會話生命週期、會話配置、持久化 JSONL 記憶體、狀態管理及敏感資料過濾。

`領域流程 (Domain Flow):`

1. 當 `AgentLoop` 執行對話時，調用 `session.Manager` 管理當前會話狀態。
2. 會話內容被序列化並儲存於 JSONL 格式之記憶體檔案中。
3. 讀寫過程中，`pkg/isolation` 和 `pkg/auth` 會過濾敏感資料與保護憑證。

`核心實體 (Key Entities):` `SessionScope`, `SessionStore`, `isolation.Sanitizer`

`相關處理器 (Related Handlers):` `pkg/session/manager.go` 中的 `Manager`、`pkg/memory/store.go` 中的 `Store`

---

## 領域關聯 (Domain Relationships)

PicoClaw 內部的領域關聯如下：

- `多平台對話通道與網關` 作為整套系統的 I/O 門戶，其接收到的 Inbound 事件會投遞至 MessageBus，驅動 `智能代理循環與管線執行`。
- `智能代理循環與管線執行` 在進行 Setup 階段時，會調用 `動態路由與多模態推理` 取得合適的模型指派並將音訊與影像解碼。
- 在 Execution 階段中，`智能代理循環與管線執行` 會調用 `擴充工具、協定與自我進化` 來執行 Tool Call，並寫入結果。
- `會話狀態與持久化` 在對話過程與工具執行中提供歷史狀態與記憶體。

## 使用方式 (Usage)

### 代理與網關領域

- `picoclaw agent`：啟動互動式對話命令行介面。
- `picoclaw agent -m "..."`：執行單次的問題查詢並輸出答案。
- `picoclaw gateway`：啟動多通道網關伺服器。

### 工具與 MCP 領域

- `picoclaw mcp add <name> -- <command>`：新增或更新一個 MCP 伺服器項目。
- `picoclaw mcp list`：列出當前配置的 MCP 伺服器。
- `picoclaw cron add <name> <schedule> <message>`：新增一個定時排程任務。
- `picoclaw skills install <skill-name>`：安裝指定的技能組。

### 配置與狀態領域

- `picoclaw onboard`：初始化專案配置與工作區。
- `picoclaw status`：查看目前代理與網關的狀態。

## 改善建議 (Improvement Suggestions)

- [ ] 精簡記憶體佔用與效能優化：目前合併大量 PR 後記憶體使用量略有上升（10-20MB），應針對 `BM25` 或 `Context` 管理進行更細緻的 GC 與快取機制優化，確保在 RISC-V 單核心板上之穩定度。
- [ ] 增強通道速率限制與並發安全：針對 `pkg/channels/manager.go` 中的 Rate Limit 機制，在高並發多會話的環境下，需引入外部分散式限流（如 Redis 支援）或自定義 token bucket，避免某些 IM 平台 API 呼叫超出限制。
- [ ] 工具安全隔離與沙盒化 (Sandbox)：`pkg/tools/shell` 目前缺乏硬性沙盒隔離，在無 root/proot 容器的環境中執行不受信的腳本會有安全疑慮。建議加強沙盒機制或預設在 gVisor/WebAssembly 中執行 shell 工具。
- [ ] 優化 SubAgent 生命週期監控與追蹤：目前多個子代理並發執行時，`spawn_status` 所提供的追蹤指標較為陽春，可建立 `Runtime EventBus` 與 WebUI 的實時連線，提供可視化的 Agent Trace 與 Tool Call 樹狀圖。
