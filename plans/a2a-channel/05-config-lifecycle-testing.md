# §5 — Config Schema、生命週期、安全性、測試、可觀測性

> 系列: PicoClaw A2A Channel Design
> 章節: 5 / 5
> 狀態: draft, awaiting approval
> 上一節: [04-registry-integration.md](./04-registry-integration.md)

## 5.1 Config Schema

```go
// pkg/config/channel_a2a.go (new)
const ChannelA2A = "a2a"

type A2ASettings struct {
    AgentID           string        `yaml:"agent_id"             json:"agent_id"`
    Port              int           `yaml:"port"                 json:"port"`                // 0 = OS-assign
    Description       string        `yaml:"description"          json:"description"`         // 公告給 peer 看的能力描述
    AnnounceInterval  time.Duration `yaml:"announce_interval"    json:"announce_interval"`   // default 30s
    PeerTTL           time.Duration `yaml:"peer_ttl"             json:"peer_ttl"`            // default 90s
    MaxTurnDefault    int           `yaml:"max_turn_default"     json:"max_turn_default"`    // default 6
    AskTimeout        time.Duration `yaml:"ask_timeout"          json:"ask_timeout"`         // default 60s per turn
    DialTimeout       time.Duration `yaml:"dial_timeout"         json:"dial_timeout"`        // default 5s
    IdleConnTTL       time.Duration `yaml:"idle_conn_ttl"        json:"idle_conn_ttl"`       // default 60s
    BindAddr          string        `yaml:"bind_addr"            json:"bind_addr"`           // default "0.0.0.0"
    MDNSDomain        string        `yaml:"mdns_domain"          json:"mdns_domain"`         // default "local."
    ServiceType       string        `yaml:"service_type"         json:"service_type"`        // default "_picoclaw-a2a._tcp"
}
```

YAML 範例：

```yaml
channels:
    a2a:
        enabled: true
        type: a2a
        allow_from:
            - "a2a:bob"
            - "a2a:carol"
        settings:
            agent_id: alice
            description: "Pico-V hardware specialist"
            port: 0
            announce_interval: 30s
            peer_ttl: 90s
            max_turn_default: 6
            ask_timeout: 60s
```

預設值取捨：

- `announce_interval=30s, peer_ttl=90s` (3x) — 容忍偶爾掉包，仍能在 ~90s 內偵測 peer 下線。
- `max_turn_default=6` — 對 chat 風格 A2A 來說，6 輪足夠完成多數查詢；超過代表 LLM 卡在來回，該停。
- `ask_timeout=60s` per turn — 跟 OpenAI/Anthropic 預設 LLM call timeout 一致。
- `idle_conn_ttl=60s` — 比一般 keep-alive 短，減少閒置 socket 數。

## 5.2 mDNS 細節

- Service type: `_picoclaw-a2a._tcp` (避免與 Hermes 的 `_agent._tcp` 衝突)
- Instance name: `<agent_id>._picoclaw-a2a._tcp.local.`
- TXT records:
    - `v=1` (protocol version)
    - `path=/a2a/v1/ws` (WS endpoint path)
    - `agent=<agent_id>` (冗餘但便 debug)
    - `desc=<description>` (簡短能力描述，≤200 bytes)
- Browse 間隔: 與 announce 同步 (每 30s 主動掃一次 + 持續 listen)。

## 5.3 生命週期 (對齊 IRC/MQTT)

```
Gateway.Start
  → for each channel: factory(...)
    → A2AChannel constructed (NewBaseChannel + zero state)
  → registry.AssembleAgents()                            ← 既有
  → for each channel implementing RegistryAware:
      → ch.SetAgentRegistry(registry)
  → channelManager.StartAll(ctx)
    → A2AChannel.Start(ctx)
      → server.Listen(:port)
      → peers.StartReaper()
      → discovery.Start()                                ← 開始 announce + browse
      → SetRunning(true)
  → bus loop running ...

Gateway.Stop
  → channelManager.StopAll(ctx)
    → A2AChannel.Stop(ctx)
      → discovery.Stop()                                  ← 停 announce
      → bridge.unregisterAllPeers()                        ← 清 registry remotes
      → server.Shutdown(ctx)                              ← 拒絕新 ask
      → clients.CloseAll()                                ← 關 client pool
      → SetRunning(false)
```

## 5.4 安全性

當前威脅模型：**LAN 內可信成員** (跟 Hermes 一致)。理由：

- mDNS 本身無認證 — 任何同網段都能宣告自己是 `alice`。
- 但 PicoClaw 已有 `allow_from`：peer 來 ask 時用 `SenderInfo.CanonicalID = "a2a:" + from`，必須在 `allow_from` 才接受。

明確界定：

| 場景                                       | 防禦                                                                                                        |
| ------------------------------------------ | ----------------------------------------------------------------------------------------------------------- |
| 同網段惡意 peer 偽裝 alice                 | mDNS 衝突 (兩個 alice 同時宣告)；本機 allow_from 仍阻擋未授權 ID。但「冒充」風險仍存。Phase 2 加 token/cert |
| Peer 送大量 ask 耗光資源                   | SubTurn concurrency semaphore 已蓋；可選: per-peer rate limit (放 Phase 2)                                  |
| Peer 送惡意 prompt 試圖讓本地 agent 做壞事 | 跟 Telegram 使用者送惡意 prompt 同類問題；走 hooks 系統的 approval/redaction                                |
| Peer 嘗試 resume 別人 session              | 我們的 session_id 是 UUID-v4，沒猜中前提；同時 sessionMap 鎖死「(extID → localKey)」一對一                  |
| 跨網段 / NAT                               | 不支援。明確標為 LAN-only                                                                                   |

