#!/usr/bin/env bash
#
# acceptance.sh - drive the built hebb binary end to end in a throwaway,
# production-like environment (temp HOME + temp vault; nothing touches the real
# home). This is the automated form of the manual UAT and is Stage 2 of the
# pipeline in TESTING.md. Runnable locally and in CI.
#
# Usage:
#   scripts/acceptance.sh [path-to-hebb-binary]
#   HEBB_BIN=/path/to/hebb scripts/acceptance.sh
# With no binary given, one is built from this checkout.
#
# Exits non-zero if any check fails. set -e is intentionally NOT used: the
# harness runs every check and reports a tally.

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK="$(mktemp -d)"
SERVE_PID=""
CANARY="HEBB-ACCEPT-CANARY-8F3K"
PORT="${HEBB_TEST_PORT:-4399}"
ok=0
fail=0

cleanup() {
  [ -n "$SERVE_PID" ] && kill "$SERVE_PID" 2>/dev/null
  rm -rf "$WORK"
}
trap cleanup EXIT

die() { echo "acceptance: $*" >&2; exit 1; }
report() { if [ "$1" -eq 0 ]; then echo "  ok    $2"; ok=$((ok + 1)); else echo "  FAIL  $2"; fail=$((fail + 1)); fi; }
has() { case "$1" in *"$2"*) return 0 ;; *) return 1 ;; esac; }

# --- resolve binary, expose it on PATH as `hebb` (production-like) ------------
if [ -n "${HEBB_BIN:-}" ]; then BIN="$HEBB_BIN"
elif [ -n "${1:-}" ]; then BIN="$1"
else
  echo "building hebb from $REPO_ROOT ..."
  ( cd "$REPO_ROOT" && go build -o "$WORK/hebb" ./cmd/hebb ) || die "build failed"
  BIN="$WORK/hebb"
fi
[ -x "$BIN" ] || die "binary not executable: $BIN"
mkdir -p "$WORK/bin"
ln -sf "$(cd "$(dirname "$BIN")" && pwd)/$(basename "$BIN")" "$WORK/bin/hebb"
export PATH="$WORK/bin:$PATH"
echo "==> using $(command -v hebb)  ($(hebb --version 2>/dev/null || echo '?'))"

# --- throwaway production-like environment ------------------------------------
HOME_DIR="$WORK/home"
DATA="$WORK/data"
LAUNCHD="$WORK/launchagents"
VAULT="$WORK/AcceptanceVault"
mkdir -p "$HOME_DIR" "$DATA" "$LAUNCHD" "$VAULT/1-Projects" "$VAULT/2-Areas" "$VAULT/.obsidian"

cat > "$VAULT/1-Projects/Aurora Overview.md" <<EOF
# Aurora Overview

$CANARY hub. See [[Aurora Decisions]] and [[Aurora Standup]].

#aurora #project
EOF
cat > "$VAULT/1-Projects/Aurora Decisions.md" <<EOF
# Aurora Decisions

Chose SQLite FTS5. Links [[Aurora Overview]]. $CANARY

#aurora
EOF
cat > "$VAULT/1-Projects/Aurora Standup.md" <<EOF
# Aurora Standup

Weekly cadence.

#aurora
EOF
cat > "$VAULT/2-Areas/Health.md" <<EOF
# Health

Running and sleep.

#health
EOF
cat > "$VAULT/.obsidian/should-not-index.md" <<EOF
# Hidden
$CANARY must not leak from an excluded dir.
EOF

# --- install ------------------------------------------------------------------
echo "==> install"
if hebb install --vault "$VAULT" --home "$HOME_DIR" --data-dir "$DATA" \
  --launchd --launchd-dir "$LAUNCHD" > "$WORK/install.out" 2>&1; then rc=0; else rc=$?; fi
