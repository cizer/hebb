package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFullReindexAndSearch(t *testing.T) {
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
	write("a.md", "# Alpha\n\nThe quick brown fox. #animals")
	write("sub/b.md", "---\ntitle: Beta\n---\n\nLazy dog sleeps. [[Alpha]]")
	write(".obsidian/skip.md", "# Excluded")

	cfg := Config{
		VaultPath:   vault,
		DBPath:      filepath.Join(vault, ".hebb", "index.db"),
		ExcludeDirs: defaultExcludeDirs,
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		t.Fatal(err)
	}

	db, err := OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	res, err := FullReindex(cfg, db)
	if err != nil {
		t.Fatal(err)
	}
	if res.Indexed != 2 {
		t.Fatalf("indexed = %d, want 2 (.obsidian excluded)", res.Indexed)
	}

	hits, err := Search(db, "fox", 10, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Path != "a.md" {
		t.Fatalf("search 'fox' = %+v, want 1 hit a.md", hits)
	}

	notes, links, _, err := Stats(db)
	if err != nil {
		t.Fatal(err)
	}
	if notes != 2 || links != 1 {
		t.Fatalf("stats notes=%d links=%d, want 2 and 1", notes, links)
	}
}
