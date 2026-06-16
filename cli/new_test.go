package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cizer/hebb/core"
)

// runNew executes `hebb new <path>` against a hermetic temp home and data dir,
// scaffolding from the repo's own vault-template/ via --asset-root so the test
// does not depend on the embedded assets being current.
func runNew(t *testing.T, target string, extra ...string) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // keep the registry write hermetic
	assetRoot := repoRoot(t)
	root := newRoot("test")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	args := append([]string{"new", target, "--home", t.TempDir(), "--data-dir", t.TempDir(), "--asset-root", assetRoot}, extra...)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("new: %v\noutput:\n%s", err, buf.String())
	}
	return buf.String()
}

// repoRoot returns the repository root (parent of the cli/ package dir), which
// holds vault-template/.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd() // .../cli
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(wd)
}

// TestNewFromScratch is the Phase 3 guarantee: `hebb new` produces a working,
// installed vault with zero personal data, from the template alone.
func TestNewFromScratch(t *testing.T) {
	target := filepath.Join(t.TempDir(), "FreshVault") // must not exist yet
	out := runNew(t, target)

	// The PARA skeleton and baseline files are present.
	for _, rel := range []string{
		"CLAUDE.md",
		"AGENTS.md",
		"templates/note.md",
		"1-Projects", "2-Areas", "3-Resources", "4-Archives",
		filepath.Join(".hebb", "config.toml"),
		filepath.Join(".hebb", "index.db"),
		filepath.Join(".hebb", "memory"),
	} {
		if _, err := os.Stat(filepath.Join(target, rel)); err != nil {
			t.Errorf("expected %s in scaffolded vault: %v", rel, err)
		}
	}

	if !strings.Contains(out, "Created vault:") || !strings.Contains(out, "Installed vault:") {
		t.Errorf("expected create+install summary, got:\n%s", out)
	}

	// The vault is searchable: a term from the baseline CLAUDE.md is indexed.
	db, err := core.OpenDB(filepath.Join(target, ".hebb", "index.db"))
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	defer db.Close()
	hits, err := core.Search(db, "PARA", 10, "", "")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	found := false
	for _, h := range hits {
		if h.Path == "CLAUDE.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'PARA' to find CLAUDE.md in the fresh vault, got %+v", hits)
	}
}

// TestNewRefusesNonEmptyTarget guards the defensive behaviour: never scaffold
// over an existing directory's contents.
func TestNewRefusesNonEmptyTarget(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "keep.md"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := newRoot("test")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"new", target, "--home", t.TempDir(), "--data-dir", t.TempDir(), "--asset-root", repoRoot(t)})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected `new` to refuse a non-empty dir; output:\n%s", buf.String())
	}
	if _, err := os.Stat(filepath.Join(target, "CLAUDE.md")); err == nil {
		t.Error("template should not have been written into a non-empty dir")
	}
}
