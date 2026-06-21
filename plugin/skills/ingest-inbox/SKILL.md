---
name: ingest-inbox
description: >-
  Sweep the user's email inbox for team and value-stream updates, durable
  information, and actions, then file the worthwhile ones into the vault.
  Triggers on "/ingest-inbox", "check my inbox and file", "sweep my inbox",
  "ingest my emails", "anything useful in my inbox", "catch up the vault from
  email", or any request to pull recent mail into the vault. Runs inside an
  interactive session (the email MCP needs interactive auth). Don't trigger for
  sending or drafting email, for a single named email the user has already
  pasted (use vault-ingest), or for calendar-only questions.
---

# Ingest Inbox

Pulls recent email, triages noise from signal, and files the signal into the vault. This skill is the *acquisition and triage* layer. It defers all filing conventions to the vault-ingest skill and all tone, people, and folder rules to the vault `CLAUDE.md`. Do not duplicate that logic here; apply it.

The goal is three things, in descending order of how safe they are to capture automatically:

1. **Updates** (team and value-stream comms, meeting recaps, decisions): low stakes, clearly filed. Safest.
2. **Durable info** (facts that belong on a dossier or team note): medium stakes, needs judgement, fails silently if wrong.
3. **Actions** (things the user or their reports must do): highest stakes. Never auto-promoted to a central register.

## Access and the automation roadmap

Email comes from the connected mail MCP (e.g. Microsoft Graph: `outlook_email_search`, `read_resource`). Connector MCPs are typically **interactively authenticated**, so this skill works inside a live session, not on a headless cron binary. That auth gap is usually the single thing standing between this skill and full automation.

Run mode is staged. The current stage is recorded in `[ingest] stage` in `.hebb/config.toml`; read it there before each sweep. An absent or zero value means stage 1. Each stage is a deliberate decision by the user, not a drift:

- **Stage 1: approve everything.** Triage, build a filing plan, present it, and write nothing until the user approves the whole plan. The safe default while trust is being built.
- **Stage 2: auto-file updates, gate the rest.** Once update filing is reliably good, file clear updates without asking and stop only for durable facts and actions.
- **Stage 3: propose facts and actions as a digest.** Auto-file updates, and surface extracted facts and actions as a short proposal the user accepts in one pass.
- **Stage 4: headless.** A scheduled unattended sweep. Blocked until two things exist: (a) a standalone mail API credential usable by the headless runtime (the interactive MCP will not be present), and (b) a data-sensitivity sign-off, because an inbox holds compensation, HR, and personal data. Both are organisational decisions, not something this skill should assume.

State the current stage in the report so the user always knows which mode ran. Do not advance a stage without the user explicitly asking.

## Workflow

### 0. Anchor the date

Confirm today's date from session context and state it in the report before stamping files. Same discipline as vault-ingest step 0.

### 1. Decide the window

Anchor the window on the last sweep, not a fixed lookback. If the vault keeps an ingest log, read the most recent `inbox-sweep` row, which records the cutoff timestamp of that sweep (the `receivedDateTime` of the newest email it considered), and start from there. If there is no prior sweep or no log, default to the last 7 days. If the user names a window, use that instead. Pull newest-first from the Inbox folder.

Add a small overlap (start an hour or two before the recorded cutoff) so nothing arriving in the same minute as the last cutoff is missed. The per-email dedup in the next step makes overlap safe.

### 2. Triage noise from signal

Most of an inbox is noise. Drop these without reading bodies:

- Wiki and issue-tracker comment digests and watch notifications
- Calendar invites, updates, cancellations, and acceptances (logistics, not content)
- HR and payroll approval tasks (leave, absence, expenses)
- Automated scan and tool notifications (security scanners, CI pipeline mail, monitoring)
- Invoices, PO chases, vendor marketing, newsletters, webinar invites
- "You were added to a team / channel" notifications (note in passing if relevant, do not file)

Keep as **signal** anything that is substantive content from a person or team:

- Team and value-stream updates, weekly reports, sprint and standup notes
- Meeting recaps and summaries (AI-generated or human)
- Decisions, scope changes, ownership changes, planning outputs
- Substantive threads that change the user's picture of a team, person, or programme
- Threads where the user is **referenced in prose or added late** ("I'll pull [name] in", "added [name] to the conversation"). Being drawn into a thread, especially a manager escalation or a conduct discussion, is itself signal, even when the user is only cc'd.

When unsure, lean towards reading it. A two-line skim of the body resolves most calls. If you run any targeted keyword search rather than reading every body, include the **user's own name** (per the vault `CLAUDE.md`) as a term: colleagues refer to them in prose, not just by recipient field.

### 3. Read and classify the signal

