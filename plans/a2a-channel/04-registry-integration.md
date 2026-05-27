# §4 — AgentRegistry 動態註冊 + SubTurn dispatcher hook

> 系列: PicoClaw A2A Channel Design
> 章節: 4 / 5
> 狀態: draft, awaiting approval
> 上一節: [03-internal-components.md](./03-internal-components.md)

## 4.1 既有現況回顧

| 元件                                 | 檔案                       | 現況                                                                                                           |
| ------------------------------------ | -------------------------- | -------------------------------------------------------------------------------------------------------------- |
| `AgentRegistry`                      | `pkg/agent/registry.go`    | 啟動時一次性建好 `agents map[string]*AgentInstance`；無動態 register/unregister API                            |
| `ListAgents` / `ListSpawnableAgents` | `pkg/agent/discovery.go`   | 把 registry 的 agent 轉成 `AgentDescriptor{ID, Name, Description}` 注入 system prompt                          |
| `SubTurnConfig.TargetAgentID`        | `pkg/agent/subturn.go`     | 已存在！LLM 透過 spawn tool 呼叫子 agent，這個欄位指向目標 agent。Sub-turn 跑該 agent 的 workspace/model/tools |
| `spawnSubTurn`                       | `pkg/agent/subturn.go:269` | 真正執行 sub-turn 的函式。會 lookup target agent、acquire concurrency semaphore、跑 runTurn                    |

關鍵發現：**`TargetAgentID` 是天然的整合點**。LLM 不知道目標 agent 是本地還是遠端 — 它只用 ID 名字。我們在 `spawnSubTurn` 開頭偵測「ID 是 remote peer」即可 fork 到 `AskPeer`。

## 4.2 RemoteAgentDescriptor — peer 在 registry 裡的長相

新增 type，與既有的 `AgentInstance` 平起平坐：

```go
// In pkg/agent/registry.go
type RemoteAgentDescriptor struct {
    ID          string             // e.g. "bob" (mDNS instance name)
    Name        string             // human-readable
    Description string             // peer 公告的能力描述
    Source      string             // "a2a-mdns" — 標明來源 channel
    RemoteHook  RemoteSpawnHook    // callback: 把 sub-turn 轉成 AskPeer
}

type RemoteSpawnHook func(ctx context.Context, cfg SubTurnConfig) (*tools.ToolResult, error)
```

`AgentRegistry` 加平行欄位：

```go
type AgentRegistry struct {
    cfg      *config.Config
    agents   map[string]*AgentInstance       // 既有：本地 agent
    remotes  map[string]*RemoteAgentDescriptor // 新增：遠端 peer
    resolver *routing.RouteResolver
    mu       sync.RWMutex
}
```

## 4.3 動態 API

```go
// RegisterDynamic 註冊一個遠端 peer 為可被 spawn 的「虛擬 agent」。
// 由 a2a channel 在 mDNS 發現新 peer 時呼叫。
func (r *AgentRegistry) RegisterDynamic(d *RemoteAgentDescriptor) error {
    r.mu.Lock()
    defer r.mu.Unlock()
    id := routing.NormalizeAgentID(d.ID)
    // peer 不能蓋掉本地真實 agent
    if _, exists := r.agents[id]; exists {
        return fmt.Errorf("agent id %q conflicts with local agent", id)
    }
    r.remotes[id] = d
    logger.InfoCF("agent", "Registered remote agent", map[string]any{
        "agent_id": id, "source": d.Source,
    })
    return nil
}

func (r *AgentRegistry) UnregisterDynamic(id string) {
    r.mu.Lock()
    defer r.mu.Unlock()
    id = routing.NormalizeAgentID(id)
    delete(r.remotes, id)
    logger.InfoCF("agent", "Unregistered remote agent", map[string]any{
        "agent_id": id,
    })
}

// ResolveRemote 回傳 peer descriptor (含 RemoteHook)，若不是 remote 則回 nil。
func (r *AgentRegistry) ResolveRemote(id string) *RemoteAgentDescriptor {
    r.mu.RLock()
    defer r.mu.RUnlock()
    return r.remotes[routing.NormalizeAgentID(id)]
}
```

## 4.4 ListAgents/ListSpawnableAgents 擴充

