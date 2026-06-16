# automation

Vault-agnostic background scripts, embedded in the `hebb` binary and
materialised to the hebb data dir by `hebb install` (`$XDG_DATA_HOME/hebb`, else
`~/.local/share/hebb`). `hebb install` renders the matching launchd jobs for any
vault whose `.hebb/config.toml` lists them under `jobs` (see `install.VaultJobs`).
Every script takes its vault from `--vault-root`; nothing is hardcoded.

| Job | Entrypoint | Schedule | What it does |
| --- | --- | --- | --- |
| `daily-digest` | `hebb digest` (Go, in the binary) | weekdays 08:00 | Refreshes the index, then writes a rolling digest of notes whose content changed since the last run to `2-Areas/_DAILY-DIGEST.md`. |
| `action-review` | `generate-action-review.py` | Sundays 07:03 | Collates open actions from every `OPEN-ACTIONS.md` register into a prioritised `2-Areas/_ACTION-REVIEW.md` + a `.json` export. |

The digest is built into the `hebb` binary (`hebb digest`), not a Python script.
Its selection is driven by the index's content-level change detection, not by
filesystem mtime: each note carries a content hash and a `content_changed_at`
watermark, and the digest reports notes whose content changed since the last
successful run. A vault-wide operation that rewrites bytes or bumps mtimes
without changing content (a sync client, a restore, a no-op find/replace) does
not register, and a genuine edit is reported even if a later rewrite bumps its
mtime past a wall-clock window.

The `daily-digest` job's launchd `Program[0]` is the `hebb` binary running
`hebb digest --vault-root <vault>`. It must be a grantable binary because macOS
TCC attributes Full Disk Access to a launchd job's `Program[0]`: a shell wrapper
has no grantable identity, so its child's reads into a protected vault folder
(`~/Documents`, iCloud Drive) would block indefinitely. `hebb doctor` lints
installed plists for this and points at the grant step. Because the digest is
pure Go, the job needs no `PYTHON` env and no materialised script.

Overrides: `hebb digest` takes `--output` (digest note path) and `--date`
(override the run date, for testing). The action review takes
`--output`/`--json-output`, `--register-name`, `--owner` (the name highlighted
under "My Actions"; empty by default), and `--mine-output` (off by default; with
`--owner`, also writes a personal worklist of just the owner's actions,
bucketed Overdue/Current/Waiting and sorted by due date).

Per-vault flags: a vault passes extra arguments to its jobs via the
`[job_args]` table in `.hebb/config.toml`; `hebb install` appends them to the
rendered launchd program after the built-in flags. For example:

```toml
[job_args]
action-review = ["--owner", "Alex Doe", "--mine-output", "2-Areas/_MY-OPEN-ACTIONS.md"]
```

Per-vault env: a vault injects extra environment variables into its jobs via the
`[job_env]` table in `.hebb/config.toml`. Keys are job names; values are
key/value string maps. Variables are merged after the job's built-in env; a
user-supplied key matching a built-in key overrides it (user wins). The output
is deterministic: built-in keys first (overridden in place), then extra keys
alphabetically. The primary use is `$HEBB_NOTIFY_URL` for headless notification
delivery (item 6). For example:

```toml
[job_env]
action-review = { HEBB_NOTIFY_URL = "https://hooks.example.com/abc" }
daily-digest  = { HEBB_NOTIFY_URL = "https://hooks.example.com/abc" }
```

Committing the webhook URL is the vault owner's call (fine for a private vault);
use `[job_env]` to keep it out of the committed file when the vault is shared or
public (set it in the environment or a secrets manager instead).

## Headless notifications

Scheduled jobs post a one-line summary to a webhook after their note write when
`[notify]` is enabled in `.hebb/config.toml`. Compatible with Slack and
Discord-style incoming webhooks (POST `application/json`, body `{"text": "..."}`).

```toml
[notify]
enabled = true
url     = "https://hooks.example.com/your-webhook"
```

URL resolution: `$HEBB_NOTIFY_URL` is checked first (injectable per job via
`[job_env]`), then `[notify] url`. This means the URL can be kept out of the
committed file for shared or public vaults. The URL is never echoed to logs or
standard output.

Delivery is best-effort: a webhook failure is logged but never blocks or fails the
note write. `hebb doctor` warns when `[notify] enabled = true` but no URL resolves.

- `daily-digest`: sends the count of notes that changed via `hebb digest` (in Go).
- `action-review`: shells out to `hebb notify` after writing the review note; uses
  `$HEBB_BIN` (pinned by the rendered launchd job) so it works without hebb on PATH.
- `update-check`: sends available version via `hebb update --check` (in Go).

`hebb notify "text"` sends a one-line message directly: useful for testing or
custom scripts. Exits non-zero on HTTP failure.
