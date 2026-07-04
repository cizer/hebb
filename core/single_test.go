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

// TestIndexFileSkipsOversizeFile guards the DoS cap on the incremental
// (watcher) path: IndexFile must skip a file over maxIndexFileBytes without
// reading or indexing it, and without error (so the watcher keeps going).
func TestIndexFileSkipsOversizeFile(t *testing.T) {
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

	big := make([]byte, maxIndexFileBytes+1)
	for i := range big {
		big[i] = 'x'
	}
	if err := os.WriteFile(filepath.Join(vault, "huge.md"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := IndexFile(cfg, db, "huge.md"); err != nil {
		t.Fatalf("IndexFile on oversize file returned an error, want nil (skip + keep going): %v", err)
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM notes WHERE path = 'huge.md'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("oversize file huge.md was indexed by IndexFile, want skipped")
	}
}

// TestIndexFileOversizeGrowthDoesNotDeleteExistingEntry proves the incremental
// path also preserves a previously indexed note when it later grows past the
// size cap: unlike a genuinely unreadable/removed file, growth past the cap
// must not trigger RemoveFile.
func TestIndexFileOversizeGrowthDoesNotDeleteExistingEntry(t *testing.T) {
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

	full := filepath.Join(vault, "grows.md")
	if err := os.WriteFile(full, []byte("# Grows\n\nfindableotter token"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := IndexFile(cfg, db, "grows.md"); err != nil {
		t.Fatal(err)
	}
	if hits, _ := Search(db, "findableotter", 10, "", ""); len(hits) != 1 {
		t.Fatalf("before growth, hits = %d, want 1", len(hits))
	}

	big := make([]byte, maxIndexFileBytes+1)
	for i := range big {
		big[i] = 'x'
	}
	if err := os.WriteFile(full, big, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := IndexFile(cfg, db, "grows.md"); err != nil {
		t.Fatalf("IndexFile on grown file returned an error, want nil: %v", err)
	}
	if hits, _ := Search(db, "findableotter", 10, "", ""); len(hits) != 1 {
		t.Fatalf("after growth, previously indexed content vanished: hits = %d, want 1", len(hits))
	}
}