`pkg/agent/discovery.go` 兩支函式都要把 remotes 也納入 descriptor 列表。改動極小：

```go
func (r *AgentRegistry) ListAgents(workspace string) []AgentDescriptor {
    r.mu.RLock()
    defer r.mu.RUnlock()

    descriptors := make([]AgentDescriptor, 0, len(r.agents)+len(r.remotes))
    // local agents
    for _, agent := range r.agents { ... }
    // remote agents
    for _, rd := range r.remotes {
        descriptors = append(descriptors, AgentDescriptor{
            ID: rd.ID, Name: rd.Name, Description: rd.Description,
        })
    }
    return descriptors
}
```

`ListSpawnableAgents` 同步擴充。`agentAllowsSubagent` 既有的 allowlist 邏輯**繼續適用** — 即使是 remote peer，仍受本地 agent config 的 `allowed_subagents` 限制。這給了「peer 出現在網路上但本地仍可拒絕用它」的 opt-in 控制。

## 4.5 SubTurn dispatcher hook

`spawnSubTurn` 開頭加 fast-path：

```go
func spawnSubTurn(ctx context.Context, al *AgentLoop, parentTS *turnState, cfg SubTurnConfig) (*tools.ToolResult, error) {
    // === NEW: remote-agent fast-path ===
    if cfg.TargetAgentID != "" {
        if remote := al.registry.ResolveRemote(cfg.TargetAgentID); remote != nil {
            return spawnRemoteSubTurn(ctx, al, parentTS, remote, cfg)
        }
    }
    // === existing local sub-turn path unchanged ===
    rtCfg := al.getSubTurnConfig()
    ...
}
```

`spawnRemoteSubTurn` 處理 remote 路徑：

```go
func spawnRemoteSubTurn(
    ctx context.Context,
    al *AgentLoop,
    parentTS *turnState,
    remote *RemoteAgentDescriptor,
    cfg SubTurnConfig,
) (*tools.ToolResult, error) {
    rtCfg := al.getSubTurnConfig()

    // 1. Concurrency semaphore — 跟本地 sub-turn 共用同一個，避免「LLM 一次叫 N 個 remote 把資源耗光」
    if parentTS.concurrencySem != nil {
        timeoutCtx, cancel := context.WithTimeout(ctx, rtCfg.concurrencyTimeout)
        defer cancel()
        select {
        case parentTS.concurrencySem <- struct{}{}:
            defer func() { <-parentTS.concurrencySem }()
        case <-timeoutCtx.Done():
            return nil, ErrConcurrencyTimeout
        }
    }

    // 2. Depth guard — remote 也算 depth+1
    if parentTS.depth+1 > rtCfg.maxDepth {
        return nil, ErrDepthLimitExceeded
    }

    // 3. Timeout
    timeout := cfg.Timeout
    if timeout <= 0 {
        timeout = rtCfg.defaultTimeout
    }
    callCtx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()

    // 4. 交給 channel 自己的 hook
    return remote.RemoteHook(callCtx, cfg)
}
```

關鍵 invariant：

- 同一個 concurrency semaphore — local 和 remote sub-turn 共用，因此 LLM 不能用 "remote 是免費的" 繞過限制。
- depth 算進來 — remote sub-turn 不算 depth=0，仍受 maxDepth=3 限制。
- timeout 套用 — context 一過期，AskPeer 自己會 cancel 並回 `ctx.Err()`。

## 4.6 bridge.go — A2A channel 怎麼提供 RemoteHook

