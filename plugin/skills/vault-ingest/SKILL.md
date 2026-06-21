---
name: vault-ingest
description: Use this skill whenever the user drops raw information into Claude that needs filing into the current vault. Triggers on "ingest this", "file this", "categorise this", "drop this in the vault", "where should this go", "add this to the vault", and any message where the user pastes contacts, notes, lists, links, screenshots, or other raw content without naming a destination. Also triggers when the user has set up an ingest/categorisation session and follows up with raw content. Don't trigger for retrieval-only questions or edits to a file already open.
---

# Vault Ingest

Files incoming information into the right place in a PARA-structured vault at
creation time, and enriches it with related existing material so the vault stays
the source of truth.

Each vault documents its own conventions (exact folders, area categories, tagging,
whether it keeps an ingest log) in its `CLAUDE.md`. **Follow that file when it
exists**; the layout and rules below are the generic defaults to fall back on.

The hebb MCP (`mcp__hebb__*`) is the primary retrieval and indexing surface.
Prefer it over directory listing or grep when looking for existing content.

## Default layout (PARA)

- `1-Projects/` — active work with a deadline or completion criterion
- `2-Areas/` — ongoing responsibilities, no end date
- `3-Resources/` — reference and learning material
- `4-Archives/` — completed projects, inactive material
- `Notes/` — atomic evergreen ideas, filename `YYYYMMDDHHNN - Clear idea.md`
- `Journal/Daily|Weekly|Monthly|Yearly/` — time-stamped reflections

File at creation; don't stage everything in a holding/inbox folder. Defer to the
vault's `CLAUDE.md` for any different or additional folders.

## Sensitive content

Some incoming content is personal: coaching reflections, performance reviews,
family or relationship context, health, finances. File these with explicit
visibility framing rather than letting them blend with work material:

- Frontmatter: `visibility: private`, `#private` tag
- 🔒 icon and a short disposition note at the top of the file or its folder index
- Folder-level rule, paraphrased: "Don't surface specifics from this folder in
  work-facing notes (1:1 prep with others, action registers, team-level notes)
  unless the user explicitly asks."

Background context for understanding the user's framing lives in these folders.
Citing the specifics in other notes does not.

**Conduct, performance and interpersonal disputes** need a firmer hand than other
sensitive content. When the incoming material is someone criticising, escalating
about, or characterising a colleague (a manager raising concerns, a heated thread,
review fallout):

- **Read the source directly.** Don't file this from a sub-agent's summary or a
  paraphrase — for a conduct matter the exact wording carries the weight. Preserve
  the load-bearing lines near-verbatim.
- **Capture it balanced.** Record what each side actually said, not just the
  loudest or most senior voice. If the substance of a complaint looks well-founded
  (or the characterisation looks second-hand or unevidenced), say so plainly with
  the evidence — that is the synthesis the user needs, not a one-sided log.
- **Decision-support, human-owned.** Mark it explicitly: any conduct or
  performance action is a human decision for the manager / line manager / People to
  make and document, not something inferred here.
- **Never harden a characterisation into a fact.** A claim that someone is
  "difficult" or "creates feelings in others" is recorded as *who said it and in
  what context*, never promoted into the person's dossier as a durable trait.
- File to the person's rolling 1:1 / interaction log (with a dated, sourced entry),
  not the durable dossier body; a short pointer on the dossier's active items is fine.

## Workflow

### 0. Anchor the date

Confirm today's date from the session context before stamping any file. When the
user says "today" or "had a chat", use that date — but **state it explicitly in
the report** so they can correct before files are stamped. Date errors create
wiki-link debt across the vault that's painful to clean up. Worth the one line.

### 1. Search before creating

Run `mcp__hebb__search_vault` for the key entities/topics in the incoming content
before creating anything. The vault is the source of truth — duplicating into a
new note when one already exists fragments it. If a related note already exists,
default to appending to it rather than starting a new one.

Use `mcp__hebb__get_context_for_topic` or `mcp__hebb__expand_context` when the
content sits in a broader topic and you want a wider sweep of related material.

If the vault keeps an ingest log and there's any chance the same source has been
ingested before, search the log too.

**Dedup per item, not just per window.** The same content often arrives through
more than one channel — a meeting or chat sweep can file something that also lands
in email, and each source tracks its own cutoff, so a windowed run can re-surface
content another sweep already filed. Always search the vault (and log) for the
specific item before writing, not just the time window. If a note already covers
it, enrich that note; never create a second one.

### 2. Decide the destination

Apply this order (most specific destination wins):

1. Tied to a project with a deadline or completion criterion → `1-Projects/<project>/`
2. Ongoing responsibility with no end date → `2-Areas/<category>/`
3. Reference or learning material → `3-Resources/<topic>/`
4. Standalone atomic insight in the user's own words → `Notes/` with `YYYYMMDDHHNN - Clear idea.md`
5. Time-stamped reflection → `Journal/Daily|Weekly|Monthly/`
6. Finished or no longer relevant → `4-Archives/` or delete

