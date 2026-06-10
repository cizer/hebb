package install

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func skillsFixture() fstest.MapFS {
	return fstest.MapFS{
		"vault-ingest/SKILL.md":       {Data: []byte("# vault-ingest\n\nfile a note")},
		"vault-ingest/scripts/run.sh": {Data: []byte("#!/bin/sh\necho hi\n")},
	}
}

func TestCodexSkillsDir(t *testing.T) {
	if got := CodexSkillsDir("/home/x"); got != filepath.Join("/home/x", ".agents", "skills") {
		t.Errorf("CodexSkillsDir = %q", got)
	}
}

func TestClaudeSkillsDir(t *testing.T) {
	if got := ClaudeSkillsDir("/home/x"); got != filepath.Join("/home/x", ".claude", "skills") {
		t.Errorf("ClaudeSkillsDir = %q", got)
	}
}

func TestInstallSkills(t *testing.T) {
	dir := t.TempDir()
	names, err := InstallSkills(skillsFixture(), dir)
	if err != nil {
		t.Fatalf("InstallSkills: %v", err)
	}
	if len(names) != 1 || names[0] != "vault-ingest" {
		t.Fatalf("names = %v, want [vault-ingest]", names)
	}
	for _, rel := range []string{"vault-ingest/SKILL.md", "vault-ingest/scripts/run.sh"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("expected %s installed: %v", rel, err)
		}
	}
}

func TestInstallSkillsIsIdempotentAndPreservesOthers(t *testing.T) {
	dir := t.TempDir()
	// A skill hebb does not own must survive.
	other := filepath.Join(dir, "my-own-skill")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "SKILL.md"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := InstallSkills(skillsFixture(), dir); err != nil {
		t.Fatal(err)
	}
	// Re-run: still succeeds (idempotent).
	if _, err := InstallSkills(skillsFixture(), dir); err != nil {
		t.Fatalf("second install: %v", err)
	}

	b, err := os.ReadFile(filepath.Join(other, "SKILL.md"))
	if err != nil || string(b) != "mine" {
		t.Fatalf("foreign skill was disturbed: %q, %v", b, err)
	}
}

func TestInstallSkillsUpdatesChangedFile(t *testing.T) {
	dir := t.TempDir()
	// Pre-seed an older version of the hebb skill.
	old := filepath.Join(dir, "vault-ingest")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "SKILL.md"), []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallSkills(skillsFixture(), dir); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(old, "SKILL.md"))
	if string(b) == "OLD" {
		t.Error("expected the hebb-owned skill to be updated to the bundled version")
	}
}
