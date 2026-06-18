# My Vault

A Markdown knowledge vault, scaffolded by `hebb new`. See `CLAUDE.md` for the
conventions and structure.

## Quick start

- Add notes under `1-Projects/`, `2-Areas/`, `3-Resources/` or `4-Archives/`
  (see `CLAUDE.md` for what each holds).
- Start a new note from `templates/note.md`.
- Search: `hebb search "<query>"`, or `hebb serve` for a local web UI on
  127.0.0.1.
- Open this directory in Claude to use the bundled MCP tools.

This vault is self-contained: `.hebb/config.toml` and `.mcp.json` identify and
wire it, and the search index (`.hebb/index.db`) is rebuilt on demand.

## Setup on a new machine

This vault is self-installing. On a fresh clone or an ephemeral machine, run:

```sh
./bootstrap.sh
```

It installs the `hebb` binary (from GitHub, if not already on your PATH) and
wires this vault (`hebb install`). It is idempotent, so it is safe to re-run.
Afterwards, re-auth any connectors (the one manual step).

### Claude Code on the web

This vault provisions itself automatically in an ephemeral Claude Code on the
web session. A committed `SessionStart` hook (`.claude/hooks/session-start.sh`,
registered in `.claude/settings.json`) runs `./bootstrap.sh` when the container
starts, before the session and its MCP servers load, so `hebb` and its tools are
ready when the agent begins. The hook is a no-op on a local machine (it only
runs when `CLAUDE_CODE_REMOTE=true`), so committing it is safe everywhere.
