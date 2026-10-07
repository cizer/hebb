package mcp

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cizer/hebb/core"
	"github.com/mark3labs/mcp-go/mcp"
)

// fixtureVault builds a small but realistic corpus: interlinked, tagged, nested
// notes plus a file inside an excluded dir that must never be indexed. It
// returns an open DB and the resolved config. The shared fixture is what makes
// cross-tool inconsistencies (the class UAT caught) observable.
func fixtureVault(t *testing.T) (*sql.DB, core.Config) {
	t.Helper()
	vault := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(vault, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// "synthwave" is unique to the hub, so a search for it matches only the hub
	// and the other Aurora notes are reached purely via the link graph.
	write("1-Projects/Aurora Overview.md", "# Aurora Overview\n\nsynthwave hub. See [[Aurora Decisions]] and [[Aurora Standup]].\n\n#aurora #project")
	write("1-Projects/Aurora Decisions.md", "# Aurora Decisions\n\nChose SQLite FTS5. Links [[Aurora Overview]].\n\n#aurora")
	write("1-Projects/Aurora Standup.md", "# Aurora Standup\n\nWeekly cadence.\n\n#aurora")
	write("2-Areas/Health.md", "# Health\n\nRunning cadence and sleep.\n\n#health")
	// Inside an excluded dir: must NOT be indexed.
	write(".obsidian/notes-must-not-index.md", "# Hidden\n\nsynthwave should not leak from here.\n")

	cfg := core.Config{VaultPath: vault, DBPath: filepath.Join(vault, ".hebb", "index.db"), ExcludeDirs: []string{".obsidian", ".trash", ".hebb", ".git"}, AutoRefresh: true}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := core.OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := core.FullReindex(cfg, db); err != nil {
		t.Fatal(err)
	}
	return db, cfg
}

// stubHealth is a fixed watcher-health snapshot for surface tests, standing in
// for a live watcher so vault_stats can be exercised deterministically.
func stubHealth(h core.WatcherHealth) watcherHealth {
	return func() core.WatcherHealth { return h }
}

// call invokes a registered tool handler by name and returns its rendered text,
// exactly what Claude receives.
func call(t *testing.T, db *sql.DB, cfg core.Config, name string, args map[string]any) string {
	t.Helper()
	return callWithHealth(t, db, cfg, stubHealth(core.WatcherHealth{Alive: true}), name, args)
}

func callWithHealth(t *testing.T, db *sql.DB, cfg core.Config, health watcherHealth, name string, args map[string]any) string {
	t.Helper()
	var handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
	for _, st := range toolset(db, cfg, health) {
		if st.Tool.Name == name {
			handler = st.Handler
		}
	}
	if handler == nil {
		t.Fatalf("tool %q not registered", name)
	}
	var req mcp.CallToolRequest
	req.Params.Name = name
	req.Params.Arguments = args
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("%s handler error: %v", name, err)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := mcp.AsTextContent(c); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func TestToolSurface(t *testing.T) {
	db, cfg := fixtureVault(t)
	got := map[string]bool{}
	for _, st := range toolset(db, cfg, stubHealth(core.WatcherHealth{Alive: true})) {
		if st.Tool.Description == "" {
			t.Errorf("tool %q has empty description", st.Tool.Name)
		}
		got[st.Tool.Name] = true
	}
	// The tool names are a stable client contract; guard them.
	for _, want := range []string{"search_vault", "expand_context", "get_context_for_topic", "vault_stats", "reindex_vault", "audit_vault"} {
		if !got[want] {
			t.Errorf("missing tool %q", want)
		}
	}
	if len(got) != 6 {
		t.Errorf("expected exactly 6 tools, got %d: %v", len(got), got)
	}
}

func TestSearchVaultTool(t *testing.T) {
	db, cfg := fixtureVault(t)

	out := call(t, db, cfg, "search_vault", map[string]any{"query": "synthwave"})
	if !strings.Contains(out, "Aurora Overview") {
		t.Errorf("search for synthwave should find Aurora Overview, got:\n%s", out)
	}

	// path_prefix filter scopes results.
	scoped := call(t, db, cfg, "search_vault", map[string]any{"query": "cadence", "path_prefix": "2-Areas/"})
	if !strings.Contains(scoped, "Health") || strings.Contains(scoped, "Aurora") {
		t.Errorf("path_prefix 2-Areas/ should return Health only, got:\n%s", scoped)
	}
}

// TestSearchVaultFindsAgentMemory replays the call that came back empty in the
// field: search_vault scoped to path_prefix ".hebb/memory" for a term that only
// a memory note carries. The fixture excludes .hebb, as installed vaults do.
func TestSearchVaultFindsAgentMemory(t *testing.T) {
	db, cfg := fixtureVault(t)
	mustWrite(t, cfg.VaultPath, ".hebb/memory/reference_raquel_disambiguation.md",
		"---\nname: Raquel disambiguation\ndescription: which Raquel is which\ntype: reference\n---\nRaquel (team A) is not Raquel (team B).\n")

	out := call(t, db, cfg, "search_vault", map[string]any{"query": "Raquel", "path_prefix": ".hebb/memory"})
	if !strings.Contains(out, "Path: .hebb/memory/reference_raquel_disambiguation.md") {
		t.Errorf("search_vault did not find the memory note under .hebb/memory:\n%s", out)
	}
}

func TestVaultStatsExcludesHiddenDirs(t *testing.T) {
	db, cfg := fixtureVault(t)
	out := call(t, db, cfg, "vault_stats", nil)
	// 4 notes: the .obsidian note must be excluded (not 5).
	if !strings.Contains(out, "Notes: 4") {
		t.Errorf("expected 4 notes (excluded dir skipped), got:\n%s", out)
	}
	if !strings.Contains(out, "aurora") {
		t.Errorf("expected aurora in top tags, got:\n%s", out)
	}
}

// TestVaultStatsReportsFreshnessAndWatcher guards the extended vault_stats
// contract: a last-index timestamp and watcher health line.
func TestVaultStatsReportsFreshnessAndWatcher(t *testing.T) {
	db, cfg := fixtureVault(t)

	alive := callWithHealth(t, db, cfg, stubHealth(core.WatcherHealth{Alive: true}), "vault_stats", nil)
	if !strings.Contains(alive, "Last indexed:") {
		t.Errorf("vault_stats missing last-index line:\n%s", alive)
	}
	if strings.Contains(alive, "Last indexed: never") {
		t.Errorf("fixture was indexed, so last-index should not be 'never':\n%s", alive)
	}
	if !strings.Contains(alive, "Watcher: alive") {
		t.Errorf("vault_stats missing alive watcher line:\n%s", alive)
	}

	// A never-started watcher must report the recorded startup error, not silence.
	dead := callWithHealth(t, db, cfg, stubHealth(core.FailedWatcherHealth(errors.New("inotify_limit"))), "vault_stats", nil)
	if !strings.Contains(dead, "Watcher: not running") || !strings.Contains(dead, "inotify_limit") {
		t.Errorf("vault_stats should report the watcher startup error, got:\n%s", dead)
	}
}

// TestReadTimeRefreshFindsNewFile is the core acceptance criterion: a file
// written after the server started is found by search_vault,
// get_context_for_topic and expand_context on the next call, with no
// reindex_vault between, and with no watcher running (health reports dead).
func TestReadTimeRefreshFindsNewFile(t *testing.T) {
	db, cfg := fixtureVault(t)
	deadWatcher := stubHealth(core.FailedWatcherHealth(errors.New("watcher killed")))

	// Write a brand-new note plus an inbound link to it, after the initial index.
	vault := cfg.VaultPath
	mustWrite(t, vault, "1-Projects/Aurora Roadmap.md", "# Aurora Roadmap\n\nquokkacanary milestone. Links [[Aurora Overview]].\n\n#aurora")

	// search_vault picks it up via the read-time refresh, no reindex_vault first.
	got := callWithHealth(t, db, cfg, deadWatcher, "search_vault", map[string]any{"query": "quokkacanary"})
	if !strings.Contains(got, "Aurora Roadmap") {
		t.Fatalf("search_vault did not self-refresh to find a new file (watcher dead):\n%s", got)
	}

	// expand_context from the existing hub now sees the new inbound link.
	exp := callWithHealth(t, db, cfg, deadWatcher, "expand_context", map[string]any{"note_path": "Aurora Overview"})
	if !strings.Contains(exp, "Aurora Roadmap") {
		t.Errorf("expand_context did not surface the newly linked note:\n%s", exp)
	}

	// get_context_for_topic likewise.
	topic := callWithHealth(t, db, cfg, deadWatcher, "get_context_for_topic", map[string]any{"topic": "quokkacanary"})
	if !strings.Contains(topic, "Aurora Roadmap") {
		t.Errorf("get_context_for_topic did not self-refresh:\n%s", topic)
	}
}

// TestReadTimeRefreshDisabled proves the [index] auto_refresh = false opt-out
// stops the read-time refresh: a file written after the index is built stays
// invisible until a manual reindex.
func TestReadTimeRefreshDisabled(t *testing.T) {
	db, cfg := fixtureVault(t)
	cfg.AutoRefresh = false
	mustWrite(t, cfg.VaultPath, "1-Projects/Hidden.md", "# Hidden\n\nnumbatcanary note")

	got := callWithHealth(t, db, cfg, stubHealth(core.WatcherHealth{Alive: true}), "search_vault", map[string]any{"query": "numbatcanary"})
	if strings.Contains(got, "Hidden") {
		t.Errorf("auto_refresh disabled but search still self-refreshed:\n%s", got)
	}
}

func mustWrite(t *testing.T, vault, rel, content string) {
	t.Helper()
	p := filepath.Join(vault, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExpandContextTool(t *testing.T) {
	db, cfg := fixtureVault(t)
	out := call(t, db, cfg, "expand_context", map[string]any{"note_path": "Aurora Overview"})
	for _, want := range []string{"Aurora Decisions", "Aurora Standup"} {
		if !strings.Contains(out, want) {
			t.Errorf("expand_context from Aurora Overview should include %q, got:\n%s", want, out)
		}
	}
}

// TestGetContextForTopicTagConsistency is the regression guard for the class of
// bug UAT caught: a note pulled in via the link graph must report the same tags
// the rest of the index has, not "Tags: none".
func TestGetContextForTopicTagConsistency(t *testing.T) {
	db, cfg := fixtureVault(t)
	out := call(t, db, cfg, "get_context_for_topic", map[string]any{"topic": "synthwave"})

	// Decisions and Standup arrive only via the link graph.
	for _, want := range []string{"Aurora Decisions", "Aurora Standup"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q assembled into context, got:\n%s", want, out)
		}
	}
	// Every assembled note here is tagged, so no "Tags: none" should appear.
	if strings.Contains(out, "Tags: none") {
		t.Errorf("link-pulled notes are missing tags (cross-tool inconsistency):\n%s", out)
	}
	if !strings.Contains(out, "aurora") {
		t.Errorf("expected aurora tag surfaced in assembled context, got:\n%s", out)
	}
}

// dirtyFixtureVault builds a small vault with a known dangling link (under
// 1-Projects/) and a note under 2-Areas/ with no such issue, so path_prefix and
// type filters have something distinct to narrow down to.
func dirtyFixtureVault(t *testing.T) (*sql.DB, core.Config) {
	t.Helper()
	vault := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(vault, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A dangling link: [[Nonexistent Note]] resolves to no note in the vault.
	// Bodies are padded well past the stub-detector token threshold (~20 tokens,
	// i.e. ~80 chars) so the only finding in 1-Projects/ is the dangling link,
	// and Steady.md links out to Anchor.md so it is not itself flagged as a stub.
	write("1-Projects/Broken.md", "# Broken\n\nSee [[Nonexistent Note]] for details on this piece of ongoing project work that is still being tracked.\n\n#project")
	write("2-Areas/Steady.md", "# Steady\n\nNo issues here, this area note is stable and links onward to [[Anchor]] for more detail.\n\n#area")
	write("2-Areas/Anchor.md", "# Anchor\n\nReference material that Steady links to, with enough body text to clear the stub threshold easily.\n\n#area")

	cfg := core.Config{
		VaultPath:   vault,
		DBPath:      filepath.Join(vault, ".hebb", "index.db"),
		ExcludeDirs: []string{".obsidian", ".trash", ".hebb", ".git"},
		AutoRefresh: true,
		Health:      core.HealthConfig{ReportUnresolvedLinks: true},
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := core.OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := core.FullReindex(cfg, db); err != nil {
		t.Fatal(err)
	}
	return db, cfg
}

// cleanFixtureVault builds a tiny, fully-connected, freshly-written vault with
// nothing for any detector to flag: no dangling/ambiguous links, no PARA drift
// (fresh mtime, no done status), no oversized notes, no stubs (both notes link
// out), and no orphans/islands (both notes fresh and in a single component).
func cleanFixtureVault(t *testing.T) (*sql.DB, core.Config) {
	t.Helper()
	vault := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(vault, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("3-Resources/Tidy One.md", "# Tidy One\n\nLinks to [[Tidy Two]].\n\n#tidy")
	write("3-Resources/Tidy Two.md", "# Tidy Two\n\nLinks back to [[Tidy One]].\n\n#tidy")

	cfg := core.Config{
		VaultPath:   vault,
		DBPath:      filepath.Join(vault, ".hebb", "index.db"),
		ExcludeDirs: []string{".obsidian", ".trash", ".hebb", ".git"},
		AutoRefresh: true,
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := core.OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := core.FullReindex(cfg, db); err != nil {
		t.Fatal(err)
	}
	return db, cfg
}

func TestAuditVaultTool(t *testing.T) {
	db, cfg := dirtyFixtureVault(t)
	out := call(t, db, cfg, "audit_vault", nil)
	if !strings.Contains(out, "Broken.md") {
		t.Errorf("audit_vault should report the dangling link in Broken.md, got:\n%s", out)
	}
	if !strings.Contains(out, "dangling_link") {
		t.Errorf("audit_vault should group the finding under dangling_link, got:\n%s", out)
	}
	if !strings.Contains(out, "Graph:") {
		t.Errorf("audit_vault should print a one-line graph summary, got:\n%s", out)
	}
}

func TestAuditVaultPathPrefixFilter(t *testing.T) {
	db, cfg := dirtyFixtureVault(t)
	out := call(t, db, cfg, "audit_vault", map[string]any{"path_prefix": "2-Areas/"})
	if strings.Contains(out, "Broken.md") {
		t.Errorf("path_prefix 2-Areas/ should exclude the 1-Projects/ dangling link, got:\n%s", out)
	}
	if !strings.Contains(out, "No findings") {
		t.Errorf("2-Areas/ has no issues of its own, expected no-findings message, got:\n%s", out)
	}
}

func TestAuditVaultTypeFilter(t *testing.T) {
	db, cfg := dirtyFixtureVault(t)

	matching := call(t, db, cfg, "audit_vault", map[string]any{"type": "dangling_link"})
	if !strings.Contains(matching, "Broken.md") {
		t.Errorf("type=dangling_link should still include Broken.md, got:\n%s", matching)
	}

	nonMatching := call(t, db, cfg, "audit_vault", map[string]any{"type": "stub"})
	if strings.Contains(nonMatching, "Broken.md") {
		t.Errorf("type=stub should filter out the dangling_link finding, got:\n%s", nonMatching)
	}
	if !strings.Contains(nonMatching, "No findings") {
		t.Errorf("no stub findings exist, expected no-findings message, got:\n%s", nonMatching)
	}
}

func TestAuditVaultCleanVaultReportsNoFindings(t *testing.T) {
	db, cfg := cleanFixtureVault(t)
	out := call(t, db, cfg, "audit_vault", nil)
	if !strings.Contains(out, "No findings") {
		t.Errorf("clean vault should report no findings, got:\n%s", out)
	}
	if strings.Contains(out, "dangling_link") || strings.Contains(out, "stub") || strings.Contains(out, "para_drift") {
		t.Errorf("clean vault should not mention any finding type, got:\n%s", out)
	}
}

// accessLogRow reads back one access_log row for assertions in the Phase 3
// tests below. ok is false when no row exists for path.
func accessLogRow(t *testing.T, db *sql.DB, path string) (accessCount, returnCount, followupCount int, lastAccessed float64, ok bool) {
	t.Helper()
	err := db.QueryRow(
		"SELECT access_count, return_count, followup_count, last_accessed FROM access_log WHERE path = ?", path,
	).Scan(&accessCount, &returnCount, &followupCount, &lastAccessed)
	if err == sql.ErrNoRows {
		return 0, 0, 0, 0, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return accessCount, returnCount, followupCount, lastAccessed, true
}

func accessLogRowCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM access_log").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestAccessLogDisabledByDefaultMakesNoWrites is the default-off guard: with
// [memory] access_log left unset (false), search_vault, expand_context and
// get_context_for_topic must make zero writes to access_log, even though all
// three return non-empty result sets against the fixture vault.
func TestAccessLogDisabledByDefaultMakesNoWrites(t *testing.T) {
	db, cfg := fixtureVault(t)
	if cfg.Memory.AccessLog {
		t.Fatal("fixture must have access_log off by default")
	}

	call(t, db, cfg, "search_vault", map[string]any{"query": "synthwave"})
	call(t, db, cfg, "expand_context", map[string]any{"note_path": "Aurora Overview"})
	call(t, db, cfg, "get_context_for_topic", map[string]any{"topic": "synthwave"})

	if n := accessLogRowCount(t, db); n != 0 {
		t.Errorf("access_log should stay empty with the flag off, got %d rows", n)
	}
}

// TestSearchVaultLogsReturns proves that with [memory] access_log on,
// search_vault bumps return_count/access_count/last_accessed for every
// returned path, and a second call increments rather than resets the counts.
func TestSearchVaultLogsReturns(t *testing.T) {
	db, cfg := fixtureVault(t)
	cfg.Memory.AccessLog = true

	before := float64(timeNowMsForTest())
	call(t, db, cfg, "search_vault", map[string]any{"query": "synthwave"})
	after := float64(timeNowMsForTest())

	access, ret, followup, lastAccessed, ok := accessLogRow(t, db, "1-Projects/Aurora Overview.md")
	if !ok {
		t.Fatalf("expected an access_log row for the returned path")
	}
	if access != 1 || ret != 1 || followup != 0 {
		t.Errorf("Aurora Overview: access=%d return=%d followup=%d, want 1/1/0", access, ret, followup)
	}
	if lastAccessed < before || lastAccessed > after {
		t.Errorf("last_accessed = %v, want within [%v, %v]", lastAccessed, before, after)
	}

	// A second identical call increments rather than overwrites.
	call(t, db, cfg, "search_vault", map[string]any{"query": "synthwave"})
	access, ret, _, _, ok = accessLogRow(t, db, "1-Projects/Aurora Overview.md")
	if !ok || access != 2 || ret != 2 {
		t.Errorf("after second search: access=%d return=%d, want 2/2 (ok=%v)", access, ret, ok)
	}
}

// TestExpandContextLogsSeedFollowupAndNeighbourReturns proves the use-vs-return
// distinction from METABOLISM.md Phase 3: expand_context bumps followup_count
// (not return_count) on the resolved seed note, and return_count on every
// returned neighbour.
func TestExpandContextLogsSeedFollowupAndNeighbourReturns(t *testing.T) {
	db, cfg := fixtureVault(t)
	cfg.Memory.AccessLog = true

	// note_path is given by title; the seed must be logged under its resolved
	// canonical path, not the raw title string.
	call(t, db, cfg, "expand_context", map[string]any{"note_path": "Aurora Overview"})

	access, ret, followup, _, ok := accessLogRow(t, db, "1-Projects/Aurora Overview.md")
	if !ok {
		t.Fatalf("expected an access_log row for the resolved seed path")
	}
	if followup != 1 || ret != 0 || access != 1 {
		t.Errorf("seed Aurora Overview: access=%d return=%d followup=%d, want 1/0/1", access, ret, followup)
	}

	for _, neighbour := range []string{"1-Projects/Aurora Decisions.md", "1-Projects/Aurora Standup.md"} {
		access, ret, followup, _, ok := accessLogRow(t, db, neighbour)
		if !ok {
			t.Fatalf("expected an access_log row for neighbour %q", neighbour)
		}
		if ret != 1 || followup != 0 || access != 1 {
			t.Errorf("neighbour %s: access=%d return=%d followup=%d, want 1/1/0", neighbour, access, ret, followup)
		}
	}

	// A second call increments the seed's followup_count and access_count, and
	// the neighbours' return_count, rather than resetting them.
	call(t, db, cfg, "expand_context", map[string]any{"note_path": "Aurora Overview"})
	access, ret, followup, _, ok = accessLogRow(t, db, "1-Projects/Aurora Overview.md")
	if !ok || access != 2 || ret != 0 || followup != 2 {
		t.Errorf("seed after 2nd call: access=%d return=%d followup=%d, want 2/0/2 (ok=%v)", access, ret, followup, ok)
	}
}

// TestGetContextForTopicLogsReturns proves get_context_for_topic bumps
// return_count/access_count for every assembled path when the flag is on.
func TestGetContextForTopicLogsReturns(t *testing.T) {
	db, cfg := fixtureVault(t)
	cfg.Memory.AccessLog = true

	call(t, db, cfg, "get_context_for_topic", map[string]any{"topic": "synthwave"})

	for _, path := range []string{"1-Projects/Aurora Overview.md", "1-Projects/Aurora Decisions.md", "1-Projects/Aurora Standup.md"} {
		access, ret, followup, _, ok := accessLogRow(t, db, path)
		if !ok {
			t.Fatalf("expected an access_log row for %q", path)
		}
		if ret != 1 || followup != 0 || access != 1 {
			t.Errorf("%s: access=%d return=%d followup=%d, want 1/1/0", path, access, ret, followup)
		}
	}
}

// timeNowMsForTest mirrors the ms-epoch-float convention used across the
// index (time.Now().UnixNano()/1e6), giving the access-log tests a comparable
// timestamp without importing core internals.
func timeNowMsForTest() float64 {
	return float64(time.Now().UnixNano()) / 1e6
}
