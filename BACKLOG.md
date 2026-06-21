# hebb product-review backlog

Follow-ups from the full product review (quality, consistency, cohesion,
usability) on 21-06-2026. Batch 1 (the P0 consistency sweep) is done and not
listed here. Each item notes where it lives and why it matters. File:line
references were accurate at review time; re-confirm before acting.

Priority key: **P1** ship soon, **P2** worthwhile, **P3** polish.

## Batch 1 (done)

Single-line error reporting (`SilenceErrors`); renamed-command output now matches
the command (`unwire`, `audit`); picker prompt no longer collides with the
install report; `install`/`new` `--help` matches the skills-install behaviour;
ARCHITECTURE/READMEs/manifests now describe the bundled skills accurately
(capture plus the `vault-gardener` maintenance skill) instead of naming a single
one; dead `PLAN.md` link removed from TESTING.md and RELEASING.md.

## Batch 2: first-run and CLI polish

- **P1 — No "next steps" after `new`/`install`.** The report ends at
  `index N notes indexed` with no call to action. Add 2-3 lines (try
  `hebb search`, `hebb serve`, open in Claude / install the plugin).
  `cli/install.go:148-152`.
- **P1 — Empty search gives no guidance.** `hebb search foo` prints only
  `(0 results)`, exit 0. Add a distinct empty state (broaden the term, or
  `hebb index` to rebuild). `cli/root.go:128-134`.
- **P1 — `notify` ignores `$HEBB_VAULT` and cwd (real bug).** `resolveVaultPath`
  returns only `--vault`, so a headless `hebb notify` in a `$HEBB_VAULT`-resolved
  vault silently finds no `[notify]` config. Resolve the vault like every other
  command (`core.ResolveVault`). `cli/notify.go:91-96`.
- **P2 — "0 notes indexed" on a clean re-install reads like failure.** Phrase as
  `up to date (N notes)` or `N notes (0 changed)`. `cli/install.go:152`.
- **P2 — `hebb install` in a plain notes folder dead-ends.** It errors "create
  one with `hebb new`", but `new` refuses a non-empty dir; the only route is the
  non-obvious `--vault .`. Either adopt the cwd as the vault, or name the real
  escape hatch in the error. `core/config.go:48`, `install/scaffold.go:69`.
- **P2 — `doctor` warns about launchd when it was never installed / off-macOS.**
  A fresh non-macOS `new` warns `0/4 plists ... missing`. Make a never-installed
  optional feature silent or `ok`, matching doctor's own "never-wired is silent"
  posture. `install/doctor.go:471,507`.
- **P2 — Template note surfaces `## Notes` as a title.** `templates/note.md` has
  an empty H1, so the parser falls back to the first heading. Give the template a
  real H1 placeholder, or have the parser ignore non-H1 headings on fallback.
  `vault-template/templates/note.md`, `core/parser.go:34-41`.
- **P3 — No `hebb version` subcommand** (only `--version`); `hebb version` errors.
  `cli/root.go`.
- **P3 — `digest` exposes two vault flags** (`--vault` and `--vault-root`). Hide
  `--vault-root` like the other machine-only flags, or drop it. `cli/digest.go:100,38-40`.
- **P3 — Thin help / no examples.** `mcp`, `index`, `search` have no `Long` and
  no command has a cobra `Example:`. Add examples to at least `new`, `install`,
  `search`, `serve`, `sync`. `cli/root.go:105-141`.
- **P3 — Inconsistent success-output style** across commands (Title-case banners
  vs lowercase `indexed N`, `sync <path>: ...`, `digest: wrote`). Pick a house style.

## Batch 3: close the agent loop

The README leads with "a vault with a metabolism / built-in health checks". The
maintenance half of the loop is now mostly closed: a parallel session shipped the
`vault-gardener` skill, which reads the `hebb audit` worklist and proposes
reviewed, reversible fixes. What remains:

- **DONE — maintenance skill.** `plugin/skills/vault-gardener` acts on the audit
  worklist (dangling/ambiguous links, PARA drift, oversized, stubs). Reaches
  health via the `hebb audit` CLI.
- **P2 — Add a read-only `audit_vault` MCP tool** wrapping `core.RunHealthFull`,
  so an agent (or non-skill client) can read the worklist directly over MCP
  rather than only by shelling out to `hebb audit`. `mcp/server.go`.

## Naming and cohesion

- **P1 — One concept, four names: `audit` / `health` / metabolism.** Batch 1
  aligned the printed CLI banner to `audit`. Still split: the config block is
  `[health]`, the web heading is "Vault health", the README headline is
  "metabolism", and `audit` aliases `health`. Decide one surface name and align
  the rest (config section, web heading, docs), or add explicit cross-references.
  Decision needed before touching code. `core/vaultconfig.go` (`[health]`),
  `web/index.html` (heading), `README.md:38,48`, `cli/health.go:20` (alias).

