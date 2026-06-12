#!/usr/bin/env bash
#
# DEPRECATED as the launchd entrypoint. The daily-digest job now runs
# `hebb digest --vault-root <vault>` directly, because macOS TCC attributes
# Full Disk Access to the launchd job's Program[0]: a shell wrapper has no
# grantable identity, so the child python's reads into a protected vault folder
# block indefinitely. Prefer `hebb digest` for scheduled and manual runs alike.
# This wrapper is retained only for manual use where a shell is convenient.
#
# Generate the daily vault-activity digest, then refresh the hebb search index.
# Manual use:
#
#   run-vault-digest.sh --vault-root <vault>
#
# Vault-agnostic: the vault is taken from --vault-root, the digest generator is
# the sibling generate-vault-digest.py, and reindexing uses the hebb binary on
# PATH. Override the interpreters with PYTHON and HEBB_BIN if needed (launchd
# ships a minimal PATH).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PYTHON="${PYTHON:-python3}"
HEBB_BIN="${HEBB_BIN:-hebb}"
VAULT_ROOT=""

while [ $# -gt 0 ]; do
  case "$1" in
    --vault-root)
      VAULT_ROOT="${2:-}"
      shift 2
      ;;
    --vault-root=*)
      VAULT_ROOT="${1#*=}"
      shift
      ;;
    *)
      shift
      ;;
  esac
done

if [ -z "$VAULT_ROOT" ]; then
  echo "run-vault-digest: --vault-root is required" >&2
  exit 2
fi

log() { printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$1"; }

log "vault-digest: start ($VAULT_ROOT)"
"$PYTHON" "$SCRIPT_DIR/generate-vault-digest.py" --vault-root "$VAULT_ROOT"

log "vault-digest: reindexing"
"$HEBB_BIN" index --vault "$VAULT_ROOT"

log "vault-digest: done"
