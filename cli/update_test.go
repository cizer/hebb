package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestUpdateSkillsOnly checks the --skills-only path: it refreshes an installed
// skill from the embedded bundle and never newly installs one. This is the work
// the freshly-installed binary is re-exec'd to do after a self-replace.
func TestUpdateSkillsOnly(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude", "skills")

	// A stale installed skill gets refreshed.
	staleVI := filepath.Join(claudeDir, "vault-ingest")
	if err := os.MkdirAll(staleVI, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staleVI, "SKILL.md"), []byte("STALE"), 0o644); err != nil {
		t.Fatal(err)
	}

	root := newRoot("test")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"update", "--skills-only", "--home", home})
	if err := root.Execute(); err != nil {
		t.Fatalf("update --skills-only: %v\n%s", err, buf.String())
	}

	b, _ := os.ReadFile(filepath.Join(staleVI, "SKILL.md"))
	if string(b) == "STALE" {
		t.Errorf("installed skill was not refreshed; output:\n%s", buf.String())
	}

	// A skill that was never installed must not appear.
	codexDir := filepath.Join(home, ".agents", "skills")
	if _, err := os.Stat(filepath.Join(codexDir, "vault-ingest")); err == nil {
		t.Error("--skills-only should not newly install skills for an unused agent")
	}
}
