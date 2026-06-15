# hebb vault metabolism — build and validation guide

A staged plan for turning hebb from a capture-and-search engine into a vault with a
metabolism: capture (exists) plus consolidation and decay (the missing middle). The
plan deliberately ships the certain, zero-risk wins first and gates the seductive,
high-blast-radius ones behind a single experiment that decides whether the whole
memory layer rests on signal or noise.

Design provenance: a 16-agent research and design sweep (neuroscience of consolidation,
forgetting curves, agent-memory architectures, graph-health metrics, PKM maturity,
context cost, dashboard design, existing tooling) plus an adversarial critique. This
guide is the converged, code-checked output, not the raw findings.

## The bet in one paragraph

Five of the six original asks (sleep consolidation, health visualisation, dashboard,
oversized-file splitting, forgetting) converge on the same missing substrate: two
numbers per note. **Retrievability** ("is this still warm?", decays with disuse, spikes
on access) and **stability** ("is this durable?", slow-moving, grows with reuse, seeded
by PARA folder). That pairing is Bjork and Bjork's two-strength model, and the quadrant
it produces is the real answer to "durable vs transitory": a note can be ice-cold and
still durable (the regulatory definition untouched for a year), which a single
last-modified date cannot distinguish from junk. But the whole edifice depends on an
access signal hebb currently throws away, and on the unproven assumption that "returned
by search" means "used". So the plan is: ship the parts that need no access signal now,
turn on access logging as a pure observation, and run a two-week experiment before
building a single line of scoring, consolidation, or decay on top of it.

The same "mtime is an unreliable change signal" thesis has a separate, already-shipped
consequence: the **daily digest**. It used to choose which notes to report purely by
filesystem mtime, so a vault-wide find/replace, a sync client, or a restore that
rewrote bytes either dropped genuinely-edited notes out of the window (mtime bumped
past it) or flooded the next digest with the whole touched set (every file restamped).
The fix is the same content-level substrate this plan rests on: each note now carries a
content hash and a `content_changed_at` watermark in the index, and `hebb digest`
reports notes whose content changed since the last run, not notes whose mtime moved.
A future access log (Phase 3) and the digest both read the index as the system of
record for change, not the filesystem clock.

## Sequencing

| Phase | What | Risk | Gate |
|---|---|---|---|
| **0** | Exact link resolution (resolved `target_path` on the links table) | Low | none — foundational |
| **1** | v0 deterministic linters: dangling link, PARA drift, oversized flag | None | none — ship this week |
| **2** | Structural health dashboard on `hebb serve` (orphans, islands, k-core), worklist-first | Low | needs Phase 0 |
| **3** | Access log as silent observer (~20 lines, log only, no scorer) | Low | none — ship alongside 1/2 |
| **EXP** | The two-week falsification experiment | None | needs Phase 3 deployed for 2 weeks |
| **4** | Consolidation ("sleep") pass, proposal-only | Medium | EXP passes |
| **5** | Decay / forget, proposal-only and reversible | High | EXP passes, Phase 4 trusted |

Everything stays **FTS5 + links + git only**. No embeddings, no vector store: the
single static binary is a load-bearing distribution property (see ARCHITECTURE.md), and
half the tempting borrowed mechanisms (similarity clustering, novelty scoring) smuggle
in a vector store. Resisting that is the discipline.

---

## Phase 0 — Exact link resolution (foundational)

Every graph metric you want (orphans, connected components, k-core coreness as the
"durable vs transitory" axis) sits on top of the link graph. Today that graph is fuzzy:
[the links table stores raw unresolved targets](core/db.go:39) and resolution is a
[substring `LIKE` match](core/context.go:36) (`n.path LIKE '%' || l.target || '.md' OR
n.title = l.target`) that can multi-resolve or mis-resolve. Graph metrics on top of an
ambiguous graph are wrong in ways invisible on a dashboard, and they would gate archive
decisions in Phase 5. Fix the graph first.

**Do:**
- At index time, resolve each `[[target]]` to a canonical note path once and store it.
  Add a nullable `target_path` to the `links` table (NULL = dangling). Keep the raw
  `target` for display and re-resolution.
