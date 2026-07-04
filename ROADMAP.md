# hebb roadmap

The single sequencing document: what to build next and in what order. Written 2026-07-04
from a review of every planning doc in the repo (PLAN.md, BACKLOG.md, METABOLISM.md,
MULTI-VAULT-WEB.md, NOTE-codesigning-fda-persistence.md) plus a deep dive on a live
1,184-note vault. Design rationale stays where it lives: ARCHITECTURE.md for the system,
METABOLISM.md for the memory layer's evidence and experiment protocol. The superseded
trackers carry a banner pointing here. Update this file in place; do not start a new one.

## Where the product is (v0.5.0 + main, 2026-07-04)

Shipped and working:

- **Engine**: FTS5 index with content-hash change detection (`content_changed_at`),
  file watcher, exact link resolution (`target_path` + ambiguity detection),
  `exclude_from_graph`.
- **CLI**: install / new / vaults / unwire, mcp, search, serve, sync, index, update,
  codex, digest, notify, restart-services, doctor (incl. TCC / full-disk-access checks),
  and `audit` with five detectors (dangling_link, ambiguous_link, para_drift, oversized,
  stub).
- **Web**: one global loopback server for all registered vaults, health-first dashboard,
  worklist.
- **Plugin**: marketplace-installable; bundled skills: vault-ingest, vault-gardener,
  ingest-inbox, ingest-meetings, ingest-youtube.
- **Automation**: launchd jobs daily-digest, action-review, update-check, global web;
  digest driven by content-level change, not mtime; per-vault `bootstrap.sh`;
  SessionStart self-provision on Claude Code web.
- **Distribution**: public repo, Apache-2.0, goreleaser releases, install.sh,
  `go install`, plugin marketplace.

Known open defects (verified in code 2026-07-04; status updated same day after the
build session):

- ~~`hebb notify` ignores `$HEBB_VAULT` / cwd~~ — fixed (notify now uses
  `core.ResolveVault`).
- ~~No next-steps output after `new` / `install`; empty search gives no guidance~~ —
  fixed.
- ~~Fresh-vault web verdict shows red "fragmented" with no minimum-size gate~~ — fixed
  (neutral verdict below 10 nodes / 5 edges).
- ~~One concept, three names~~ — fixed: `audit` is the command and config section
  (`[health]` parses as a deprecated alias), "Vault health" is the web phrase,
  "metabolism" is a design-doc term only.
- Ad-hoc signing breaks Full Disk Access across updates (NOTE-codesigning), so a
  `hebb update` can silently kill the scheduled jobs on macOS. **Still open** (needs an
  Apple Developer cert; WS5.1).

## Status after the 2026-07-04 build session

Shipped in one pass (all tested, uncommitted at time of writing): WS1 in full
(naming alignment, same-directory scoped link resolution, P1 bug fixes, `audit_vault`
MCP tool, file-size indexing cap, LIKE escaping, govulncheck + SHA-pinned CI, Go
1.26.4). WS2.1-2.2 (`cooling_project` + `stale_pin` detectors, `hebb themes`),
WS2.4-2.5 (weekly `consolidate` launchd job writing `2-Areas/_CONSOLIDATION.md`,
digest link, `_NOW.md` pin contract + starter, gardener playbooks). WS3.1 (access log,
config-gated `[memory] access_log`, off by default) and WS3.2 (`hebb eval` golden-set
harness). WS4.1-4.3 (series-info contract, ingest-meetings carry-forwards, `agenda`
skill). Most of WS5.3 (CLI/web/docs polish).

Still gated / open, in dependency order:

1. **Enable the access log on the live vault** (`[memory] access_log = true`) to start
   the two-week experiment clock (WS3.3); existing vaults must also add
   `"consolidate"` to their `jobs` list by hand (documented in CONFIG.md).
2. WS2.3 `emerging_bucket` — blocked on the top-30 precision eyeball test on a real
   vault.
3. WS3.3 experiment → METABOLISM Phases 4-5 gate.
4. WS5.1 Developer ID signing (Apple cert), WS5.2 Homebrew tap.
5. WS4.4 calendar-aware surface (blocked on headless calendar auth).
6. Leftover P2s tracked in BACKLOG.md (install-in-plain-folder dead-end, Obsidian
   link fallback, doctor launchd noise, `hebb ingest stage` CLI).

## The through-line

hebb's arc so far is capture, then retrieval, then maintenance. Each stage shipped as
deterministic engine work surfaced through the same loop: index, worklist, agent-confirmed
action. The next stage is **organisation**, then (gated) **memory**:

