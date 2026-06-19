# A2A 雙 Agent 測試 — Step by Step

在同一台機器啟兩個 picoclaw agent(`alice`、`bob`),驗證:

- Test 1:兩者能以 mDNS 互相發現。
- Test 2:對 `alice` 發一個請求,它透過 `spawn` 叫 `bob` 處理並把答案傳回。

harness 用「同一主機兩個原生 process」(非 Docker)—— mDNS 多播在本機 loopback 可靠,
也避開 macOS Docker Desktop 觸不到 LAN 的限制。

## Docker 版

雙 agent 測試可用 `docker-compose.agent.yml` 跑(每個 agent 是 host 端 `agents/<name>/` 資料夾,各自 bind-mount 進容器)。詳見 `docs/docker-a2a-agent.md` 的「範例:跑 A2A 雙 agent 測試」與一鍵腳本 `scripts/a2a-test/verify-docker.sh`。

## 前置 (Prerequisites)

| 需求                    | 說明                                                   |
| ----------------------- | ------------------------------------------------------ |
| Go 1.25+、python3、curl | 建置與發請求                                           |
| MiniMax 國際站金鑰      | `api.minimax.io`(Test 2 需要;Test 1 不打 LLM,可用假值) |
| 平台                    | macOS 或 Linux 本機                                    |

```bash
export MINIMAX_API_KEY=sk-...        # 國際站 (api.minimax.io) 金鑰
export WORKDIR=/tmp/pico-a2a-test    # 測試工作目錄
export BIN=$WORKDIR/picoclaw
```

---

## Step 1 — 建置 gateway binary

從 repo 根目錄:

```bash
mkdir -p "$WORKDIR"
go build -tags goolm,stdjson -o "$BIN" ./cmd/picoclaw
```

> `goolm` tag 用純 Go 的 olm,免裝 C 的 `libolm` 標頭。

## Step 2 — 產生 alice / bob 設定

```bash
python3 scripts/a2a-test/gen-configs.py "$WORKDIR"
```

它在 `$WORKDIR/{alice,bob}/config.json` 各寫一份(都啟 a2a channel、用 `minimax-i18n`
國際站、注入你的金鑰):

```txt
alice  agent_id=alice  a2a:18791  gw:18790   spawn+subagent 工具、allow_agents=['*']
bob    agent_id=bob    a2a:28791  gw:28790   只回答
```

---

## Step 3 — Test 1:互相 mDNS 發現

```bash
scripts/a2a-test/run-test1.sh
```

它會啟兩個 agent、等 30 秒、檢查雙向是否互登錄,最後關閉。預期結尾:

```txt
alice sees bob : 1    bob sees alice : 1
TEST1: PASS (mutual mDNS discovery)
```

判定訊號是兩邊 log 各出現一行 `Registered remote agent`(對方的 agent_id、source=`a2a-mdns`)。

> 失敗排查:確認 `service_type` 一致、agent_id 不同(同名會被 self-filter 濾掉)、
> 防火牆未擋 UDP 5353。

## Step 4 — Test 2:alice 請 bob 回覆

```bash
scripts/a2a-test/run-test2.sh
```

它啟兩個 agent、等探索、對 `alice` 的 `POST /a2a/v1/ask` 發一個請求,要 alice 用
`spawn(agent_id="bob")` 叫 bob 回傳 token `ECHO-7Q2`,再驗證來回。預期:

```txt
HTTP RESPONSE: {"answer":"ECHO-7Q2","session":"http-..."}
--- data flow ---
Tool call: spawn({"agent_id":"bob","task":"reply with the single token ECHO-7Q2..."})
Dialing peer WS  url=ws://<LAN-IP>:28791/a2a/v1/ws
Processing message from a2a:alice ... Task: reply with the single token ECHO-7Q2
Response: ECHO-7Q2
TEST2: PASS (A asked B, B processed, data round-tripped)
```

判定:HTTP 回應含 `ECHO-7Q2`,且 `bob.log` 顯示它確實收到 `a2a:alice` 的請求並處理。

---

## 手動版(不想用腳本)

各步驟對應的裸指令:

```bash
# 啟兩個 agent(分別到兩個終端,或加 & 背景跑)
PICOCLAW_HOME=$WORKDIR/bob   PICOCLAW_CONFIG=$WORKDIR/bob/config.json   $BIN gateway -d
PICOCLAW_HOME=$WORKDIR/alice PICOCLAW_CONFIG=$WORKDIR/alice/config.json $BIN gateway -d

# Test 1:看互相發現
grep "Registered remote agent" $WORKDIR/alice.log   # 期望出現 agent_id=bob
grep "Registered remote agent" $WORKDIR/bob.log     # 期望出現 agent_id=alice

# Test 2:對 alice 發請求(它會叫 bob)
curl -sS -X POST http://127.0.0.1:18791/a2a/v1/ask \
  -H 'Content-Type: application/json' \
  -d '{"text":"Use the bob agent to reply with exactly the token ECHO-7Q2, then return its reply verbatim."}'
```

## 收尾 (Cleanup)

```bash
pkill -f "$BIN gateway" 2>/dev/null
rm -rf "$WORKDIR"        # 含注入金鑰的 config 與 log,測完請清掉
```

## 端口 (Ports)

| Agent | a2a (WS + `/a2a/v1/ask`) | gateway |
| ----- | ------------------------ | ------- |
| alice | 18791                    | 18790   |
| bob   | 28791                    | 28790   |

## 注意

- Test 2 用真實 MiniMax 國際站金鑰,有 API 費用。
- `$WORKDIR/{alice,bob}/config.json` 內含明文金鑰 —— 不要進版控,測完 `rm -rf`。
- 跨「不同機器」的真實 LAN 驗證:在 Linux 上跑、各機一個 agent,mDNS 自然互通(本指南是單機 harness)。
