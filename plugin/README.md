# hebb Claude Code plugin

The Claude-facing adapter for hebb. It wires hebb into a Claude Code session for
whatever vault you open:

- **MCP server** (`.mcp.json`): launches `hebb mcp` and points it at the opened
  project via `HEBB_VAULT=${CLAUDE_PROJECT_DIR}`. Gives Claude the tools
  `search_vault`, `expand_context`, `get_context_for_topic`, `vault_stats`,
  `reindex_vault`.
- **Skills** (`skills/`): `vault-ingest`, loaded namespaced as `hebb:vault-ingest`.

The plugin is the agent-facing layer only. The `hebb` binary (the engine, CLI,
and per-vault data: `install`/`new`/`doctor`, config, index, memory) is separate
and must be on `PATH` (`brew`/`npm`). MCP is a standard, so the same `hebb mcp`
server also works with other clients (e.g. Codex via its own config); this plugin
is just the Claude adapter.

## Try it (local dev)

```sh
cd <a vault>
claude --plugin-dir /path/to/hebb/plugin
```

Then `/mcp` should list the `hebb` server, and `hebb:vault-ingest` should be
available. `/reload-plugins` picks up edits without restarting.

A marketplace/distribution source is Phase 4 (release) work.
