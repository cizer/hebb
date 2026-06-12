# Agent guide (Codex)

This is a hebb knowledge vault: a collection of Markdown notes you own. hebb
indexes, searches and maintains it. This file orients Codex and other AGENTS.md
readers; Claude uses the hebb plugin and reads `CLAUDE.md` instead.

## Conventions

`CLAUDE.md` in this folder is the source of truth for layout (PARA folders),
titles, `[[Wiki Links]]`, tags and templates. Read it and follow it. Everything
below is just how to reach the vault from Codex.

## hebb MCP tools

Register this vault once with `hebb codex` (it adds an `[mcp_servers.hebb]` entry
to `~/.codex/config.toml`). That gives you the hebb tools, which are the primary
retrieval and indexing surface, prefer them over raw directory listing or grep:

- `search_vault` full-text search across the vault.
- `get_context_for_topic` / `expand_context` assemble related notes by following
  `[[Wiki Links]]` and tags.
- `vault_stats` size and coverage of the index.
- `reindex_vault` force a full rebuild (rarely needed; see below).

The index keeps itself fresh: new and changed notes are picked up automatically
on the next search, and a file watcher reindexes edits as they happen, so you do
not need to reindex after writing. Use `reindex_vault` only if results look stale
despite that, or after bulk file moves. From a shell you can also run
`hebb search "<query>"` or `hebb serve` for a local web UI on 127.0.0.1.

## Memory

Agent memory for this vault lives under `.hebb/memory/` and travels with the
vault. It is hidden from Obsidian and excluded from the index.
