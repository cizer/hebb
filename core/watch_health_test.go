package core

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcherHealthReportsLiveness(t *testing.T) {
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
	if h := w.Health(); !h.Alive {
		t.Errorf("freshly started watcher Alive = false, want true")
	}
	if h := w.Health(); !h.LastEventAt.IsZero() {
		t.Errorf("no events yet but LastEventAt = %v, want zero", h.LastEventAt)
	}

	// A write should drive a content event and update LastEventAt.
	time.Sleep(150 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(vault, "new.md"), []byte("# New\n\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !w.Health().LastEventAt.IsZero() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if w.Health().LastEventAt.IsZero() {
		t.Error("LastEventAt still zero after a write event")
	}

	w.Close()
	// Close stops the loop; the dead flag should settle shortly after.
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if !w.Health().Alive {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("watcher still Alive after Close")
}

func TestFailedWatcherHealth(t *testing.T) {
	h := FailedWatcherHealth(errors.New("boom"))
	if h.Alive {
		t.Error("failed watcher health Alive = true, want false")
	}
	if h.StartErr != "boom" {
		t.Errorf("StartErr = %q, want boom", h.StartErr)
	}
}
