package mcp

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

	cfg := core.Config{VaultPath: vault, DBPath: filepath.Join(vault, ".hebb", "index.db"), ExcludeDirs: []string{".obsidian", ".trash", ".hebb", ".git"}}
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

// call invokes a registered tool handler by name and returns its rendered text,
// exactly what Claude receives.
func call(t *testing.T, db *sql.DB, cfg core.Config, name string, args map[string]any) string {
	t.Helper()
	var handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
	for _, st := range toolset(db, cfg) {
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
	for _, st := range toolset(db, cfg) {
		if st.Tool.Description == "" {
			t.Errorf("tool %q has empty description", st.Tool.Name)
		}
		got[st.Tool.Name] = true
	}
	// Names are the drop-in contract with the Node onevault-mcp; guard them.
	for _, want := range []string{"search_vault", "expand_context", "get_context_for_topic", "vault_stats", "reindex_vault"} {
		if !got[want] {
			t.Errorf("missing tool %q", want)
		}
	}
	if len(got) != 5 {
		t.Errorf("expected exactly 5 tools, got %d: %v", len(got), got)
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
