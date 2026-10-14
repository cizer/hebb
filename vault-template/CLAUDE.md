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
- **Templates:** start a new note from `templates/note.md`. Meeting series use
  `templates/series-info.md` instead — see `_NOW.md` and that template for the
  detail.
- **`_NOW.md`:** a front-and-centre pin board, not a durable index. Each pin
  carries a `review-by: YYYY-MM-DD` line; `hebb audit` flags any pin past that
  date as `stale_pin`. Refresh the date or remove the pin — don't let a flagged
  one linger.
- **Ingest folders on demand:** `Notes/`, `Journal/` and `OPEN-ACTIONS.md` are
  not part of this scaffold. The ingest skills create them the first time
  they're needed, following the conventions in the skills themselves.

## Searching with hebb

- In Claude, the MCP tools read this vault directly: `search_vault`,
  `expand_context`, `get_context_for_topic`, `vault_stats`, `reindex_vault` and
  `audit_vault`. Prefer them over raw directory listing or grep.
- `hebb search "<query>"` from the command line, or `hebb serve` for a local web
  UI on 127.0.0.1.
- **The CLI is the fallback, not grep.** hebb is one binary: `hebb mcp` and the
  CLI read the same index, so when the MCP server is not connected (the harness
  reports it failed, or no `mcp__hebb__*` tools are present) use the CLI from a
  shell: `hebb search "<query>"` for `search_vault`, `hebb index` for
  `reindex_vault`, `hebb audit` for `audit_vault`. If `hebb` is not on PATH,
  run `./bootstrap.sh`, which installs the pinned hebb and wires this vault,
  then `export PATH="$HOME/.local/bin:$PATH"`. Only if that fails too fall
  back to grep, and say in the reply that the index was not used.

## Memory

Agent memory for this vault lives under `.hebb/memory/` and travels with the
vault. It is hidden from Obsidian and excluded from the index.
