# automation

Vault-agnostic background scripts, embedded in the `hebb` binary and
materialised to the hebb data dir by `hebb install` (`$XDG_DATA_HOME/hebb`, else
`~/.local/share/hebb`). `hebb install` renders the matching launchd jobs for any
vault whose `.hebb/config.toml` lists them under `jobs` (see `install.VaultJobs`).
Every script takes its vault from `--vault-root`; nothing is hardcoded.

| Script | Job | Schedule | What it does |
| --- | --- | --- | --- |
| `run-vault-digest.sh` | `daily-digest` | weekdays 08:00 | Runs `generate-vault-digest.py`, then `hebb index` to refresh the search index. |
| `generate-vault-digest.py` | (called by the wrapper) | - | Writes a rolling digest of notes touched in the last working-day window to `2-Areas/_DAILY-DIGEST.md`. |
| `generate-action-review.py` | `action-review` | Sundays 07:03 | Collates open actions from every `OPEN-ACTIONS.md` register into a prioritised `2-Areas/_ACTION-REVIEW.md` + a `.json` export. |

Overrides: `run-vault-digest.sh` honours `PYTHON` and `HEBB_BIN` (launchd ships a
minimal PATH). Both Python scripts take `--output`/`--json-output`; the action
review also takes `--register-name`, `--owner` (the name highlighted under
"My Actions"; empty by default), and `--mine-output` (off by default; with
`--owner`, also writes a personal worklist of just the owner's actions,
bucketed Overdue/Current/Waiting and sorted by due date).

Per-vault flags: a vault passes extra arguments to its jobs via the
`[job_args]` table in `.hebb/config.toml`; `hebb install` appends them to the
rendered launchd program after the built-in flags. For example:

```toml
[job_args]
action-review = ["--owner", "Alex Doe", "--mine-output", "2-Areas/_MY-OPEN-ACTIONS.md"]
```