report "$rc" "install exits 0"
sed 's/^/      /' "$WORK/install.out"
[ -f "$VAULT/.hebb/config.toml" ]; report $? "config.toml written"
[ ! -e "$VAULT/.mcp.json" ]; report $? "no per-vault .mcp.json by default (plugin provides MCP)"
[ -f "$VAULT/.hebb/index.db" ]; report $? "index built"
[ -d "$VAULT/.hebb/memory" ]; report $? "memory dir under .hebb"
ls "$HOME_DIR"/.claude/projects/*/memory >/dev/null 2>&1; report $? "memory linked into claude project dir"
[ -L "$VAULT/.claude/skills/vault-ingest" ]; report $? "vault-ingest linked project-scoped (<vault>/.claude/skills)"
ls "$LAUNCHD"/local.hebb.*.web.plist >/dev/null 2>&1; report $? "web launchd plist rendered"

# --- plugin-less wiring (--mcp-json opt-in) -----------------------------------
echo "==> install --mcp-json (opt-in plugin-less wiring)"
hebb install --vault "$VAULT" --home "$HOME_DIR" --data-dir "$DATA" --mcp-json >/dev/null 2>&1
[ -f "$VAULT/.mcp.json" ]; report $? "--mcp-json writes .mcp.json"
[ -f "$VAULT/.claude/settings.json" ]; report $? "--mcp-json writes project settings"
rm -f "$VAULT/.mcp.json"; rm -rf "$VAULT/.claude/settings.json"  # back to plugin mode for the rest

# --- doctor -------------------------------------------------------------------
echo "==> doctor"
if hebb doctor --vault "$VAULT" --home "$HOME_DIR" --data-dir "$DATA" \
  --launchd-dir "$LAUNCHD" > "$WORK/doctor.out" 2>&1; then rc=0; else rc=$?; fi
report "$rc" "doctor exits 0 (no FAIL checks)"
sed 's/^/      /' "$WORK/doctor.out"
doc="$(cat "$WORK/doctor.out")"
if has "$doc" "FAIL"; then report 1 "doctor reports no FAIL checks"; else report 0 "doctor reports no FAIL checks"; fi
has "$doc" "config"; report $? "doctor checks config"
has "$doc" "memory"; report $? "doctor checks memory"

# --- search -------------------------------------------------------------------
echo "==> search"
out="$(hebb search --vault "$VAULT" "$CANARY" 2>&1)"
{ has "$out" "Aurora Overview" && has "$out" "Aurora Decisions"; }; report $? "search finds both canary notes"
if has "$out" "should-not-index"; then report 1 "excluded dir not indexed"; else report 0 "excluded dir not indexed"; fi
out="$(hebb search --vault "$VAULT" --tag aurora aurora 2>&1)"
has "$out" "Aurora Overview"; report $? "tag filter returns aurora notes"

# --- serve + HTTP API ---------------------------------------------------------
echo "==> serve + HTTP API"
hebb serve --vault "$VAULT" --port "$PORT" > "$WORK/serve.out" 2>&1 &
SERVE_PID=$!
stats="$(curl -fsS --retry 30 --retry-connrefused --retry-delay 1 "http://127.0.0.1:$PORT/api/stats" 2>/dev/null)"
has "$stats" '"noteCount":4'; report $? "api/stats reports 4 notes (excluded dir skipped)"
srch="$(curl -fsS "http://127.0.0.1:$PORT/api/search?q=$CANARY" 2>/dev/null)"
has "$srch" "Aurora Overview"; report $? "api/search returns canary note"
kill "$SERVE_PID" 2>/dev/null; wait "$SERVE_PID" 2>/dev/null; SERVE_PID=""

# --- mcp over stdio -----------------------------------------------------------
echo "==> mcp over stdio"
{
  printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"acceptance","version":"0"}}}'
  printf '%s\n' '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'
  printf '%s\n' "{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"tools/call\",\"params\":{\"name\":\"search_vault\",\"arguments\":{\"query\":\"$CANARY\"}}}"
  printf '%s\n' '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"get_context_for_topic","arguments":{"topic":"Aurora"}}}'
} > "$WORK/mcp_in.jsonl"
hebb mcp --vault "$VAULT" < "$WORK/mcp_in.jsonl" > "$WORK/mcp_out.jsonl" 2>/dev/null &
MCP_PID=$!
# Safety watchdog (mcp exits on stdin EOF; this only fires if it hangs).
# disown so killing it does not print a job-control "Terminated" message.
( sleep 30; kill -9 "$MCP_PID" 2>/dev/null ) & WD=$!
disown "$WD" 2>/dev/null || true
wait "$MCP_PID" 2>/dev/null
kill "$WD" 2>/dev/null || true
mcp="$(cat "$WORK/mcp_out.jsonl")"
has "$mcp" '"hebb"'; report $? "mcp initialize returns server hebb"
for tool in search_vault expand_context get_context_for_topic vault_stats reindex_vault; do
  has "$mcp" "\"$tool\""; report $? "mcp tools/list includes $tool"
done
has "$mcp" "Aurora Overview"; report $? "mcp search_vault returns canary note"
if has "$mcp" "Tags: none"; then report 1 "mcp context tags consistent (no 'Tags: none')"; else report 0 "mcp context tags consistent (no 'Tags: none')"; fi

# --- plist validity (macOS only) ----------------------------------------------
echo "==> plist validity"
if command -v plutil >/dev/null 2>&1; then
  pl=0
  for p in "$LAUNCHD"/*.plist; do plutil -lint "$p" >/dev/null 2>&1 || pl=1; done
  report "$pl" "rendered plists pass plutil -lint"
else
  echo "  skip  plutil unavailable (non-macOS)"
fi

# --- tally --------------------------------------------------------------------
echo
echo "acceptance: $ok passed, $fail failed"
[ "$fail" -eq 0 ] || exit 1
