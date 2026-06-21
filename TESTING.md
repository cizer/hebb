# hebb test strategy

How hebb is tested today, and the two-stage deployment pipeline it feeds.

## Principles

1. **Test the real dependency, not a mock.** The engine is tested against a real
   SQLite FTS5 database and a real filesystem, because the index *is* the
   product. Mocks would hide exactly the FTS5/parser behaviour we care about.
2. **Hermetic.** Every test uses temp dirs (vault, `HOME`, data dir,
   LaunchAgents) and no network. Nothing writes to the real home. This is why
   install is parameterised by its target directories.
3. **Test the surfaces callers actually use.** The MCP tool output, the CLI, and
   the web API are tested at their boundary, not just the functions beneath
   them. Both bugs found during UAT lived at a surface/seam, not in a unit.
4. **Consistent locally and in CI.** The same commands run in both. The
   acceptance harness is a script you can run on your machine, not CI-only.
5. **Fail fast.** Fastest checks first (build, fmt, vet, then tests); the slow,
   production-like acceptance stage runs only once the fast stage is green.
6. **Secure by default.** No secrets in the repo; release credentials live only
   in CI secrets.
7. **Migrate the index in place; never break an old vault.** A vault's
   `index.db` is the one piece of on-disk state coupled to the binary version.
   Every index-schema change MUST ship an idempotent migration in `OpenDB`
   (see `migrateLinksTargetPath`, `migrateNotesContentChange`): check for the
   column/table via `PRAGMA table_info`, add it if absent, backfill on the
   upgrade path only, and no-op on a fresh DB and on every later open. A new
   binary then upgrades an old vault lazily the first time it opens the index,
   with no forced rebuild. There is no schema-version guard that would catch a
   *forgotten* migration, so a missing one is a silent break: a migration test
   that opens a fixture DB built on the prior schema is required for any schema
   change. A future `PRAGMA user_version` guard that fails loudly on an
   un-migrated DB would harden this.

## Current layers

The whole suite (`go test ./...`) is ~73 tests and runs in ~5s, cold. All
hermetic.

| Layer | Where | Exercises | Real deps |
|---|---|---|---|
| Unit | `core` (parser, config, slugify), `install` (mcpjson, settings merge, bootstrap dry-run, VaultJobs gating), `launchd` (render) | Pure logic and rendering | none |
| Integration (engine) | `core/index_test`, `context_test`, `watch_test`, `single_test` | Temp vault → `FullReindex` → real SQLite FTS5 → search / context / stats / incremental watch | SQLite FTS5, filesystem |
| Surface | `mcp/server_test` (tool output + cross-tool tag consistency), `cli/*_test` (install/doctor via cobra), `web/server_test` (HTTP), `install/*_test` (wiring, conflict-safety, idempotency) | The strings/exit codes/files callers and Claude consume | filesystem |
| Embed contract | root `plugin_test`, `assets_test` | The plugin manifest/`.mcp.json`/skill, and that the binary embeds the automation scripts VaultJobs depends on | embedded FS |
| Static | `go vet`, `gofmt`; launchd plists validated with `plutil -lint` (macOS only, skipped elsewhere) | Vet, formatting, plist validity | toolchain |
| Acceptance | **scripted** (`scripts/acceptance.sh`, 40 checks); runs in CI Stage 2 and locally | The built binary end to end | OS, real-ish env |

Counts as of writing: core 15, install 32, launchd 6, cli 8, web 2, mcp 5, root (plugin + assets) 5.

## Remaining gaps

The original gap (a manual acceptance layer, no race or cross-platform coverage)
is closed: `scripts/acceptance.sh` drives the built binary on macOS and Linux in
CI, and Stage 1 runs `-race` plus a cross-compile matrix. Stage 3 release is
wired as a `release` job in `ci.yml`, gated on `needs: [build, acceptance]` so it
publishes only after both stages pass. What is still open: `staticcheck` /
`golangci-lint` / `govulncheck` are not yet wired in.

## Pipeline: two stages

Continuous deployment: green `main` produces a candidate artifact; a tagged
commit that passes both stages is released.

### Stage 1 - Build (fast tests)

Gate on every push / PR. Target wall-clock: under ~2 minutes.

- `go build ./...`
- `gofmt -l .` (fail if anything is unformatted)
- `go vet ./...`
- `go test ./...` (unit + integration + surface; ~5s)
- `go test -race ./...` (the watcher and `serve` run concurrently with SQLite;
  the race detector guards those paths)
- optional: `staticcheck` / `golangci-lint`
- cross-compile the release artifacts (matrix below) to prove they build

Properties: fastest checks first, zero manual input, blocking, same as the local
pre-commit/pre-push hooks.

### Stage 2 - Acceptance (production-like)

Runs only after Stage 1 is green, on the target OSes (macOS and Linux runners),
against the **natively built binary**, not `go test`. It is `scripts/acceptance.sh`
(runnable locally and in CI) and automates the UAT.

Against a throwaway fixture vault and a temp `HOME`, it:

1. puts the built `hebb` on `PATH`
2. `hebb install --vault V --home H --data-dir D --launchd --launchd-dir L` → assert exit 0; default install is data-side (`.hebb/config.toml`, index, materialised automation, memory link, rendered plists) and writes **no** per-vault `.mcp.json`. A second run with `--mcp-json` asserts the plugin-less `.mcp.json` + settings are written on demand.
3. `hebb doctor` → assert health (expected ok/warn set; no skills check, mcp.json reports plugin mode)
4. `hebb search <canary>` → assert the known notes come back and the excluded dir is skipped
5. `hebb serve --port <free>` → `curl /api/stats` and `/api/search` → assert the JSON
6. `hebb mcp` over stdio → JSON-RPC `initialize`, `tools/list` (the 5 tools), `tools/call search_vault` (canary) and `get_context_for_topic` (tag consistency) → assert responses
7. macOS only: `plutil -lint` the rendered plists
8. run the automation against the fixture: `hebb digest` (the Go launchd entrypoint: index refresh + content-driven digest note; assert a canary note appears and the retired Python generator/wrapper are not materialised) and the materialised `generate-action-review.py` (action review + JSON, owner/overdue flags) → assert their output

Notes: `--load` (launchctl bootstrap) is **not** run in CI (no GUI session, and
it would mutate the runner); acceptance renders and validates plists instead.
A darwin binary cannot run on a Linux runner, so each OS runner builds and tests
its own binary; cross-compiled release artifacts are a separate build step.

### Stage 3 - Release (the deploy)

On a version tag, once acceptance is green: publish a GitHub release with the
built binaries, bump the Homebrew tap formula, optionally publish to npm. That
is the "deploy a new artifact" step.

## Build matrix

`darwin/arm64`, `darwin/amd64`, `linux/amd64`, `linux/arm64`. macOS is primary
(launchd); Linux proves portability and gives cheaper CI runners for the fast
stage.

## Local mirror

The cloud stages mirror what runs locally, so CI never surprises you:

| Local layer | Command |
|---|---|
| Pre-commit | `gofmt`, `go vet` |
| Pre-push | `go test ./...` |
| Full (pre-release) | `go test -race ./...` + the acceptance harness |
| Cloud CI | Stage 1 then Stage 2 above |
