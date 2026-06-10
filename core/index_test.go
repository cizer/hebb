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

// TestReindexExcludesMachineryDirs proves tool-machinery dirs (here .claude)
// are not indexed: their markdown (slash commands, agents, skills) must not
// leak into search.
func TestReindexExcludesMachineryDirs(t *testing.T) {
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
	write("note.md", "# Real\n\nordinary content")
	write(".claude/commands/deploy.md", "# Deploy\n\nclaudemachinerytoken")
	write(".claude/skills/x/SKILL.md", "# Skill\n\nclaudemachinerytoken")

	cfg := Config{VaultPath: vault, DBPath: filepath.Join(vault, ".hebb", "index.db"), ExcludeDirs: defaultExcludeDirs}
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
	if res.Indexed != 1 {
		t.Fatalf("indexed = %d, want 1 (.claude excluded)", res.Indexed)
	}
	if hits, _ := Search(db, "claudemachinerytoken", 10, "", ""); len(hits) != 0 {
		t.Fatalf(".claude markdown leaked into the index: %+v", hits)
	}
}

// TestIndexSkipsSymlinkedNotes ensures a symlinked .md is never followed and
// indexed: a note symlinked to a file outside the vault could otherwise pull
// host content (e.g. a secret) into the searchable index. Both the full walk
// and the single-file (watcher) path must refuse it.
func TestIndexSkipsSymlinkedNotes(t *testing.T) {
	vault := t.TempDir()
	outside := t.TempDir()

	secret := filepath.Join(outside, "secret.md")
	if err := os.WriteFile(secret, []byte("# Secret\n\ntopsecretcanary token"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "real.md"), []byte("# Real\n\nordinary note"), 0o644); err != nil {
		t.Fatal(err)
	}
	leak := filepath.Join(vault, "leak.md")
	if err := os.Symlink(secret, leak); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}

	cfg := Config{VaultPath: vault, DBPath: filepath.Join(vault, ".hebb", "index.db"), ExcludeDirs: defaultExcludeDirs}
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
	if res.Indexed != 1 {
		t.Fatalf("indexed = %d, want 1 (symlinked note skipped)", res.Indexed)
	}
	if hits, _ := Search(db, "topsecretcanary", 10, "", ""); len(hits) != 0 {
		t.Fatalf("symlinked secret leaked into index: %+v", hits)
	}

	// The watcher's single-file path must also refuse the symlink.
	if err := IndexFile(cfg, db, "leak.md"); err != nil {
		t.Fatalf("IndexFile: %v", err)
	}
	if hits, _ := Search(db, "topsecretcanary", 10, "", ""); len(hits) != 0 {
		t.Fatalf("IndexFile indexed a symlinked secret: %+v", hits)
	}
}
