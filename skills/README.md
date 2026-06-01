# skills

Generic, vault-agnostic Claude skills that ship inside the hebb binary
(`go:embed`), are materialised to the hebb data dir on `hebb install`, and are
symlinked into `~/.claude/skills`. Vault-specific conventions live in each
vault's `CLAUDE.md`, not here.

- `vault-ingest/` — file incoming raw content into a PARA vault via the hebb MCP.
