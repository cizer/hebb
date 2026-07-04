package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/cizer/hebb/core"
)

// TestSearchSelfRefreshesBeforeQuerying is the CLI acceptance criterion: hebb
// search finds a note written moments earlier with no prior hebb index. The CLI
// has no watcher, so the read-time refresh is the only thing that can surface it.
func TestSearchSelfRefreshesBeforeQuerying(t *testing.T) {
	vault := t.TempDir()
	if err := core.DefaultVaultConfig("T").Save(vault); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "seed.md"), []byte("# Seed\n\noriginal"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Build the initial index.
	cfg, err := core.ResolveVault(vault, "")
	if err != nil {
		t.Fatal(err)
	}
	db, err := core.OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.FullReindex(cfg, db); err != nil {
		t.Fatal(err)
	}
	db.Close()

	// Write a new note after indexing, then search for it with no reindex.
	if err := os.WriteFile(filepath.Join(vault, "fresh.md"), []byte("# Fresh\n\npangolincanary token"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		root := newRoot("test")
		var buf bytes.Buffer
		root.SetOut(&buf)
		root.SetErr(&buf)
		root.SetArgs([]string{"search", "pangolincanary", "--vault", vault})
		if err := root.Execute(); err != nil {
			t.Fatalf("search: %v\n%s", err, buf.String())
		}
	})
	if !contains(out, "Fresh") {
		t.Errorf("hebb search did not self-refresh to find a note written after indexing:\n%s", out)
	}
}

// TestSearchNoResultsShowsGuidance asserts that a zero-hit search prints a
// distinct empty state (not just the bare "(0 results)" line) suggesting how
// to recover, and still exits 0.
func TestSearchNoResultsShowsGuidance(t *testing.T) {
	vault := t.TempDir()
	if err := core.DefaultVaultConfig("T").Save(vault); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "seed.md"), []byte("# Seed\n\nunrelated content"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := core.ResolveVault(vault, "")
	if err != nil {
		t.Fatal(err)
	}
	db, err := core.OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.FullReindex(cfg, db); err != nil {
		t.Fatal(err)
	}
	db.Close()

	var out string
	root := newRoot("test")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"search", "zzz-no-such-term-zzz", "--vault", vault})
	out = captureStdout(t, func() {
		if err := root.Execute(); err != nil {
			t.Fatalf("search: %v\n%s", err, buf.String())
		}
	})
	if contains(out, "(0 results)") {
		t.Errorf("expected the bare '(0 results)' line to be replaced by guidance, got:\n%s", out)
	}
	if !contains(out, "No matches") {
		t.Errorf("expected a 'No matches' empty state, got:\n%s", out)
	}
	if !contains(out, "hebb index") {
		t.Errorf("expected empty-state guidance to mention `hebb index`, got:\n%s", out)
	}
}

// captureStdout swaps os.Stdout for the duration of fn and returns what was
// written. searchCmd prints results to os.Stdout directly (via tabwriter), not
// through cobra's writer, so this is how the surface is observed.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = orig
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func contains(s, sub string) bool {
	return bytes.Contains([]byte(s), []byte(sub))
}
