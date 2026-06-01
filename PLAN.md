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
- ✅ symlink `skills/*` into `~/.claude/skills` from the data dir (conflict-safe, repoints stale links)
- ✅ render launchd jobs (`local.hebb.<slug>.<job>`, `plutil`-valid); `--launchd`/`--load` (launchctl bootstrap, dry-run preview); web job built in, automation jobs gated on their script existing
- ✅ symlink memory into `~/.claude/projects/<project-slug>/memory` (Claude Code path-slug exact); build the first index
- ✅ `hebb doctor` — read-only health check (config, .mcp.json, index, settings, skills, memory, launchd), exits non-zero on failure

Remaining before a live install is meaningful (content migration, not tool work):
- move the real skills `~/.claude/skills/*` and automation `vault/bin/*` into `skills/` and `automation/` (today placeholders); they then embed into the binary and install materialises + links/renders them
- distribute the binary so `command: hebb` resolves on PATH (Phase 4); the assets are already standalone (embedded), so this is the only remaining standalone gap

### Phase 3 — `hebb new` + vault-template ⬜
- `vault-template/`: PARA skeleton, baseline `CLAUDE.md` (generic, split from the personal one), note templates, memory seed
- `hebb new <path>` scaffolds a fresh vault then installs against it; from-scratch test (zero personal data)

### Phase 4 — Package + distribute ⬜
- Homebrew tap and/or npm, GitHub Actions CI, fuller README

### Phase 5 — Cutover ⬜
- repoint the live machine to `hebb`, relocate memory to the synced location, remove `vault/bin` scripts, retire `onevault-mcp`

## Resume point

**Next: migrate skill + automation content into the repo (then Phase 3 `hebb new`).** The `hebb install`/`doctor` mechanism is built and tested; `skills/` and `automation/` still hold placeholders, so a real install links/renders nothing for them yet. Nothing is wired into the live setup; `onevault-mcp` still serves Richie's vault until the Phase 5 cutover. Run install/doctor against a throwaway vault with `--home`/`--asset-root`/`--launchd-dir` to exercise the full surface safely.