1. **Capture** (done): ingest skills, vault-ingest.
2. **Retrieval** (done): FTS5 + context graph + MCP.
3. **Maintenance** (done, v1): audit detectors + gardener + dashboard.
4. **Organisation** (this roadmap's headline): a recurring organiser pass that notices
   what themes the daily work keeps returning to, proposes buckets, and keeps the
   front-and-centre surfaces honest.
5. **Memory** (gated): access-driven relevance, consolidation, decay (METABOLISM
   Phases 3-5).

The live-vault evidence for stage 4 is unambiguous. Surfaces owned by a launchd job
(daily digest, action review, open actions) stay current; surfaces owned "by hand or by
sweeps" (`_NOW.md`, project/areas dashboards, meetings index) all rotted within two to
four weeks. And a trivial 30-day link-frequency query over the existing index cleanly
surfaced the vault's true active themes and people, so the organiser is a query over
data hebb already has, not an ML project.

## Principles (hard rules, carried forward)

- Single static binary. FTS5 + links + git only. No embeddings, no vector store.
- The engine computes and **proposes**; every content mutation is human-confirmed
  through a skill.
- No composite "vault health score", ever.
- "Returned by search" is never treated as "used".
- Forget means a reversible move to Archives with a tombstone. Hard delete is in no
  code path.
- Never auto-touch regulated / compliance-tagged content.
- Web stays loopback-only with the Host guard.
- Scheduled jobs are deterministic (Go) under launchd; headless agent runs are blocked
  by macOS TCC, so never design a job that needs one.
- **New: no curated surface without a job.** If hebb (or a skill) creates a dashboard
  or pin-board note, something mechanical must own its freshness or decay.

---

## Workstream 1 — Trust and correctness (now)

Small, certain wins that everything later builds on.

1. **Naming decision** (decide first; it names things the other workstreams create).
   One name across CLI / config / web / README for audit / health / metabolism.
   Recommendation: `audit` is the command and the finding stream (already renamed),
   "vault health" is the human phrase on the web dashboard, and "metabolism" retires to
   a design-doc term. Follow-through: config `[health]` gains an `[audit]` alias with a
   deprecation note, web heading and README align, `health` / `reset` command aliases
   documented as deprecated for a future major.
2. **Scoped link resolution.** Resolve `[[target]]` preferring a same-directory match,
   then a unique-basename match. On the live vault most of the 50 ambiguous-link
   findings are structural basenames (`_SERIES-INFO`, `OPEN-ACTIONS`); this fixes them
   without renaming files and makes every WS2 metric trustworthy.
3. **P1 bug fixes** (all verified open): notify vault resolution; next-steps after
   `new` / `install`; empty-search guidance; fresh-vault "fragmented" verdict gated on a
   minimum node/edge count.
4. **`audit_vault` MCP tool**: read-only worklist over MCP, so agents stop shelling out
   for findings. Follow the naming convention decided in slice 1.
5. **Deferred security items** (from the 02-06 review): file-size cap on indexing,
   escape LIKE metacharacters in `resolvePath`, govulncheck in CI, SHA-pin GitHub
   Actions.

## Workstream 2 — The organiser (headline)

Capture processes forward; nothing runs backwards over the vault to reconsolidate. The
organiser is a weekly, proposal-only pass built from index queries, reusing the existing
audit -> worklist -> gardener loop rather than inventing a new channel.

1. **Temporal audit detectors** (extend `core/health.go`, thresholds in config):
   - `cooling_project`: a `1-Projects/` folder with no `content_changed_at` inside it
     for N days (folder-level para_drift).
   - `stale_pin`: a `_NOW.md` pin past its `review-by:` date (contract in slice 5).
2. **Recurrence report** (`hebb themes`; final name follows WS1.1): trailing-window
   (14 / 30 day) link-target frequency, tag momentum, and co-occurrence over
   `content_changed_at`, using resolved `target_path` only. Read-only, CLI + JSON.
   This is "where do the discussions keep returning" as a first-class query.
3. **`emerging_bucket` detection**: clusters of recent notes that interlink or share
   tags but share no folder or index note; propose a sub-project folder or an additive
   map-of-content. The METABOLISM Phase 4 safety carve-out applies unchanged: additive
   MOC of links plus one line each, never rewritten prose, never regulated content.
   Gate: run the cheap precision test first (top-30 candidates, eyeball on a real
   vault, exactly the METABOLISM dedup protocol). If precision is poor, ship slices
   1-2 and 4-5 without it.
4. **Weekly `consolidate` job** (launchd, deterministic, cloned from the digest job
   pattern): refresh index, run the temporal detectors + themes, write
   `2-Areas/_CONSOLIDATION.md` with at most ~5 ranked proposals, each citing its source
   notes as wiki-links, and link it from the daily digest. vault-gardener learns to
   action consolidation findings. Proposal volume stays tiny by design: the known
   failure mode (METABOLISM's contrarian case) is a proposal list nobody actions.
5. **`_NOW.md` contract**: pins carry a `review-by:` date; the vault-template ships a
   starter `_NOW.md` documenting the convention; ingest skills keep updating pins
   opportunistically; `stale_pin` enforces decay. This is the "no curated surface
   without a job" rule made concrete.

Deliberately independent of WS3's experiment gate: the organiser uses change and link
recurrence only, so its value lands even if the access-signal bet fails.

## Workstream 3 — Memory substrate (cheap now, gated later)

METABOLISM.md remains the spec; this roadmap only sequences it.

1. **Ship Phase 3 now**: the access log (~20 lines, config-gated, `return_count` and
   `followup_count` captured separately from day one). It is pure observation and
   starts the experiment clock; there is no reason to wait.
2. **Build the retrieval eval harness** before any automation mutates at scale: a small
   golden set of (query -> notes-that-should-come-back) pairs, runnable before / after a
   gardening action. METABOLISM names this the single biggest gap in the plan; it is
   also the counter-metric for WS2's gardener actions.
3. **Run the two-week falsification experiment** exactly per METABOLISM (Q1
   discrimination, Q2 confusion matrix, Q3 use-vs-return; decision rule as written).
4. **Gate**: METABOLISM Phase 4 (consolidation scoring) and Phase 5 (decay / forget)
   proceed only if the experiment passes, proposal-only and reversible as specced. If
   it fails, stop: WS1 + WS2 already delivered the durable wins.

## Workstream 4 — Meeting agendas (vault + skills)

Diagnosis from the live vault: meeting prep is retrieval, but agenda-setting is
accumulation, and the accumulated state does not exist yet. Series folders have
`_SERIES-INFO` and fresh `OPEN-ACTIONS`, but standing items live only in prose and
nothing carries "parked for next time" between occurrences.

1. **Series contract**: machine-readable frontmatter in `_SERIES-INFO` (cadence, day,
   attendee links, standing-items list). Ship a `templates/series-info.md` in the
   vault-template; a one-off gardener pass backfills live vaults.
2. **Carry-forwards**: ingest-meetings writes a "Next time" section on each filed
   occurrence and mirrors those items into the series register.
3. **`agenda` skill** (plugin/skills): compose the next agenda from standing items,
   open actions worth chasing, the last occurrence's "Next time", WS2 themes touching
   the series' attendees, and relevant `_NOW.md` pins; then **pre-create the next dated
   note** with the agenda in it, so the post-meeting sweep enriches the same file
   (dedup-then-enrich already handles this).
4. Later, optional: a calendar-aware "today's meetings" surface. Blocked on headless
   calendar auth; stays interactive until that exists.

## Workstream 5 — Reliability, distribution, polish

1. **Developer ID signing + notarisation** (fixes NOTE-codesigning: TCC grants keyed to
   cdhash break on every update). Needs an Apple Developer account; until then the
   doctor full-disk-access warning is the safety net and re-granting after update is a
   documented step.
2. Homebrew tap (optional), `goreleaser-action` bump.
3. **P2 / P3 fold-in from BACKLOG.md**, grouped as capacity filler: CLI polish
   (`hebb version`, help examples, output house style), web accessibility and error
   surfacing, docs drift (template CLAUDE.md / README claims, scaffold vs skill drift,
   dual skill delivery precedence), MCP description alignment. File:line detail stays
   in BACKLOG.md; do not add new items there.

---

## Sequence and gates

| # | Item | Depends on / gate |
|---|---|---|
| 1 | WS1.1 naming decision | nothing; decide before new names exist |
| 2 | WS1.2 scoped link resolution + WS3.1 access log | both small; the log starts the experiment clock |
| 3 | WS1.3 P1 bug fixes | nothing |
| 4 | WS2.1 temporal detectors + WS2.2 themes report | WS1.2 (clean graph) |
| 5 | WS2.4 consolidate job + WS2.5 `_NOW` contract | WS2.1-2 |
| 6 | WS4.1-3 agendas | parallel with WS2; mostly skills/template work |
| 7 | WS2.3 emerging_bucket | precision test passes |
| 8 | WS3.2 eval harness, WS3.3 experiment | two weeks of live Phase 3 data |
| 9 | METABOLISM Phases 4-5 | experiment passes; otherwise stop |
| 10 | WS5 items | as they unblock (signing needs the Apple cert) |
| — | WS1.4-5, WS5.3 | fill capacity between slices |

## Non-goals (parked deliberately)

- Per-turn prompt-hook context injection (PLAN.md's parked rationale stands).
- Embeddings / vector store; cloud or remote MCP transport; auth on the web UI.
- Auto-acting on proposals; headless auto-archive.
- A single vault-health score.

## Doc map

- **ROADMAP.md** (this file): the plan. One plan; update in place.
- **ARCHITECTURE.md**: system design.
- **METABOLISM.md**: memory-layer design, evidence, and experiment protocol (WS3 spec).
- **BACKLOG.md**: file:line detail behind WS1 / WS5 items; frozen for new entries.
- **PLAN.md** (untracked): historical build tracker for the Go rewrite; complete.
- **MULTI-VAULT-WEB.md**: historical design record; all slices shipped.
- **CONFIG.md / TESTING.md / RELEASING.md**: reference, unaffected.
