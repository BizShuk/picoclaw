#!/usr/bin/env python3
"""Seed agents/alice and agents/bob with API-key-injected configs.

Each folder is a self-contained PicoClaw agent directory and its own git
repository. Run from the repo root:

    MINIMAX_API_KEY=sk-... python3 scripts/a2a-test/gen-configs.py

What it does:
  1. Ensures agents/alice and agents/bob exist (mkdir -p workspace, sessions, logs).
  2. Ensures each has a git repo (git init if missing). Skips silently if it
     already has one or the user has staged uncommitted work.
  3. Writes config.json with agent_id / ports set per agent.
  4. Injects MINIMAX_API_KEY into the model_list entry. If the env var is
     missing, the placeholder MINIMAX-REPLACE-ME is left in place and a
     warning is printed.
  5. Creates .env (from .env.example) and .gitignore if missing.

This script is idempotent: re-running it with the same key updates config.json
in place. Existing git history is preserved.
"""
import json
import os
import shutil
import subprocess
import sys
from pathlib import Path

KEY = os.environ.get("MINIMAX_API_KEY", "")
API_BASE = "https://api.minimax.io/v1"
REPO_ROOT = Path(__file__).resolve().parent.parent.parent
AGENTS_DIR = REPO_ROOT / "agents"


def warn(msg):
    print(f"[warn] {msg}", file=sys.stderr)


def ensure_agent(name, a2a_port, gw_port, can_spawn):
    agent_dir = AGENTS_DIR / name
    agent_dir.mkdir(parents=True, exist_ok=True)
    for sub in ("workspace", "sessions", "logs"):
        (agent_dir / sub).mkdir(exist_ok=True)

    # .gitignore
    gi = agent_dir / ".gitignore"
    if not gi.exists():
        gi.write_text(".env\nconfig.active.json\nsessions/\nlogs/\nworkspace/\n")

    # .env.example
    env_example = agent_dir / ".env.example"
    if not env_example.exists():
        env_example.write_text("MINIMAX_API_KEY=sk-replace-me\n")

    # .env (skeleton copy if missing; user fills the key in)
    env_file = agent_dir / ".env"
    if not env_file.exists():
        shutil.copy(env_example, env_file)
        env_file.chmod(0o600)

    # git init (skip if .git already present or git unavailable)
    if not (agent_dir / ".git").exists():
        try:
            subprocess.run(
                ["git", "init", "-q"], cwd=agent_dir, check=True,
                env={**os.environ, "GIT_AUTHOR_NAME": name, "GIT_AUTHOR_EMAIL": f"{name}@local",
                     "GIT_COMMITTER_NAME": name, "GIT_COMMITTER_EMAIL": f"{name}@local"},
            )
            print(f"[{name}] git init done")
        except (FileNotFoundError, subprocess.CalledProcessError) as e:
            warn(f"{name}: git init skipped ({e})")

    # config.json
    cfg = {
        "version": 3,
        "agents": {"defaults": {
            "workspace": "/root/.picoclaw/workspace",
            "restrict_to_workspace": True,
            "model_name": "MiniMax-M2.5-i18n",
            "max_tokens": 4096 if not can_spawn else 8192,
            "context_window": 131072,
            "max_tool_iterations": 12 if not can_spawn else 40,
            "summarize_message_threshold": 20,
            "summarize_token_percent": 75,
        }},
        "model_list": [{
            "model_name": "MiniMax-M2.5-i18n",
            "provider": "minimax-i18n",
            "model": "MiniMax-M2.5",
            "api_base": API_BASE,
            "api_keys": [KEY if KEY else "MINIMAX-REPLACE-ME"],
            "extra_body": {"reasoning_split": True},
        }],
        "channel_list": {"a2a": {
            "enabled": True, "type": "a2a", "allow_from": [],
            "settings": {
                "agent_id": name, "port": a2a_port,
                "description": f"PicoClaw {name} (agents/{name})",
                "announce_interval": 5000000000,
                "ask_timeout": 120000000000,
                "max_turn_default": 6,
            },
        }},
        "gateway": {"host": "0.0.0.0", "port": gw_port, "hot_reload": False, "log_level": "debug"},
    }
    if can_spawn:
        cfg["agents"]["list"] = [{"id": "main", "default": True, "subagents": {"allow_agents": ["*"]}}]
        cfg["tools"] = {
            "exec": {"enabled": True, "enable_deny_patterns": True},
            "read_file": {"enabled": True, "mode": "bytes"},
            "write_file": {"enabled": True},
            "edit_file": {"enabled": True},
            "append_file": {"enabled": True},
            "list_dir": {"enabled": True},
            "message": {"enabled": True},
            "spawn": {"enabled": True},
            "subagent": {"enabled": True},
            "skills": {"enabled": False},
            "find_skills": {"enabled": False},
            "install_skill": {"enabled": False},
            "mcp": {"enabled": False},
            "web": {"enabled": False},
        }
        cfg["channel_list"]["pico"] = {
            "enabled": True, "type": "pico", "allow_from": [],
            "settings": {
                "token": "local-dev", "allow_token_query": True,
                "allow_origins": ["*"], "ping_interval": 30,
                "read_timeout": 60, "max_connections": 100,
            },
        }
    else:
        cfg["tools"] = {"message": {"enabled": True}}

    cfg_path = agent_dir / "config.json"
    cfg_path.write_text(json.dumps(cfg, indent=2) + "\n")

    # README
    readme = agent_dir / "README.md"
    if not readme.exists():
        readme.write_text(
            f"# {name} — PicoClaw agent\n\n"
            f"See `../../agents/README.md` for the full convention.\n\n"
            f"- a2a port: {a2a_port}\n- gateway port: {gw_port}\n- can_spawn: {can_spawn}\n"
        )

    print(f"[{name}] {cfg_path}  a2a={a2a_port} gw={gw_port}  "
          f"{'key-injected' if KEY else 'placeholder-key (set MINIMAX_API_KEY)'}")


def main():
    if not KEY:
        warn("MINIMAX_API_KEY not set; config.json will keep MINIMAX-REPLACE-ME placeholder.")
    ensure_agent("alice", 18791, 18790, can_spawn=True)
    ensure_agent("bob",   28791, 28790, can_spawn=False)


if __name__ == "__main__":
    main()
