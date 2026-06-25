# Picoclaw Skills Research: Mapping to A2A Agent Card

## 1. Skill Struct Definitions

### Primary "loaded skill" struct (in-memory, used everywhere)
**File:** `/Users/bytedance/projects/m-agent/picoclaw/pkg/skills/loader.go:33-38`

```go
type SkillInfo struct {
    Name        string `json:"name"`
    Path        string `json:"path"`
    Source      string `json:"source"`
    Description string `json:"description"`
}
```

- `Name` — canonical skill name (validated, slug-like).
- `Path` — absolute path to the `SKILL.md` file on disk.
- `Source` — one of `"workspace" | "global" | "builtin"` (priority order).
- `Description` — human-readable summary.

### Frontmatter parser struct (raw metadata from disk)
**File:** `/Users/bytedance/projects/m-agent/picoclaw/pkg/skills/loader.go:28-31`

```go
type SkillMetadata struct {
    Name        string `json:"name"`
    Description string `json:"description"`
}
```

Note: this is the only `SkillMetadata` shape the loader recognizes from frontmatter, even though actual SKILL.md files contain additional fields (`homepage`, `metadata`, etc.) — they're parsed but dropped.

### Registry / catalog-level struct (different "skills" concept — for registries/hubs like ClawHub)
**File:** `/Users/bytedance/projects/m-agent/picoclaw/pkg/skills/registry.go:28-36`

```go
type SkillMeta struct {
    Slug             string
    DisplayName      string
    Summary          string
    LatestVersion    string
    IsMalwareBlocked bool
    IsSuspicious     bool
    RegistryName     string
}
```

This is for *remote* registries (installable skills), not loaded skills. Not relevant for A2A Agent Card mapping.

## 2. Skill Loading & "All Skills" API

### Loader construction
**File:** `/Users/bytedance/projects/m-agent/picoclaw/pkg/skills/loader.go:88-95`
```go
func NewSkillsLoader(workspace, globalSkills, builtinSkills string) *SkillsLoader
```
- `workspace` skills at `<workspace>/skills/`
- `globalSkills` at `~/.picoclaw/skills/`
- `builtinSkills` at `./skills/`

### List all skills (THE function for Agent Card mapping)
**File:** `/Users/bytedance/projects/m-agent/picoclaw/pkg/skills/loader.go:97-145`
```go
func (sl *SkillsLoader) ListSkills() []SkillInfo
```
- Walks all three skill roots in priority order (workspace > global > builtin), dedupes by name, returns flat list of `SkillInfo`.

### Other relevant accessors on `SkillsLoader`
- `LoadSkill(name string) (string, bool)` — loader.go:147 (returns the markdown body, not metadata)
- `BuildSkillsSummary() string` — loader.go:195 (XML-ish formatted summary of all skills)
- `SkillRoots() []string` — loader.go:67 (list of root dirs)
- `getSkillMetadata(skillPath string) *SkillMetadata` — loader.go:220 (internal; reads YAML/JSON frontmatter + H1+first-paragraph fallback)

### ContextBuilder access points (agent-level)
**File:** `/Users/bytedance/projects/m-agent/picoclaw/pkg/agent/context.go:1291-1331`
```go
func (cb *ContextBuilder) ListSkillNames() []string
func (cb *ContextBuilder) GetSkillsInfo() map[string]any
func (cb *ContextBuilder) ResolveSkillName(name string) (string, bool)
```

## 3. Skill Source Format (on disk)

Skills are directories named `<skill-name>/` containing a `SKILL.md` file. The frontmatter may be YAML or JSON; the body is markdown.

**Concrete example** — `/Users/bytedance/projects/m-agent/picoclaw/workspace/skills/github/SKILL.md`:
```markdown
---
name: github
description: "Interact with GitHub using the `gh` CLI. Use `gh issue`, `gh pr`, `gh run`, and `gh api` for issues, PRs, CI runs, and advanced queries."
metadata: {"nanobot":{"emoji":"🐙","requires":{"bins":["gh"]},...}}
---

# GitHub Skill
...
```

Other examples on disk: `summarize/`, `weather/`, `tmux/`, `hardware/`, `skill-creator/`, `agent-browser/`, `picoclaw-agent/`.

