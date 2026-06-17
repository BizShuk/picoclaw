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
