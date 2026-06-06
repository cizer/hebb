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
- ✅ symlink `skills/*` from the data dir into each vault's `<vault>/.claude/skills` (project-scoped, so vaults stay isolated and hebb never touches the global `~/.claude/skills`; conflict-safe, repoints stale links). doctor flags skills shadowed by a same-named personal skill (Claude precedence: personal > project)
- ✅ render launchd jobs (`local.hebb.<slug>.<job>`, `plutil`-valid); `--launchd`/`--load` (launchctl bootstrap, dry-run preview); web job built in, automation jobs gated on their script existing
- ✅ symlink memory into `~/.claude/projects/<project-slug>/memory` (Claude Code path-slug exact), sourced from `<vault>/.hebb/memory` so agent memory syncs with the vault but is hidden from Obsidian and excluded from the index; build the first index
- ✅ `hebb doctor` — read-only health check (config, .mcp.json, index, settings, skills, memory, launchd), exits non-zero on failure

Remaining before a live install is meaningful (content migration, not tool work):
- move the real skills `~/.claude/skills/*` and automation `vault/bin/*` into `skills/` and `automation/` (today placeholders); they then embed into the binary and install materialises + links/renders them
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
  stdio (initialize, tools/list, tools/call) → plutil-lint plists (macOS). 26 checks.
  Automates the manual UAT; `--load` not run in CI. Runnable locally too.
- ⬜ **Stage 3 — release (the deploy):** on a version tag past acceptance, publish a
  GitHub release with binaries, bump a Homebrew tap formula, optional npm.
- ⬜ pre-commit/pre-push hooks mirroring Stage 1 (consistent local + CI); optional staticcheck; fuller README.

### Phase 5 — Cutover ⬜
- repoint the live machine to `hebb`, relocate memory to the synced location, remove `vault/bin` scripts, retire `onevault-mcp`

## Resume point

**Next: migrate automation scripts, then Phase 4 Stage 3 (release).** Phases 0-3 done. Skills are sorted for the vault layer: `vault-ingest` is the one generic, project-scoped vault skill (`build` and `publish-artifact` dropped as non-vault function), so a fresh vault's `doctor` reports `skills 1/1`. Still placeholders: `automation/` (daily-digest, action-review) — those launchd jobs stay gated until their scripts land. Nothing is wired into the live setup; `onevault-mcp` serves the live vault until the Phase 5 cutover. Run `new`/install/doctor against a throwaway dir with `--home`/`--data-dir`/`--launchd-dir` to exercise the full surface safely.

**Open question:** Claude Code precedence is personal > project, and project skills install as symlinks into `<vault>/.claude/skills`. Whether Claude Code follows symlinks there is undocumented — verify by opening Claude in a hebb vault and checking the skill loads; if not, switch install to copy skill files instead of symlinking.

## Parked ideas

Not committed to a phase; recorded for later.

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
