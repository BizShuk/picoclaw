# §3 — A2AChannel 內部元件設計

> 系列: PicoClaw A2A Channel Design
> 章節: 3 / 5
> 狀態: draft, awaiting approval
> 上一節: [02-wire-protocol.md](./02-wire-protocol.md)

## 3.1 A2AChannel struct (a2a.go)

```go
type A2AChannel struct {
    *channels.BaseChannel
    bc       *config.Channel
    cfg      *config.A2ASettings
    bus      *bus.MessageBus

    agentID  string                 // 本機 A2A 識別
    port     int                    // 實際綁定 port (0 → OS assign)

    server   *wsServer              // WS server (server.go)
    clients  *wsClientPool          // 出 dial 的 pool (client.go)
    peers    *peerTable             // peer_id → PeerInfo
    discovery *discovery            // mDNS announce + browse

    sessions *sessionMap            // external_session_id ↔ local_session_key
    turns    *turnCounter           // session_id → turn count

    bridge   *registryBridge        // 通 AgentRegistry 的橋

    ctx      context.Context
    cancel   context.CancelFunc
}
```

實作 `channels.Channel`：

- `Start(ctx)` — 依序起 server → discovery → peer reaper goroutine；安裝 bridge。
- `Stop(ctx)` — cancel context → discovery 停 → server.Shutdown → clients.CloseAll → bridge 移除所有 peer-as-subagent。
- `Send(ctx, OutboundMessage)` — 把 agent 的 outbound 包成 `reply` frame 送回對應 WS。透過 `msg.Context.Raw["a2a_frame_id"]` 找對應 ask。

## 3.2 peerTable (peer_table.go)

```go
type PeerInfo struct {
    AgentID   string
    Host      string
    Port      int
    Version   int      // protocol version from mDNS TXT
    LastSeen  time.Time
}

type peerTable struct {
    mu    sync.RWMutex
    peers map[string]*PeerInfo
    ttl   time.Duration
    onAdd    func(*PeerInfo)
    onRemove func(string)
}
```

- `Upsert(p)` — 新 peer 觸發 `onAdd` (註冊 sub-agent)；known peer 只更新 LastSeen。
- `ReapStale()` — scan map，`now - LastSeen > ttl` → delete + `onRemove`。由獨立 goroutine 每 `announce_interval` 跑一次。
- 鎖策略：讀多寫少 → `RWMutex`；`Upsert`/`Reap` 寫鎖、`Get`/`List` 讀鎖。

## 3.3 WS server (server.go)

```go
type wsServer struct {
    addr     string
    httpSrv  *http.Server
    handler  *frameHandler
}
```

- 採 `nhooyr.io/websocket` (context-aware、無 goroutine 洩漏疑慮、比 gorilla 簡單)。
- 一個 endpoint — `GET /a2a/v1/ws`，要求 subprotocol `picoclaw-a2a.v1`。
- Upgrade 成功 → spawn `serveConn(conn)`：read loop 解析 envelope → 分派到 `handleAsk` / `handleReply` / `handleBye`。
- 每條 inbound 連線維護自己的 `peerWriter` (serialized writes)，所有 outbound frame 經這個 writer 確保不會在同條 WS 上 race。

### handleAsk 流程

```text
ask 到 → 驗 turn ≤ max_turn → sessionMap.Resolve(session_id) → 拿到/建本地 session_key
                                                    ↓
   InboundMessage{
     Channel: "a2a",
     ChatID: "a2a:" + from,
     Sender: SenderInfo{
        Platform: "a2a",
        PlatformID: from,
        CanonicalID: "a2a:" + from,
     },
     SessionKey: local_session_key,
     Content: question,
     Context.Raw: {
        "a2a_frame_id": ask.frame_id,
        "a2a_peer_id": from,
        "a2a_session_id": session_id,
        "a2a_max_turn": max_turn,
        "a2a_turn": turn,
     }
   }
                                                    ↓
   bus.PublishInbound(ctx, msg)            ← Agent loop 接手，跟 Telegram 一樣處理
```

## 3.4 WS client pool (client.go)

