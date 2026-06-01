package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcherIndexesNewFile(t *testing.T) {
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
	if _, err := FullReindex(cfg, db); err != nil {
		t.Fatal(err)
	}

	w, err := Watch(cfg, db)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	time.Sleep(150 * time.Millisecond) // let the watcher settle

	if err := os.WriteFile(filepath.Join(vault, "new.md"), []byte("# New\n\nplatypus sighting"), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if hits, _ := Search(db, "platypus", 10, "", ""); len(hits) == 1 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("watcher did not index the new file within 3s")
}
