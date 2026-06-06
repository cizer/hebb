# hebb build plan

Progress tracker for the Go rewrite. The design lives in [ARCHITECTURE.md](ARCHITECTURE.md). Build to the `build` skill conventions (TDD, vertical slices, conventional commits, small batches). Reference to port from: `~/personal/onevault-mcp` (Node).

## Phases

### Phase 0 — Scaffold ✅
Go module, cobra CLI skeleton, repo created, architecture doc added.

### Phase 1 — Engine + surfaces ✅  (parity with onevault-mcp)
- ✅ core: vault resolution, FTS5 schema (identical to the Node reference), markdown parser (frontmatter, H1 title, wiki-links, tags, strip), full reindex + single-file index/remove, search + LIKE fallback, context tools (`ExpandContext`, `GetContextForTopic`), stats
- ✅ cli: `hebb index`, `hebb search`
- ✅ mcp: `hebb mcp` — all five tools, names/descriptions matched for a drop-in `.mcp.json` swap
- ✅ serve: `hebb serve` — web UI (embedded page), `/api/search|stats|reindex`, `obsidian://` deep links
- ✅ watcher: fsnotify incremental reindex, wired into `mcp` and `serve`; index db serialised (`MaxOpenConns=1` + `busy_timeout`)
- ✅ tests: core + web; `go vet` clean; verified live (MCP over stdio, HTTP)

### Phase 2 — `hebb install` ✅  (mechanism complete)
Wire a vault into the machine, idempotently. Every step is parameterised by its
target dirs (temp-dir tested) and defensive — it never clobbers a real file/dir:
- ✅ core: `.hebb/config.toml` model (name, exclude_dirs, web_port, jobs, skills); `ResolveVault` honors it
- ✅ init `.hebb/config.toml` + generate the project-scoped, portable `.mcp.json` (`command: hebb`, `args: [mcp]`)
- ✅ project settings: merge MCP enable + tool allow-list into `<vault>/.claude/settings.json` (non-destructive)
- ✅ standalone binary: `skills/`, `automation/`, `vault-template/` are `go:embed`'d and materialised to the hebb data dir (`$XDG_DATA_HOME/hebb`, else `~/.local/share/hebb`) on install, so no repo checkout is needed; `--asset-root` is a dev override that links straight from a source tree
- ✅ skills are delivered by the hebb Claude Code plugin (`plugin/`), not by install (B2): install never touches `<vault>/.claude/skills`. (Earlier project-scoped symlinking was removed once the plugin was adopted.)
- ✅ render launchd jobs (`local.hebb.<slug>.<job>`, `plutil`-valid); `--launchd`/`--load` (launchctl bootstrap, dry-run preview); web job built in, automation jobs gated on their script existing
- ✅ symlink memory into `~/.claude/projects/<project-slug>/memory` (Claude Code path-slug exact), sourced from `<vault>/.hebb/memory` so agent memory syncs with the vault but is hidden from Obsidian and excluded from the index; build the first index
- ✅ `hebb doctor` — read-only health check (config, .mcp.json, index, settings, memory, launchd), exits non-zero on failure

Remaining before a live install is meaningful (content migration, not tool work):
- ✅ automation migrated (C): `run-vault-digest.sh`, `generate-vault-digest.py`, `generate-action-review.py` are genericised (parameterised by `--vault-root`, no hardcoded paths) in `automation/`, embedded, materialised + launchd-rendered. `update-engineering-knowledge.sh` was NOT migrated (OneVault-specific: pulls RFC/ADR git repos + a Confluence sync) and has no VaultJobs entry; revisit only if a vault needs it. (Skills already done: they live in `plugin/skills/`, delivered by the plugin.)
- distribute the binary so `command: hebb` resolves on PATH (Phase 4); the assets are already standalone (embedded), so this is the only remaining standalone gap

### Phase 3 — `hebb new` + vault-template ✅
- ✅ `vault-template/`: PARA skeleton (`1-Projects/`..`4-Archives/`), generic baseline `CLAUDE.md`, starter `README.md`, a `templates/note.md`, and an empty memory seed under `.hebb/memory/`. Embedded via `go:embed all:vault-template` (dotfiles included).
- ✅ `install.Scaffold` copies the template tree into a target, defensively (refuses a non-empty dir, never clobbers); `hebb new <path>` scaffolds then installs against it, reusing the shared `installVault` runner. From-scratch test proves a fresh vault indexes and is searchable with zero personal data; verified standalone from the embedded assets (`new` -> `doctor` -> `search`).
- Note: a fresh vault indexes its own `CLAUDE.md`/`README.md`/`templates/note.md` (3 notes). Excluding `templates/` or root config notes from the index is a possible later refinement.

