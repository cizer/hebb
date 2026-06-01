package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIndexFileAndRemove(t *testing.T) {
	vault := t.TempDir()
	cfg := Config{VaultPath: vault, DBPath: filepath.Join(vault, ".hebb", "index.db"), ExcludeDirs: defaultExcludeDirs}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := os.WriteFile(filepath.Join(vault, "a.md"), []byte("# A\n\nhello kangaroo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := IndexFile(cfg, db, "a.md"); err != nil {
		t.Fatal(err)
	}
	if hits, _ := Search(db, "kangaroo", 10, "", ""); len(hits) != 1 {
		t.Fatalf("after IndexFile, hits = %d, want 1", len(hits))
	}
	if err := RemoveFile(db, "a.md"); err != nil {
		t.Fatal(err)
	}
	if hits, _ := Search(db, "kangaroo", 10, "", ""); len(hits) != 0 {
		t.Fatalf("after RemoveFile, hits = %d, want 0", len(hits))
	}
}