**Loader extraction logic** (`loader.go:220-271`):
1. Reads `SKILL.md`.
2. Splits frontmatter (`---` delimited) from body.
3. If frontmatter is JSON → unmarshals `name`/`description`.
4. If frontmatter is YAML → uses `parseSimpleYAML` which only extracts `name` and `description` (loader.go:328-346).
5. If no frontmatter, falls back to first H1 + first paragraph in body.

**Result:** Today the loader only surfaces `Name` + `Description` (and `Path`/`Source` for plumbing). All other frontmatter fields (`homepage`, `metadata`, etc.) are dropped on load.

## 4. Agent → Skills Wiring

Skills are **per-agent**, not global. Each `AgentInstance` has a `SkillsFilter` (allowlist of skill names it may use).

### Agent config layer
**File:** `/Users/bytedance/projects/m-agent/picoclaw/pkg/config/config.go:313-321`
```go
type AgentConfig struct {
    ID        string
    Default   bool
    Name      string
    Workspace string
    Model     *AgentModelConfig
    Skills    []string   // <-- per-agent allowlist
    Subagents *SubagentsConfig
}
```

### AgentInstance field
**File:** `/Users/bytedance/projects/m-agent/picoclaw/pkg/agent/instance.go:23-62`
- `SkillsFilter []string` (line 43)

### Resolver: precedence is frontmatter > config
**File:** `/Users/bytedance/projects/m-agent/picoclaw/pkg/agent/instance.go:402-413`
```go
func resolveAgentSkillsFilter(agentCfg, definition) []string {
    // 1) AGENT.md frontmatter "skills:" array wins
    if definition.Agent != nil && definition.Agent.Frontmatter.Skills != nil {
        return append([]string(nil), definition.Agent.Frontmatter.Skills...)
    }
    // 2) else config.json agents.list[].skills
    if agentCfg == nil || agentCfg.Skills == nil { return nil }
    return append([]string(nil), agentCfg.Skills...)
}
```
`AgentFrontmatter.Skills []string` defined in `pkg/agent/definition.go:36`.

### Active skills for a given turn
**File:** `/Users/bytedance/projects/m-agent/picoclaw/pkg/agent/agent_utils.go:481-520`
```go
func activeSkillNames(agent *AgentInstance, opts processOptions) []string
```
- Combines `agent.SkillsFilter` + `opts.ForcedSkills` (per-message overrides).
- Resolves each name through `ContextBuilder.ResolveSkillName` (case-insensitive match against `ListSkills()`).
- Filters by turn profile if `TurnProfile.AllowedSkills` is set.

### Where to read agent skills at runtime for Agent Card
1. `agentInstance.SkillsFilter` → list of allowed names.
2. For each name, resolve to `SkillInfo` via `agentInstance.ContextBuilder.skillsLoader.ListSkills()` (filter to names in `SkillsFilter`).
3. For per-turn scoping, `turnState.activeSkills` (in `pkg/agent/turn_state.go:193, 265`) holds the currently-active skill names.

## 5. Existing Agent-Card / Discovery Concepts

### What exists today (none of this is A2A-spec Agent Card)
- **mDNS discovery** — `/Users/bytedance/projects/m-agent/picoclaw/pkg/channels/a2a/discovery.go:14-87`
  - TXT records only carry: `v=1`, `path=/a2a/v1/ws`, `agent=<id>`, `desc=<200-char description>`.
  - `desc` is sourced from `cfg.Description` (A2ASettings field), not from skill metadata.
- **`A2ASettings.Description`** — `/Users/bytedance/projects/m-agent/picoclaw/pkg/config/config_a2a.go:9` — human-readable agent description (capped at 200 chars in the TXT record).
- **`PeerInfo.Description`** — `/Users/bytedance/projects/m-agent/picoclaw/pkg/channels/a2a/peer_table.go:15` — description of a remote peer, populated from the same TXT field.
- **No `agent.json`, no `well-known`, no `AgentCard` struct, no `capabilities` field on the A2A channel.**
- `voice_capabilities.go` exists but is for ASR/TTS, unrelated to skill capability export.