If genuinely uncertain (e.g. could be project-scoped or general reference), ask
with `AskUserQuestion`. Don't ask reflexively — when the answer is clear from the
content, just file. Reflexive questions waste the user's time.

### 3. Split entities into separate notes when they'll grow independently

A list of three agencies becomes three agency notes plus a small index, not one
combined file. Rule of thumb: if each item could plausibly accumulate its own
contacts, links, history, or related material over time, give it its own note and
wiki-link them together. If the items are tightly bound (attributes of a single
concept), keep them in one note.

**Recurring streams.** For things that arrive periodically (fortnightly product
updates, sprint updates, weekly reports, meeting series), set up the folder
structure once and subsequent items slot in without re-deciding:

- `<Area>/Teams/<Team>/_TEAM-INFO.md` + `Updates/` (or `Sprint Updates/`) subfolder
- `<Series>/_SERIES-INFO.md` + dated notes for meeting series
- `<Source>/_INDEX.md` + dated pointer notes for low-priority registers

### 4. Pull in related existing material

After creating the new note, search the vault for content about the same
entities/topics and decide what to link or consolidate. If the vault's `CLAUDE.md`
names legacy or import folders, sweep those too.

**Worth pulling** (durable, reusable reference): profiles and bios, positioning or
testimonial wording, contacts that aren't time-bound, past engagements that
explain current relationships, reusable templates or playbooks.

**Skip** (point-in-time noise): one-off action items, staffing decisions from years
ago, passing daily-note mentions with no standalone substance, logistics-only
meeting notes.

When pulling content forward, fix typos and modernise formatting. Add a date
marker or "(legacy)" annotation if the details might be stale.

**Propagate programme-level changes to the connective surfaces.** A dated note is
not enough when the incoming content changes the state of something the vault
tracks elsewhere. If a decision, slip, reorganisation or status shift lands,
update the surfaces that readers actually hit: the relevant project index, any
risk register, and a front-and-centre / "what matters now" note if the vault keeps
one. Two of those surfaces stay gated, never updated silently: marking a risk
materialised or closed is a real state change, so propose it in the plan rather
than applying it unasked; and a central action register stays behind the ask-once
gate (see Action propagation below), never written as part of propagation without
it. A change that lives only in one dated note is effectively lost; the value of
the vault is that the connected surfaces stay true. Keep the dated note as the
canonical record and point the surfaces at it. Hold a "would this still matter in
a month?" bar: prefer enriching an existing note or register over spawning a thin
new note.

### 5. Mark deprecations, don't delete

If told someone has left, a contract has lapsed, or a project has wound down, mark
the historical record clearly ("Left", "Past engagements", "Status: closed")
rather than removing it. The history is often why the current state makes sense.

### 6. Let the index refresh itself

No manual reindex step. The search index keeps itself fresh: new and changed
notes are picked up automatically on the next search, and a file watcher
reindexes edits as they happen. `mcp__hebb__reindex_vault` exists as an escape
hatch for a suspected-stale index or bulk file moves; you do not need it after a
normal ingest.

### 7. Log the ingest (if the vault keeps one)

If the vault's `CLAUDE.md` defines an ingest log, append one row per ingest
operation (not per file), newest at the top:

| Date | Source | Type | Primary destination | Notes |

- **Date** — the date the ingest happened, not the date of the source.
- **Source** — what came in (email subject + sender, file name, pasted-text
  description, transcript). Specific enough to be searchable later.
- **Type** — a short tag from the log's existing vocabulary (e.g. `meeting`,
  `comms`, `memo`, `project`, `reference`); consistency matters more than taxonomy.
- **Primary destination** — wiki link to the main note created.
- **Notes** — a sentence or two on what got created and propagated.

The log is the source of truth for "have I ingested this before?"; search it
first if duplication is a risk. (The log entry is picked up by the index like any
other write; no reindex needed.)

### 8. Report what happened

Close with a short summary in the chat: the **date you're filing under**, which
notes were filed where (as markdown links so the user can click through), what
was pulled from existing material, what was skipped and why. Keep it tight, and
mention the ingest log entry if one was made.

**Action propagation.** Capture any actions raised by the ingest inside the
canonical note, and flag the action-bearing ones in a dedicated section
(`## Actions raised` or `## Items worth a glance`). **Don't promote actions to a
central register** (OPEN-ACTIONS or equivalent) without asking. Ask once: "Add
these to the action register or leave in the note?" Repeated registry updates
during rapid-fire ingest sessions create friction and risk over-tracking. If the
user has already said "leave actions for now" in this session, take it as
standing instruction and don't re-ask.

## Sources and access

### Email (`.eml`)

Parse with Python's `email` module (`policy.default`). Extract plain-text body +
named attachments.

**Strip when ingesting:**
- Distribution lists longer than ~10 names — replace with a paraphrase
  ("broad cross-group distribution including X, Y, key roles")
- Mobile / personal phone numbers
- Signature blocks and marketing footers
- Tracking pixels and wrapped links

**Preserve:**
- Sender name + role + email
- Substantive named participants (To/CC referenced in content)
- The actual content body

