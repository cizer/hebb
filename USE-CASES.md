# Common use cases

Practical, end to end recipes for hebb. Each one states the goal, the steps, and
what you get. They assume the `hebb` binary is on your `PATH` (see the README for
install). Commands resolve their vault from the current directory, so most of
these are run from inside the vault unless a `--vault` is shown.

- [1. Search a folder of markdown notes you already have](#1-search-a-folder-of-markdown-notes-you-already-have)
- [2. Give Claude or Codex a memory of your notes](#2-give-claude-or-codex-a-memory-of-your-notes)
- [3. Capture new material into the vault](#3-capture-new-material-into-the-vault)
- [4. Keep the vault healthy](#4-keep-the-vault-healthy)
- [5. Prepare and run recurring meetings](#5-prepare-and-run-recurring-meetings)
- [6. See what your work is converging on](#6-see-what-your-work-is-converging-on)
- [7. Keep a vault in sync across machines](#7-keep-a-vault-in-sync-across-machines)
- [8. Run several vaults at once](#8-run-several-vaults-at-once)
- [9. Use hebb in an ephemeral or Claude Code on the web session](#9-use-hebb-in-an-ephemeral-or-claude-code-on-the-web-session)
- [10. Automate the background upkeep](#10-automate-the-background-upkeep)

---

## 1. Search a folder of markdown notes you already have

**You want:** instant full-text search over an existing vault (for example an
Obsidian folder), from the terminal or a browser, without changing how you write.

```sh
cd ~/Notes            # your existing markdown folder
hebb install          # adds .hebb/, builds the index, offers to wire your agents
hebb search "quarterly planning"
hebb search "planning" --tag work --path-prefix 1-Projects --limit 20
hebb serve            # web UI on http://127.0.0.1:4321
```

**You get:** a SQLite FTS5 index that picks up new and changed notes
automatically on the next search, filterable by tag and folder. `hebb serve`
adds a search box and a health dashboard, bound to loopback only. Your notes stay
plain markdown on disk; hebb never takes ownership.

## 2. Give Claude or Codex a memory of your notes

**You want:** your AI assistant to search and recall what you already know, pull
in the connected material, and write back into the same vault.

```sh
# Claude Code (works in every vault you open):
/plugin marketplace add cizer/hebb
/plugin install hebb@hebb

# Codex:
cd ~/Notes && hebb codex

# Claude Desktop:
hebb install --claude-desktop     # then restart Claude Desktop

# Anything else that speaks MCP:
hebb mcp                          # stdio MCP server, pinned via $HEBB_VAULT
```

Then, in the agent, just ask: "What do I already have on onboarding?" or "Pull
together everything related to the Helsinki project." The agent uses hebb's MCP
tools: `search_vault` (full-text), `expand_context` (follow `[[wiki-links]]` from
a seed note), `get_context_for_topic` (search + graph + tags for a broad
question), `vault_stats`, and `reindex_vault`.

**You get:** recall instead of amnesia. Because hebb walks your links and tags,
the agent gets the *related* notes, not just literal keyword matches.

## 3. Capture new material into the vault

**You want:** to file incoming material into the right place without hand-sorting
it. hebb ships agent skills for this; trigger them in Claude Code (or Codex).

- **Paste anything and file it:** drop raw text, a list, contacts, or a link into
  Claude and say "ingest this" or "file this in the vault". The `vault-ingest`
  skill puts it in the right PARA folder using your vault's conventions.
- **Sweep your inbox:** `/ingest-inbox` (or "catch up the vault from email")
  pulls durable information and actions from recent mail and files the worthwhile
  ones.
- **Sweep meetings and chat:** `/ingest-meetings` pulls recent transcripts, AI
  recaps, and substantive threads (for example from Teams) into the vault.
- **Capture a YouTube video:** paste a link and say "summarise this into the
  vault", and `ingest-youtube` distils the transcript into a note.

These run in an interactive session because the email, meeting, and video
sources need interactive authentication. They defer all filing conventions to
`vault-ingest` and your vault `CLAUDE.md`, so everything lands consistently.

**You get:** a vault that grows from the sources you already use, filed the way
you would file it, with you approving writes at the trust level you set
(`[ingest] stage` in `config.toml`).

## 4. Keep the vault healthy

**You want:** to stop the slow rot (dangling links, done projects left in
`1-Projects/`, oversized notes, near-empty stubs) as the collection grows.

```sh
hebb audit                    # read-only worklist, grouped by finding type
hebb audit --unresolved       # also list links to notes that do not exist yet
hebb audit --json | jq '.[]'  # machine-readable, for scripting
```

The `hebb audit` output (also shown on the `hebb serve` health dashboard) is
advisory and changes nothing. To act on it, use the `vault-gardener` skill in
Claude: "garden the vault" or "fix the dangling links". It reads the same
worklist and proposes one reviewed, reversible edit at a time, applying each only
after you confirm.

To prove a gardening or consolidation action did not hurt retrieval, keep a
golden query file and run `hebb eval` before and after:

```sh
hebb eval --json > before.json
# ...do the merge / split / archive sweep...
hebb eval --json > after.json    # diff recall and MRR
```

**You get:** a vault that stays navigable, with a detect (audit), remediate
(gardener), verify (eval) loop that never edits your notes without review.

## 5. Prepare and run recurring meetings

**You want:** each instance of a standing meeting to arrive with its agenda ready
and its follow-ups filed.

- Before the meeting: `/agenda` (or "prep the next standup") builds the next
  agenda for a named series from its standing items, carried-forward "next time"
  notes, chased open actions, and relevant themes, and pre-creates the dated
  occurrence note.
- After the meeting: `/ingest-meetings` files the transcript and recap, enriching
  that same occurrence note in place.

**You get:** a continuous thread per meeting series, so nothing carried forward
gets lost between occurrences.

## 6. See what your work is converging on

**You want:** a periodic, honest read of where your recent work keeps returning,
and a short list of things worth consolidating.

```sh
hebb themes --days 30     # most-linked targets, tag momentum, co-linked pairs
hebb digest               # refresh index, then write the daily digest note
hebb consolidate          # weekly consolidation review: a few ranked proposals
```

`hebb themes` reads the index over a trailing window and changes nothing.
`hebb digest` and `hebb consolidate` write generated notes (a daily digest and a
proposal-only weekly review); the proposals cite their source notes as
`[[wiki-links]]`, and you action them by hand or via `vault-gardener`.

**You get:** signal about the shape of your knowledge over time, driven by
content-level change detection rather than file timestamps.

## 7. Keep a vault in sync across machines

**You want:** a vault that is a git repository to stay in sync automatically while
you work, across a laptop and a desktop.

```sh
cd ~/Notes && hebb install     # for a git repo, this enables [git] auto-sync by default
```

While a `hebb serve` or `hebb mcp` process is live, hebb pulls at startup and
commits and pushes after edits settle (a debounced watcher). Configure it under
`[git]` in `config.toml` (`auto_pull`, `auto_push`, `debounce_seconds`,
`commit_message`). `hebb sync` runs one pull, commit, push by hand; it never
force-pushes, and a conflicting pull is aborted and reported for you to resolve.

**You get:** the same vault, current on every machine, with no manual commits in
the common case.

## 8. Run several vaults at once

**You want:** separate vaults (for example work and personal) that never bleed
into each other, like separate git repos.

```sh
hebb new ~/vaults/work
hebb new ~/vaults/personal
hebb vaults                    # list the vaults hebb knows about
hebb --vault ~/vaults/work search "roadmap"
export HEBB_VAULT=~/vaults/personal   # or set a default for this shell
hebb serve                     # one web server serves all of them, switch via the tabs
```

Every command resolves its vault from `--vault`, then `$HEBB_VAULT`, then the
nearest `.hebb/` above the current directory. The web UI is one machine-global
service that switches between registered vaults.

**You get:** many independent vaults from a single binary and a single web
server, each self-contained and self-describing.

## 9. Use hebb in an ephemeral or Claude Code on the web session

**You want:** a vault to provision itself when it is cloned onto a fresh or
throwaway machine, with no manual setup.

Every scaffolded vault commits a `bootstrap.sh` at its root and a Claude Code
`SessionStart` hook.

```sh
git clone <your-vault-repo> && cd <vault>
./bootstrap.sh                 # installs the hebb binary if absent, then wires the vault
```

In a Claude Code on the web session the hook runs `bootstrap.sh` automatically on
container start, before the session and its MCP tools load, so hebb is ready when
the agent begins. The hook is a no-op on a local machine. Set
`[bootstrap] track_latest = true` to have clones install the newest release
instead of the pinned one.

**You get:** a vault that stands itself up anywhere, including cloud sessions,
with the one manual step being to re-authenticate any connectors.

## 10. Automate the background upkeep

**You want:** the digest, action review, consolidation, and update checks to run
on a schedule without you.

```sh
hebb install --launchd --load     # render and start this vault's launchd jobs (macOS)
```

Choose which jobs run with `jobs` in `config.toml` (`daily-digest`,
`action-review`, `web`, `update-check`, `consolidate`). To get their output without an
interactive session, set a webhook under `[notify]` (or inject
`$HEBB_NOTIFY_URL` per job via `[job_env]`); the jobs then post a short summary.
Keep `[update] auto = false` (the default) to be notified of a new release
without self-replacing the binary, or set it `true` to auto-update.

**You get:** the vault's routine maintenance running quietly in the background,
reaching you only when there is something to see.

---

For per-key configuration of any of the above, see [CONFIG.md](CONFIG.md). For
the design behind it, see [ARCHITECTURE.md](ARCHITECTURE.md).
