# automation

Vault-agnostic background scripts, embedded in the `hebb` binary and
materialised to the hebb data dir by `hebb install` (`$XDG_DATA_HOME/hebb`, else
`~/.local/share/hebb`). `hebb install` renders the matching launchd jobs for any
vault whose `.hebb/config.toml` lists them under `jobs` (see `install.VaultJobs`).
Every script takes its vault from `--vault-root`; nothing is hardcoded.

| Script | Job | Schedule | What it does |
| --- | --- | --- | --- |
| `generate-vault-digest.py` | `daily-digest` (via `hebb digest`) | weekdays 08:00 | Writes a rolling digest of notes touched in the last working-day window to `2-Areas/_DAILY-DIGEST.md`. |
| `generate-action-review.py` | `action-review` | Sundays 07:03 | Collates open actions from every `OPEN-ACTIONS.md` register into a prioritised `2-Areas/_ACTION-REVIEW.md` + a `.json` export. |
| `run-vault-digest.sh` | (none) | - | Deprecated shell wrapper for manual digest runs only; see below. |

The `daily-digest` job's launchd `Program[0]` is the `hebb` binary running
`hebb digest --vault-root <vault>`, which runs `generate-vault-digest.py` then
refreshes the index in-process. It is the hebb binary rather than the
`run-vault-digest.sh` wrapper because macOS TCC attributes Full Disk Access to a
launchd job's `Program[0]`: a shell wrapper has no grantable identity, so the
child interpreter's reads into a protected vault folder (`~/Documents`, iCloud
Drive) block indefinitely. `hebb doctor` lints installed plists for this and
points at the grant step. `run-vault-digest.sh` is retained for manual use only
and honours `PYTHON` and `HEBB_BIN`; for scheduled and manual runs alike prefer
`hebb digest`, which honours `PYTHON` (launchd ships a minimal PATH that resolves
python3 to the Full-Disk-Access-less Xcode shim).

Overrides: both Python scripts take `--output`/`--json-output`; the action
review also takes `--register-name`, `--owner` (the name highlighted under
"My Actions"; empty by default), and `--mine-output` (off by default; with
`--owner`, also writes a personal worklist of just the owner's actions,
bucketed Overdue/Current/Waiting and sorted by due date). Arguments after `--`
on `hebb digest` are passed through to `generate-vault-digest.py`.

Per-vault flags: a vault passes extra arguments to its jobs via the
`[job_args]` table in `.hebb/config.toml`; `hebb install` appends them to the
rendered launchd program after the built-in flags. For example:

```toml
[job_args]
action-review = ["--owner", "Alex Doe", "--mine-output", "2-Areas/_MY-OPEN-ACTIONS.md"]
```
