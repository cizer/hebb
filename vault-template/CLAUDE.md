# Vault guide

This is a knowledge vault: a collection of Markdown notes you own. `hebb` is the
tool that indexes, searches and maintains it. The notes are the data; hebb is the
function. This file orients an agent (and you) to the conventions; edit it freely.

## Structure (PARA)

Notes live in four top-level folders:

- `1-Projects/` for things with a goal and an end date.
- `2-Areas/` for ongoing responsibilities with no end date.
- `3-Resources/` for reference material and topics of interest.
- `4-Archives/` for inactive items from the other three.

Put each note in the folder that matches its role now, and move it when that
changes. Anything under `.hebb/`, `.obsidian/`, `.git/` and `.trash/` is not
indexed.

## Conventions

- **Title:** the first H1 (`# ...`), or a `title:` field in YAML frontmatter. If
  neither is present the file name is used.
- **Links:** use `[[Wiki Links]]` to connect notes. hebb follows these to
  assemble related context, so link generously.
- **Tags:** `#like-this` inline, or a `tags:` list in frontmatter. Tags drive
  filtering and topic bundles.
- **Templates:** start a new note from `templates/note.md`.

## Searching with hebb

- `hebb search "<query>"` from the command line, or `hebb serve` for a local web
  UI on 127.0.0.1.
- In Claude, the MCP tools `search_vault`, `expand_context` and
  `get_context_for_topic` read this vault directly.

## Memory

Agent memory for this vault lives under `.hebb/memory/` and travels with the
vault. It is hidden from Obsidian and excluded from the index.
