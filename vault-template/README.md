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
