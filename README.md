# hebb

[![CI](https://github.com/cizer/hebb/actions/workflows/ci.yml/badge.svg)](https://github.com/cizer/hebb/actions/workflows/ci.yml)

A portable engine for markdown knowledge vaults: index, search, scaffold, maintain.

`hebb` is the function layer for a "second brain" vault. The vault (markdown, attachments, memory) is the data; `hebb` is the tool that operates on it. It is **multi-vault**, like git is multi-repo: run `hebb` inside a vault directory, or pass `--vault`.

Go rewrite of the Node reference `onevault-mcp`. Design in [ARCHITECTURE.md](ARCHITECTURE.md); build status and roadmap in [PLAN.md](PLAN.md).

## Status

**Phase 3 complete; plugin adopted** — `hebb new` scaffolds a fresh vault from the bundled template and installs it; `hebb install` and `hebb doctor` work end-to-end (config, launchd jobs, memory symlink, first index). The agent-facing layer (the MCP server and the `vault-ingest` skill) ships as the hebb Claude Code plugin in [`plugin/`](plugin/), so install is now purely data-side; `--mcp-json` writes a per-vault `.mcp.json` + settings for plugin-less use. Builds on Phase 1 parity with `onevault-mcp` (index, search, MCP server, web UI, file watcher). The daily-digest and action-review automation scripts are now migrated into [`automation/`](automation/), embedded in the binary and rendered as launchd jobs by `hebb install`. Not yet wired into a live machine; `onevault-mcp` keeps serving the live vault until the Phase 5 cutover.

## Install

Once the repo is public, the one-liner:

```sh
curl -fsSL https://raw.githubusercontent.com/cizer/hebb/main/install.sh | sh
```

While the repo is private, the same script via your `gh` auth (it fetches the
binary with `gh` too):

```sh
gh api repos/cizer/hebb/contents/install.sh -H "Accept: application/vnd.github.raw" | sh
```

Or with Go: `go install github.com/cizer/hebb/cmd/hebb@latest` (set
`GOPRIVATE=github.com/cizer/*` while private). The installer drops `hebb` in
`~/.local/bin` (override `HEBB_INSTALL_DIR`); pin a version with
`HEBB_VERSION=vX.Y.Z`. Then `hebb new <path>` or `hebb install` in an existing
vault.

## Commands

Working today:
- `hebb index` — build or refresh the FTS5 index
- `hebb search <query>` — full-text search (`--tag`, `--path-prefix`, `--limit`)
- `hebb mcp` — MCP server over stdio for Claude (`search_vault`, `expand_context`, `get_context_for_topic`, `vault_stats`, `reindex_vault`)
- `hebb serve` — local web search UI on 127.0.0.1 (`--port`, or `$HEBB_WEB_PORT`)
- `hebb install` — wire a vault into the machine, idempotently: writes `.hebb/config.toml`, symlinks memory, builds the first index. On a terminal it then offers an interactive picker to wire your agents (Codex, Claude Desktop, or a plugin-less per-vault `.mcp.json`); pick non-interactively with `--codex` / `--claude-desktop` / `--mcp-json`, or `--no-interaction` to skip. The Claude Code plugin is a separate one-time install (see below). Standalone (automation scripts are embedded and materialised to `~/.local/share/hebb` for launchd); `--asset-root` uses a repo checkout's `automation/`, `--launchd` renders launchd jobs (`--load` bootstraps them).
- `hebb doctor` — read-only health check of a vault and its install; exits non-zero if anything is broken.
- `hebb new <path>` — scaffold a fresh vault from the bundled template (PARA skeleton, baseline `CLAUDE.md` + `AGENTS.md`, a note template, a memory seed) and install it. Refuses to scaffold into a non-empty directory.
- `hebb codex` — register this vault with the Codex CLI by merging an `[mcp_servers.hebb]` block into `~/.codex/config.toml` (idempotent, non-destructive; `--mcp-name` for a second vault). The Codex counterpart to the Claude plugin.
- `hebb reset` — the inverse of `install`: un-wire a vault from the machine (memory symlink, launchd jobs, Codex block, opt-in `.mcp.json`, and the regenerable index). Dry run by default; `--force` to apply. Never removes vault content (notes, `.hebb/memory`, `.hebb/config.toml`).

Vault selection: `--vault <path>`, `$HEBB_VAULT`, or the nearest `.hebb/` above the working directory.

Planned (stub for now): `hebb sync`.

## Build

```sh
go build ./...
go test ./...
go run ./cmd/hebb --help
```

`hebb --version` on a dev build shows the git revision (e.g. `0.0.0-dev
(abc123def456-dirty)`) from Go's embedded VCS info, so builds are
distinguishable; releases stamp a clean version via
`-ldflags "-X main.version=vX.Y.Z"`.

Test strategy and the (planned) two-stage CD pipeline are in [TESTING.md](TESTING.md).

## Layout

- `core/` — UI-agnostic engine (index, search, context, watcher)
- `cli/` — CLI over core
- `mcp/` — MCP server surface
- `web/` — web search UI (page embedded via go:embed)
- `cmd/hebb/` — entrypoint
- `plugin/` — the hebb Claude Code plugin (manifest, `.mcp.json`, `vault-ingest` skill)
- `automation/ launchd/ vault-template/` — embedded in the binary; used by `hebb install`/`hebb new`
