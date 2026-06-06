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
review also takes `--register-name` and `--owner` (the name highlighted under
"My Actions"; empty by default).
