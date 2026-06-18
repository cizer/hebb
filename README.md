<p align="center">
  <img src=".github/assets/hero.svg" alt="hebb: a connected memory for your notes, served to your AI over the Model Context Protocol" width="100%">
</p>

<p align="center">
  <a href="https://github.com/cizer/hebb/actions/workflows/ci.yml"><img src="https://github.com/cizer/hebb/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux-1f2937" alt="Platform: macOS and Linux">
  <img src="https://img.shields.io/badge/runtime-single%20static%20binary-6366f1" alt="Single static binary">
  <img src="https://img.shields.io/badge/protocol-MCP-06b6d4" alt="Model Context Protocol">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache--2.0-8b5cf6" alt="License: Apache-2.0"></a>
</p>

<p align="center">
  <b>Point hebb at a folder of markdown notes and it becomes a fast, connected knowledge base, plus a memory your AI tools can search, recall, and grow with you.</b>
</p>

---

## What hebb does for every vault

Most note collections quietly rot. Files pile up, links break, and the one note you needed is buried under a thousand you have forgotten. Your AI assistant, meanwhile, starts every conversation with amnesia.

hebb fixes both problems at the source, without changing how you write. It indexes your notes for instant full-text search, follows your `[[wiki-links]]` and tags to assemble genuinely related context, and serves all of it to Claude and Codex over the [Model Context Protocol](https://modelcontextprotocol.io) (MCP). Your agent stops guessing and starts recalling: it searches what you already know, pulls in the connected material, and writes back into the same vault.

The result is a vault that compounds in value. Every note you add makes search sharper, the link graph richer, and your AI more useful. Your notes stay plain markdown on your own disk. hebb indexes and serves them. It never takes ownership, never phones home, and never locks you in.

- **For you:** a search engine and a structural health check for your own knowledge.
- **For your AI:** a real, searchable long-term memory it can call, expand, and update.
- **For your data:** plain files, local-first, single binary, no cloud, no database server.

## Why it matters

- **Connected recall, not just keyword hits.** hebb walks `[[wiki-links]]` and shared tags to gather a topic's neighbourhood, so an agent gets the *related* notes, not only the literal matches. This is the difference between an assistant that quotes one line and one that understands the surrounding context.
- **Agent-native by design.** A single MCP server gives Claude Code, Claude Desktop, and Codex the same five tools: `search_vault`, `get_context_for_topic`, `expand_context`, `vault_stats`, and `reindex_vault`. Wire it once and every agent speaks to your vault the same way.
- **Effortlessly current.** New and changed notes are picked up automatically on the next search, and a live file watcher reindexes edits as they land. Your agent never has to stop and reindex after writing.
- **Local, fast, and private.** Pure-Go SQLite FTS5 search in a single static binary. No service, no cloud, no cgo. Your vault never leaves your machine.
- **Multi-vault, like git is multi-repo.** Install the binary once, then create or attach as many independent vaults as you like. Each is self-contained and self-describing.
- **A vault with a metabolism.** Built-in health checks surface dangling links, PARA drift, and oversized notes, so the collection stays healthy as it grows instead of silently decaying.

## Key features

| | Feature | What you get |
|---|---|---|
| 🔎 | **Full-text search** | Instant SQLite FTS5 search across your whole vault, from the CLI, the web UI, or your agent. Filter by tag and folder. |
| 🧠 | **Connected context** | A live link and tag graph that gathers a topic's neighbours, so recall is contextual rather than literal. |
| 🤖 | **MCP server** | Five tools that give Claude and Codex a searchable, expandable memory of everything you know. |
| ♻️ | **Self-refreshing index** | Automatic pickup of new and changed notes, plus a file watcher for live edits. No manual reindexing. |
| 🩺 | **Vault health** | Deterministic linters for dangling links, PARA drift, and oversized notes, on the CLI and a web dashboard. |
| 🌐 | **Local web UI** | A clean search-and-health interface on `127.0.0.1`, bound to loopback only. |
| 🗂️ | **Multi-vault** | One install, many vaults. Each command resolves its vault from the current directory, like `git`. |
| 🔁 | **Git auto-sync** | Optional commit, pull, and push of your markdown while hebb runs, so a vault stays in sync across machines. |
| 📰 | **Daily digest** | A generated note summarising what genuinely changed, driven by content-level change detection rather than file timestamps. |
| 📦 | **Single static binary** | macOS or Linux, arm64 or amd64. No runtime, no dependencies, trivial to distribute. |

Every piece is composable. Use the CLI, the web UI, the MCP server, the Claude Code plugin, and the file watcher independently or together, all over one engine.

## Getting started

Three steps: install hebb, set up a vault, connect your agents.

### 1. Install

hebb is a single static binary for **macOS or Linux** (arm64 or amd64). Pick one method:

**Install script.** Downloads the matching release binary to `~/.local/bin`:

```sh
curl -fsSL https://raw.githubusercontent.com/cizer/hebb/main/install.sh | sh
```

Override the target dir with `HEBB_INSTALL_DIR`, or pin a version with `HEBB_VERSION=vX.Y.Z`.

**Go:** `go install github.com/cizer/hebb/cmd/hebb@latest`.

**Homebrew:** planned, not yet enabled.

Make sure the install dir is on your `PATH`, then check the binary:

```sh
export PATH="$HOME/.local/bin:$PATH"   # add to your shell profile if it isn't already
hebb --version
```

Later, upgrade in place with `hebb update`.

### 2. Set up a vault

A *vault* is just a folder of markdown notes. hebb adds a `.hebb/` directory to it (like `.git` adds `.git/`) and wires it into your machine and agents. Start one of two ways.

**A new vault:**

```sh
hebb new ~/notes
```

Scaffolds a PARA skeleton (`1-Projects/`, `2-Areas/`, `3-Resources/`, `4-Archives/`), a baseline `CLAUDE.md` and `AGENTS.md`, a note template, and an empty memory seed, then installs it. It refuses to scaffold into a non-empty directory, so it never overwrites existing files.

**An existing folder of notes:**

```sh
hebb install --vault ~/existing-notes
```

Indexes the folder in place and wires it. Pass `--vault` the first time because the folder has no `.hebb/` yet for hebb to find. After that the vault self-identifies, so you can just `cd ~/existing-notes && hebb <command>`.

Both paths run `hebb install`. It is idempotent and never modifies your notes. It:

- writes `.hebb/config.toml` (the committed, per-vault config) and builds the search index at `.hebb/index.db`;
- symlinks the vault's agent memory (`.hebb/memory/`) into Claude's project directory;
- installs hebb's skills into `~/.claude/skills` (`--no-skills` to skip);
- offers an interactive picker to connect your agents (or pass `--codex` / `--claude-desktop` / `--mcp-json` explicitly, or `--no-interaction` to skip);
- with `--launchd`, renders background jobs (`--load` also starts them).

Check the result any time with `hebb doctor`.

### 3. Connect your agents

The install picker can do this for you, or wire each one explicitly:

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

### 4. Try it

```sh
cd ~/notes
printf '# Search engines\nNotes on ranking and FTS. #ideas\n' > 1-Projects/Search.md
hebb search "ranking"   # full-text search
hebb serve              # browse and check vault health at http://127.0.0.1:4321
```

Then ask your agent something only your vault knows. It will search, expand the surrounding context, and answer from what you have written.

## Commands

- `hebb new <path>`: scaffold a fresh vault (PARA skeleton, `CLAUDE.md` + `AGENTS.md`, note template) and install it.
- `hebb install`: wire a vault into the machine (config, index, memory, agent skills into `~/.claude/skills`, optional launchd jobs) and offer to connect your agents. Idempotent. `--no-skills` to skip the skills.
- `hebb vaults`: list the vaults hebb knows about (the registry `hebb serve` switches between); marks the current vault and flags any whose directory is gone.
- `hebb search <query>`: full-text search (`--tag`, `--path-prefix`, `--limit`).
- `hebb mcp`: MCP server over stdio for Claude, Codex, and any MCP client (the five tools above).
- `hebb serve`: local web search and health UI on 127.0.0.1 (`--port`, `$HEBB_WEB_PORT`). Serves every vault hebb knows about (the current vault plus the machine registry) on one port, switchable via a picker, so multiple vaults never collide on a port. Under launchd this is a single machine-global service (`local.hebb.web`), not one job per vault.
- `hebb audit` (alias: `hebb health`): advisory worklist of vault-content issues (dangling links, ambiguous links, PARA drift, oversized notes) over the index. Read-only, repairs nothing; exits 0 even with findings (`--json` for tooling). Thresholds in `[health]`. Distinct from `hebb doctor`, which checks the install wiring.
- `hebb codex`: register the vault as a Codex MCP server (`~/.codex/config.toml`) and install hebb's agent skills into Codex's skills dir (`~/.agents/skills`), non-destructively. `--no-skills` to skip the skills.
- `hebb doctor`: read-only health check (config, `.mcp.json`, index, settings, memory, Codex and Claude Desktop wiring, launchd); content-compares each against what install would write today and reports drift, warning on a binary path that still resolves to a working hebb and failing on one that points at nothing. Never runs a configured command; non-zero exit if anything is broken.
- `hebb unwire` (alias: `hebb reset`): un-wire a vault from the machine (memory link, launchd jobs, agent configs, index, and the registry entry; the global web service is retired only when the last vault is removed). Dry run by default; `--force` to apply. Never touches your notes.
- `hebb sync`: commit, pull (rebase), and push the vault's markdown via git. Never force-pushes; a conflicting pull is aborted and reported. With `[git] enabled` (on by default for a git repo at install time), hebb also auto-syncs while a `serve`/`mcp` process runs: pull at startup, commit and push after edits settle.
- `hebb update`: check for and install a newer hebb release (checksum-verified, atomic replace), then re-apply the release's skills to whichever skills dirs already have them (so new and changed skills land on upgrade) and restart the running web services onto the new binary. `--check` only reports. Self-replaces only a binary hebb owns; a Homebrew or `go install` binary is left to its package manager. A scheduled `update-check` job notifies of new releases via `[notify]` when configured (set `[update] auto = true` to also install them).
- `hebb restart-services`: restart hebb's running launchd web services (across all vaults on the machine) onto the current binary. Run it after any out-of-band binary update (a dev build, `go install`, brew) or if a service misbehaves; `hebb update` already calls it. Scheduled jobs re-exec on their next run and are left alone. macOS only; a no-op where launchctl is absent.
- `hebb index`: build or refresh the index (usually automatic).
- `hebb digest`: refresh the index, then write the daily vault digest. The launchd `daily-digest` entrypoint: it is the hebb binary (not a shell wrapper) so macOS grants it Full Disk Access to read protected vault folders. Selection is driven by the index's content-level change detection (a per-note content hash plus a change watermark), so it reports notes whose content changed since the last run, never notes a bulk operation merely re-stamped with a new mtime. `--output` sets the digest note path; `--date` overrides the run date for testing.
- `hebb notify [text]`: post a one-line summary to the configured webhook (`[notify] url` or `$HEBB_NOTIFY_URL`). POST `application/json`, body `{"text": "..."}`. Exits non-zero on HTTP failure. Also called automatically by `hebb digest` and `hebb update --check` after their writes when notify is enabled. The URL is never logged.

Vault selection everywhere: `--vault <path>`, `$HEBB_VAULT`, or the nearest `.hebb/` above the working directory.

## Agents

hebb is the engine; thin adapters connect it to each tool, all over the same MCP server:

- **Claude Code**: `hebb install` materialises the `vault-ingest` skill into `~/.claude/skills` so it works in any context, and the [`plugin/`](plugin/) additionally offers it (plus the MCP server) via the marketplace for those who prefer that.
- **Codex**: an MCP-server entry pinned to the vault plus the same skills materialised into `~/.agents/skills`, written by `hebb codex` (or the `hebb install` picker). The Codex counterpart to the plugin.
- **Claude Desktop**: an MCP-server entry pinned to a vault, written by the `hebb install` picker.
- **Anything else that speaks MCP**: point it at `hebb mcp`.

## What to commit

Commit `.hebb/config.toml` and your notes, so a cloned or synced vault self-identifies. The index (`.hebb/index.db`) is derived and rebuilt on demand, so gitignore it. Memory under `.hebb/memory/` travels with the vault. `hebb` also commits a `bootstrap.sh` at the vault root, so a fresh clone or an ephemeral machine self-installs: run `./bootstrap.sh` to install the binary (pinned to the version that wrote it) and wire the vault. A scaffolded vault also commits a Claude Code `SessionStart` hook (`.claude/hooks/session-start.sh`) that runs `bootstrap.sh` automatically in an ephemeral Claude Code on the web session, so the vault self-provisions there with no manual step; the hook is a no-op on a local machine. If the vault is a git repo, `hebb install` enables `[git]` auto-sync by default when it first writes `config.toml` (set `enabled = false` to opt out; an existing config is never changed). See `hebb sync` above.

`config.toml` holds `name`, `exclude_dirs`, `web_port`, `jobs`, per-job `[job_args]` / `[job_env]`, and the `[git]` (auto-sync), `[update]` (auto-update), `[index]` (auto-refresh), `[ingest]` (ingest policy), `[notify]` (headless webhook), `[health]` (health thresholds), and `[bootstrap]` (clone self-install) sections. Every key is optional and falls back to a sensible default. See **[CONFIG.md](CONFIG.md)** for the full reference, with an annotated example and per-field defaults.

## Multiple vaults

hebb is multi-vault like git is multi-repo: install the binary once, then create or attach as many vaults as you like. Each is independent. Every command resolves its vault from the current directory (nearest `.hebb/` above the cwd), or an explicit `--vault <path>`, or `$HEBB_VAULT`.

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

See [ARCHITECTURE.md](ARCHITECTURE.md) for the design, and [METABOLISM.md](METABOLISM.md) for the vault-health roadmap.

## Build

```sh
go build ./...
go test ./...
```

`hebb --version` shows the git revision on dev builds; releases stamp a clean tag. Test strategy and the CD pipeline are in [TESTING.md](TESTING.md); releasing in [RELEASING.md](RELEASING.md).

## License

[Apache-2.0](LICENSE).
