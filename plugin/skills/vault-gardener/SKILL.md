---
name: vault-gardener
description: Use this skill to remediate vault-health findings, acting on the worklist that `hebb audit` produces. Triggers on "garden the vault", "clean up the vault", "tidy the vault", "fix vault health", "resolve the audit findings", "work the health worklist", "fix the ambiguous links", "fix the dangling links", "archive the done projects", "deal with the stubs", or any request to act on what `hebb audit` (or the health dashboard) reports. Don't trigger for filing new incoming content (use vault-ingest), for retrieval-only questions, or for edits to a file the user is already working in.
---

# Vault Gardener

Turns the `hebb audit` worklist into reviewed, reversible fixes. This skill
detects nothing itself: it reads the findings the engine already produces and,
one at a time, proposes a concrete edit, applies it only after you confirm, and
always leaves a way back.

Each vault documents its own conventions (folder names, the archive location,
what "done" means, tag vocabulary) in its `CLAUDE.md`. **Follow that file when it
exists**; the rules below are the generic defaults.

The hebb MCP (`mcp__hebb__*`) is the retrieval and indexing surface. Prefer it
over directory listing or grep.

## Core rules (non-negotiable)

- **Propose, then confirm, then apply.** Show the exact change (a diff, or a
  precise before/after) and get explicit approval with `AskUserQuestion` before
  writing anything. Never auto-edit.
- **One finding at a time**, or one named class in a batch the user has approved.
  Never sweep the whole worklist silently.
- **Move, never delete.** "Remove" means move the note to the archive folder
  (default `4-Archives/`) with a frontmatter tombstone (`archived_on`,
  `archived_reason`, `prior_path`), preserving the body and backlinks so it can
  be restored. The vault is git-backed, so every change is a revertible commit.
  Hard `rm` is never used.
- **Preserve history.** Mark deprecations ("Status: closed", "Superseded by
  [[...]]") rather than erasing them.
- **Stay in scope.** Fix the finding in front of you; do not refactor unrelated
  notes along the way.

## Workflow

### 1. Get the worklist

Run `hebb audit --json` to get the findings as a JSON array. Each finding has
`type`, `path`, `detail`, and `severity`. Group by `type` and tell the user the
counts. Ask which category (or specific finding) to work, or take the one they
named. For the full unresolved-link list, use `hebb audit --json --unresolved`.

### 2. Work one finding at a time, by type

**`ambiguous_link`** (a `[[link]]` that matches more than one note)
1. Read the source note around the link (`mcp__hebb__expand_context` or read the
   file) to infer the intended target.
2. `mcp__hebb__search_vault` for the link text to see the candidate notes.
3. Propose rewriting the link to an unambiguous form: a path-qualified
   `[[folder/Note]]`, or the exact title that resolves to a single note.
4. Confirm, then edit the source note. This is the highest-value, safest class.

**`para_drift`** (a `1-Projects/` note that is done or long untouched)
1. Confirm it is genuinely finished (frontmatter `status`, or ask).
2. Propose moving it to the archive folder with a tombstone (`archived_on`,
   `archived_reason`), keeping all backlinks intact.
3. Confirm, then move (write the note to the archive path; never delete).

**`stub`** (a near-empty note that links nowhere)
1. `mcp__hebb__search_vault` / `get_context_for_topic` on its title to find a
   related note it might belong in.
2. Propose either merging its content into the related note and replacing the
   stub with a redirect pointer, or archiving it if it carries nothing.
3. Confirm, then apply.

**`dangling_link` / unresolved links** (a link to a note that does not exist)
- Most are intentional links to not-yet-written notes; **do not touch them by
  default.** Act only when the user asks, or when a link is an obvious typo of an
  existing note (search to find the near-match), then propose the correction and
  confirm.

**`oversized`** (a large, multi-section note) — the heaviest case
1. Only if the user wants it split. Read the note and identify the H2/H3 sections
   that are genuinely independent ideas.
2. Propose one atomic child note per independent section, each made
   self-contained (resolve pronouns, restate the subject) with a `parent:`
   backlink, and rewrite the original into a thin map-of-content that wiki-links
   the children.
3. Confirm each split; apply; the original survives as the map (no content lost).
- A long but single-topic note (a meeting, a detailed design doc) is not a split
  candidate. Leave it.

### 3. Reindex and report

After applying approved changes, run `mcp__hebb__reindex_vault` (or `hebb index`)
so the worklist reflects the fix. Close with a short chat summary: what changed,
what was archived and where, and what you left for the user to decide, as
clickable links.

## What this skill should not do

- Don't auto-apply anything; every write is confirmed first.
- Don't hard-delete; archive with a tombstone.
- Don't act on dangling or unresolved links wholesale; they are usually
  intentional future-note links.
- Don't split a long single-topic note just because it is large.
- Don't override vault conventions; defer to the vault's `CLAUDE.md`.
- For a regulated or compliance vault, never archive or consolidate a note tagged
  regulated/compliance without explicit, per-note confirmation; surface it for a
  human decision instead.

## Example

The user says "clean up the ambiguous links."

1. `hebb audit --json`; filter to `type == "ambiguous_link"` (say there are 12).
2. Take the first: `2-Areas/Foo.md` contains `[[Sync]]`, which matches three
   notes.
3. `expand_context` on `Foo.md` shows it is about the BE VS architecture sync;
   `search_vault "Sync"` lists the candidates.
4. Propose: rewrite `[[Sync]]` to `[[2-Areas/BE VS Arch & Eng Sync]]`. Show the
   before/after. Confirm.
5. Apply, then move to the next finding.
6. After the batch, `reindex_vault` and report which links were disambiguated and
   which were left for the user to decide.
