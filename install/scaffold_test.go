package install

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func sampleTemplate() fstest.MapFS {
	return fstest.MapFS{
		"CLAUDE.md":             {Data: []byte("# Vault guide\n\nPARA conventions.\n")},
		"README.md":             {Data: []byte("# My Vault\n")},
		"templates/note.md":     {Data: []byte("---\ntitle:\n---\n\n#\n")},
		"1-Projects/.gitkeep":   {Data: []byte{}},
		"2-Areas/.gitkeep":      {Data: []byte{}},
		".hebb/memory/.gitkeep": {Data: []byte{}},
	}
}

func TestScaffoldCopiesTemplate(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "FreshVault") // does not exist yet
	rep, err := Scaffold(sampleTemplate(), dst)
	if err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	if len(rep.Steps) != 1 || rep.Steps[0].Name != "scaffold" {
		t.Fatalf("unexpected report: %+v", rep.Steps)
	}
	for _, rel := range []string{
		"CLAUDE.md", "README.md", "templates/note.md",
		"1-Projects/.gitkeep", "2-Areas/.gitkeep", ".hebb/memory/.gitkeep",
	} {
		if _, err := os.Stat(filepath.Join(dst, filepath.FromSlash(rel))); err != nil {
			t.Errorf("expected %s to exist: %v", rel, err)
		}
	}
}

func TestScaffoldRefusesNonEmptyDir(t *testing.T) {
	dst := t.TempDir() // exists and we add a file
	if err := os.WriteFile(filepath.Join(dst, "existing.md"), []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Scaffold(sampleTemplate(), dst); err == nil {
		t.Fatal("expected Scaffold to refuse a non-empty directory")
	}
	// The pre-existing file must be untouched.
	b, err := os.ReadFile(filepath.Join(dst, "existing.md"))
	if err != nil || string(b) != "keep me" {
		t.Fatalf("existing file was disturbed: %q, %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(dst, "CLAUDE.md")); err == nil {
		t.Error("template should not have been written into a non-empty dir")
	}
}

func TestScaffoldIntoEmptyExistingDir(t *testing.T) {
	dst := t.TempDir() // exists, empty
	if _, err := Scaffold(sampleTemplate(), dst); err != nil {
		t.Fatalf("Scaffold into empty dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "CLAUDE.md")); err != nil {
		t.Errorf("expected CLAUDE.md after scaffolding into empty dir: %v", err)
	}
}
