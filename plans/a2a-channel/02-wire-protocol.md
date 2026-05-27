# §2 — Wire Protocol & maxTurn 計數規則

> 系列: PicoClaw A2A Channel Design
> 章節: 2 / 5
> 狀態: draft, awaiting approval
> 上一節: [01-architecture.md](./01-architecture.md)

## 設計取向

- JSON over WebSocket text frames — 跟 Hermes 一致，易 debug；二進位最佳化先不做 (YAGNI)。
- Envelope + payload — 所有 frame 共用同一個 envelope，`type` 區分語意。
- `frame_id` (UUID) — 每個 ask 都有 frame_id，reply 用 `in_reply_to` 配對。讓 `AskPeer` 可在同一條 WS 上同時 in-flight 多個 ask。
- 版本協商 via `Sec-WebSocket-Protocol` — handshake 階段就拒絕版本不相容 peer，不留 wire ambiguity。

## Envelope (每個 frame 都有)

```json
{
  "v": 1,
  "type": "ask | reply | error | ping | pong | bye",
  "frame_id": "uuid-v4",
  "in_reply_to": "uuid-v4 | null",
  "session_id": "uuid-v4",
  "from": "alice",
  "to": "bob",
  "ts": 1234567890,
  "payload": { ... }
}
```

## Frame 種類

| type            | 由誰送    | payload                                              | 用途                                             |
| --------------- | --------- | ---------------------------------------------------- | ------------------------------------------------ |
| `ask`           | initiator | `{ question: string, max_turn: int, turn: int }`     | 提問或接續對話                                   |
| `reply`         | responder | `{ answer: string, turn: int, done: bool }`          | 回覆 (`done=true` 代表 responder 認為對話可結束) |
| `error`         | 任一方    | `{ code: string, message: string, retryable: bool }` | 顯式失敗 (TTL、maxTurn、unknown session)         |
| `ping` / `pong` | 任一方    | `{}`                                                 | keepalive (每 20s 一次，60s 無 pong 則斷線)      |
| `bye`           | 任一方    | `{ reason: string }`                                 | 主動關閉 session                                 |

## maxTurn 規則 (對稱雙邊計算)

一次 turn = 一次 `ask` + 對應 `reply` 的配對。

```text
turn:  1         2          3
       A→B       B→A reply  A→B follow-up
       ask       reply      ask
```

- `ask.turn` 由 initiator 設定為這次提問的 turn 編號 (從 1 開始)。
- `ask.max_turn` 是這個 session 上限。
- 雙邊各自維護 `session_id → turnCounter` 的 in-memory 表 (在 `turn_counter.go`)。
- Initiator 送 `ask` 前先檢查：若 `localCounter[session]+1 > max_turn` → 不送，回 SubTurn 一個 `MaxTurnExceeded`。
- Responder 收到 `ask` 先檢查：
    - `ask.turn > localCounter[session]+1` → 回 `error{code:"out_of_order"}`
    - `ask.turn > ask.max_turn` → 回 `error{code:"max_turn_exceeded"}`
- Counter 在「成功送出 ask」與「成功收到/送出 reply」時 atomic incremental。
- `done=true` 的 reply 讓 responder 清掉 counter，session 進入 ended 狀態；之後同 session_id 的 ask 一律 `error{code:"session_ended"}`。

## 版本協商

- WS handshake 時 client 送 `Sec-WebSocket-Protocol: picoclaw-a2a.v1`
- Server 只 accept 自己支援的版本；不 match → 503
- mDNS TXT 也帶 `v=1`，client dial 前可預先過濾

## Wire 範例 — 一個完整 3-turn session

```text
A → B  ask    {frame_id:f1, session_id:s1, payload:{question:"What's the CPU temp?", max_turn:3, turn:1}}
B → A  reply  {frame_id:f2, in_reply_to:f1, payload:{answer:"72°C", turn:1, done:false}}

A → B  ask    {frame_id:f3, session_id:s1, payload:{question:"Is that too hot?", max_turn:3, turn:2}}
B → A  reply  {frame_id:f4, in_reply_to:f3, payload:{answer:"Safe.", turn:2, done:false}}

A → B  ask    {frame_id:f5, session_id:s1, payload:{question:"Trend last 5min?", max_turn:3, turn:3}}
B → A  reply  {frame_id:f6, in_reply_to:f5, payload:{answer:"Climbing 2°C", turn:3, done:true}}

A → B  ask    {frame_id:f7, session_id:s1, payload:{turn:4, max_turn:3}}
B → A  error  {frame_id:f8, in_reply_to:f7, payload:{code:"max_turn_exceeded"}}
```

## 失敗模式表

| 觸發點                         | 偵測方    | 動作                                                      |
| ------------------------------ | --------- | --------------------------------------------------------- |
| max_turn 超限                  | initiator | 不送 ask，AskPeer 回 `ErrMaxTurnExceeded`                 |
| max_turn 超限 (responder 嚴格) | responder | 回 `error{code:"max_turn_exceeded"}`                      |
| 未知 session_id                | responder | 回 `error{code:"unknown_session"}`                        |
| WS 斷線中                      | initiator | 嘗試一次重連；失敗則 AskPeer 回 `ErrPeerUnreachable`      |
| 60s 無 pong                    | 任一方    | 主動 close → bye；session 不結束，下次 ask 自動重連       |
| reply 帶 done=true             | 雙方      | 清掉本地 counter；session_id 進入 ended set (TTL 5m 後清) |

## 下一節預告

§3 — A2AChannel 內部元件詳細設計 (peer table TTL、WS server handler、WS client pool、ask_client 的 ask↔reply 配對機制)。
