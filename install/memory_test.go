package install

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClaudeProjectSlug(t *testing.T) {
	// Matches Claude Code's per-project dir naming: every non-alphanumeric
	// char becomes '-', case preserved, no collapsing.
	cases := map[string]string{
		"/Users/richie.mackay/personal/hebb": "-Users-richie-mackay-personal-hebb",
		"/v/work":                            "-v-work",
	}
	for in, want := range cases {
		if got := ClaudeProjectSlug(in); got != want {
			t.Errorf("ClaudeProjectSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSymlinkMemoryCreatesLink(t *testing.T) {
	vault := t.TempDir()
	projects := t.TempDir()
	slug := ClaudeProjectSlug(vault)

	status, err := SymlinkMemory(vault, projects, slug)
	if err != nil {
		t.Fatalf("SymlinkMemory: %v", err)
	}
	if status != "symlinked" {
		t.Errorf("status = %q, want symlinked", status)
	}
	// The vault memory dir is created...
	if fi, err := os.Stat(filepath.Join(vault, "memory")); err != nil || !fi.IsDir() {
		t.Fatalf("vault memory dir not created: %v", err)
	}
	// ...and linked into the project dir.
	link := filepath.Join(projects, slug, "memory")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("memory not symlinked: %v", err)
	}
	if target != filepath.Join(vault, "memory") {
		t.Errorf("memory -> %s, want %s", target, filepath.Join(vault, "memory"))
	}
}

func TestSymlinkMemoryIdempotent(t *testing.T) {
	vault := t.TempDir()
	projects := t.TempDir()
	slug := ClaudeProjectSlug(vault)
	if _, err := SymlinkMemory(vault, projects, slug); err != nil {
		t.Fatal(err)
	}
	status, err := SymlinkMemory(vault, projects, slug)
	if err != nil {
		t.Fatal(err)
	}
	if status != "exists" {
		t.Errorf("2nd run status = %q, want exists", status)
	}
}

func TestSymlinkMemoryNeverClobbersRealDir(t *testing.T) {
	vault := t.TempDir()
	projects := t.TempDir()
	slug := ClaudeProjectSlug(vault)
	// Claude Code already created a real memory dir with content at the target.
	realMem := filepath.Join(projects, slug, "memory")
	if err := os.MkdirAll(realMem, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(realMem, "MEMORY.md")
	if err := os.WriteFile(sentinel, []byte("existing memory"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, err := SymlinkMemory(vault, projects, slug)
	if err != nil {
		t.Fatal(err)
	}
	if status != "conflict" {
		t.Errorf("status = %q, want conflict", status)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("existing memory was destroyed: %v", err)
	}
}
