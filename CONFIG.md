# Vault config (`config.toml`)

Every vault has a committed config at `<vault>/.hebb/config.toml`. It identifies
the vault and configures the parts of hebb that vary per vault. `hebb install`
and `hebb new` generate it; it is safe to hand-edit, is re-read on every command,
and travels with the vault (commit it). Malformed TOML is a hard error.

Every key and section is optional. Anything you omit falls back to the default
below, so a minimal config is just `name`.

## Example

A full config with every section, showing the defaults:

```toml
name = "PersonalVault"
exclude_dirs = [".obsidian", ".trash", ".hebb", ".git", ".claude"]
web_port = 4321
jobs = ["daily-digest", "action-review", "web", "update-check"]

# Extra CLI args appended to a job's launchd program (keyed by job name).
[job_args]
action-review = ["--owner", "Alex Doe"]

# Extra env vars injected into a job (keyed by job name). A key matching a
# built-in env var overrides it.
[job_env]
action-review = { HEBB_NOTIFY_URL = "https://hooks.example.com/abc" }

# Git auto-sync. Enabled automatically when the vault is a git repo at install.
[git]
enabled = true
auto_pull = true
auto_push = true
debounce_seconds = 10
commit_message = "hebb: sync vault"

[update]
auto = false

[index]
auto_refresh = true

[ingest]
stage = 1
scratch_dirs = ["Daily/Scratch", "Inbox/Staging"]

[notify]
enabled = false
url = ""

[health]
project_stale_days = 180
size_threshold = 1200
report_unresolved_links = false
# attachment_extensions defaults to a built-in list when omitted.
```

## Top-level keys

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `name` | string | vault directory name | Human name for the vault. Used in launchd job labels. |
| `exclude_dirs` | list of string | `[".obsidian", ".trash", ".hebb", ".git", ".claude"]` | Directory names skipped entirely during the index walk, matched at any depth. Notes inside become invisible to search. Setting this replaces the default list, so include the defaults you still want. |
| `web_port` | int | `4321` | Port for `hebb serve` (also overridable per run with `--port` or `$HEBB_WEB_PORT`). |
| `jobs` | list of string | `["daily-digest", "action-review", "web", "update-check"]` | Which launchd jobs `hebb install --launchd` renders. Known names: `daily-digest`, `action-review`, `web`, `update-check`. Unknown names are ignored. Automation jobs render only if their script is present. |

## `[git]` — git auto-sync

Keeps the vault's markdown in sync with its git remote. See `hebb sync`. Off
unless `enabled`, which `hebb install` sets to `true` automatically when the
vault is a git repo (only when first writing the config; an existing config is
never changed). Auto-sync runs only while a `hebb serve` or `hebb mcp` process
is live: it pulls at startup and commits+pushes after edits settle.

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `enabled` | bool | `false` (auto-set `true` for a git repo at install) | Master switch for git mode. |
| `auto_pull` | bool | `true` when enabled | Pull (rebase) at process startup. Set `false` to disable just the pull. |
| `auto_push` | bool | `true` when enabled | Commit and push after edits settle. Set `false` to disable just the commit+push. |
| `debounce_seconds` | int | `10` | Quiet period after the last edit before the watcher syncs. |
| `commit_message` | string | `"hebb: sync vault"` | Message for auto-commits. |

`hebb sync` never force-pushes; a conflicting pull is aborted and reported for
you to resolve by hand.

## `[update]` — self-update

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `auto` | bool | `false` | When `true`, the scheduled `update-check` job installs a newer release; when `false` it only notifies. Self-replacing a binary unattended is opt-in. |

## `[index]` — index refresh

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `auto_refresh` | bool | `true` | Refresh changed notes at read time (on a search, context, or stats call). Set `false` to leave refreshing to the file watcher alone. Does not affect watcher health reporting. |

## `[ingest]` — ingest policy

Records ingest behaviour that must travel with the vault rather than live in
per-user agent memory, so a cloned or second-machine vault inherits it.

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `stage` | int | `1` | Automation trust level: `1` (approve every write) through `3`. `hebb doctor` warns at `4` or above (headless, not yet supported) or below `1`. Never advanced automatically; raising it is your call. |
| `scratch_dirs` | list of string | empty | Vault-root-relative path prefixes (case-sensitive). Notes under them stay indexed and searchable, but ingest skills never treat them as ingest sources. Use for transient pads (daily scratch, paste staging). |

## `[notify]` — headless notifications

Posts a short summary to a webhook after headless job runs (`daily-digest`,
`action-review`, `update-check`), so output reaches you without an interactive
session. The URL is never echoed to logs or stdout.

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `enabled` | bool | `false` | Master switch for notifications. |
| `url` | string | empty | Webhook URL (receives `POST application/json`, body `{"text": "..."}`). |

URL resolution order: `$HEBB_NOTIFY_URL` first, then this `url`. `config.toml` is
committed, so for a shared or public vault keep the URL out of the file and
inject `$HEBB_NOTIFY_URL` per job via `[job_env]` instead. Committing the URL is
fine for a private vault.

## `[health]` — vault-health detectors

Thresholds and link-classification settings for `hebb health`, the read-only
advisory worklist. Wiki-links are resolved case-insensitively (matching
Obsidian).

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `project_stale_days` | int | `180` | Days without modification before a `1-Projects/` note is flagged as PARA drift. |
| `size_threshold` | int | `1200` | Estimated token count (`len(body)/4`) above which a note is checked for multiple sections (oversized candidate). |
| `report_unresolved_links` | bool | `false` | List each unresolved wiki-link (a link to a note that does not exist) as a `dangling_link` finding. Obsidian treats these as expected future notes, so they are counted but not listed by default. `hebb health --unresolved` forces listing for one run. |
| `attachment_extensions` | list of string | built-in list | File extensions (no leading dot) treated as attachment links and excluded from dangling checks, since hebb does not index non-note files. Empty uses the built-in default (`png`, `jpg`, `pdf`, `pptx`, `canvas`, `excalidraw`, ...). Setting it replaces the default rather than extending it. |

Folder links (a target ending in `/`, or one naming a real directory) are never
treated as broken note links.

## `[job_args]` and `[job_env]` — per-job overrides

Both are keyed by job name; entries for jobs not in `jobs` (or unknown to hebb)
are ignored.

- `[job_args]`: extra command-line arguments appended to a job's rendered
  launchd program, after the built-in flags. Each value is a list of strings.
  Example: `action-review = ["--owner", "Alex Doe"]`.
- `[job_env]`: extra environment variables injected into a job's launchd
  environment. Each value is a string-to-string map. A key matching a built-in
  env var overrides it (you win). The main use is `$HEBB_NOTIFY_URL` (see
  `[notify]`). Example: `action-review = { HEBB_NOTIFY_URL = "https://..." }`.

## Two distinctions worth remembering

- **`exclude_dirs` vs `[ingest] scratch_dirs`.** `exclude_dirs` removes a
  directory from the index entirely, so its notes are invisible to search.
  `scratch_dirs` keeps notes fully searchable but marks them off-limits as
  ingest sources. Use `exclude_dirs` for non-notes (tooling, attachments you
  never search); use `scratch_dirs` for real notes you do not want filed
  automatically.
- **Pointers default on.** `auto_pull`, `auto_push`, and `auto_refresh` are
  on when omitted; only an explicit `false` turns them off. The plain bools
  (`[git] enabled`, `[update] auto`, `[notify] enabled`) default off.
