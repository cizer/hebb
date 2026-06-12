---
name: ingest-meetings
description: >-
  Sweep the user's meeting and chat platform (e.g. Microsoft Teams) for recent
  meeting transcripts, AI recaps, and substantive chat threads, then file the
  worthwhile ones into the vault. Triggers on "/ingest-meetings", "sweep
  teams", "check for meeting transcripts", "pull recent meeting transcripts",
  "any meetings to file", "catch up the vault from meetings/chats", or any
  request to pull recent meetings or chats into the vault. Runs inside an
  interactive session (the platform MCP needs interactive auth). Don't trigger
  for a single named meeting the user has already pointed at (use
  vault-ingest), for email (use ingest-inbox), or for sending messages.
---

# Ingest Meetings

Pulls recent meeting transcripts and chat content from the connected platform, triages noise from signal, and files the signal into the vault. This is the *acquisition and triage* layer for meetings and chat, sibling to ingest-inbox (email). It defers all filing conventions to the vault-ingest skill and all tone, people, and folder rules to the vault `CLAUDE.md`. Do not duplicate that logic here; apply it.

Same value hierarchy as ingest-inbox, in descending order of how safe each is to capture automatically: **updates** (meeting notes, recaps) → **durable info** (dossier and team-note facts) → **actions** (never auto-promoted to a central register).

## Access and the automation roadmap

Content comes from the connected MCP — for Microsoft 365 that is calendar search (find meetings), resource reads (event details, transcripts, chat messages), and chat search. Connector MCPs are typically interactively authenticated, so this skill runs in a live session, not headless.

Run mode follows the **same four-stage model as ingest-inbox** (Stage 1 approve-everything → Stage 2 auto-file updates → Stage 3 propose facts/actions as a digest → Stage 4 headless, blocked on a standalone platform credential + a data-sensitivity sign-off). The two skills share the stage; do not advance one without the other unless the user says so. State the current stage in the report.

## Workflow

### 0. Anchor the date

Confirm today's date from session context and state it in the report before stamping files.

### 1. Decide the window

If the vault keeps an ingest log, window from the most recent meetings-sweep row's recorded cutoff. No prior sweep: default to the last 7 days. The user names a window: use that. Add an hour or two of overlap; per-item dedup makes overlap safe.

### 2. Enumerate meetings (calendar pass)

Search the calendar across the window. Triage **before** fetching any transcripts — fetches are expensive and most meetings don't need capturing:

- **Keep:** 1:1s with reports and stakeholders, recurring syncs that have an existing series folder, decision/planning meetings, programme-substantive sessions.
- **Drop:** cancelled/declined events, leave notices, socials, focus blocks, meetings the user didn't attend, logistics-only invites.
- **Dedup now, not later:** check the vault and the ingest log. Meetings often arrive by other routes (pasted AI summaries, recap emails via ingest-inbox), so a transcript is frequently the *second* copy. If a note exists, the transcript at most enriches it — never duplicates it.

### 3. Fetch transcripts (shortlist only)

For each shortlisted meeting, read the calendar event and follow its transcript reference.

Platform mechanics worth knowing (Microsoft Graph specifics; analogous elsewhere):

- **Occurrence-window misses are common.** A recurring meeting that ran earlier or later than its scheduled slot can return not-found for the scheduled occurrence. Retry without the occurrence time bounds to list the series' recent transcripts, then match by creation timestamp to the window.
- A recurring series returns its most recent transcripts, capped; older occurrences may be gone.
- Many meetings are simply not transcribed. **Don't fake reach** — record "no transcript available" in the plan and move on. An AI recap sometimes exists in the meeting chat even when the transcript doesn't; the chat pass can catch it.

### 4. Chat pass

Search chats across the window. **Prefer undated keyword/scoped queries** (e.g. `from:person`) and filter by returned timestamps: on Microsoft Graph the undated path uses the search index and reaches channels, DMs, and older chats, while date-bounded scans cover only a small set of recently-modified chats with no channels. Use the date-bounded path only for a queryless recency sweep, and state the coverage gap in the report. Search a few angles (key people, key topics, decision language, shared-file messages) rather than one query.

- **Signal:** decisions, ownership/scope changes, substantive updates, AI recaps posted into meeting chats, shared documents worth pulling.
- **Noise:** logistics, social chatter, reactions, link-only messages with no decision attached.
- Read promising threads in full before judging.

### 5. Classify and plan (approval gate)

Apply vault-ingest's source rules: transcripts parsed per its VTT handling with the raw file saved to the destination's `Artifacts/`; AI recaps get the AI-generated caveat and thematic restructuring. Destinations follow the vault's established patterns — 1:1 transcripts to the person's rolling log (per the vault's people pattern), series meetings to their series folder, project meetings to the project.

Present the plan before writing: meetings found (with transcript/recap/none status), already-filed items being skipped or enriched, chats worth capturing, durable facts with target notes, actions with owners, uncertain identities flagged. Honour the current stage's gate exactly.

### 6. Write on approval

Vault-ingest conventions throughout. Two meeting-specific disciplines:

- **Transcripts are verbatim and candid.** They carry profanity, personal chat, and offhand remarks about colleagues. File the professional substance; leave the candour in the raw transcript (preserved in `Artifacts/` for provenance). Never promote a candid remark about a person into a dossier as a durable fact without the user's explicit judgement — flag it in the plan instead.
- **Personal or sensitive content** in transcripts (health, family, compensation) follows the vault's private-visibility rules or is omitted and reported.

**Actions discipline:** capture in the canonical note; ask once before promoting to any register; "leave actions for now" stands for the session.

### 7. Log and report

No manual reindex: writes (including the log row) are picked up by the index on the next search, and the file watcher reindexes live edits, so `mcp__hebb__reindex_vault` is only an escape hatch. If the vault keeps an ingest log, append one meetings-sweep row recording the **cutoff** (end of the calendar window swept), what was filed/enriched/skipped, chat-coverage caveats, the run stage, and flagged items. Close with the date filed under, the stage, clickable links, what was skipped and why, and anything awaiting a decision.

## Running it repeatedly (idempotency)

Same two mechanisms as ingest-inbox: window from the last sweep's cutoff, and per-item dedup before any write. Meeting-specific cases:

- **Meeting already filed from another source** (pasted summary, recap email): enrich the existing note — attach the transcript to `Artifacts/`, append a "transcript adds" section only if it materially adds something. Show as "already filed, enriching" in the plan.
- **Same-day rerun:** report "nothing new since the last sweep" and stop.
- **Recurring series:** one note per occurrence in the series folder. Never a second note for the same occurrence.

When in doubt, append. Fragmentation is the failure mode to avoid.

## What this skill should not do

- Do not run headless or assume the platform MCP is available outside an interactive session.
- Do not fetch transcripts for unshortlisted meetings — triage first.
- Do not imply full chat coverage when the scan is bounded.
- Do not file verbatim candour or promote offhand personal remarks into dossiers.
- Do not advance the run stage on its own; stages are shared with ingest-inbox and changed only by the user.
- Do not auto-promote actions, duplicate vault-ingest's filing logic, or create holding folders.