## Web UI

- **P1 — Fresh vault renders an alarming red "fragmented" verdict.** A `hebb new`
  vault (4 notes, 0 links) shows a red 25% ring saying "your notes are
  fragmented". Gate the negative verdict on a minimum node/edge count; show a
  neutral "too few links yet to assess". `web/index.html:608,617,587`.
- **P2 — Obsidian links fail silently with no fallback.** Every "open" is an
  `obsidian://` URI; if Obsidian is absent or the vault `name` does not match the
  Obsidian vault, the click is a no-op. Add a copy-path fallback and document the
  name dependency. `web/index.html:434,654`, `web/server.go:98`.
- **P2 — Search/reindex errors are swallowed.** `search()` ends in
  `.catch(() => {})` and reindex silently reverts. Surface an inline error like
  the health view does. `web/index.html:456,808`.
- **P2 — Accessibility gaps.** Results have no `aria-live`; keyboard selection
  sets no `aria-selected`; footer "links" are hrefless `<a>` with click handlers
  (use `<button>`); the wordmark is a clickable `<div>`. `web/index.html:362,422,491,367,328`.
- **P3 — Vault switcher discoverability.** Tabs hide below 2 vaults; active tab
  has no `aria-current`. `web/index.html:833`.
- **P3 — "changes nothing" reassurance** sits above a "refresh index" button that
  does mutate the index; scope the wording to "changes nothing in your notes".
  `web/index.html:758,799`.
- **P3 — Dark-mode finding/chart hues are hard-coded light-palette hex**, bypassing
  the theme tokens. `web/index.html:593,39-60`.
- **P3 — No `<noscript>` fallback**; JS-disabled load is a blank page.

## Agent / MCP / skills

- **P2 — Skill discovery depends on magic phrases**, and the `[ingest] stage`
  trust model is a hidden TOML field with no CLI to view/set it. Surface the
  slash-commands in docs; consider `hebb ingest stage [n]`.
  `plugin/skills/ingest-*/SKILL.md`, `core/vaultconfig.go:213-241`.
- **P2 — Dual Claude delivery has no precedence/de-dup story.** Plugin
  (namespaced `hebb:vault-ingest`) and `~/.claude/skills` (unnamespaced) ship the
  same skills. Document precedence, or have install skip the copy when the plugin
  is present. `plugin/.claude-plugin/plugin.json:10`, `install/run.go:130-141`.
- **P2 — Stale source-of-truth comments.** `install/run.go:61-62` and the
  `assets.go` package doc still say "skills are delivered by the plugin, not
  install", which the code contradicts. Fix the comments.
- **P3 — `expand_context` description says "Markdown file"** but the param accepts
  a path or title; align to "a seed note (by path or title)". `mcp/server.go:88-92`.
- **P3 — MCP descriptions say "corpus"; everywhere else says "vault".** Use
  "vault". `mcp/server.go:62,89,111,133,152`.
- **P3 — MCP tool names inconsistent** (`vault_stats` is the odd noun-first one);
  document a convention and align any new tool (e.g. `audit_vault`). `mcp/server.go`.
- **P3 — `expand_context` vs `get_context_for_topic` overlap;** add a one-line
  "use X when ..." hint to each. `mcp/server.go:88-130`.

## Docs

- **P2 — Scaffold vs skill/automation drift.** `vault-ingest` routes to `Notes/`,
  `Journal/`, and an `OPEN-ACTIONS.md` register the scaffold never creates. Add
  them to the template, or note in `vault-template/CLAUDE.md` that the skill
  creates them on demand. `plugin/skills/vault-ingest/SKILL.md:24-26`,
  `automation/README.md:12`.
- **P2 — `vault-template/CLAUDE.md` lists only 3 MCP tools;** the headline count
  is 5. List all five (or point at AGENTS.md). `vault-template/CLAUDE.md:35-36`.
- **P2 — `vault-template/README.md` says `.mcp.json` always wires the vault;** it
  is opt-in (`--mcp-json`). `vault-template/README.md:15-16`.
- **P3 — METABOLISM.md reads as all-future** though Phases 0-2 shipped as
  `hebb audit`. Add a "live today" status banner.
- **P3 — Introduce the "data vs function" framing in the README** (it is
  load-bearing in ARCHITECTURE but absent from the README).
- **P3 — Gloss `[ingest]`** at first README mention.

## Aliases

- The `audit`/`health` and `unwire`/`reset` aliases are good migration aids. Once
  the above naming decision lands, document them as deprecated and plan removal in
  a future major. `cli/health.go:20`, `cli/reset.go:20`.
