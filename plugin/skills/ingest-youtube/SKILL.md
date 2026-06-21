---
name: ingest-youtube
description: >-
  Pull the transcript from a YouTube link and file distilled insights into the
  vault. Triggers on "/ingest-youtube", a pasted youtube.com or youtu.be link
  with intent to capture, "get the transcript", "extract insights from this
  video", "summarise this YouTube video into the vault", or any request to turn
  a YouTube video into a vault note. Runs in an interactive session
  (auto-caption-only videos need browser cookies, which can prompt the macOS
  keychain). Don't trigger when the user only wants a video summarised in chat
  with no filing, or for non-YouTube media.
---

# Ingest YouTube

Acquisition and distillation layer for YouTube videos, a sibling to ingest-inbox
(email) and ingest-meetings. It fetches a video's captions, distils them into
useful notes, and files them. It **defers all filing conventions to the
vault-ingest skill and all tone, folder, tag and privacy rules to the vault
`CLAUDE.md`**. Do not duplicate that logic here; apply it.

Default to keeping **distilled insights only**, not the raw transcript, because
the transcript is third-party content. Save the raw transcript alongside a note
only if the user asks.

## Prerequisites

- `yt-dlp` via Python: check `python3 -m yt_dlp --version`. If missing, install
  with `python3 -m pip install --user --upgrade yt-dlp`.
- The fetch step needs outbound network, so run it with the Bash sandbox
  disabled.

## Workflow

### 0. Anchor the date

Confirm today's date from session context and state it before stamping files.

### 1. Take the link(s)

Get the YouTube URL(s) from the user. Note the video id (the `v=` value or the
`youtu.be/<id>` slug).

### 2. Fetch captions (no video download)

First attempt, no cookies (works for videos with manual captions):

```
python3 -m yt_dlp --skip-download --write-subs --write-auto-subs \
  --sub-langs "en.*,en" --sub-format vtt -o "/tmp/ytt.%(ext)s" "<URL>"
```

If yt-dlp reports *"a PO token was not provided"* or *"no subtitles for the
requested languages"*, the video has auto-captions only and YouTube is gating
them. Retry with the user's browser cookies. **Ask which browser first**
(Chrome / Safari / Brave / Firefox), then add:

```
  --cookies-from-browser <browser>
```

Warn that Chrome/Brave/Edge may trigger a one-time macOS "Safe Storage" keychain
prompt the user must approve. Run this in the background so the keychain prompt
does not block. Do not scan or enumerate browser cookie stores yourself; let
yt-dlp read only what it needs from the single named browser.

### 3. Flatten the VTT

Strip the WEBVTT headers, cue timings, inline `<...>` tags and the
rolling-duplicate lines auto-captions emit. This step is local (no network), so
it runs under the normal sandbox. Use `*.en-orig.vtt` if `*.en.vtt` is absent.
The helper is inlined so the skill is self-contained in any agent runtime:

```
python3 - /tmp/ytt.en.vtt /tmp/ytt.txt <<'PY'
import re, sys
raw = open(sys.argv[1], encoding="utf-8").read()
lines = []
for ln in raw.splitlines():
    s = ln.strip()
    if not s or s.startswith(("WEBVTT", "NOTE", "Kind:", "Language:")) or "-->" in s:
        continue
    s = re.sub(r"<[^>]+>", "", s).replace("&nbsp;", " ").strip()
    if s and (not lines or s != lines[-1]):
        lines.append(s)
out = []
for ln in lines:
    if out and (ln == out[-1] or out[-1].endswith(ln)):
        continue
    out.append(ln)
text = re.sub(r"\s+", " ", " ".join(out)).strip()
open(sys.argv[2], "w", encoding="utf-8").write(text + "\n")
sys.stderr.write(f"words={len(text.split())} chars={len(text)}\n")
PY
```

It prints the word and character count to stderr.

### 4. Distil

Read the cleaned transcript. Produce a **reference note**, not a transcript
dump: the video title and a source link, a one or two line summary, the key
ideas/practices, and, for coaching or learning material, concrete prompts or
takeaways the user can reuse. Keep British English, concise and plain per
`CLAUDE.md`.

### 5. File via vault-ingest conventions

Place per PARA: coaching/learning usually `2-Areas/Personal Development/` or
`3-Resources/`. Title Case filename. Frontmatter with `title`, `tags`
(type + domain, e.g. `[reference, personal-development, coaching]`), the `source`
URL, and `created`. Add `[[wiki links]]` to related notes. Apply the CLAUDE.md
privacy rules (a private-area topic stays factual and unshared).

### 6. Log

If the vault keeps an ingest log, append a row (newest first, matching the log's
schema): `date | title | folder | tags | source (YouTube <id>) | private`. No
manual reindex: new and changed notes (including the log row) are picked up by
the index on the next search, and the file watcher reindexes live edits;
`mcp__hebb__reindex_vault` is only an escape hatch for a suspected-stale index.

### 7. Clean up

Remove the `/tmp/ytt.*` artefacts. Do not commit or push: if the vault runs a
hebb sync job it auto-commits and pushes new content, so a manual commit just
races it. (Only if a vault has no sync job, tell the user the change is
uncommitted and let them decide.)

## Notes

- Manual-caption videos need no cookies; auto-caption-only videos do.
- For several links, loop the same steps; dedupe by video id against the ingest
  log.
