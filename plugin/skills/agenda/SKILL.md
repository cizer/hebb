---
name: agenda
description: >-
  Compose the next agenda for a named recurring meeting series from its
  standing items, carried-forward "next time" notes, chased open actions, and
  relevant themes and pins, then pre-create the next dated occurrence note so
  the post-meeting ingest sweep enriches it in place. Triggers on "/agenda",
  "agenda for my next X", "what should we cover in X", "prep the next X
  meeting", "set up next week's X note", or any request to build the agenda
  for a recurring series. Don't trigger for briefing/preparation research on
  an upcoming meeting (use meeting-prep), for filing a meeting that already
  happened (use ingest-meetings), or for a one-off meeting with no series.
---

# Agenda

Composes the next agenda for a named recurring meeting series and pre-creates
its dated occurrence note so the agenda is already sitting in the file the
post-meeting sweep will enrich. This is the *accumulation* half of the meeting
loop: ingest-meetings captures what happened and parks what's next; this skill
reads that parked state back out and turns it into a concrete agenda before
the meeting happens.

Each vault documents its own conventions (series folder layout, archive
location, tag vocabulary) in its `CLAUDE.md`. **Follow that file when it
exists**; the rules below are the generic defaults.

The hebb MCP (`mcp__hebb__*`) is the retrieval surface — prefer
`search_vault` / `expand_context` / `get_context_for_topic` over directory
listing or grep. `hebb themes --json` and `hebb audit --json` are CLI reads
over the index; both are read-only.

## Core rules (non-negotiable)

- **One series per invocation.** If the request names more than one, or is
  ambiguous about which series is meant, ask.
- **Propose, then confirm, then write.** The composed agenda is a draft for
  the user to edit; nothing is written to the vault until they approve it.
- **Never invent agenda items.** Every line on the agenda must trace back to a
  standing item, a carried-forward note, an open action, a theme, or a pin —
  each with its source. If there is nothing to carry forward, say so rather
  than padding the agenda.
- **Never touch regulated or compliance-tagged content** without the user
  driving that part of the conversation explicitly.
- UK English throughout, including in the written note.

## Workflow

### 1. Locate the series

Search for the series by name (`mcp__hebb__search_vault`, then confirm by
folder). A series lives in a PARA folder with a `_SERIES-INFO` note and,
usually, an `OPEN-ACTIONS.md` register alongside it; occurrence notes are
dated files in the same folder (e.g. `2026-07-01 Platform sync.md`).

- **Multiple matches:** list the candidates and ask which one.
- **No `_SERIES-INFO` note found but a plausible folder of dated occurrence
  notes exists:** this is a series that predates the WS4.1 series contract.
  Offer to back-fill one from `templates/series-info.md` (title, cadence,
  day, time, attendees inferred from the occurrence notes where possible) and
  confirm the details with the user before writing it. Do this first — every
  later step reads the series contract.
- **Nothing found at all:** this isn't a recurring series; say so and suggest
  meeting-prep instead.

### 2. Gather, in this order

Collect material before composing anything. Keep every item's source
alongside it — the agenda in step 3 must cite provenance.

**(a) Standing items** — the `## Standing items` list in `_SERIES-INFO`.
These appear on every agenda by definition.

**(b) Series register carry-forwards** — the `## Next time` section in
`_SERIES-INFO`. Per the ingest-meetings carry-forward discipline, this is
where items parked from past occurrences accumulate, each with a wiki-link
back to the occurrence note it came from. Read every item; keep the link.

**(c) Last occurrence's own carry-forwards** — open the most recent dated
occurrence note in the series folder and check its own `## Next time`
section. The register mirror in (b) should already hold these, but mirroring
can lag or miss (e.g. an item added after the last mirror pass), so check the
source directly and reconcile: if an item is in the occurrence note but not
the register, treat it as a carry-forward anyway and note that the register
is behind.

**(d) Open actions worth chasing** — read the series' `OPEN-ACTIONS.md`
register (a markdown table: Status, Action, Owner, Review due, First raised,
Latest source). Not every open action belongs on an agenda. Include only
ones plausibly discussable at this meeting: overdue or due-soon, owned by an
attendee of this series, or stale enough (no movement since it was raised)
that a verbal chase is more useful than another silent register cycle. Leave
the rest in the register untouched.

