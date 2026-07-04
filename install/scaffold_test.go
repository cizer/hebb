package install

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func sampleTemplate() fstest.MapFS {
	return fstest.MapFS{
		"CLAUDE.md":                {Data: []byte("# Vault guide\n\nPARA conventions.\n")},
		"README.md":                {Data: []byte("# My Vault\n")},
		"_NOW.md":                  {Data: []byte("# Now\n\n## Example\n\nreview-by: 2026-07-18\n")},
		"templates/note.md":        {Data: []byte("---\ntitle:\n---\n\n# Note title\n")},
		"templates/series-info.md": {Data: []byte("---\ntitle:\n---\n\n# Series title\n")},
		"1-Projects/.gitkeep":      {Data: []byte{}},
		"2-Areas/.gitkeep":         {Data: []byte{}},
		".hebb/memory/.gitkeep":    {Data: []byte{}},
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
		"CLAUDE.md", "README.md", "_NOW.md", "templates/note.md", "templates/series-info.md",
		"1-Projects/.gitkeep", "2-Areas/.gitkeep", ".hebb/memory/.gitkeep",
	} {
		if _, err := os.Stat(filepath.Join(dst, filepath.FromSlash(rel))); err != nil {
			t.Errorf("expected %s to exist: %v", rel, err)
		}
	}
}

// TestScaffoldShipsPinBoardAndSeriesTemplate guards the WS2.5/WS4.1 contract:
// Scaffold walks the template tree generically, so _NOW.md (the pin board)
// and templates/series-info.md (the meeting-series contract) ship to a fresh
// vault purely by existing under vault-template/, with no enumeration to
// update here or in the embed. This test scaffolds from the real repo
// vault-template/ (not the synthetic sampleTemplate) so it fails if either
// file goes missing from the actual template on disk.
func TestScaffoldShipsPinBoardAndSeriesTemplate(t *testing.T) {
	wd, err := os.Getwd() // .../install
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Dir(wd)
	tmpl := os.DirFS(filepath.Join(repoRoot, "vault-template"))

	dst := filepath.Join(t.TempDir(), "FreshVault")
	if _, err := Scaffold(tmpl, dst); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	for _, rel := range []string{"_NOW.md", "templates/series-info.md"} {
		if _, err := os.Stat(filepath.Join(dst, filepath.FromSlash(rel))); err != nil {
			t.Errorf("expected %s to exist in a freshly scaffolded vault: %v", rel, err)
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
