package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContextGraph(t *testing.T) {
	vault := t.TempDir()
	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(vault, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Alpha.md", "# Alpha\n\nThe alpha note. #topic")
	write("Beta.md", "# Beta\n\nBeta links to [[Alpha]]. #topic")

	cfg := Config{VaultPath: vault, DBPath: filepath.Join(vault, ".hebb", "index.db"), ExcludeDirs: defaultExcludeDirs}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := FullReindex(cfg, db); err != nil {
		t.Fatal(err)
	}

	if !hasRel(ExpandContext(db, "Alpha", 1, 10), "Beta.md", "incoming_link") {
		t.Fatalf("expected incoming_link Beta.md when expanding Alpha")
	}
	if !hasRel(ExpandContext(db, "Beta", 1, 10), "Alpha.md", "outgoing_link") {
		t.Fatalf("expected outgoing_link Alpha.md when expanding Beta")
	}
	if len(GetContextForTopic(db, "alpha", 10, "")) == 0 {
		t.Fatalf("expected topic context for 'alpha'")
	}
}

func hasRel(rs []ContextResult, path, rel string) bool {
	for _, r := range rs {
		if r.Path == path && r.Relationship == rel {
			return true
		}
	}
	return false
}
