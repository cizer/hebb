# hebb Claude Code plugin

The Claude-facing adapter for hebb. It wires hebb into a Claude Code session for
whatever vault you open:

- **MCP server** (`.mcp.json`): launches `hebb mcp` and points it at the opened
  project via `HEBB_VAULT=${CLAUDE_PROJECT_DIR}`. Gives Claude the tools
  `search_vault`, `expand_context`, `get_context_for_topic`, `vault_stats`,
  `reindex_vault`.
- **Skills** (`skills/`): the capture skills `vault-ingest` (file pasted
  content into the right PARA location), `ingest-inbox`, `ingest-meetings` and
  `ingest-youtube` (sweep email, meetings/chat and YouTube for signal and file
  it), the maintenance skill `vault-gardener` (act on the `hebb audit`
  worklist), and `agenda` (compose the next agenda for a recurring meeting
  series and pre-create its occurrence note). Each loaded namespaced, e.g.
  `hebb:vault-gardener`.

The plugin is the agent-facing layer only. The `hebb` binary (the engine, CLI,
and per-vault data: `install`/`new`/`doctor`, config, index, memory) is separate
and must be on `PATH` (`brew`/`npm`). MCP is a standard, so the same `hebb mcp`
server also works with other clients (e.g. Codex via its own config); this plugin
is just the Claude adapter.

## Bundled skills

Skills trigger on phrases, or explicitly via their slash command. One-line
hints below; the authoritative trigger list is the `description` in each
skill's `SKILL.md` under [`skills/`](skills/).

- `/hebb:vault-ingest` - file pasted raw content into the right PARA location.
  Triggers on "ingest this", "file this", or pasting notes, contacts, or links
  without naming a destination.
- `/hebb:ingest-inbox` - sweep email for durable signal and file it. Triggers
  on "sweep my inbox", "check my inbox and file", "catch up the vault from
  email".
- `/hebb:ingest-meetings` - sweep the meeting/chat platform (e.g. Teams) for
  transcripts, recaps, and threads. Triggers on "sweep teams", "pull recent
  meeting transcripts", "any meetings to file".
- `/hebb:ingest-youtube` - pull a YouTube transcript and file distilled
  insights. Triggers on a pasted YouTube link with intent to capture, "extract
  insights from this video".
- `/hebb:vault-gardener` - act on the `hebb audit` worklist. Triggers on
  "garden the vault", "fix vault health", "resolve the audit findings".
- `/hebb:agenda` - compose the next agenda for a recurring meeting series and
  pre-create its occurrence note. Triggers on "agenda for my next X", "what
  should we cover in X", "prep the next X meeting".

## Two delivery paths

The same skill files ship two ways:

- **This plugin** loads them namespaced, e.g. `hebb:vault-ingest`, wherever the
  plugin is enabled.
- **`hebb install`** also copies unnamespaced ones into `~/.claude/skills`, so
  they work in plain Claude Code without the plugin (pass `--no-skills` to
  skip; `hebb codex` does the same for Codex).

If both are present, Claude may see both copies. Pick one, preferably the
plugin: with the plugin installed it is safe to remove the copies under
`~/.claude/skills`, and `hebb install --no-skills` keeps them from coming back
on a re-install.

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
