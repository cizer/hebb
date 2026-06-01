package install

import (
	"os"
	"path/filepath"
	"testing"
)

// makeSkills creates a srcDir with the given skill subdirectories plus a
// non-directory README that must be ignored.
func makeSkills(t *testing.T, names ...string) string {
	t.Helper()
	src := t.TempDir()
	for _, n := range names {
		if err := os.MkdirAll(filepath.Join(src, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return src
}

func TestSymlinkSkillsCreatesLinks(t *testing.T) {
	src := makeSkills(t, "build", "vault-ingest")
	dst := filepath.Join(t.TempDir(), "skills") // dst should be created

	rep, err := SymlinkSkills(src, dst)
	if err != nil {
		t.Fatalf("SymlinkSkills: %v", err)
	}
	for _, name := range []string{"build", "vault-ingest"} {
		link := filepath.Join(dst, name)
		target, err := os.Readlink(link)
		if err != nil {
			t.Errorf("%s is not a symlink: %v", name, err)
			continue
		}
		if target != filepath.Join(src, name) {
			t.Errorf("%s -> %s, want %s", name, target, filepath.Join(src, name))
		}
		if statusOf(rep, name) != "symlinked" {
			t.Errorf("%s status = %q, want symlinked", name, statusOf(rep, name))
		}
	}
	// The README file must not be linked.
	if _, err := os.Lstat(filepath.Join(dst, "README.md")); !os.IsNotExist(err) {
		t.Error("README.md (a file) should not be symlinked")
	}
}

func TestSymlinkSkillsIdempotent(t *testing.T) {
	src := makeSkills(t, "build")
	dst := filepath.Join(t.TempDir(), "skills")
	if _, err := SymlinkSkills(src, dst); err != nil {
		t.Fatal(err)
	}
	rep, err := SymlinkSkills(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	if statusOf(rep, "build") != "exists" {
		t.Errorf("2nd run status = %q, want exists", statusOf(rep, "build"))
	}
}

func TestSymlinkSkillsNeverClobbersRealDir(t *testing.T) {
	src := makeSkills(t, "build")
	dst := t.TempDir()
	// A real (non-symlink) skill dir already lives at the destination with
	// user content. Install must not destroy it.
	real := filepath.Join(dst, "build")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(real, "SKILL.md")
	if err := os.WriteFile(sentinel, []byte("user content"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := SymlinkSkills(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	if statusOf(rep, "build") != "conflict" {
		t.Errorf("status = %q, want conflict", statusOf(rep, "build"))
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("user content was destroyed: %v", err)
	}
}

func TestSymlinkSkillsRepointsStaleLink(t *testing.T) {
	src := makeSkills(t, "build")
	dst := t.TempDir()
	// A symlink pointing at the wrong place (e.g. an old install location).
	stale := filepath.Join(t.TempDir(), "old-build")
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(stale, filepath.Join(dst, "build")); err != nil {
		t.Fatal(err)
	}
	rep, err := SymlinkSkills(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	if statusOf(rep, "build") != "repointed" {
		t.Errorf("status = %q, want repointed", statusOf(rep, "build"))
	}
	target, _ := os.Readlink(filepath.Join(dst, "build"))
	if target != filepath.Join(src, "build") {
		t.Errorf("link -> %s, want %s", target, filepath.Join(src, "build"))
	}
}