For each signal email, read the full body. Large bodies may exceed the inline limit and get saved to a file; slice that file by character range rather than retrying. Strip quoted history, signatures, and distribution lists per vault-ingest's email rules.

For each, decide what it yields:

- **Update** to file as a note (apply vault-ingest destination rules; reuse existing series and team folders, do not fragment).
- **Durable info** for a dossier or team note (apply the people and dating rules from the vault `CLAUDE.md`).
- **Actions** raised (capture inside the canonical note, flag in an "Actions raised" or "Items worth a glance" section).

Resolve names and references against existing vault notes before writing. Name resolution is where this skill earns its keep and also where it can fail silently, so surface every uncertain identity in the plan rather than guessing into a dossier. When the vault keeps person notes, check the note for an explicit pronouns line before using gendered pronouns; default to they/them when absent.

### 4. Build the filing plan and present it (approval gate)

Before writing anything, present a compact plan:

- **Files to create or update**, with destination and a one-line description each
- **Durable facts** to be written, with the target dossier or note and the source email
- **Actions** extracted, with owner, and where each will be captured
- **Skipped as noise**, summarised in one line (count plus categories)
- **Already filed, skipping** (caught by dedup), so the user sees the skill noticed
- **Uncertain identities or sensitive content** flagged for a decision

In Stage 1, write nothing until the user approves. They can edit the plan. Honour the current stage's gate exactly.

### 5. Write on approval

Apply vault-ingest conventions for everything: destinations, splitting entities, recurring-stream folders, wiki-links, frontmatter, tags, language, tone. Apply the sensitive-content rules: anything touching compensation, HR, health, or personal life gets the vault's private-visibility framing, or is skipped and reported rather than blended into work notes. Never propagate sensitive specifics into work-facing notes.

**Conduct / interpersonal threads.** Escalations, complaints, and characterisations of a colleague arrive by email as often as anywhere. Follow vault-ingest's conduct rule: read the full thread directly (don't file from a thread summary), capture it balanced with the load-bearing lines near-verbatim, mark it decision-support / human-owned, never harden a characterisation into a dossier fact, and file to the person's rolling log rather than the dossier body.

**Actions discipline.** Capture actions in the canonical note. Do not write them to a central action register without asking once. If the user has said "leave actions for now" in the session, treat it as standing and do not re-ask.

### 6. Indexing is automatic

No manual reindex. New and changed notes (including the log row) are picked up by the index on the next search, and the file watcher reindexes live edits; `mcp__hebb__reindex_vault` is only an escape hatch for a suspected-stale index.

### 7. Log the sweep

If the vault keeps an ingest log, append **one row per sweep** (newest at top), matching the log's schema. Record the **cutoff timestamp** so the next sweep can window from it: "cutoff: <receivedDateTime of the newest email considered>". Also cover what was filed, propagated, and skipped as already-filed or noise, the run stage, and any flagged identities or sensitive items.

### 8. Report

Close with: the date filed under, the **run stage**, what was filed where as clickable links, durable facts written, actions captured and whether any need promoting, what was skipped as noise (count), and any flagged identities or sensitive content awaiting a decision. Keep it tight.

## Running it repeatedly (idempotency)

This skill is designed to be safe to run every day. Two mechanisms prevent duplicates:

1. **Windowing.** Each sweep starts from the previous sweep's cutoff (step 1), so a daily run only looks at roughly the last day. Record the cutoff in the log every time; the next run reads it back.
2. **Per-email dedup before writing.** The window is a coarse filter; it can overlap, and threads gain new replies. So before creating any note, confirm it is not already filed: search the vault for the subject and key entities, and check the ingest log. If already present, skip.

Specific cases to handle:

- **Same-day rerun.** The window since the last sweep is near-empty. Report "nothing new since the HH:MM sweep" and stop.
- **A thread that got a new reply.** Append the new substance to the existing note rather than creating a second one. Match on subject or conversation, not exact text.
- **Something previously judged noise.** Leave it as noise. Do not resurface filtered items.
- **An email already filed manually.** The dedup search catches it. Show it under "already filed, skipping".

When in doubt between creating a new note and appending, append. Fragmentation is the failure mode to avoid.

## What this skill should not do

- Do not run headless or assume the mail MCP is available outside an interactive session.
- Do not advance the run stage on its own. Stage changes are the user's call.
- Do not auto-promote actions to a central register without asking.
- Do not write durable facts from low-confidence identity matches. Flag them instead.
- Do not blend sensitive content (compensation, HR, personal) into work-facing notes.
- Do not duplicate vault-ingest's filing logic or re-explain PARA. Defer to it.
- Do not create a holding or inbox folder in the vault. File at creation.
- Do not treat paths listed under `[ingest] scratch_dirs` in `.hebb/config.toml` as ingest sources. Those paths are searchable but are transient pads, not filing destinations.