### Phase 4 — Pipeline, package + distribute 🚧
Two-stage continuous-deployment pipeline (GitHub Actions; remote is
`github.com/cizer/hebb`). Test strategy and stage definitions in [TESTING.md](TESTING.md);
workflow in `.github/workflows/ci.yml`.

- ✅ **Stage 1 — build (fast tests), every push/PR:** `go build`, `gofmt -l`,
  `go vet`, `go test ./...` (~5s), `go test -race` (watcher/serve), and a
  cross-compile of the matrix (darwin arm64/amd64, linux amd64/arm64). macOS + Linux runners.
- ✅ **Stage 2 — acceptance (production-like), gated on Stage 1:** `scripts/acceptance.sh`
  drives the *built binary* against a throwaway vault + temp `HOME` on macOS and
  Linux: install → doctor → index/search (canary) → serve+curl the API → mcp over
  stdio (initialize, tools/list, tools/call) → plutil-lint plists (macOS) → run the
  materialised automation scripts (digest + action-review) against the fixture. 40 checks.
  Automates the manual UAT; `--load` not run in CI. Runnable locally too.
- 🟡 **Stage 3 — release (the deploy): scaffolded, not yet exercised.** `.goreleaser.yaml` (cross-compiles darwin/linux × amd64/arm64, CGO off, version stamped from the tag, archives + checksums + GitHub release) + `.github/workflows/release.yml` (fires on `v*` tag, gates on `go vet`+`go test`, runs GoReleaser) + `RELEASING.md`. Validated by YAML parse + manual cross-compile of all 4 targets; `goreleaser check` not run (tool not installed locally) so the first tag exercises the config. Homebrew tap (`cizer/homebrew-hebb`) is configured-but-commented so the first release can't fail on a missing tap; enable later. Remaining: cut the first tag, optional npm, optional macOS signing/notarization, LICENSE (Legal).
- ⬜ pre-commit/pre-push hooks mirroring Stage 1 (consistent local + CI); optional staticcheck; fuller README.

### Phase 5 — Cutover ⬜
- repoint the live machine to `hebb`, relocate memory to the synced location, remove `vault/bin` scripts, retire `onevault-mcp`

## Resume point

**Immediate next: Phase 4 Stage 3 release (still deferred), or live UAT, or tidy-ups.** State as of this point: plugin migration **A + B1 + B2 done**, automation migration **C done**, and the **Codex adapter done**, on `main`, CI green, working tree clean. Phases 0-3 done; the plugin is adopted (see decision below).

