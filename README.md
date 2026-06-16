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
- **Self-refreshing index.** New and changed notes are picked up automatically on the next search, and the file watcher reindexes live edits, so agents never have to reindex after writing. `reindex_vault` stays as a manual escape hatch for a suspected-stale index or bulk file moves.

## Install

hebb is a single static binary for **macOS or Linux** (arm64 or amd64). Pick one method:

**Install script.** Downloads the matching release binary to `~/.local/bin`:

```sh
# public repo:
curl -fsSL https://raw.githubusercontent.com/cizer/hebb/main/install.sh | sh

# private repo (uses your GitHub CLI auth for both the script and the binary):
gh api repos/cizer/hebb/contents/install.sh -H "Accept: application/vnd.github.raw" | sh
```

Override the target dir with `HEBB_INSTALL_DIR`, or pin a version with `HEBB_VERSION=vX.Y.Z`.

**Go:** `go install github.com/cizer/hebb/cmd/hebb@latest` (set `GOPRIVATE=github.com/cizer/*` while the repo is private).

**Homebrew:** planned, not yet enabled.

Then make sure the install dir is on your `PATH`, and check the binary:

```sh
export PATH="$HOME/.local/bin:$PATH"   # add to your shell profile if it isn't already
hebb --version
```

Later, upgrade in place with `hebb update`.

## Set up a vault

A *vault* is just a folder of markdown notes. hebb adds a `.hebb/` directory to it (like `.git` adds `.git/`) and wires it into your machine and agents. Start one of two ways.

### A new vault

```sh
hebb new ~/notes
```

Scaffolds a PARA skeleton (`1-Projects/`, `2-Areas/`, `3-Resources/`, `4-Archives/`), a baseline `CLAUDE.md` and `AGENTS.md`, a note template, and an empty memory seed, then installs it. Refuses to scaffold into a non-empty directory, so it never overwrites existing files.

### An existing folder of notes

```sh
hebb install --vault ~/existing-notes
```

Indexes the folder in place and wires it. Pass `--vault` the first time because the folder has no `.hebb/` yet for hebb to find. After that the vault self-identifies, so you can just `cd ~/existing-notes && hebb <command>`.

### What `hebb install` does

Both paths run `hebb install`. It is idempotent and never modifies your notes:

- writes `.hebb/config.toml` (the committed, per-vault config) and builds the search index at `.hebb/index.db`;
- symlinks the vault's agent memory (`.hebb/memory/`) into Claude's project directory;
- installs hebb's skills into `~/.claude/skills` (`--no-skills` to skip);
- offers an interactive picker to connect your agents (or pass `--codex` / `--claude-desktop` / `--mcp-json` explicitly, or `--no-interaction` to skip);
- with `--launchd`, renders background jobs (`--load` also starts them).

Check the result any time with `hebb doctor`, and browse the vault with `hebb serve` (local web UI on `127.0.0.1`).

### Connect your agents

The picker can do this for you, or wire each explicitly:

- **Claude Code.** Install the plugin (works in every vault):
  ```
  /plugin marketplace add cizer/hebb
  /plugin install hebb@hebb
  ```
  `hebb install` also drops the `vault-ingest` skill into `~/.claude/skills`, so it works even without the plugin.
- **Codex.** `hebb codex` adds an `[mcp_servers.hebb]` entry to `~/.codex/config.toml` and installs the skills into `~/.agents/skills`.
- **Claude Desktop.** `hebb install --claude-desktop` (restart Claude Desktop afterwards).
- **Anything else that speaks MCP.** Point it at `hebb mcp`.

