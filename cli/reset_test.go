package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/cizer/hebb/install"
)

func runReset(t *testing.T, vault, home string, extra ...string) string {
	t.Helper()
	root := newRoot("test")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	args := append([]string{"reset", "--vault", vault, "--home", home}, extra...)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("reset: %v\n%s", err, buf.String())
	}
	return buf.String()
}

func TestResetCommandDryRunByDefault(t *testing.T) {
	vault := t.TempDir()
	home := t.TempDir()
	// Install data-side so there is an index + memory link to find.
	if err := os.WriteFile(filepath.Join(vault, "note.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runInstall(t, vault, "--home", home)

	out := runReset(t, vault, home) // no --force
	if !bytes.Contains([]byte(out), []byte("dry run")) {
		t.Errorf("expected a dry-run banner, got:\n%s", out)
	}
	// Default install built an index; reset (dry) must not have removed it.
	if _, err := os.Stat(filepath.Join(vault, ".hebb", "index.db")); err != nil {
		t.Errorf("dry run should not remove the index: %v", err)
	}
	// Memory link still present.
	if _, err := os.Lstat(filepath.Join(home, ".claude", "projects", install.ClaudeProjectSlug(vault), "memory")); err != nil {
		t.Errorf("dry run should not remove the memory link: %v", err)
	}
}

func TestResetCommandForceUnwiresKeepsContent(t *testing.T) {
	vault := t.TempDir()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(vault, "note.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runInstall(t, vault, "--home", home)
	memSeed := filepath.Join(vault, ".hebb", "memory", "seed.md")
	if err := os.WriteFile(memSeed, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	runReset(t, vault, home, "--force")

	// Index cleared, memory link gone.
	if _, err := os.Stat(filepath.Join(vault, ".hebb", "index.db")); !os.IsNotExist(err) {
		t.Error("force reset should clear the index")
	}
	if _, err := os.Lstat(filepath.Join(home, ".claude", "projects", install.ClaudeProjectSlug(vault), "memory")); !os.IsNotExist(err) {
		t.Error("force reset should remove the memory link")
	}
	// Content kept.
	if _, err := os.Stat(filepath.Join(vault, "note.md")); err != nil {
		t.Error("note must be kept")
	}
	if b, err := os.ReadFile(memSeed); err != nil || string(b) != "keep me" {
		t.Errorf("memory content must be kept, got err=%v b=%q", err, b)
	}
	if _, err := os.Stat(filepath.Join(vault, ".hebb", "config.toml")); err != nil {
		t.Error("config.toml must be kept")
	}
}