```go
// pkg/channels/a2a/bridge.go
type registryBridge struct {
    registry *agent.AgentRegistry   // 注入
    channel  *A2AChannel
}

func (b *registryBridge) onPeerAdded(p *PeerInfo) {
    desc := &agent.RemoteAgentDescriptor{
        ID:          p.AgentID,
        Name:        p.AgentID,
        Description: fmt.Sprintf("Remote A2A peer at %s:%d", p.Host, p.Port),
        Source:      "a2a-mdns",
        RemoteHook:  b.makeHook(p.AgentID),
    }
    if err := b.registry.RegisterDynamic(desc); err != nil {
        logger.WarnCF("a2a", "Failed to register peer", map[string]any{
            "peer": p.AgentID, "err": err.Error(),
        })
    }
}

func (b *registryBridge) onPeerRemoved(peerID string) {
    b.registry.UnregisterDynamic(peerID)
}

func (b *registryBridge) makeHook(peerID string) agent.RemoteSpawnHook {
    return func(ctx context.Context, cfg agent.SubTurnConfig) (*tools.ToolResult, error) {
        // 從 cfg 提取/生成 session_id
        sessionID := extractOrNewSessionID(ctx, cfg)
        maxTurn := extractMaxTurn(cfg)

        question := cfg.SystemPrompt
        if cfg.ActualSystemPrompt != "" {
            question = cfg.ActualSystemPrompt + "\n\n" + cfg.SystemPrompt
        }

        answer, err := b.channel.AskPeer(ctx, peerID, sessionID, maxTurn, question)
        if err != nil {
            return nil, err
        }
        return &tools.ToolResult{
            Content: answer,
            // 把 session_id 回填到 metadata，下次 spawn 可以續上
            Metadata: map[string]any{
                "a2a_session_id": sessionID,
                "a2a_peer_id":    peerID,
            },
        }, nil
    }
}
```

## 4.7 注入時機 — channels Manager 與 A2AChannel 啟動順序

`pkg/channels/manager.go` 已有 `Channel.Start(ctx)` 序列。但 A2A channel 需要 `AgentRegistry` 才能裝 bridge — 這需要新增一個 inject 介面：

```go
// In pkg/channels/a2a/a2a.go
// RegistryAware 是 a2a channel 專屬的 opt-in 介面，由 Gateway 在組裝時注入。
type RegistryAware interface {
    SetAgentRegistry(*agent.AgentRegistry)
}

func (c *A2AChannel) SetAgentRegistry(r *agent.AgentRegistry) {
    c.bridge = &registryBridge{registry: r, channel: c}
    c.peers.onAdd = c.bridge.onPeerAdded
    c.peers.onRemove = c.bridge.onPeerRemoved
}
```

`pkg/gateway/` 在組裝完 `AgentRegistry` 後，scan 所有 channel：若實作 `RegistryAware` 就呼叫 `SetAgentRegistry`。這保持 channel base 介面乾淨、a2a 走 opt-in extension，跟 PicoClaw 既有「`MessageEditor` / `TypingCapable` 等 capability interface」風格一致。

## 4.8 session_id 在 SubTurn 之間的傳遞

關鍵問題：第一次 spawn 給 peer 後，第二次 spawn 怎麼知道要續上同一個 session_id？

答案：透過 `SubTurnConfig.InitialMessages` 機制。當 LLM 第二次想問同一個 peer，它的 conversation history 已含上一次的 tool result（包含 `a2a_session_id`）。但 LLM 不會自動把這 ID 塞回去。

兩種解法，採方案 A：

- **方案 A**：channel 自己以 `(parent_session_key, peer_id)` 為 key 維護「最後使用的 session_id」cache。同一 parent + 同一 peer 預設 resume，除非 LLM 明示 `new_session`。簡單。
- **方案 B**：擴 SubTurnConfig 多 `RemoteSessionID` 欄位，由 spawn tool 暴露給 LLM。明示但累贅。

`bridge.makeHook` 改成：

```go
sessionID := b.channel.sessions.GetOrCreate(parentSessionKey, peerID)
```

需要從 ctx 拉出 parent session_key（已有方式：`parentTS` 帶 sessionKey 資訊）。

## 4.9 maxTurn 的來源

LLM spawn 子 agent 時通常不會明示 max_turn。我們在 hook 預設用 `cfg.A2ASettings.MaxTurnDefault`（config 已有）。若使用者透過 spawn tool 指定，也吃進來：

```go
func extractMaxTurn(cfg agent.SubTurnConfig) int {
    // 1. 從 cfg.Tools 找 a2a 專用 tool args (若有)
    // 2. 否則用 channel.cfg.MaxTurnDefault
    // 3. clamp [1, 20]
    ...
}
```

## 下一節預告

§5 — Config schema、生命週期、安全性、測試策略、可觀測性、刻意延後不做的事項 (rollout 注意事項)。
