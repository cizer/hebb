# hebb

A portable engine for markdown knowledge vaults: index, search, scaffold, maintain.

`hebb` is the function layer for a "second brain" vault. The vault (markdown, attachments, memory) is the data; `hebb` is the tool that operates on it. It is **multi-vault**, like git is multi-repo: run `hebb` inside a vault directory, or pass `--vault`.

Go rewrite of the Node reference `onevault-mcp`. Design in [ARCHITECTURE.md](ARCHITECTURE.md); build status and roadmap in [PLAN.md](PLAN.md).

## Status

**Phase 1 complete** — feature parity with `onevault-mcp` (index, search, MCP server, web UI, file watcher). Not yet wired into a live machine; that is Phase 2 (`hebb install`).

## Commands

Working today:
- `hebb index` — build or refresh the FTS5 index
- `hebb search <query>` — full-text search (`--tag`, `--path-prefix`, `--limit`)
- `hebb mcp` — MCP server over stdio for Claude (`search_vault`, `expand_context`, `get_context_for_topic`, `vault_stats`, `reindex_vault`)
- `hebb serve` — local web search UI on 127.0.0.1 (`--port`, or `$HEBB_WEB_PORT`)

Vault selection: `--vault <path>`, `$HEBB_VAULT`, or the nearest `.hebb/` above the working directory.

Planned (stubs for now): `hebb new`, `hebb install`, `hebb sync`, `hebb doctor`.

## Build

```sh
go build ./...
go test ./...
go run ./cmd/hebb --help
```

## Layout

- `core/` — UI-agnostic engine (index, search, context, watcher)
- `cli/` — CLI over core
- `mcp/` — MCP server surface
- `web/` — web search UI (page embedded via go:embed)
- `cmd/hebb/` — entrypoint
- `skills/ automation/ launchd/ config/ vault-template/` — installed by `hebb install` (Phase 2+)
