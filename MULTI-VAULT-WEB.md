# Design: one web server, many vaults

## Problem

`hebb serve` is single-vault: it opens one vault's index, watches it, and binds a
port (default `4321`). Every vault's `config.toml` defaults to the same `4321`
and nothing allocates a unique port, so running two vaults' web UIs at once (or
two per-vault launchd `web` jobs) collides on the port: the second `bind` fails
and a launchd `KeepAlive` job crash-loops.

## Decision

Don't allocate per-vault ports. Run **one** loopback web server that knows about
every vault and switches between them. This dissolves the port problem, fits
hebb's multi-vault identity, gives one URL with a vault picker, and collapses the
per-vault launchd `web` jobs into a single machine-global service.

## Keystone: a vault registry

A global registry lists the vaults on this machine. It is the prerequisite for a
server that can enumerate them (and is useful beyond the web UI: `update`,
`restart-services`, and a future `hebb vaults` could all iterate it).

- **Location:** `$XDG_CONFIG_HOME/hebb/vaults.toml`, else `~/.config/hebb/vaults.toml`.
- **Shape:** a list of `{ name, path }`, `path` canonicalised (abs, symlinks
  resolved). De-duplicated by path.
- **Writers:** `hebb install` / `hebb new` register the vault (idempotent);
  `hebb reset` deregisters it (follow-up).
- **Readers:** the web server; later, other machine-global commands.

```toml
[[vault]]
name = "PersonalVault"
path = "/Users/me/code/PersonalVault"
```

## Server model

`ServeMulti(targets, port)` serves several vaults on one port. Per-vault
resources (index `*sql.DB` with `MaxOpenConns=1`, file watcher, the existing
per-vault mux) are opened **lazily** on first access, so you don't pay for vaults
you never open, and each vault stays independent (no cross-vault lock contention).

**Active-vault selection is a cookie**, which keeps the existing page almost
unchanged (its `/api/*` calls stay path-absolute and just resolve to the active
vault):

- `GET /?vault=<slug>` sets a `hebb_vault` cookie and serves that vault.
- Otherwise the `hebb_vault` cookie picks the vault; absent/invalid falls back to
  the first registered vault.
- `GET /api/vaults` returns `{ active, vaults: [{slug, name}] }` for the picker.
- Every other request is delegated to the active vault's existing mux unchanged,
  so search, stats, health, reindex, and the Obsidian links all work per vault.

The page gains only a `<select>` populated from `/api/vaults`; `onchange`
navigates to `/?vault=<slug>`. The loopback `Host` guard wraps the whole server.

## `hebb serve`

Builds its target list from the registry plus the current vault (if one resolves
from cwd/`--vault`), de-duplicated, and calls `ServeMulti`. With a single vault
the picker simply shows one entry, so behaviour degrades gracefully to today's
single-vault UI. `--port` / `$HEBB_WEB_PORT` still choose the (one) port.

## launchd (follow-up slice)

The per-vault `web` job becomes a single machine-global service running `hebb
serve` (all registered vaults) on the default port. `hebb install` stops
rendering a per-vault `web` job and instead ensures the one global job; existing
per-vault `web` plists are removed on the next install/reset. Until this lands,
the multi-vault server already fixes the manual `hebb serve` case; the per-vault
launchd `web` jobs keep their old single-vault behaviour (and their port caveat)
until migrated.

## Out of scope / non-goals

- No change to the MCP surface: `hebb mcp` stays per-vault over stdio (no ports).
- No auth: still loopback-only + `Host` guard. The picker lists vault names to
  anyone who can already reach localhost, which is the existing trust boundary.
- `web_port` stays meaningful only as the single server's port; per-vault values
  are ignored by the global service.

## Build slices

1. **Registry** (`core/registry.go`) + `hebb install` registration. Tested.
2. **Multi-vault server** (`web.ServeMulti`) + `hebb serve` wiring + the page
   selector. Tested.
3. **launchd**: one global `web` service; retire per-vault `web` jobs; `hebb
   reset` deregisters from the registry.
