package install

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestMaterializeAssetsWritesTree(t *testing.T) {
	fsys := fstest.MapFS{
		"skills/build/SKILL.md":    {Data: []byte("build skill")},
		"skills/vault-ingest/x.md": {Data: []byte("x")},
		"automation/run-digest.sh": {Data: []byte("#!/bin/sh\necho hi\n")},
		"vault-template/CLAUDE.md": {Data: []byte("# template")},
	}
	dataDir := t.TempDir()

	n, err := MaterializeAssets(fsys, dataDir)
	if err != nil {
		t.Fatalf("MaterializeAssets: %v", err)
	}
	if n != 4 {
		t.Errorf("wrote %d files, want 4", n)
	}
	// Content preserved.
	got, err := os.ReadFile(filepath.Join(dataDir, "skills", "build", "SKILL.md"))
	if err != nil || string(got) != "build skill" {
		t.Errorf("skill content = %q, %v", got, err)
	}
	// Automation scripts are executable (embed loses the +x bit).
	fi, err := os.Stat(filepath.Join(dataDir, "automation", "run-digest.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&0o111 == 0 {
		t.Errorf("automation script mode = %v, want executable", fi.Mode())
	}
}

func TestMaterializeAssetsIdempotent(t *testing.T) {
	fsys := fstest.MapFS{"skills/build/SKILL.md": {Data: []byte("v1")}}
	dataDir := t.TempDir()
	if _, err := MaterializeAssets(fsys, dataDir); err != nil {
		t.Fatal(err)
	}
	n, err := MaterializeAssets(fsys, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("re-materialise wrote %d, want 0 (idempotent)", n)
	}
}

func TestMaterializeAssetsUpdatesChanged(t *testing.T) {
	dataDir := t.TempDir()
	if _, err := MaterializeAssets(fstest.MapFS{"skills/a/S.md": {Data: []byte("old")}}, dataDir); err != nil {
		t.Fatal(err)
	}
	n, err := MaterializeAssets(fstest.MapFS{"skills/a/S.md": {Data: []byte("new")}}, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("changed file rewrite = %d, want 1", n)
	}
	got, _ := os.ReadFile(filepath.Join(dataDir, "skills", "a", "S.md"))
	if string(got) != "new" {
		t.Errorf("content = %q, want new", got)
	}
}