`★ Insight ─────────────────────────────────────`
PicoClaw 既有的 hook 系統 (`pkg/agent/hooks.go`) 在 inbound 之後、tool execution 之前都會跑 — 這意味著：當 peer 透過 A2A 問本地 agent 危險問題時，現有的 approval hook 就會擋下來（例如要求人工確認 shell 命令）。所以 A2A channel 不需要再造一層 safety filter，借助既有 hook 即可。這是「naturally integrate」的另一個體現。
`─────────────────────────────────────────────────`

## 5.5 可觀測性

走 `pkg/events` runtime event bus，新增以下 event kinds：

```go
// In pkg/events/kinds.go (extend)
const (
    KindA2APeerDiscovered  = "a2a.peer.discovered"
    KindA2APeerLost        = "a2a.peer.lost"
    KindA2AAskStart        = "a2a.ask.start"
    KindA2AAskComplete     = "a2a.ask.complete"
    KindA2AAskError        = "a2a.ask.error"
    KindA2AMaxTurnExceeded = "a2a.max_turn.exceeded"
)
```

每個 event payload 包含：`session_id, peer_id, turn, max_turn, latency_ms (for complete), error_code (for error)`。

`pkg/health` 接 a2a status：列出已知 peers + 本地 server 是否聽中。

Logging：所有重要轉折點都用 structured logging，category=`a2a`，跟 IRC channel 同風格。

## 5.6 測試策略

### Unit Tests (a2a_test.go + 各檔案 \_test.go)

| 元件              | 測什麼                                                                                           |
| ----------------- | ------------------------------------------------------------------------------------------------ |
| `protocol.go`     | envelope round-trip (marshal/unmarshal)、unknown frame type 容忍                                 |
| `peer_table.go`   | Upsert/Get/Reap、TTL 過期、onAdd/onRemove 觸發                                                   |
| `session_map.go`  | Resolve 同一 extID 重複呼叫回相同 localKey、不同 peer 不混                                       |
| `turn_counter.go` | CheckAndIncrement boundary (1, max_turn, max_turn+1)、done=true 後 reject 後續                   |
| `ask_client.go`   | pendingMap deliver/cancel/late-arrival、frame_id collision (生 UUID 應幾乎不可能但驗 panic-free) |
| `client.go`       | idle close + reconnect、dial timeout、concurrent ask on same conn                                |

### Integration Tests (a2a_integration_test.go, build tag `+integration`)

兩個 in-process `A2AChannel`，繞過 mDNS（直接 `peers.Upsert`），跑端到端：

1. A.AskPeer("bob", "What is 1+1?") → 假 agent B (用 mock LLM) 回 "2"
2. A 連續問 6 次同 session → 第 7 次回 ErrMaxTurnExceeded
3. A 問 → B 中途 panic → A 收到 error reply
4. A 問 → A.Stop() → 所有 pending ask 立即 cancel
5. A 問 → B 不存在 → ErrPeerUnreachable

### Manual / Network Test (docs/architecture/a2a.md 附 runbook)

兩台筆電同 WiFi：

```bash
# Laptop 1
picoclaw agent --config alice.yaml

# Laptop 2
picoclaw agent --config bob.yaml

# 在 alice 對話：「請問 bob 系統的記憶體用量」
# Alice 的 LLM 應 spawn sub-agent bob → A2A AskPeer → bob 回答
```

## 5.7 刻意延後 (YAGNI)

對標 Hermes 的 Phase 2 路線：

| Feature                      | 為什麼先不做                                                    |
| ---------------------------- | --------------------------------------------------------------- |
| Cross-subnet relay           | mDNS 本來就只活同網段；跨段是另一個層級的問題                   |
| 訊息加密 (TLS over WS)       | LAN-only 前提；標準 wss 升級走在後                              |
| Token / mTLS auth            | 同上；先靠 allow_from                                           |
| 持久化 session_map           | session 跨重啟值不大；reboot 後另起新對話更安全                 |
| Streaming reply              | reply 一次 dump 即可；要 progressive 再加 `frame.type=progress` |
| Media attachment             | content 限文字；要傳檔案走 PicoClaw 既有 MediaStore + URL       |
| Sub-agent 結果自動 aggregate | 由 LLM 自己 reason 多個 ask 結果即可                            |

## 5.8 Rollout

1. **Phase 0 (本系列計畫)**：實作 + unit + integration，但 default `enabled: false`。
2. **Phase 0.5**：在 dogfooding 環境 (兩台同 LAN) 跑一週，收集 runtime event 觀察穩定度。
3. **Phase 1 (`enabled: true` available)**：documentation + sample config，發 release notes。
4. **Phase 2**：依使用回饋，加 token auth / streaming / metrics 等。

## 5.9 全節 summary (對映 user criteria)

| User 要求                 | 兌現方式                                                                                             |
| ------------------------- | ---------------------------------------------------------------------------------------------------- |
| WebSocket 通訊            | §2 envelope over `nhooyr/websocket`；§3 server + client pool                                         |
| session_id 續上           | §3 sessionMap (ext↔local)；§4.8 (parent, peer) cache                                                 |
| `maxTurn` 在 content      | §2 ask payload 帶 `max_turn` + `turn`；§2 對稱雙邊計數                                               |
| 用 mDNS                   | §3 discovery.go；§5.2 service type & TXT                                                             |
| 自然整合 PicoClaw channel | §1 收在 `pkg/channels/a2a/`；§3 走 BaseChannel + bus.PublishInbound                                  |
| 設計 agent loop           | §4 復用 SubTurn dispatcher，加 `RemoteAgentDescriptor` + `spawnRemoteSubTurn` fast-path，不另建 loop |
