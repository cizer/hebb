package core

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// refreshFixture builds a vault, opens its db and does an initial full index so
// staleness tests start from a reconciled index.
func refreshFixture(t *testing.T) (Config, func(rel, content string)) {
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
	cfg := Config{VaultPath: vault, DBPath: filepath.Join(vault, ".hebb", "index.db"), ExcludeDirs: defaultExcludeDirs, AutoRefresh: true}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		t.Fatal(err)
	}
	return cfg, write
}

// backdateAll sets every .md mtime well into the past so a freshly written file
// is unambiguously newer; the millisecond-resolution mtime comparison is exact,
// but on fast machines two writes can land in the same millisecond, so tests
// that need a detectable change backdate the baseline first.
func backdateAll(t *testing.T, cfg Config) {
	t.Helper()
	past := time.Now().Add(-time.Hour)
	files, err := enumerateMarkdown(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range files {
		full := filepath.Join(cfg.VaultPath, filepath.FromSlash(rel))
		if err := os.Chtimes(full, past, past); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRefreshChangedPicksUpNewFile(t *testing.T) {
	cfg, write := refreshFixture(t)
	write("a.md", "# Alpha\n\nfirst note")
	db, err := OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := FullReindex(cfg, db); err != nil {
		t.Fatal(err)
	}

	// A file written after the index was built must be found after a refresh,
	// with no full reindex in between.
	write("b.md", "# Beta\n\nplatypus sighting")
	res, err := RefreshChanged(cfg, db)
	if err != nil {
		t.Fatal(err)
	}
	if res.Indexed != 1 {
		t.Errorf("indexed = %d, want 1 (only the new file)", res.Indexed)
	}
	if hits, _ := Search(db, "platypus", 10, "", ""); len(hits) != 1 {
		t.Errorf("new file not searchable after refresh: %+v", hits)
	}
}

func TestRefreshChangedRemovesDeletedFile(t *testing.T) {
	cfg, write := refreshFixture(t)
	write("a.md", "# Alpha\n\nkeepme")
	write("gone.md", "# Gone\n\nvanishingtoken")
	db, err := OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := FullReindex(cfg, db); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(filepath.Join(cfg.VaultPath, "gone.md")); err != nil {
		t.Fatal(err)
	}
	res, err := RefreshChanged(cfg, db)
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 1 {
		t.Errorf("removed = %d, want 1", res.Removed)
	}
	if hits, _ := Search(db, "vanishingtoken", 10, "", ""); len(hits) != 0 {
		t.Errorf("deleted file still searchable: %+v", hits)
	}
}

// TestRefreshChangedReadsNothingWhenUnchanged is the acceptance criterion that
// a staleness pass over an unchanged vault performs no file reads beyond stat().
// readFile is instrumented to count opens.
func TestRefreshChangedReadsNothingWhenUnchanged(t *testing.T) {
	cfg, write := refreshFixture(t)
	for i := 0; i < 50; i++ {
		write(filepath.Join("notes", string(rune('a'+i%26))+string(rune('a'+i/26))+".md"), "# N\n\nbody")
	}
	db, err := OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := FullReindex(cfg, db); err != nil {
		t.Fatal(err)
	}

	var reads int64
	orig := readFile
	readFile = func(name string) ([]byte, error) {
		atomic.AddInt64(&reads, 1)
		return orig(name)
	}
	defer func() { readFile = orig }()

	res, err := RefreshChanged(cfg, db)
	if err != nil {
		t.Fatal(err)
	}
	if res.Indexed != 0 || res.Removed != 0 {
		t.Errorf("unchanged vault: indexed=%d removed=%d, want 0/0", res.Indexed, res.Removed)
	}
	if n := atomic.LoadInt64(&reads); n != 0 {
		t.Errorf("RefreshChanged opened %d files on an unchanged vault, want 0 (stat-only)", n)
	}
}

// TestRefreshChangedIgnoresExcludedAndSymlinked proves a newer .md under an
// excluded directory or behind a symlink triggers no reindex.
func TestRefreshChangedIgnoresExcludedAndSymlinked(t *testing.T) {
	cfg, write := refreshFixture(t)
	write("real.md", "# Real\n\nordinary")
	db, err := OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := FullReindex(cfg, db); err != nil {
		t.Fatal(err)
	}

	// A newer note inside an excluded dir.
	write(".obsidian/new.md", "# Hidden\n\nleaktoken")
	// A symlinked note pointing at a secret outside the vault.
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.md")
	if err := os.WriteFile(secret, []byte("# Secret\n\ntopsecretcanary"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(cfg.VaultPath, "leak.md")); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}

	res, err := RefreshChanged(cfg, db)
	if err != nil {
		t.Fatal(err)
	}
	if res.Indexed != 0 {
		t.Errorf("indexed = %d, want 0 (excluded and symlinked files ignored)", res.Indexed)
	}
	if hits, _ := Search(db, "leaktoken", 10, "", ""); len(hits) != 0 {
		t.Errorf("excluded-dir note leaked into index: %+v", hits)
	}
	if hits, _ := Search(db, "topsecretcanary", 10, "", ""); len(hits) != 0 {
		t.Errorf("symlinked secret leaked into index: %+v", hits)
	}
}

// TestFullReindexSkipsUnchangedByMtime proves the changed-only FullReindex does
// not re-parse files whose mtime is unchanged, while a forced reindex does.
func TestFullReindexSkipsUnchangedByMtime(t *testing.T) {
	cfg, write := refreshFixture(t)
	write("a.md", "# Alpha\n\none")
	write("b.md", "# Beta\n\ntwo")
	db, err := OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := FullReindex(cfg, db); err != nil {
		t.Fatal(err)
	}
	backdateAll(t, cfg)
	// Reindex once so the stored mtimes match the backdated files.
	if _, err := FullReindexForce(cfg, db); err != nil {
		t.Fatal(err)
	}

	// Touch only a.md to a newer mtime.
	now := time.Now()
	if err := os.Chtimes(filepath.Join(cfg.VaultPath, "a.md"), now, now); err != nil {
		t.Fatal(err)
	}

	var reads int64
	orig := readFile
	readFile = func(name string) ([]byte, error) {
		atomic.AddInt64(&reads, 1)
		return orig(name)
	}
	defer func() { readFile = orig }()

	res, err := FullReindex(cfg, db)
	if err != nil {
		t.Fatal(err)
	}
	if res.Indexed != 1 {
		t.Errorf("changed-only reindex indexed = %d, want 1 (only a.md)", res.Indexed)
	}
	if n := atomic.LoadInt64(&reads); n != 1 {
		t.Errorf("changed-only reindex opened %d files, want 1", n)
	}

	atomic.StoreInt64(&reads, 0)
	if _, err := FullReindexForce(cfg, db); err != nil {
		t.Fatal(err)
	}
	if n := atomic.LoadInt64(&reads); n != 2 {
		t.Errorf("forced reindex opened %d files, want 2 (all re-parsed)", n)
	}
}

// TestLastFullReindexRecorded proves FullReindex stamps index_meta so vault_stats
// and doctor can report freshness.
func TestLastFullReindexRecorded(t *testing.T) {
	cfg, write := refreshFixture(t)
	write("a.md", "# Alpha\n\nbody")
	db, err := OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, ok := LastFullReindex(db); ok {
		t.Error("LastFullReindex ok = true before any reindex, want false")
	}
	before := time.Now().Add(-time.Second)
	if _, err := FullReindex(cfg, db); err != nil {
		t.Fatal(err)
	}
	ts, ok := LastFullReindex(db)
	if !ok {
		t.Fatal("LastFullReindex ok = false after reindex, want true")
	}
	if ts.Before(before) {
		t.Errorf("last reindex %v is before the reindex call %v", ts, before)
	}
}