When in doubt, strip. The source `.eml` file remains accessible if specifics are
needed later.

### AI-generated meeting summaries

Copilot recaps, Otter notes, Granola exports — anything that's already paraphrased
rather than verbatim transcript.

- Add an explicit "⚠️ **AI-generated summary**; specifics worth verifying." caveat
  at the top of the canonical note.
- Restructure thematically (by topic) rather than by speaker. Summary-level
  attribution is unreliable.
- Watch for room-mic artefacts: a city name or room name as a "speaker"
  ("Amsterdam said X") almost always means the conference-room mic, not a person —
  note this when it appears.
- Mark verifiable facts (dates, decisions, named action owners) separately from
  paraphrased themes if the summary mixes them.

### SharePoint / Teams / Microsoft 365

1. Try the Microsoft Graph MCP first (`sharepoint_search`, `chat_message_search`,
   `read_resource` with `file:///{driveId}/{itemId}`).
2. If that fails (indexing lag, personal-OneDrive scope, permissions), ask the
   user to download a local copy and drop the path.
3. **Don't fake success.** If the content can't be reached, say so clearly and
   offer the workaround.

### VTT transcripts

Parse to extract `<v Speaker>text</v>` tuples. Collapse consecutive turns by the
same speaker. Compute participation share by word count for the attendees table.
Save the raw VTT to `Artifacts/` alongside the meeting note.

**Candour guardrail.** Verbatim transcripts are candid: they contain profanity,
personal remarks, and offhand comments about colleagues. File the professional
substance; never auto-promote a candid personal remark into a durable person note.
The candid material belongs in the raw artefact (`Artifacts/`), not in a dossier.
Flag anything that warrants a dossier update and wait for the user's explicit
judgement before writing it.

### DOCX / PDF / PPTX

- DOCX: parse `word/document.xml` for paragraph text.
- PDF: use the `pages` parameter to read in chunks if large; large PDFs require
  explicit page ranges.
- PPTX: read via Microsoft Graph if SharePoint-hosted; otherwise download locally.
- Save originals to `Artifacts/` alongside the note when substantive.

## Conventions

- **Language**: match the vault's documented language and the tone of its existing
  notes; default to British English.
- **Person notes**: if the vault keeps person notes, check the note for an explicit
  pronouns line before using gendered pronouns; default to they/them when absent.
- **Tags**: 3–5 per note. Combine a type tag (`#resource`, `#area`, `#project`,
  `#permanent`) with context (`#work`, `#personal`, `#private`) and domain tags.
- **Wiki links**: `[[Note Name]]` for internal references. Link liberally — a link
  to a not-yet-written note is a useful breadcrumb, not an error.
- **Frontmatter**: keep it lean. `tags: [...]` is usually enough. Don't add
  elaborate templates unless asked.
- **Filenames**: Title Case for most notes; `YYYYMMDDHHNN - Clear idea.md` only for
  atomic `Notes/` entries.
- **Pasted images**: store in `assets/` alongside the note as
  `Pasted image YYYYMMDDHHMMSS.png`.

## What this skill should not do

- Don't create a holding/inbox folder; file at creation.
- Don't add elaborate templates, frontmatter blocks, or dashboards unless asked.
- Don't ask clarifying questions reflexively — only when the destination or scope
  is genuinely ambiguous from the content itself.
- Don't override vault-specific conventions; defer to the vault's `CLAUDE.md`.
- **Don't fake reach.** If a tool can't access the content (auth-walled URL,
  expired session, permissions, indexing lag), say so explicitly, offer the
  workaround (download locally, paste content, switch MCP), and stop. Filing a
  thin pointer is fine; filing a hallucinated extract is not.
- **Don't propagate sensitive specifics** from folders tagged `#private` into
  work-facing notes unless the user explicitly asks.
- **Don't treat scratch_dirs as ingest sources.** Paths listed under `[ingest]
  scratch_dirs` in `.hebb/config.toml` are indexed and searchable, but they are
  transient pads, not filing destinations. Never file new material into them or
  pull content from them as if it were signal.

## Example walkthrough

The user pastes a list of three agency contacts.

1. Anchor today's date and state it in the report.
2. `mcp__hebb__search_vault` for each agency name to find existing notes (and
   check the ingest log if duplication is a risk).
3. Destination is `3-Resources/Agencies/` — reference material, ongoing
   relationships, will likely grow. If it had felt project-scoped, ask once.
4. Create one note per agency under `3-Resources/Agencies/`, plus a small
   `Agencies.md` index that wiki-links them.
5. Search the vault for related material; pull durable profiles and contacts, skip
   stale resourcing chatter.
6. Mark anything clearly past instead of dropping the history.
7. If the vault keeps an ingest log, append a row (date, "Agency contacts — pasted
   text", type `reference`, destination `[[Agencies]]`, notes). The index picks up
   the new notes and the log entry on its own.
8. Report today's filing date, which notes were filed where (clickable links),
   what was pulled, what was skipped. Any actions raised stay in the canonical
   note unless the user asks to promote them.
