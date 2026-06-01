# hebb

[![CI](https://github.com/cizer/hebb/actions/workflows/ci.yml/badge.svg)](https://github.com/cizer/hebb/actions/workflows/ci.yml)

A portable engine for markdown knowledge vaults: index, search, scaffold, maintain.

`hebb` is the function layer for a "second brain" vault. The vault (markdown, attachments, memory) is the data; `hebb` is the tool that operates on it. It is **multi-vault**, like git is multi-repo: run `hebb` inside a vault directory, or pass `--vault`.

Go rewrite of the Node reference `onevault-mcp`. Design in [ARCHITECTURE.md](ARCHITECTURE.md); build status and roadmap in [PLAN.md](PLAN.md).

## Status

**Phase 3 complete** — `hebb new` scaffolds a fresh vault from the bundled template and installs it; `hebb install` and `hebb doctor` work end-to-end (config, `.mcp.json`, project settings, skills symlinks, launchd jobs, memory symlink, first index). Builds on Phase 1 parity with `onevault-mcp` (index, search, MCP server, web UI, file watcher). Not yet wired into a live machine; the skill/automation content still has to be migrated into the repo, and `onevault-mcp` keeps serving the live vault until the Phase 5 cutover.

## Commands

Working today:
- `hebb index` — build or refresh the FTS5 index
- `hebb search <query>` — full-text search (`--tag`, `--path-prefix`, `--limit`)
- `hebb mcp` — MCP server over stdio for Claude (`search_vault`, `expand_context`, `get_context_for_topic`, `vault_stats`, `reindex_vault`)
- `hebb serve` — local web search UI on 127.0.0.1 (`--port`, or `$HEBB_WEB_PORT`)
- `hebb install` — wire a vault into the machine, idempotently: writes `.hebb/config.toml` and the project-scoped `.mcp.json`, merges project settings, materialises the bundled skills to `~/.local/share/hebb` and links them into the vault's `.claude/skills` (project-scoped), symlinks memory, builds the first index. Standalone (assets are embedded in the binary); `--asset-root` links skills from a repo checkout instead, `--launchd` renders launchd jobs (`--load` bootstraps them).
- `hebb doctor` — read-only health check of a vault and its install; exits non-zero if anything is broken.
- `hebb new <path>` — scaffold a fresh vault from the bundled template (PARA skeleton, baseline `CLAUDE.md`, a note template, a memory seed) and install it. Refuses to scaffold into a non-empty directory.

Vault selection: `--vault <path>`, `$HEBB_VAULT`, or the nearest `.hebb/` above the working directory.

Planned (stub for now): `hebb sync`.

## Build

```sh
go build ./...
go test ./...
go run ./cmd/hebb --help
```

Test strategy and the (planned) two-stage CD pipeline are in [TESTING.md](TESTING.md).

## Layout

- `core/` — UI-agnostic engine (index, search, context, watcher)
- `cli/` — CLI over core
- `mcp/` — MCP server surface
- `web/` — web search UI (page embedded via go:embed)
- `cmd/hebb/` — entrypoint
- `skills/ automation/ launchd/ config/ vault-template/` — installed by `hebb install` (Phase 2+)
