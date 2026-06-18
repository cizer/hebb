#!/bin/sh
# hebb SessionStart hook for Claude Code on the web. Committed with the vault.
# It runs synchronously, before the session and its MCP servers start, so hebb
# is ready when the agent loop begins. On a remote (web) session it installs
# hebb and wires this vault by running the committed bootstrap.sh. It is a
# no-op on a local session (hebb is already installed) and when bootstrap.sh
# is absent, so it is safe everywhere.
set -eu
[ "${CLAUDE_CODE_REMOTE:-}" = "true" ] || exit 0
DIR="${CLAUDE_PROJECT_DIR:-$(cd "$(dirname "$0")/../.." && pwd)}"
[ -f "$DIR/bootstrap.sh" ] && sh "$DIR/bootstrap.sh" || true