```go
type wsClientPool struct {
    mu      sync.Mutex
    conns   map[string]*peerConn   // key: peer_id
    idleTTL time.Duration          // 60s 無流量則 close
}

type peerConn struct {
    peer     *PeerInfo
    ws       *websocket.Conn
    writer   *peerWriter
    pending  *pendingMap           // frame_id → chan *Frame (for AskPeer 配對)
    lastUsed atomic.Int64
    closed   atomic.Bool
}
```

- Lazy dial — 第一次對某 peer 送 ask 才 dial；成功後 cache。
- 單向 ask 通道 — 本 process 對某 peer 發 ask 只走 client 連線；對方對我發 ask 只走 server 連線。雙邊邏輯對稱、不會搞混。
- Idle close — 背景 goroutine 每 30s scan；過期 → close + 移除。session 仍活，下次 ask 自動重 dial。
- Reconnect-on-demand — AskPeer 偵測 conn 已 closed → 重 dial 一次再試。

## 3.5 ask_client.go — ask ↔ reply 配對

關鍵：同條 WS 上多個 ask in-flight，每個 ask 要等自己的 reply。

```go
type pendingMap struct {
    mu sync.Mutex
    m  map[string]chan *Frame   // frame_id → 等 reply 的 channel
}

func (p *pendingMap) Wait(frameID string) chan *Frame {
    ch := make(chan *Frame, 1)
    p.mu.Lock()
    p.m[frameID] = ch
    p.mu.Unlock()
    return ch
}

func (p *pendingMap) Deliver(inReplyTo string, f *Frame) bool {
    p.mu.Lock()
    ch, ok := p.m[inReplyTo]
    delete(p.m, inReplyTo)
    p.mu.Unlock()
    if ok {
        ch <- f
        return true
    }
    return false  // late reply / unknown frame_id
}
```

### AskPeer(ctx, peerID, sessionID, maxTurn, question) 流程

```text
1. turns.CheckAndIncrement(sessionID, maxTurn)         ← 超限 → ErrMaxTurnExceeded
2. conn := clients.GetOrDial(peerID)                   ← 失敗 → ErrPeerUnreachable
3. frame := buildAsk(sessionID, maxTurn, turn, question)
4. ch := conn.pending.Wait(frame.frame_id)
5. conn.writer.Send(frame)
6. select {
     case reply := <-ch:                               ← 收到對應 reply
        return reply.payload.answer, nil
     case <-ctx.Done():                                ← timeout / cancel
        conn.pending.Cancel(frame.frame_id)
        return "", ctx.Err()
   }
```

## 3.6 sessionMap (session_map.go)

```go
type sessionMap struct {
    mu        sync.RWMutex
    ext2local map[string]string   // external session_id → local session_key
    local2ext map[string]string   // 反向 (outbound: 知道 reply 掛哪個 session_id)
}

// Resolve 在 inbound 來時取得本地 session_key；不存在則 create
func (s *sessionMap) Resolve(extID, peerID string, store session.SessionStore) (string, bool) {
    s.mu.RLock()
    if k, ok := s.ext2local[extID]; ok {
        s.mu.RUnlock()
        return k, false  // existing
    }
    s.mu.RUnlock()
    scope := session.SessionScope{
        Version: 1,
        AgentID: "a2a-incoming",
        Channel: "a2a",
        Dimensions: []string{"sender"},
        Values: map[string]string{"sender": peerID},
    }
    localKey := session.BuildSessionKey(scope)
    s.mu.Lock()
    s.ext2local[extID] = localKey
    s.local2ext[localKey] = extID
    s.mu.Unlock()
    return localKey, true
}
```

- 兩個不同 peer 同時發起會各自有自己的 session_key (scope 含 sender 維度)，不會混。
- Initiator 側：AskPeer 入口生 session_id (UUID) 後也呼叫 `sessionMap.Bind(extID, localKey)`，讓對方 reply 進來時找得到。

## 下一節預告

§4 — AgentRegistry 動態註冊機制 (`RegisterDynamic` / `UnregisterDynamic`)、bridge.go 怎麼把 mDNS 上來的 peer 變成 sub-agent、SubTurn dispatcher 怎麼偵測「目標 agent 是 remote peer」並 fork 到 `AskPeer`。
