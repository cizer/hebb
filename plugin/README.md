# hebb Claude Code plugin

The Claude-facing adapter for hebb. It wires hebb into a Claude Code session for
whatever vault you open:

- **MCP server** (`.mcp.json`): launches `hebb mcp` and points it at the opened
  project via `HEBB_VAULT=${CLAUDE_PROJECT_DIR}`. Gives Claude the tools
  `search_vault`, `expand_context`, `get_context_for_topic`, `vault_stats`,
  `reindex_vault`.
- **Skills** (`skills/`): `vault-ingest`, `vault-gardener`, `ingest-inbox`,
  `ingest-meetings`, each loaded namespaced (e.g. `hebb:vault-gardener`).

The plugin is the agent-facing layer only. The `hebb` binary (the engine, CLI,
and per-vault data: `install`/`new`/`doctor`, config, index, memory) is separate
and must be on `PATH` (`brew`/`npm`). MCP is a standard, so the same `hebb mcp`
server also works with other clients (e.g. Codex via its own config); this plugin
is just the Claude adapter.

## Install (persistent)

Install once via the marketplace defined at the repo root
(`.claude-plugin/marketplace.json`) and it stays enabled across sessions:

```sh
# from GitHub (public repo):
/plugin marketplace add cizer/hebb
# or from a local checkout:
/plugin marketplace add /path/to/hebb

/plugin install hebb@hebb
```

This records the plugin in `~/.claude/settings.json` (`enabledPlugins`); no flag
thereafter. The `hebb` binary must still be on `PATH` (the MCP server runs
`hebb mcp`).

## Try it (local dev)

```sh
cd <a vault>
claude --plugin-dir /path/to/hebb/plugin
```

`--plugin-dir` loads the plugin from a checkout for live editing - dev only, and
you'd retype it every session, so prefer the marketplace install above.
`/reload-plugins` picks up edits without restarting.

Other clients use the MCP server directly, not this plugin: Claude Desktop via
`claude_desktop_config.json`, Codex via `hebb codex`.