**(e) Theme signals** — run `hebb themes --json --days 30` and filter the
`link_targets` and `tag_momentum` entries to ones plausibly touching this
series' attendees or topic (matching attendee names/wiki-link targets, or
tags the series' notes carry). This is speculative signal, not confirmed
agenda content: take at most 1-2 items, and carry them as clearly marked
"possible discussion" rather than folded in with the confirmed carry-forwards.
If nothing in the window touches this series, omit the section rather than
force an item in.

**(f) `_NOW.md` pins** — read the vault's `_NOW.md` and keep any pin whose
text names an attendee of this series or the series' topic. Cite the pin
heading; note its `review-by` date so the user can see if it's already stale.

### 3. Compose and propose

Group the draft agenda under four headings, omitting any that are empty:

```
## Agenda — <Series title>, <next occurrence date>

### Standing items
- <item> (standing)

### Carried from last time
- <item> — from [[<occurrence note or _SERIES-INFO>]]

### Actions to chase
- <action> — owner: <owner>, due: <review_due> — [[OPEN-ACTIONS register]]

### Possibly worth raising
- <theme or pin item> — possible discussion, source: [[<target>]] / _NOW.md pin "<heading>"
```

Present this to the user as a proposal, not a fait accompli. Expect and
invite edits: reordering, dropping items, adding something the user knows
about that isn't in the vault. Iterate until they confirm it's ready to file.
Judgement about what actually matters for this meeting stays with the user —
this skill assembles candidates, it does not decide the meeting's priorities.

### 4. On confirmation: compute the date and pre-create the note

Work out the next occurrence date from `_SERIES-INFO`'s `cadence` and `day`
frontmatter (weekly/biweekly/monthly measured from the last occurrence note's
date; adhoc has no computed date — ask). If the frontmatter is missing either
field, or the computed date looks wrong (falls on a day the series doesn't
actually meet, per the occurrence history), ask rather than guess.

Pre-create the dated occurrence note in the series folder, named consistently
with the vault's existing occurrence notes (e.g. `YYYY-MM-DD <Series
name>.md`), with frontmatter matching the vault's occurrence-note pattern
(typically `title`, `tags`, a link back to the series) and this body:

```
## Agenda

<the agreed agenda from step 3, confirmed items only — drop "possibly worth
raising" items the user didn't want carried, keep the ones they did>

## Notes

## Next time
```

This is deliberately a stub outside the agenda section: `## Notes` and
`## Next time` are left empty for the meeting itself and for the
post-meeting ingest sweep. **Pre-creating this file is safe because
ingest-meetings dedups by existing note before it writes** — when the sweep
later finds this occurrence's transcript or recap, it recognises the note
already exists and enriches it (fills `## Notes`, appends real carry-forwards
to `## Next time`) rather than creating a duplicate. Say this explicitly in
the confirmation message so the user knows why a same-named future note is
the correct outcome, not a mistake.

**Mark consumed carry-forwards in the register.** Every item from step 2(b)
or 2(c) that made it onto the pre-created agenda has now "landed" on a
concrete occurrence note in the same sense ingest-meetings uses when it
removes a landed item after the next occurrence is filed. Update
`_SERIES-INFO`'s `## Next time` section now: remove each item that was
carried onto the new note, replacing its old occurrence-note link with a
link to the newly created one where the item still needs tracking as an
open thread (rather than leaving both the old and new link sitting in the
register). Items the user chose to drop are simply removed, not carried.

### 5. Report

Close with the occurrence note's path (clickable), the confirmed date, which
carry-forwards landed on it (and were cleared from the register), which open
actions were surfaced, and anything left out that the user should know about
(stale register items not chased, theme signal considered but omitted).

## What this skill should not do

- Don't run a full meeting-prep briefing (attendee research, prior-context
  fan-out) — that's meeting-prep's job; this skill only accumulates
  series-specific state into an agenda.
- Don't file a past meeting or write transcript content — that's
  ingest-meetings.
- Don't invent, embellish, or reorder-by-guessing agenda items; every line
  must cite a source, and the user drives judgement about what matters.
- Don't touch regulated or compliance-tagged content without the user
  explicitly driving that part.
- Don't create a `_SERIES-INFO` note silently — offer it, confirm the
  details, then write it.
- Don't process more than one series per invocation.
- Don't pull every open action from `OPEN-ACTIONS.md` onto the agenda —
  only the ones plausibly discussable now.
- Don't let theme signal (`hebb themes`) outweigh confirmed carry-forwards —
  cap it at 1-2 clearly-marked possibilities.