- Detect ambiguity: if a target matches more than one note, record that (it is both a
  data-quality finding for the linter and a correctness fix for the metrics).
- Reuse the existing resolution intent from [outgoing()](core/context.go:33), but make
  it exact (prefer exact `title` or exact basename match over substring `LIKE`).

**Payoff:** this single change makes the dangling-link detector exact (NULL `target_path`
= dangling), and makes every Phase 2 metric trustworthy.

---

## Phase 1 — v0 deterministic linters (ship this week)

Pure deterministic facts over data already indexed. Zero judgment, zero data-loss risk.
These are the certain wins and should be live and dogfooded before a line of
consolidation or decay is written.

Home: engine in `core/` (new `core/health.go`), surfaced as a read-only `hebb health`
CLI (model it exactly on [hebb doctor](cli/doctor.go): "read-only, repairs nothing,
exits non-zero on breach") and as derived conclusions on the existing `vault_stats` MCP
tool. No skill needed for detection.

**Detectors:**
1. **Dangling link** — a links row whose `target_path` is NULL after Phase 0. Output:
   source note, raw target, suggested fix (nearest match if any).
2. **PARA drift** — a note under `1-Projects/` whose `status` is done/closed/complete,
   or untouched beyond `project_stale_days`. Read `status` from the
   [frontmatter column](core/db.go:36) (confirm its stored format in `core/parser.go`).
3. **Oversized** — body over `size_threshold` tokens (use `len(body)/4` as the token
   heuristic, no tokenizer dependency) **AND** containing 3 or more substantial H2/H3
   sections. Size alone is a signal, not a law: a long single-topic reference note is
   fine; a long multi-topic note is the split candidate. Count headings from the raw
   file (the indexed `body` is markdown-stripped — confirm in `core/parser.go`).

**Thresholds** (`project_stale_days`, `size_threshold`, etc.) live in
`.hebb/config.toml`, never hardcoded.

**Defer:** near-duplicate / dedup detection. "Two notes about the same entity" via
bm25+title+tag overlap is fuzzy judgment in a deterministic costume and will flood the
worklist with false positives (shared vocabulary is not the same topic). Before building
it, run the cheap precision test below.

### Cheapest test for dedup (one afternoon, before building it)
Throwaway script against the real ~1000-note vault: for each note, run the existing
`Search()` on its title plus top terms, group mutual top-N hits into candidate clusters,
dump the top 30 to stdout. Eyeball them. If 20+ of 30 are real near-duplicates worth
merging, the FTS-only approach is validated and dedup ships. If it is mostly noise, cut
dedup from v0; the other four detectors still deliver the linter.

---

## Phase 2 — Structural health dashboard

Extend [hebb serve](web/server.go) from search-only into a worklist-first dashboard.
The landing view is a **ranked do-this-now queue** (each row wired to propose an action),
not a wall of charts. Steal CodeScene's hotspot model and the SRE actionability test: if
a tile triggers no action, delete it.

**Metrics** (all over the Phase-0-cleaned graph, no timestamps needed):
- **Orphans / leaves** — degree 0 or 1, but flagged **only** when in a connective folder
  (`2-Areas/`, `3-Resources/`) and older than N days. Orphans in `Journal/`, `Notes/`,
  `4-Archives/` are expected, encoded as a rule not a flag.
- **Connected components** (union-find) — report the giant-component ratio and any small
  islands outside Archives as the highest-value gardening target.
- **k-core coreness** (iterative degree-peel, O(V+E)) — the structural durable-vs-
  transitory axis that needs no timestamps. High coreness = deeply woven; coreness 0-1 =
  periphery.
- **Oversized** — from Phase 1.

**Surface:** `hebb health --json` (CLI) + `/api/health/worklist` and
`/api/health/metrics` on the web server. Keep the existing loopback bind and
DNS-rebinding Host guard: the vault is personal and GDPR-relevant data and must never
bind wide.

**Hard rule — no single "Vault Health Score".** A composite scalar is the textbook
Goodhart trap and is actively dangerous once an autonomous gardener can optimise it (it
would shred good notes to lower average size). Show a **vector** of named sub-scores,
each paired with a counter-metric (median note size DOWN paired with retrieval success;
notes-archived paired with re-retrieval-of-archived as a "we forgot too much" error
signal). Lagging tiles trend down as the vault gets healthier; never show total-note-count
or total-words (hoarding-incentive vanity metrics).

**Validation:** run `hebb health` against the real vault and eyeball whether the
coreness ranking actually puts your known durable reference notes at high coreness. If it
mis-ranks them as periphery, the structural axis is wrong (likely a Phase 0 resolution
bug) and no later access logging saves it.

---

## Phase 3 — Access log as a silent observer (~20 lines, no scorer)

This is the keystone: the one cheap write that the whole memory layer depends on. Build
**only the log**. No scoring, no decay, no dashboard wiring yet.

**Schema** (new table in the gitignored `index.db`, so it never touches markdown, never
enters `notes_fts`, never hits git):
```sql
CREATE TABLE IF NOT EXISTS access_log (
  path TEXT PRIMARY KEY,
  last_accessed REAL NOT NULL,
  access_count INTEGER NOT NULL DEFAULT 0,
  -- optional, for the use-vs-return distinction (see below):
  return_count INTEGER NOT NULL DEFAULT 0,   -- times surfaced in a result set
  followup_count INTEGER NOT NULL DEFAULT 0  -- times expanded/read after surfacing
);
```

**Instrumentation point (the correction):** not `refresh()`. `refresh()` runs before the
query and has no result paths. Write the access bump in each handler **after** the
result set is built, for every path surfaced:
- [search_vault handler](mcp/server.go:69) — after `core.Search` returns
- [expand_context handler](mcp/server.go:95) — after `core.ExpandContext` returns
- [get_context_for_topic handler](mcp/server.go:117) — after `core.GetContextForTopic`
  returns

**Capture the use-vs-return distinction from day one.** "Returned by search" is not
"used" (the agent pulls 15 notes and cites one). Record `return_count` for every path
that appears in a result set, and `followup_count` for the stronger signal: a path that
is the seed of a later `expand_context` call, or appears in a result and is then the
subject of a follow-up read. You need both columns to test, in the experiment, whether
return frequency is just noise.

**Keep the write cheap.** Every read becomes a write contending on a single connection
([MaxOpenConns(1)](core/db.go:15) + WAL). Use one `INSERT ... ON CONFLICT(path) DO UPDATE`
per surfaced path, batched in one transaction per handler call. Gate it behind a
`.hebb/config.toml` flag so the cost is measurable and reversible. Treat the per-read
cost budget as the same order as `RefreshChanged`.

**State durability note (resolve before the experiment matters long-term):** `index.db`
is gitignored and rebuilt on demand, so `access_log` is lost on a full reindex unless
carried by `path` key, and a renamed note loses its history. For the two-week experiment
this is tolerable (no full reindex mid-window); for production scoring it is a
correctness landmine. Decide later whether access state survives reindex (keyed by path)
and what "last accessed" means across synced machines (cold on the laptop, hot on the
desktop). Do not solve it now; just do not let a full reindex wipe the experiment.

---

## THE ANALYSIS — the two-week falsification experiment

This is the decision point. It costs ~20 lines of logging plus a read-only query, and it
tells you whether to build the entire scoring / consolidation / decay layer or to stop.
Do **not** build the scorer first.

### Protocol
1. Deploy Phase 3 (log only). Use the vault normally through Claude for **two weeks** of
   real agent usage. Change nothing else.
2. Hand-label a ground-truth set: pick **30 notes you know are durable** (key
   Area/Resource references you would be upset to lose) and **30 you know are transitory**
   (old Journal entries, one-off captures). Record the labels in a scratch file.
3. Run a read-only query/notebook over `.hebb/index.db` answering three falsifiable
   questions. No file moves, no writes to notes.

### The three questions

**Q1 — Discrimination.** Does `access_count` actually separate notes, or is it flat /
dominated by a handful of hub notes returned by everything?
- Compute the distribution of `access_count`. Look at the share held by the top 10 notes.
- *Fail signal:* a few hubs absorb most accesses and the long tail is ~uniform. That
  means "returned" is mostly query-breadth noise, and the proxy is dead on arrival.

**Q2 — Durable-vs-transitory confusion matrix.** On the 60 labelled notes, compute a
minimal dry-run quadrant and check misclassification.
- Durability proxy (no access needed): PARA folder prior + link degree (from the Phase 0
  resolved graph) + optional human `importance`.
- Retrievability proxy: `R = (1 + 0.2 * days_since_access / S)^(-1)`, with `S` seeded by
  folder (e.g. 365d for Areas/Resources, 30d for Notes, 7d for Journal).
- Quadrant: high-durability + low-R = DURABLE-COLD (must be protected, never a forget
  candidate); low-durability + low-R = TRANSITORY (the only archive candidate).
- *Fail signal:* any of your 30 known-durable notes land in TRANSITORY. If a single
  last-modified heuristic would archive your most valuable rarely-touched notes, the
  forgetting capability is unsafe and must not ship.

**Q3 — Use-vs-return gap.** Compare `return_count` against `followup_count` on the
high-access notes. Spot-check: are the highest-access notes ones the agent genuinely
worked with, or just lexical matches it ignored?
- *Fail signal:* `followup_count` is near zero while `return_count` is high across the
  board. That confirms "returned != used" and means stability must be driven by the
  stronger signal (followup / cite / edit), never by raw returns, or the signal inverts
  and protects your worst (keyword-dense, match-everything) notes.

### Decision rule

| Outcome | Action |
|---|---|
| Q1 flat/hub-dominated | Access is noise. Ship **only** Phase 1+2 structural metrics. Abandon scoring/decay. |
| Q1 passes, Q2 misclassifies durable notes | The proxy or the S-priors are wrong. Retune priors and re-test; do **not** ship decay yet. |
| Q1 passes, Q3 shows return != use | Drive stability from `followup_count` only, not `return_count`. Re-test before Phase 4/5. |
| All three pass | The substrate is real and differentiated (no Obsidian plugin does this). Proceed to Phase 4, still proposal-only. |

A second, even cheaper sanity check supports the same decision: dry-run two ranked
forget-candidate lists, one by mtime-only and one by `(stability, access-driven R)`. If
they are near-identical, access logging buys nothing and you ship the simpler mtime
version. If they diverge specifically because mtime-only flags durable references that the
access-aware version spares, the two-score model has earned its keep.

---

## Phase 4 — Consolidation ("sleep") pass — gated, proposal-only

Only after the experiment passes. A launchd job (cloning the existing
`generate-action-review.py` pattern: read the index, write one review note, mutate
nothing else) that clusters overlapping recent `Journal/`/`Notes/` entries via
FTS + shared tags + the link graph (no embeddings), and emits a ranked
`_DREAM-REVIEW.md` + `.json` worklist of consolidation candidates, each citing sources as
wikilinks. The actual merges live in a generic `vault-gardener` skill, human-confirmed.

**Critical safety carve-out:** the consolidation gist must be an additive, clearly-labelled
**map-of-content of links plus one line each**, not rewritten prose. A prose "gist" that
lands in `2-Areas/`, earns high strength, and outranks its sources is the worst failure
mode: confidently wrong synthesis the agent cites while the veridical originals get
downscaled. Never downscale sources in the same pass that creates a synthesis. And **never
auto-consolidate anything tagged regulated/compliance** — for a Kansspelautoriteit / UKGC
regulated vault a garbled participant-facing rule is a live compliance hazard. Reserve
prose synthesis for explicit human promotion.

---

## Phase 5 — Decay / forget — gated, reversible forever

The riskiest ask, sequenced last because its blast radius is highest. Only the
low-stability **AND** low-retrievability quadrant is ever a candidate, hard-gated so
high-coreness or durable-folder or human-flagged-important notes are mechanically exempt.

- **"Forget" means downgrade, never delete.** Drop from default search ranking, then move
  to `4-Archives/` with a frontmatter tombstone (`archived_on`, `archived_reason`,
  `prior_path`), preserving body and backlinks (the Anki "suspend" model). git history and
  the Archive folder are the floor. Hard `rm` is in no code path.
- **Propose, never auto-act.** The job writes `_FORGET-REVIEW.md`; file moves are
  human-or-agent-approved. Note: Stage-4 headless auto-archive is **not currently
  supported** (doctor warns about it), so any "autonomous forget" story is vapour until
  that stage exists. Default is propose-only, full stop.
- **Counter-metric against over-forgetting.** Instrument re-retrieval of archived notes:
  if the agent searches for and needs something the pass archived, log it and surface it
  as a "we forgot too much" error. Never give the pass a single scalar to optimise.
- **EU AI Act framing:** demotion proposals that affect retained knowledge are
  decision-support; a human makes and documents the final archive decision. The worklist
  is framed as input, not action.

---

## Hard nevers (safety rails, all phases)

- No single composite "Vault Health Score" anywhere an autonomous pass can optimise it.
- No embeddings / vector store: FTS5 + links + git only.
- "Returned by search" is never treated as "used" for stability; require the stronger
  followup/cite/edit signal.
- "Forget" is always a reversible move to Archives with a tombstone, never a delete.
- No auto-consolidation or auto-forget of regulated/compliance-tagged content.
- Every content mutation is human-confirmed; the engine and jobs compute and propose only.

## Open landmines to resolve before Phases 4-5

- **No retrieval-quality eval harness.** You would be automating edits to a retrieval
  system you cannot score. Build a small fixed set of `(query -> notes-that-should-come-back)`
  golden pairs and run it before/after a gardening action, or the gardener works blind and
  the counter-metrics are unfalsifiable. This is the single biggest gap in the whole plan.
- **Per-note state across reindex / rename / sync** (see Phase 3 note): decide where
  durable access/stability state lives so a full reindex does not wipe it and a synced
  multi-machine setup does not decay inconsistently.
- **Review-throughput economics.** A propose-only system whose proposals are never
  actioned becomes a guilt-list; an agent that rubber-stamps to clear the queue silently
  reintroduces auto-mutation risk. Decide who reviews and at what cadence before turning on
  a nightly proposal generator.

## The contrarian case (keep it honest)

The strongest argument against most of this: for a 1000-note single-author vault, the
sleep/decay apparatus may be a solution in search of a problem, and "forgetting" is partly
an aesthetic borrowed from biology. The certain wins are Phases 0-2 (linters + a clean
graph + a worklist dashboard). The prediction worth taking seriously: if the sleep pass
ships, it runs a handful of times, generates proposals nobody actions, and gets quietly
disabled, while the broken-link linter runs forever. The plan's defence is the experiment:
Phases 0-3 are cheap and certain regardless, and the experiment forces decay to earn its
complexity against real data before any of it is built. If the experiment fails, you have
spent ~20 lines, not a scorer plus a dashboard plus two jobs, finding out.

## Checklist

- [x] Phase 0: resolve link targets at index time, add `target_path` to `links`, detect ambiguity (incl. incremental-path fix so inbound links re-resolve)
- [x] Phase 1: `core/health.go` + `hebb health` CLI: dangling-link, PARA-drift, oversized detectors
- [ ] Phase 1: dedup precision test (throwaway script, top-30 eyeball) before building dedup
- [x] Phase 2: orphans / components / k-core in `core/health.go`; worklist-first panel on `hebb serve`
- [ ] Phase 2: validate coreness puts known-durable notes at high coreness
- [ ] Phase 3: `access_log` table + writes in the three MCP handlers (post-result), behind a config flag
- [ ] Phase 3: capture `return_count` and `followup_count` separately
- [ ] EXP: run the vault for two weeks; hand-label 30 durable + 30 transitory
- [ ] EXP: answer Q1 (discrimination), Q2 (confusion matrix), Q3 (use-vs-return); apply the decision rule
- [ ] Gate: only proceed to Phase 4/5 if the experiment passes; keep everything proposal-only and reversible
