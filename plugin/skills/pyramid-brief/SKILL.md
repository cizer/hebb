---
name: pyramid-brief
description: >-
  Structure a decision-facing piece of writing using Barbara Minto's Pyramid
  Principle: the answer first in one sentence, then three supporting reasons,
  then evidence under each. Use for emails, status updates, recommendations,
  proposals, briefing notes and any document written to a reader who has to
  decide or act. Triggers on "/pyramid-brief", "restructure this", "tighten
  this up", "what's the point of this draft", "does this land", "turn this
  into an email", "make this exec-ready", or any request to sharpen a draft
  aimed at a boss, board, client or stakeholder. Don't trigger for capturing
  or filing content (use vault-ingest or the ingest-* skills), for composing a
  meeting agenda (use agenda), for vault health (use vault-gardener), or for
  any record where chronology or verbatim fidelity is the point.
---

# Pyramid Brief

Applies Barbara Minto's Pyramid Principle, the structure McKinsey trains its
consultants on, to writing that has to persuade or inform a decision. The
default failure it fixes: warming up, giving context, and landing the
conclusion at the bottom where nobody reads it.

The shape is always the same. **The answer first, in one sentence. Then three
supporting reasons. Then the evidence under each.**

## When this applies, and when it does not

This is a structure for **arguments**, not for **records**. Applying it to a
record destroys the property that makes the record useful.

**Apply it to:** emails, status and project updates, recommendations,
proposals, board and exec papers, briefing notes, decision docs, a summary
written for someone who has to act on it.

**Do not apply it to:**

- Meeting notes and minutes: the record is what was said, in order.
- Journal and daily entries: chronological reflection, not a case.
- Atomic and reference notes (`Notes/`, `3-Resources/`): a concept note
  explains one idea; it is not building to a conclusion.
- Ingest logs, changelogs, action registers: these are tables and lists.
- Transcripts and anything in `Artifacts/`: verbatim by definition.
- Agendas: that is the `agenda` skill, and an agenda is a sequence.

If the piece has no single conclusion it is arguing toward, it is a record.
Leave it alone.

## Core rules (non-negotiable)

- **The answer goes first**, in one sentence, before any context.
- **Three reasons**, and they must be MECE: no overlap between them, and
  together they cover the case.
- **Evidence sits under a reason, or it is cut.** Anything that does not fit
  under one of the three does not belong in the piece.
- **Run the buried lede test before restructuring** anything already written.
  You need to know what the draft was secretly arguing before you rebuild it.
- **Keep the author's voice.** Structure the argument; do not flatten the
  prose into consultant register.
- **Structure is not review.** For regulated, participant-facing or externally
  published copy, the shape does not substitute for sign-off by the relevant
  function.

## The five moves

Run move 1 first on an existing draft. On a blank page, start at move 2.

### 1. The buried lede test

> Read this draft and tell me the one conclusion it is actually building
> toward. If I only had one sentence to give my boss, what would it be? Then
> tell me where in the draft I hid it.

Name the conclusion and quote the line where it currently sits. If the
conclusion turns out to be somewhere around the last paragraph, that is the
diagnosis, and move 2 is the fix.

If the draft has **no** single conclusion, stop and say so. Either it is a
record (see above), or the thinking is not finished yet. Restructuring will
not rescue an argument that does not exist.

### 2. The restructure

> Rewrite this using the Minto Pyramid: open with my single main conclusion in
> one sentence, then exactly three supporting reasons, then the evidence under
> each. Cut anything that does not sit under one of the three.

Report what was cut, so the author can object. Material that was cut but is
genuinely needed usually signals a missing fourth reason or a wrong top-line.

**On "exactly three".** Three is a forcing device: enough to be substantive,
few enough to be held in a reader's head. If the case genuinely has two
reasons, give two and say so. Never pad to three, because an invented third is
precisely what move 3 exists to catch.

### 3. The three reason audit

> Check my three reasons. Do they overlap, and together do they fully cover
> the case? Flag any that is a detail pretending to be a reason.

The two failure modes to hunt: reasons that are the same point in different
clothes, and a supporting detail promoted to reason status because it was
interesting. Say which, and propose the fix.

### 4. The so what pass

> Go line by line and ask so what after each one. Delete every line that
> cannot answer it. Show me what survives.

Show the surviving text, and list what went, so the cut is reviewable.

**Guard against over-pruning.** A caveat, constraint, risk or dependency that
changes what the reader should do **does** answer "so what". Do not strip
necessary qualifications to hit a word count. Vagueness is not concision.

### 5. The email version

> Compress this into an email under 150 words in the same structure: answer,
> three reasons, evidence only where it earns its place. Keep my voice.

Under 150 words, same pyramid, evidence only where it pulls its weight.

## Grounding the evidence

When the piece draws on the vault, pull the evidence rather than recalling it.
Run `mcp__hebb__search_vault` for the claims being made, and
`mcp__hebb__get_context_for_topic` when the argument spans a topic. Cite the
notes with wiki links so the reader can follow the chain. Never assert a
figure, date or decision from memory when the vault holds the record.

## What this skill should not do

- Don't restructure a record. Check the "when it does not apply" list first.
- Don't invent a third reason to satisfy the shape.
- Don't manufacture evidence, figures or impact numbers to fill a gap. If a
  reason is unevidenced, say it is unevidenced.
- Don't strip the author's voice, or rewrite into house consultant-speak.
- Don't delete caveats that change the decision just to hit the word limit.
- Don't restructure silently. Always show what moved and what was cut.
