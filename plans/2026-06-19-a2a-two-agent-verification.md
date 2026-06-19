# A2A 雙 Agent 驗證計畫 (Two-Agent Verification Plan)

日期 (Date): 2026-06-19
目標:啟兩個 picoclaw A2A agent,驗證 (1) 互相收到 mDNS、(2) 一個 agent 請求另一個回覆並觀察處理的資料流。

## 先決事實 (Codebase Facts — 影響怎麼測)

| 事實 | 位置 | 對測試的影響 |
| --- | --- | --- |
| peer 只能由 mDNS 探索產生,無靜態設定 | `a2a/discovery.go:194` (唯一 `Upsert`) | Test 2 必須先 Test 1 成功 |
| 探索到 peer → 登錄成可 spawn 的遠端 agent,並印 log | `agent/registry.go` `RegisterDynamic` (`Registered remote agent`) | Test 1 的判定訊號就是這行 log |
| `KindA2A*` 事件「有定義但沒被 emit」 | `events/kind.go` | 不能靠 runtime event;觀測靠 `logger("a2a",…)` + HTTP 回應 |
| agent 找 peer 走 subturn spawn | `agent/subturn.go:323-325` → `RemoteHook` → `AskPeer` | Test 2 要 prompt 讓 Alice 對 Bob 的 agent_id 下 spawn/subagent 工具 |
| Dial peer 會印 log | `a2a/client.go` (`Dialing peer WS`) | Test 2 觀測 A→B 連線的訊號 |

## 共用測試平台 (Shared Harness)

推薦:`docker compose` 兩個 service,`bob` 用 `network_mode: "service:alice"` 共用同一 network namespace。
理由:mDNS 走多播 `224.0.0.251`,跨 container bridge 不通、host networking 在 macOS 觸不到實體 LAN;
共用 netns 時多播在同一 netns loopback,跨平台穩定,且仍真實走「announce + browse + register」整條碼路。

```txt
單一 netns (alice 的)
├── alice  agent_id=alice  a2a:18791  gateway:18790  webui:18800
└── bob    agent_id=bob    a2a:28791  gateway:28790  webui:28800   (network_mode: service:alice)
   兩者各自 announce + browse,多播 loopback → 互相發現
```

需要的檔案 (待實作):
- `config/config.a2a.alice.json`、`config/config.a2a.bob.json`:各自 agent_id 與 port,皆啟 a2a + minimax,共用 `MINIMAX_API_KEY`。
  - 或:沿用單一 render 後 config,用 env 覆蓋 `PICOCLAW_CHANNELS_A2A_AGENT_ID` / `PICOCLAW_CHANNELS_A2A_PORT` 與 gateway/webui port。
- `docker/docker-compose.a2a-pair.yml`:兩 service、共用 netns、各自 mount config、`.env` 注入金鑰、`log_level: debug`。

替代平台 (較輕,但有 caveat):本機兩個 process。macOS 上 `hashicorp/mdns` 綁 5353 可能與系統 `mDNSResponder` 衝突;
若可行,各 process 設不同 agent_id/port 即可。Linux 實機可用 host networking 做真實跨機/跨網段驗證 (最高保真)。

前置:`log_level` 設 `debug`,並把兩個 process 的 stdout 分別導到 `alice.log` / `bob.log`。

---

## Test 1 — 互相收到 mDNS (Mutual Discovery)

不需 LLM。只驗證 announce/browse/register。

步驟:
1. 啟動 alice 與 bob (compose up)。
2. 等 1–2 個 announce 週期 (預設 30s;可把 `announce_interval` 調小加速,但需用 ns 整數或改用預設)。
3. 判定 (兩個方向都要成立):

| 方向 | 檢查 | 判定 |
| --- | --- | --- |
| alice 看到 bob | `grep "Registered remote agent" alice.log` | 出現 `agent_id: bob, source: a2a-mdns` |
| bob 看到 alice | `grep "Registered remote agent" bob.log` | 出現 `agent_id: alice, source: a2a-mdns` |
| 探索啟動 | `grep "mDNS discovery started" *.log` | 兩邊各一行 |