See [Agents](#agents) for how each adapter works.

### What to commit

Commit `.hebb/config.toml` and your notes, so a cloned or synced vault self-identifies. The index (`.hebb/index.db`) is derived and rebuilt on demand, so gitignore it. Memory under `.hebb/memory/` travels with the vault. If the vault is a git repo, `hebb install` enables `[git]` auto-sync by default when it first writes `config.toml` (set `enabled = false` to opt out; an existing config is never changed). See `hebb sync` below.

`config.toml` holds `name`, `exclude_dirs`, `web_port`, `jobs`, per-job `[job_args]` / `[job_env]`, and the `[git]` (auto-sync), `[update]` (auto-update), `[index]` (auto-refresh), `[ingest]` (ingest policy), `[notify]` (headless webhook), and `[health]` (health thresholds) sections. Every key is optional and falls back to a sensible default. See **[CONFIG.md](CONFIG.md)** for the full reference, with an annotated example and per-field defaults.

### Multiple vaults

hebb is multi-vault like git is multi-repo: install the binary once, then create or attach as many vaults as you like. Each is independent. Every command resolves its vault from the current directory (nearest `.hebb/` above the cwd), or an explicit `--vault <path>`, or `$HEBB_VAULT`.

### Try it

```sh
cd ~/notes
printf '# Search engines\nNotes on ranking and FTS. #ideas\n' > 1-Projects/Search.md
hebb search "ranking"   # full-text search
hebb serve              # browse at http://127.0.0.1:4321
```

## Commands

- `hebb new <path>` — scaffold a fresh vault (PARA skeleton, `CLAUDE.md` + `AGENTS.md`, note template) and install it.
- `hebb install` — wire a vault into the machine (config, index, memory, agent skills into `~/.claude/skills`, optional launchd jobs) and offer to connect your agents. Idempotent. `--no-skills` to skip the skills.
- `hebb search <query>` — full-text search (`--tag`, `--path-prefix`, `--limit`).
- `hebb mcp` — MCP server over stdio (the five tools above).
- `hebb serve` — local web search UI on 127.0.0.1 (`--port`, `$HEBB_WEB_PORT`).
- `hebb codex` — register the vault as a Codex MCP server (`~/.codex/config.toml`) and install hebb's agent skills into Codex's skills dir (`~/.agents/skills`), non-destructively. `--no-skills` to skip the skills.
- `hebb doctor` — read-only health check (config, `.mcp.json`, index, settings, memory, Codex and Claude Desktop wiring, launchd); content-compares each against what install would write today and reports drift, warning on a binary path that still resolves to a working hebb and failing on one that points at nothing. Never runs a configured command; non-zero exit if anything is broken.
- `hebb health` — advisory worklist of vault-content issues (dangling links, ambiguous links, PARA drift, oversized notes) over the index. Read-only, repairs nothing; exits 0 even with findings (`--json` for tooling). Thresholds in `[health]`. Distinct from `hebb doctor`, which checks the install.
- `hebb reset` — un-wire a vault from the machine (memory link, launchd jobs, agent configs, index). Dry run by default; `--force` to apply. Never touches your notes.
- `hebb sync` — commit, pull (rebase), and push the vault's markdown via git. Never force-pushes; a conflicting pull is aborted and reported. With `[git] enabled` (on by default for a git repo at install time), hebb also auto-syncs while a `serve`/`mcp` process runs: pull at startup, commit+push after edits settle.
- `hebb update` — check for and install a newer hebb release (checksum-verified, atomic replace), then re-apply the release's skills to whichever skills dirs already have them (so new and changed skills land on upgrade). `--check` only reports. Self-replaces only a binary hebb owns; a Homebrew or `go install` binary is left to its package manager. A scheduled `update-check` job notifies of new releases via `[notify]` when configured (set `[update] auto = true` to also install them).
- `hebb index` — build or refresh the index (usually automatic).
- `hebb digest`: refresh the index, then write the daily vault digest. The launchd `daily-digest` entrypoint: it is the hebb binary (not a shell wrapper) so macOS grants it Full Disk Access to read protected vault folders. Selection is driven by the index's content-level change detection (a per-note content hash plus a change watermark), so it reports notes whose content changed since the last run, never notes a bulk operation merely re-stamped with a new mtime. `--output` sets the digest note path; `--date` overrides the run date for testing.
- `hebb notify [text]` — post a one-line summary to the configured webhook (`[notify] url` or `$HEBB_NOTIFY_URL`). POST `application/json`, body `{"text": "..."}`. Exits non-zero on HTTP failure. Also called automatically by `hebb digest` and `hebb update --check` after their writes when notify is enabled. The URL is never logged.

Vault selection everywhere: `--vault <path>`, `$HEBB_VAULT`, or the nearest `.hebb/` above the working directory.

## Agents

hebb is the engine; thin adapters connect it to each tool, all over the same MCP server:

- **Claude Code** — `hebb install` materialises the `vault-ingest` skill into `~/.claude/skills` so it works in any context, and the [`plugin/`](plugin/) additionally offers it (plus the MCP server) via the marketplace for those who prefer that.
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
automation/   optional background jobs (action review; the digest is built into hebb digest)
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
