# Backlog

## 方案 B:狀態獨立的 app-workspace agent (clean isolation)

來源:`docs/design/2026-06-17-docker-app-workspace-agent-design.md` (附錄方案 B)。

現況 (方案 A) 把 app folder 直接 mount 成 workspace,picoclaw 會在使用者 repo 內寫出
`sessions/` (`pkg/agent/instance.go:129`)。方案 B 消除此污染:

- picoclaw 狀態 (含 `sessions/`) 放獨立 volume,例如 workspace=`/var/lib/picoclaw/workspace`。
- 使用者 app 改 mount 在 `/app`,透過 `tools.allow_read_paths` + `tools.allow_write_paths`
  (config `pkg/config/config.go:1021-1022`,env `PICOCLAW_TOOLS_ALLOW_READ_PATHS` /
  `_WRITE_PATHS`) 授權 fs 工具與 shell 存取 `/app`。
- shell cwd 可經 `allowedPathPatterns` 放行 `/app` (`pkg/tools/shell.go:358`)。
- persona 改用放在 `/app` 的 `AGENT.md`;image 內固定,可跨不同 app 重用。

優點:app 資料夾零污染、persona 不必動使用者 repo。
代價:agent 的「base」是設定面的 (allow paths) 而非 workspace 根,設定略多。

## Agent 互呼授權 (Agent-Spawn Approval)

來源:2026-06-19,A2A 雙 agent 驗證後的決策。

現況:spawn/subagent/delegate 的權限政策已改為「預設允許,只有 deny 需明列」
(`agentAllowsSubagent` in `pkg/agent/registry.go`;config `subagents.allow_agents` /
`subagents.deny_agents`)。預設允許讓 A2A mesh 好用,但少了即時把關。

待實作:human-in-the-loop 的 agent 互呼「核准」機制 —— 當一個 agent 要 spawn / ask
另一個 agent (尤其 A2A 遠端 peer) 時,先發出 approval 請求,經人工 (或策略) 核准後才執行。

- 進入點:`spawnSubTurn` / `spawnRemoteSubTurn` (`pkg/agent/subturn.go:313,323`)。
- 可重用既有 hook/steering 或 runtime event 機制送出「待核准」訊號,核准前阻塞或拒絕。
- deny-list 與 approval 互補:deny 是靜態硬封鎖,approval 是動態逐次把關。