**Codex adapter done:** `hebb codex` merges an `[mcp_servers.hebb]` block into `~/.codex/config.toml`, surgically (replace/append only its own block; preserves other servers, comments, top-level keys) and idempotently, pinning the vault via `env.HEBB_VAULT` + `cwd` (deterministic; `HEBB_VAULT` wins in `ResolveVault`). `startup_timeout_sec=20` for cold-index starts; `--mcp-name` registers a second vault; `--codex-config`/`--home` for testing. Schema verified against the Codex docs (command/args/env/cwd/startup_timeout_sec/enabled). `install/codex.go` renders+merges; tests cover render, create, idempotency, non-destructive merge, repoint, and a real TOML-parse round-trip (BurntSushi/toml). `cli/codex.go` wires the command. `vault-template/` now ships a generic `AGENTS.md` (Codex's CLAUDE.md equivalent: defers conventions to CLAUDE.md, lists the hebb MCP tools); `hebb new` scaffolds it. Skills remain Claude-only.

Plugin migration recap: **A** graduated `plugin/` into the repo (manifest + `.mcp.json` with `HEBB_VAULT=${CLAUDE_PROJECT_DIR}` + the vault-ingest skill; spike validated live). **B1** made per-vault `.mcp.json` + settings opt-in via `hebb install --mcp-json`. **B2** removed skills from install entirely (deleted the skills block from `install.Run`, `install/skills.go`/`_test.go`, `checkSkills`, the dead root `skills/`; dropped `all:skills` from the embed). `plugin/skills/vault-ingest` is the single skill copy; Richie reworked it from real onevault usage (genericised, no hardcoded paths) and that revision is committed. **Install is purely data-side: `.hebb/config.toml`, memory symlink, first index, plus launchd jobs + automation materialise.**

**C done (automation migration):** `automation/` now holds the genericised `run-vault-digest.sh` (daily-digest launchd entrypoint: runs the generator then `hebb index`), `generate-vault-digest.py` (rolling digest to `2-Areas/_DAILY-DIGEST.md`), and `generate-action-review.py` (collates `OPEN-ACTIONS.md` registers to `2-Areas/_ACTION-REVIEW.md` + `.json`, `--owner` highlights "My Actions"). All parameterised by `--vault-root`, no hardcoded paths/names. They embed via `assets.go`, materialise to the data dir, and `VaultJobs` renders all three default jobs (daily-digest, action-review, web). `assets_test.go` pins the embedded filenames to the VaultJobs contract; `scripts/acceptance.sh` runs both scripts against the canary vault (now 40 checks, was 28). `update-engineering-knowledge.sh` was deliberately NOT migrated (OneVault-specific, no job).

**Codex adapter done:** `hebb codex` merges `[mcp_servers.hebb]` into `~/.codex/config.toml` (surgical, idempotent, vault pinned via `env.HEBB_VAULT` + `cwd`); `vault-template/` ships a generic `AGENTS.md`. See `install/codex.go`, `cli/codex.go`.

**Plugin marketplace done:** `.claude-plugin/marketplace.json` at the repo root sources `./plugin`, so the plugin installs persistently via `/plugin marketplace add cizer/hebb` (or a local dir) + `/plugin install hebb@hebb` - no more `--plugin-dir` every session (that's dev-only). `marketplace_test.go` guards it. Repo is going public (Richie's call). Distribution model confirmed: public repo ⇒ anyone can add the marketplace; the binary still needs a channel (below). Other clients (Claude Desktop via `claude_desktop_config.json`, Codex via `hebb codex`) use the MCP server directly, not the plugin.

**Next up (queued):**
- **`hebb reset` (vault teardown) — task #20, not started.** Inverse of install: remove the memory symlink (only if it resolves into the vault), bootout+delete `local.hebb.<slug>.*` launchd plists, remove the `[mcp_servers.<name>]` codex block (add a removal path to `install/codex.go`'s surgical merge), remove the opt-in `.mcp.json`/settings, clear `.hebb/index.db`. KEEP notes + `.hebb/memory` + config by default; `--purge` (guarded) for the lot. Dry-run by default, `--force` to apply; `install.Teardown(opts)` + `cli/reset.go`, hermetic tests. Motivation: live-UAT cleanup is a fiddly manual dance (it's step 7 of the manual UAT instructions). Only ever remove what hebb created.
- **Live UAT (clean room):** manual instructions given to Richie — build current binary to `~/.local/bin/hebb`, `hebb new ~/Documents/UATVault`, doctor/search, load the plugin via `claude --plugin-dir <repo>/plugin`, optional codex/launchd. Surfaced + fixed one bug already (digest/action-review crashed on an `--output` outside the vault; `0d6b775`).
- **Phase 4 Stage 3 release — scaffolded; cut the first tag.** GoReleaser + `release.yml` + `RELEASING.md` are in. To ship: make the repo public, then `git tag v0.1.0 && git push origin v0.1.0` → the workflow publishes the GitHub release with cross-compiled binaries. `go install github.com/cizer/hebb/cmd/hebb@latest` works once public. Enable the Homebrew tap when wanted (create `cizer/homebrew-hebb` + token, uncomment the `brews:` block). Watch the first run for any `goreleaser` schema gripe (not validated locally).
- **Phase 5 cutover** (OneVault, last).

Possible tidy-up: the `Skills` field in `core.VaultConfig` / `DefaultVaultConfig` is now dead config (install ignores it post-B2) — drop it or leave as schema doc. Test vaults: PersonalVault (live hebb vault); OneVault untouched. `spike/hebb-plugin` is gitignored scratch; `plugin/` is canonical. Exercise the full surface against a throwaway dir with `--home`/`--data-dir`/`--launchd-dir`/`--mcp-json`/`--codex-config`.

## Parked ideas

Not committed to a phase; recorded for later.

### Decided: package the Claude surface as a plugin (spike validated 06-06-2026)
Ship a `hebb` Claude Code plugin for the agent-facing layer, wrapping the `hebb` binary which keeps the engine, CLI, and per-vault data (`install`/`new`/`doctor`, config, index, memory).

**Spike result** (throwaway `spike/hebb-plugin`, gitignored): a plugin with `.mcp.json` (`command: hebb`, `args: [mcp]`, `env: HEBB_VAULT=${CLAUDE_PROJECT_DIR}`) + `skills/vault-ingest`, loaded via `claude --plugin-dir`. Confirmed live: the MCP server launches and resolves the opened vault, the skill loads namespaced as `hebb:vault-ingest`, and it needs **zero hebb code change** (hebb already reads `$HEBB_VAULT`). This dissolves the personal>project shadowing problem and the symlink-following question.

**Architecture it settles:** *portable core* = the `hebb` binary + MCP server (agent-neutral; MCP is a standard). *Per-agent adapters* = thin: Claude = this plugin; Codex = a `~/.codex/config.toml` `[mcp_servers]` entry + the workflow as `AGENTS.md` guidance (skills are Claude-only); other MCP clients = their own config.

**Migration work (before Stage 3 release; reshapes install + distribution):**
- Graduate `spike/hebb-plugin` into the repo as the canonical plugin (manifest + `.mcp.json` + skills); a local marketplace or git source for install.
- Slim `install`: drop project-scoped skill-symlinking and per-vault `.mcp.json`/settings MCP wiring (the plugin provides both, user-level); keep `.hebb/config.toml`, first index, memory. Decide whether to keep a per-vault `.mcp.json` as a plugin-less fallback.
- Distribution = binary (brew/npm) + plugin (marketplace), plus a Codex adapter (hebb could emit the `config.toml` MCP entry).
- Remote/cloud caveat: the plugin MCP is local stdio over the local filesystem, so it only works where the binary + vault are present; true remote use needs an HTTP/SSE MCP transport with auth (separate future work).

### Deferred review findings (security/design review, 02-06-2026)
Lower-priority items from the architecture/design/security review. The three high-value fixes (plist XML escaping, web Host-header guard against DNS rebinding, symlink containment in the indexer) are done and on `claude/architecture-design-security-review-B5xbX`. Remaining, in rough priority order:

- **File-size cap on indexing** (`core/index.go`, `core/single.go`): files are read whole into memory and FTS-indexed with no upper bound, so a multi-GB `.md` is a local memory/DoS hazard. Add a sane cap (e.g. skip-and-warn above N MB).
- **Escape `LIKE` metacharacters in `resolvePath`** (`core/context.go`): the `path LIKE '%'+input+'%'` fallback is parameterised (no injection) but `%`/`_` in a title or path act as wildcards, so lookups can resolve to an unexpected note. Escape them for predictable matches.
- **`govulncheck` in CI** (`.github/workflows/ci.yml`): add a standing dependency-vulnerability scan. `staticcheck` is already noted under Phase 4.
- **SHA-pin GitHub Actions** (`.github/workflows/ci.yml`): actions are pinned to major versions; pin to commit SHAs for supply-chain robustness.

Informational (no action unless they bite): single-process-per-vault index assumption (concurrent `serve` + `mcp` rely on SQLite locking, not design - worth a docs note); `Watch` failures are swallowed in `mcp`/`web` (a one-line stderr warning would aid diagnosis); `go.mod` pins an exact patch toolchain (`go 1.26.3`) - relax to `go 1.26` unless deliberate.

### Per-request context interception via Claude Code hooks
Have hebb run on every prompt in a vault session to inject (not rewrite) context before the model responds, as a deterministic "push" complement to the MCP "pull" tools.
- **Mechanism:** a `hebb hook` subcommand wired into the vault's `.claude/settings.json` by `install`. `UserPromptSubmit` fires before the model sees a prompt and its stdout is injected into context; `SessionStart` runs once per session (good for a one-shot orientation load).
- **Why parked:** always-on per-turn injection tends to be net negative (token + noise cost, per-turn latency, partial duplication of the pull tools). The viable shape is gated: a cheap relevance check, or a SessionStart one-shot, opt-in per vault via `config.toml`. Augment only; never silently rewrite the user's prompt.
- **If revived:** prototype `hebb hook` + a `SessionStart` one-shot first, measure token/quality impact, then consider per-turn injection behind a config flag. Confirm the exact hook stdin/stdout/exit-code contract against the Claude Code docs before building.
