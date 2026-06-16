# launchd

Go package (`package launchd`) that renders parameterised macOS launchd agent
definitions from a `Job` spec via the embedded `job.plist.tmpl`, and writes them
to a LaunchAgents directory idempotently. The per-vault `Job` specs are built in
`install` (`VaultJobs`); `install` also bootstraps them via `launchctl`.

## Jobs and TCC

The stock per-vault jobs (`install.VaultJobs`):

| Job | `Program[0]` | Schedule | What it runs |
| --- | --- | --- | --- |
| `web` | `hebb` | RunAtLoad, KeepAlive | `hebb serve` (the local web UI). |
| `daily-digest` | `hebb` | weekdays 08:00 | `hebb digest` (index refresh + digest note, pure Go). |
| `action-review` | `python3` | Sundays 07:03 | `generate-action-review.py`. |
| `update-check` | `hebb` | Mondays 09:00 | `hebb update --check`. |

Every job's `Program[0]` is a grantable binary (the `hebb` binary or an absolute
`python3`), never a shell script or interpreter shim. macOS TCC attributes a
job's Full Disk Access grant to `Program[0]`: a shell wrapper has no grantable
identity, so its child interpreter blocks indefinitely on reads into protected
vault folders. `hebb doctor`'s `launchd-tcc` check lints installed plists for
this. `install` prefers a stable symlink (e.g. `/opt/homebrew/bin/hebb`) over a
versioned Homebrew Cellar path so the grant survives an upgrade.

See ../ARCHITECTURE.md for how this fits the install flow.
