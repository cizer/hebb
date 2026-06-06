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

## Workflow

### 1. Search before creating

Run `mcp__hebb__search_vault` for the key entities/topics in the incoming content
before creating anything. The vault is the source of truth — duplicating into a
new note when one already exists fragments it. If a related note already exists,
default to appending to it rather than starting a new one.

Use `mcp__hebb__get_context_for_topic` or `mcp__hebb__expand_context` when the
content sits in a broader topic and you want a wider sweep of related material.

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

### 5. Mark deprecations, don't delete

If told someone has left, a contract has lapsed, or a project has wound down, mark
the historical record clearly ("Left", "Past engagements", "Status: closed")
rather than removing it. The history is often why the current state makes sense.

### 6. Reindex after writing

End the writing operation with `mcp__hebb__reindex_vault`. The search index is the
primary retrieval path — a stale index means invisible content. Once per ingest
operation is enough, not once per file.

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

Reindex once more after writing the log entry — it's a vault write. The log is the
source of truth for "have I ingested this before?"; search it first if duplication
is a risk.

### 8. Report what happened

Close with a short summary in the chat: which notes were filed where (as markdown
links so the user can click through), what was pulled from existing material, what
was skipped and why. Keep it tight, and mention the ingest log entry if one was made.

## Conventions

- **Language**: match the vault's documented language and the tone of its existing
  notes; default to British English.
- **Tags**: 3–5 per note. Combine a type tag (`#resource`, `#area`, `#project`,
  `#permanent`) with context and domain tags.
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

## Example walkthrough

The user pastes a list of three agency contacts.

1. `mcp__hebb__search_vault` for each agency name to find existing notes (and check
   the ingest log if duplication is a risk).
2. Destination is `3-Resources/Agencies/` — reference material, ongoing
   relationships, will likely grow. If it had felt project-scoped, ask once.
3. Create one note per agency under `3-Resources/Agencies/`, plus a small
   `Agencies.md` index that wiki-links them.
4. Search the vault for related material; pull durable profiles and contacts, skip
   stale resourcing chatter.
5. Mark anything clearly past instead of dropping the history.
6. `mcp__hebb__reindex_vault`.
7. If the vault keeps an ingest log, append a row (date, "Agency contacts — pasted
   text", type `reference`, destination `[[Agencies]]`, notes). Reindex again.
8. Report which notes were filed where (clickable links), what was pulled, what was
   skipped.
