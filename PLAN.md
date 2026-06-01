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

### Phase 2 — `hebb install` ⬜  (next)
Wire a vault into the machine, idempotently:
- init `.hebb/config.toml`; generate the project-scoped `.mcp.json`
- register the MCP / lay down Claude settings + permissions; symlink `skills/*` into `~/.claude/skills`
- render + bootstrap launchd jobs (labels per vault)
- symlink the memory dir into `~/.claude/projects/<vault-slug>/memory`; build the first index
- `hebb doctor` — health check of vault + install

### Phase 3 — `hebb new` + vault-template ⬜
- `vault-template/`: PARA skeleton, baseline `CLAUDE.md` (generic, split from the personal one), note templates, memory seed
- `hebb new <path>` scaffolds a fresh vault then installs against it; from-scratch test (zero personal data)

### Phase 4 — Package + distribute ⬜
- Homebrew tap and/or npm, GitHub Actions CI, fuller README

### Phase 5 — Cutover ⬜
- repoint the live machine to `hebb`, relocate memory to the synced location, remove `vault/bin` scripts, retire `onevault-mcp`

## Resume point

**Next: Phase 2, `hebb install`.** Nothing is wired into the live setup yet; `onevault-mcp` still serves Richie's vault until the Phase 5 cutover.