### Grep results
- `agent\s*card|agentcard|agent\.json|well-known` — no matches in the repo.
- `capabilities` in Go code — only appears in: `mcp/manager.go` (MCP server capabilities), `tools/delegate.go` (LLM prompt text), `voice_capabilities.go` (audio), `devices/events/events.go` (device metadata), `pkg/agent/context.go:284` (LLM prompt fragment). None of these are Agent-Card-shaped.
- A2A package has zero references to `Skill` or `skill` (verified).

**Conclusion:** No pre-existing notion of a Google A2A Agent Card. The current A2A "advertisement" surface is mDNS TXT records with a single description string, plus a custom WebSocket protocol (`picoclaw-a2a.v1`). To produce an A2A-compliant Agent Card with a `skills` array, this needs to be built from scratch.

## 6. Feasibility: A2A Agent Card Skills Mapping

The A2A Agent Card `skills` array expects each entry to have: `id`, `name`, `description`, optional `tags` (string[]), optional `examples` (string[]), and the card itself has `defaultInputModes` / `defaultOutputModes`.

### Mapping from current picoclaw `SkillInfo`
| A2A field | Source | Status |
|---|---|---|
| `id` | `SkillInfo.Name` | Ready (validated slug) |
| `name` | `SkillInfo.Name` (or display name from frontmatter) | Ready; could use `name` from frontmatter if richer |
| `description` | `SkillInfo.Description` | Ready |
| `tags` | none today | **Missing** — would need to parse frontmatter `tags:` (currently dropped by `parseSimpleYAML`) |
| `examples` | none | **Missing** — could derive from "When to use (trigger phrases)" sections in the markdown body, or from frontmatter `examples:` field |
| Card-level `defaultInputModes`/`defaultOutputModes` | per-agent config | Not present — would need a new config field |
| Card-level `capabilities.streaming` etc. | derived | Not present — would need to derive from A2A channel protocol support |

### Per-agent scoping works cleanly
The `AgentInstance.SkillsFilter` already gives the set of skills for a given agent; `ContextBuilder.skillsLoader.ListSkills()` (filtered by `SkillsFilter`) gives the full `SkillInfo` for each.

### What needs to be added for a complete A2A Agent Card
1. Extend `SkillInfo` (or parse richer struct) to capture `tags`, `examples`, `inputModes`, `outputModes` from frontmatter — or add a new `SkillDescriptor` type for the Agent Card projection.
2. Extend `parseSimpleYAML` (loader.go:328) to read more frontmatter fields, or do a richer unmarshal in `getSkillMetadata`.
3. Add a new HTTP endpoint (e.g. `/.well-known/agent.json` or `/a2a/v1/agent-card`) to `pkg/channels/a2a/server.go` (the existing `wsServer` already has an HTTP mux at server.go:49-55; adding a route there is the natural place).
4. Build the Agent Card at runtime by reading `AgentRegistry` → agent → `SkillsFilter` → `ContextBuilder.ListSkills()` filtered by `SkillsFilter`, projecting into A2A schema.
5. Decide how to populate `tags`/`examples` (likely new frontmatter convention or derive from markdown body — many existing SKILL.md files already have a "When to use (trigger phrases)" section that maps cleanly to `examples`).

## Key file:line summary
- Skill struct: `pkg/skills/loader.go:33-38` (SkillInfo), `pkg/skills/loader.go:28-31` (SkillMetadata)
- List all skills: `pkg/skills/loader.go:97` (`SkillsLoader.ListSkills`)
- Per-agent skills filter: `pkg/agent/instance.go:43` (AgentInstance.SkillsFilter), `pkg/agent/instance.go:402-413` (resolveAgentSkillsFilter)
- Per-agent config: `pkg/config/config.go:313-321` (AgentConfig.Skills)
- Frontmatter spec: `pkg/agent/definition.go:30-39` (AgentFrontmatter) — note only `skills` is the array form
- Per-turn active skills: `pkg/agent/turn_state.go:193,265`, `pkg/agent/agent_utils.go:481-520`
- A2A channel: `pkg/channels/a2a/a2a.go`, `pkg/channels/a2a/server.go:43-87` (HTTP mux for adding Agent Card endpoint)
- A2A settings: `pkg/config/config_a2a.go:5-18`
- A2A protocol envelopes: `pkg/channels/a2a/protocol.go` (currently ASK/REPLY/ERROR/PING/PONG/BYE only — no agent-card frame type)
