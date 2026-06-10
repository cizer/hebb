# hebb

[![CI](https://github.com/cizer/hebb/actions/workflows/ci.yml/badge.svg)](https://github.com/cizer/hebb/actions/workflows/ci.yml)

**hebb turns a folder of markdown notes into a fast, connected knowledge base — and gives your AI tools a memory they can search and recall.**

Point hebb at a vault of `.md` files. It builds a full-text index, follows your `[[wiki-links]]` and tags to assemble related context, and serves both over the [Model Context Protocol](https://modelcontextprotocol.io) (MCP) — so Claude and Codex can search your notes and pull in connected material while they work. Your notes stay plain markdown on your disk; hebb indexes and serves them, it never takes ownership.

Multi-vault, like git is multi-repo: run `hebb` inside a vault directory, or pass `--vault`.

## Why hebb

- **Agent-native.** One MCP server gives Claude Code, Claude Desktop, and Codex the same tools: `search_vault`, `get_context_for_topic`, `expand_context`, `vault_stats`, `reindex_vault`.
- **Connected recall, not just keywords.** hebb walks `[[wiki-links]]` and shared tags to gather a topic's context, so an agent gets the related notes, not just the literal hits.
- **Local and fast.** Pure-Go SQLite FTS5 — no service, no cloud, no cgo. A single static binary; your vault never leaves your machine.
- **Composable.** A CLI, a local web UI, an MCP server, a Claude Code plugin, and a file watcher over one engine. Use the parts you want.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/cizer/hebb/main/install.sh | sh
```

Installs `hebb` to `~/.local/bin` (override `HEBB_INSTALL_DIR`; pin a version with `HEBB_VERSION=vX.Y.Z`). With Go: `go install github.com/cizer/hebb/cmd/hebb@latest`. Requires macOS or Linux (arm64 or amd64).

## Quickstart

```sh
hebb new ~/notes              # scaffold a vault (PARA folders + templates) and install it
cd ~/notes
echo "# Idea\nA note about search engines. #ideas" > 1-Projects/Idea.md
hebb search "search engines"  # full-text search
hebb serve                    # local web UI on 127.0.0.1
```

Wire your agents — `hebb install`/`hebb new` offer an interactive picker, or do it directly:

```sh
hebb codex                                  # register the vault with the Codex CLI
# Claude Code plugin (search + the vault-ingest skill, all vaults):
#   /plugin marketplace add cizer/hebb
#   /plugin install hebb@hebb
```

## Commands

- `hebb new <path>` — scaffold a fresh vault (PARA skeleton, `CLAUDE.md` + `AGENTS.md`, note template) and install it.
- `hebb install` — wire a vault into the machine (config, index, memory, optional launchd jobs) and offer to connect your agents. Idempotent.
- `hebb search <query>` — full-text search (`--tag`, `--path-prefix`, `--limit`).
- `hebb mcp` — MCP server over stdio (the five tools above).
- `hebb serve` — local web search UI on 127.0.0.1 (`--port`, `$HEBB_WEB_PORT`).
- `hebb codex` — register the vault as a Codex MCP server (`~/.codex/config.toml`) and install hebb's agent skills into Codex's skills dir (`~/.agents/skills`), non-destructively. `--no-skills` to skip the skills.
- `hebb doctor` — read-only health check; non-zero exit if anything is broken.
- `hebb reset` — un-wire a vault from the machine (memory link, launchd jobs, agent configs, index). Dry run by default; `--force` to apply. Never touches your notes.
- `hebb sync` — commit, pull (rebase), and push the vault's markdown via git. Never force-pushes; a conflicting pull is aborted and reported. Enable `[git]` in `config.toml` to also auto-sync: pull when a hebb process starts, commit+push after edits settle.
- `hebb update` — check for and install a newer hebb release (checksum-verified, atomic replace). `--check` only reports. Self-replaces only a binary hebb owns; a Homebrew or `go install` binary is left to its package manager. A scheduled `update-check` job notifies of new releases (set `[update] auto = true` to install them).
- `hebb index` — build or refresh the index (usually automatic).

Vault selection everywhere: `--vault <path>`, `$HEBB_VAULT`, or the nearest `.hebb/` above the working directory.

## Agents

hebb is the engine; thin adapters connect it to each tool, all over the same MCP server:

- **Claude Code** — the [`plugin/`](plugin/) (MCP server + the `vault-ingest` filing skill), installed once via the marketplace; works in every vault.
- **Codex** — an MCP-server entry pinned to the vault plus the same skills materialised into `~/.agents/skills`, written by `hebb codex` (or the `hebb install` picker). The Codex counterpart to the plugin.
- **Claude Desktop** — an MCP-server entry pinned to a vault, written by the `hebb install` picker.
- **Anything else that speaks MCP** — point it at `hebb mcp`.

## How it works

```
core/         engine: index, search, context graph, file watcher
cli/          the hebb command
mcp/          MCP server surface
web/          local web UI (embedded)
plugin/       Claude Code plugin (manifest, .mcp.json, vault-ingest skill)
automation/   optional background jobs (digest, action review)
vault-template/  the `hebb new` scaffold
```

Per vault, hebb keeps a `.hebb/` directory (like `.git`): `config.toml`, the derived `index.db`, and `memory/`. Commit `config.toml` and your notes; the index is rebuilt on demand.

See [ARCHITECTURE.md](ARCHITECTURE.md) for the design.

## Build

```sh
go build ./...
go test ./...
```

`hebb --version` shows the git revision on dev builds; releases stamp a clean tag. Test strategy and the CD pipeline are in [TESTING.md](TESTING.md); releasing in [RELEASING.md](RELEASING.md).

## License

[Apache-2.0](LICENSE).
