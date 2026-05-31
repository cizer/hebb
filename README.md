# hebb

A portable engine for markdown knowledge vaults: index, search, scaffold, maintain.

`hebb` is the function layer for a "second brain" vault. The vault (markdown, attachments, memory) is the data; `hebb` is the tool that operates on it. It is **multi-vault**, like git is multi-repo: run `hebb` inside a vault directory, or pass `--vault`.

Status: early. Being ported to Go from the Node reference `onevault-mcp`. See `ARCHITECTURE.md` for the data/function model, the multi-vault design and the build plan.

## Layout

- `core/` — UI-agnostic engine (index, search, scaffold, sync, hygiene)
- `cli/` — thin CLI over core
- `mcp/` — MCP server surface (project-scoped per vault)
- `cmd/hebb/` — CLI entrypoint
- `skills/` — Claude skills installed by `hebb install`
- `automation/` — scheduled jobs
- `launchd/` — launchd plist templates
- `config/` — settings and permissions templates
- `vault-template/` — scaffold used by `hebb new`

## Commands

Stubs today: `new` · `install` · `index` · `search` · `serve` · `sync` · `doctor`

## Build

```sh
go build ./...
go run ./cmd/hebb --help
```
