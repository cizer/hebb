package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runDoctor(t *testing.T, vault, home string) (string, error) {
	t.Helper()
	root := newRoot("test")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"doctor", "--vault", vault, "--home", home})
	err := root.Execute()
	return buf.String(), err
}

func TestDoctorCommandHealthy(t *testing.T) {
	vault := t.TempDir()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(vault, "note.md"), []byte("# A\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runInstall(t, vault, "--home", home) // install builds config, mcp, settings, memory, index

	out, err := runDoctor(t, vault, home)
	if err != nil {
		t.Fatalf("doctor reported failure on a healthy vault: %v\n%s", err, out)
	}
	for _, want := range []string{"config", "mcp.json", "index", "settings", "memory"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor output missing %q check:\n%s", want, out)
		}
	}
}

func TestDoctorCommandFailsOnEmptyVault(t *testing.T) {
	vault := t.TempDir() // never installed
	home := t.TempDir()
	out, err := runDoctor(t, vault, home)
	if err == nil {
		t.Errorf("doctor should fail (non-zero) on an uninstalled vault:\n%s", out)
	}
	if !strings.Contains(out, "FAIL") {
		t.Errorf("expected a FAIL marker in output:\n%s", out)
	}
}
