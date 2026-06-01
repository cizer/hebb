package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runInstall executes `hebb install --vault <vault>` and returns combined output.
func runInstall(t *testing.T, vault string) string {
	t.Helper()
	root := newRoot("test")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"install", "--vault", vault})
	if err := root.Execute(); err != nil {
		t.Fatalf("install: %v\noutput:\n%s", err, buf.String())
	}
	return buf.String()
}

func TestInstallCommandEndToEnd(t *testing.T) {
	vault := t.TempDir()
	// A minimal markdown corpus so the first index has something to do.
	if err := os.WriteFile(filepath.Join(vault, "note.md"), []byte("# Hello\n\nbody #tag\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := runInstall(t, vault)

	for _, want := range []string{
		filepath.Join(vault, ".hebb", "config.toml"),
		filepath.Join(vault, ".mcp.json"),
		filepath.Join(vault, ".hebb", "index.db"),
	} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("expected %s to exist after install: %v", want, err)
		}
	}
	if !strings.Contains(out, "1 notes indexed") {
		t.Errorf("expected index summary in output, got:\n%s", out)
	}
}

func TestInstallCommandIdempotent(t *testing.T) {
	vault := t.TempDir()
	if err := os.WriteFile(filepath.Join(vault, "note.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runInstall(t, vault)
	out := runInstall(t, vault)
	if !strings.Contains(out, "config.toml    exists") {
		t.Errorf("second install should report config.toml exists, got:\n%s", out)
	}
	if !strings.Contains(out, ".mcp.json      unchanged") {
		t.Errorf("second install should report .mcp.json unchanged, got:\n%s", out)
	}
}
