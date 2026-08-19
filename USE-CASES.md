# Common use cases

hebb's day-to-day home is a Claude Code or Codex session opened on your vault.
Once it is wired (a one-time step, see [Getting started](README.md#getting-started)),
you talk to your agent in plain language and it uses hebb's five MCP tools and
bundled skills to search, recall, capture, and maintain the vault for you.

The recipes below are written as short sessions: what you say, and what hebb does
underneath. They work the same in Claude and Codex, since both speak to the same
MCP server (`search_vault`, `get_context_for_topic`, `expand_context`,
`vault_stats`, `reindex_vault`) and run the same skills. Where a skill has a slash
command it is shown; the plain-language phrasing works too.

The CLI is still there for setup and background services, gathered at the end in
[Beyond the session](#8-beyond-the-session).

- [1. Recall what you already know](#1-recall-what-you-already-know)
- [2. Draft from your own knowledge](#2-draft-from-your-own-knowledge)
- [3. Capture and file something on the spot](#3-capture-and-file-something-on-the-spot)
- [4. Catch the vault up from your inbox, meetings, and videos](#4-catch-the-vault-up-from-your-inbox-meetings-and-videos)
- [5. Tidy the vault (gardening)](#5-tidy-the-vault-gardening)
- [6. Prepare a recurring meeting](#6-prepare-a-recurring-meeting)
- [7. Ask what your work is converging on](#7-ask-what-your-work-is-converging-on)
- [8. Beyond the session](#8-beyond-the-session)

---

## 1. Recall what you already know

You are about to start something and want what your past self already wrote,
including the notes around it, not just the one with the keyword.

> **You:** What do I already have on the Helsinki migration?

**What happens:** the agent calls `get_context_for_topic`, which blends full-text
search, link-graph expansion, and shared tags, then answers with the connected
notes cited by title. You get the neighbourhood of the topic, not a single hit.

> **You:** Find my note on the FTS ranking approach.

**What happens:** a direct `search_vault` call, ranked by relevance, filterable by
tag or folder if you narrow it ("...in 1-Projects", "...tagged #db").

> **You:** Start from [[Onboarding]] and pull in everything it links to.

**What happens:** `expand_context` walks the `[[wiki-links]]` out from that seed
note one or two hops, so the agent works from the whole cluster.

**You get:** recall instead of amnesia. Because hebb follows your links and tags,
the agent gathers the *related* material, then reasons over it.

## 2. Draft from your own knowledge

Recall and writing in one turn: the agent reads the vault, then produces
something grounded in it, and can save the result back.

> **You:** Draft a short brief on our caching strategy from what's in the vault,
> and save it under 3-Resources.

**What happens:** the agent gathers the relevant notes (`get_context_for_topic`
plus `expand_context`), writes the brief citing them, and, once you approve,
writes the new note into the vault. The file watcher indexes it immediately, so
your next question can already find it.

**You get:** output that reflects what you actually know, with no copy-pasting
between your notes and the chat.

## 3. Capture and file something on the spot

You have a chunk of raw material (pasted text, a list of contacts, a link, a
screenshot) and want it filed correctly without sorting it yourself.

> **You:** *(paste the raw content)* File this in the vault.

**What happens:** the `vault-ingest` skill triages the content, proposes a PARA
destination and a title using your vault's own conventions (from its `CLAUDE.md`),
and writes the note after you confirm. Drop several items at once and it files
each. Say "where should this go?" if you only want the suggestion.

**You get:** a vault that grows as you work, filed the way you would file it, at
the trust level you set (`[ingest] stage` in `config.toml`).

## 4. Catch the vault up from your inbox, meetings, and videos

Pull the signal out of the tools you already use and file it, in one sweep.

> **You:** /ingest-inbox
>
> *(or: "sweep my inbox and file anything useful")*

**What happens:** the `ingest-inbox` skill reads recent mail through the email
MCP, separates durable information and actions from noise, and files the keepers,
deferring all filing rules to `vault-ingest`. Its siblings work the same way:

> **You:** /ingest-meetings

files recent meeting transcripts, AI recaps, and substantive chat threads (for
example from Teams) via `ingest-meetings`.

> **You:** *(paste a YouTube link)* Summarise this into the vault.

distils the transcript into a note via `ingest-youtube`.

**Note:** these run in an interactive session, because the email, meeting, and
video sources need interactive authentication.

**You get:** the worthwhile parts of your week captured, without leaving the
session or hand-copying anything.

## 5. Tidy the vault (gardening)

Over time links break, done projects linger in `1-Projects/`, notes bloat, and
stubs pile up. Ask the agent to work through them.

> **You:** Garden the vault.
>
> *(or: "fix the dangling links", "archive the done projects")*

**What happens:** the `vault-gardener` skill runs `hebb audit` for you, reads the
resulting worklist, and walks through it one item at a time. For each finding it
proposes a concrete, reversible edit (repoint a broken link, archive a finished
project, merge a stub) and applies it only after you approve. Nothing is changed
in bulk and nothing is changed without you seeing it.

> **You:** Did that hurt search?

**What happens:** the agent can run `hebb eval` before and after a big change and
compare recall and mean reciprocal rank against your golden query file, so a
cleanup that quietly degrades retrieval is caught.

**You get:** a detect, remediate, verify loop that keeps the vault navigable and
never edits your notes behind your back.

## 6. Prepare a recurring meeting

Each instance of a standing meeting arrives with its agenda ready and its
follow-ups filed.

> **You:** /agenda for the Monday standup
>
> *(or: "prep the next standup")*

**What happens:** the `agenda` skill assembles the next agenda for that series
from its standing items, carried-forward "next time" notes, chased open actions,
and relevant themes, then pre-creates the dated occurrence note. After the
meeting, `/ingest-meetings` enriches that same note in place.

**You get:** a continuous thread per meeting series, so nothing carried forward
falls through the cracks between occurrences.

## 7. Ask what your work is converging on

A periodic read of where your recent work keeps returning.

> **You:** What themes are emerging in my recent notes?

**What happens:** the agent runs `hebb themes` and reports back the notes your
recent work most often links to, which tags are gaining or losing momentum, and
which pairs of notes keep being cited together.

> **You:** Anything worth consolidating this week?

**What happens:** the agent runs `hebb consolidate`, which drafts a short,
proposal-only review (stale pins, cooling projects, recurring themes) with each
proposal citing its source notes as `[[wiki-links]]`. You then action the ones
you like, by hand or by asking `vault-gardener` to do it.

**You get:** signal about the shape of your knowledge over time, driven by
content-level change detection rather than file timestamps.

## 8. Beyond the session

The pieces that live outside the chat. Most are one-time setup or background
services; see [CONFIG.md](CONFIG.md) for the full configuration.

- **First-time setup.** `hebb new <path>` scaffolds a fresh vault, or
  `hebb install --vault <folder>` adopts an existing one, then wires your agents.
  See [Getting started](README.md#getting-started).
- **The web console.** `hebb serve` opens a visual search box and vault-health
  dashboard at `http://127.0.0.1:4321` (loopback only). Handy when you want to
  browse findings rather than ask for them. See
  [Open the web console](README.md#5-open-the-web-console).
- **Several vaults.** Open a different folder and the agent acts on that vault;
  each is independent, like separate git repos. The web console switches between
  them via tabs.
- **Sync across machines.** For a vault that is a git repo, hebb auto-pulls at
  startup and commits and pushes after edits settle while a session is live
  (`[git]` in `config.toml`).
- **Scheduled upkeep.** `hebb install --launchd --load` (macOS) runs the daily
  digest, action review, weekly consolidation, and update checks in the
  background, posting a summary to a webhook (`[notify]`) if you want the output
  without an open session.

---

New here? Start with [Getting started](README.md#getting-started) to install hebb
and connect your agent, then come back and try recipe 1.
