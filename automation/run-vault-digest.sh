#!/usr/bin/env bash
#
# Generate the daily vault-activity digest, then refresh the hebb search index.
# Invoked by launchd (local.hebb.<vault>.daily-digest) on weekdays at 08:00 as:
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