補強 (選用):
- 用 webui/launcher 的 agents 列表確認遠端 agent 入列 (`registry.ListAgents` 含 remotes)。
- 失敗排查:確認兩者 `service_type` 一致 (`_picoclaw-a2a._tcp`)、agent_id 不同 (self-filter 會濾掉同名)、netns 真的共用。

通過標準:雙向都出現對方的 `Registered remote agent`。

---

## Test 2 — A 請求 B 回覆 + 觀察資料 (Cross-Agent Ask)

前提:Test 1 通過 (alice 的 registry 已有 bob)。需 LLM (MiniMax)。

設計一個「確定性」任務,讓資料可被驗證 (避免 LLM 不確定性):
- 範例 prompt 給 alice:
  `Use the "bob" agent to process this: reply with exactly the token ECHO-7Q2. Return bob's reply verbatim.`
- echo 一個唯一 token (`ECHO-7Q2`) 最容易驗證資料來回;或用算術 `17*23=391`。

步驟:
1. 對 alice 發 HTTP 請求:
   ```bash
   curl -fsS -X POST http://localhost:18790/a2a/v1/ask \
     -H 'Content-Type: application/json' \
     -d '{"text":"Use the bob agent to process this: reply with exactly the token ECHO-7Q2. Return bob'\''s reply verbatim."}'
   ```
2. 觀察資料流 (debug log + HTTP 回應),逐跳對照下表。

### 觀測矩陣 (What data processed — 看哪裡)

| 跳 (Hop) | 處理的資料 | 觀測點 |
| --- | --- | --- |
| 1. caller → alice | 原始 prompt | curl 送出的 body |
| 2. alice 決策 | spawn 工具呼叫 (target=`bob`, question=…) | `alice.log` debug:tool call args |
| 3. alice → bob | WS dial + ask frame | `alice.log` `Dialing peer WS`;ask 的 `question` |
| 4. bob 收到 | inbound 內容 (`a2a:alice`, content=question) | `bob.log`:publish inbound / turn 開始 |
| 5. bob 處理 | bob 跑 turn 產生答案 | `bob.log`:turn 輸出 (含 `ECHO-7Q2`) |
| 6. bob → alice | reply frame (answer) | `bob.log` Send;`alice.log` 收到 reply |
| 7. alice → caller | 最終答案 (含 bob 結果) | curl 的 HTTP 回應 `{"answer": "...ECHO-7Q2..."}` |

通過標準:
- HTTP 回應的 `answer` 含 `ECHO-7Q2` (證明資料 A→B→A→caller 完整來回)。
- `bob.log` 顯示它確實收到 alice 的 inbound 並處理 (證明是 B 處理、非 A 自答)。

### 想看「原始 frame JSON」(選用,需小幅 instrument)

目前碼路只 log `Dialing peer WS`,不印 envelope 內容。若要逐字看 ask/reply frame:
- 在 `a2a/server.go serveConn`(收到 `TypeAsk` 後)與 `a2a.go Send`(送 reply 前)各加一行
  `logger.InfoCF("a2a", "frame", {...})` 印 `session_id/from/question/answer`。
- 屬暫時性 debug instrument,驗證後移除;或改抓 `bob` inbound publish 的既有 log 即可滿足「資料被處理」的證據。

---

## 風險與注意 (Risks)

- Test 2 依賴 Test 1:peer 無靜態注入,mDNS 不通則 A 不認識 B。
- A2A 事件未 emit:只能靠 log,故務必開 `debug` 並分流兩份 log。
- LLM 不確定性:prompt 要「點名 bob 的 agent_id」並指定確定性輸出 (echo token);必要時加一句強制使用 sub-agent 工具。
- 平台:macOS 跨 container/host LAN 多播不可靠 → 用共用 netns;真實跨機驗證請在 Linux + host networking。
- 加速 announce:`announce_interval` 為 `time.Duration` 無自訂 JSON,JSON 要填 ns 整數,否則留空吃預設 30s。

## 產出 (Deliverables if executed)

1. 兩份 config (或 env 覆蓋方案) + `docker-compose.a2a-pair.yml`。
2. 一支驗證腳本 `scripts/verify-a2a-pair.sh`:啟動 → 等探索 → grep 判定 Test 1 → curl 判定 Test 2 → 印 PASS/FAIL 與資料流摘要。
3. (選用) 暫時 frame log instrument。
```
