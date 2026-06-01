package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// runInstall executes `hebb install --vault <vault>` (plus any extra args) and
// returns combined output. A temp --home keeps the run hermetic.
func runInstall(t *testing.T, vault string, extra ...string) string {
	t.Helper()
	root := newRoot("test")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	args := append([]string{"install", "--vault", vault, "--home", t.TempDir(), "--data-dir", t.TempDir()}, extra...)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("install: %v\noutput:\n%s", err, buf.String())
	}
	return buf.String()
}

func TestInstallCommandEndToEnd(t *testing.T) {
	vault := t.TempDir()
	if err := os.WriteFile(filepath.Join(vault, "note.md"), []byte("# Hello\n\nbody #tag\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := runInstall(t, vault)

	for _, want := range []string{
		filepath.Join(vault, ".hebb", "config.toml"),
		filepath.Join(vault, ".mcp.json"),
		filepath.Join(vault, ".claude", "settings.json"),
		filepath.Join(vault, ".hebb", "index.db"),
	} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("expected %s to exist after install: %v", want, err)
		}
	}
	if !regexp.MustCompile(`index\s+1 notes indexed`).MatchString(out) {
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
	if !regexp.MustCompile(`config\.toml\s+exists`).MatchString(out) {
		t.Errorf("second install should report config.toml exists, got:\n%s", out)
	}
	if !regexp.MustCompile(`\.mcp\.json\s+unchanged`).MatchString(out) {
		t.Errorf("second install should report .mcp.json unchanged, got:\n%s", out)
	}
}

func TestInstallCommandRendersLaunchd(t *testing.T) {
	vault := t.TempDir()
	if err := os.WriteFile(filepath.Join(vault, "note.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	launchdDir := t.TempDir()

	root := newRoot("test")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"install", "--vault", vault, "--home", home, "--data-dir", t.TempDir(), "--launchd", "--launchd-dir", launchdDir})
	if err := root.Execute(); err != nil {
		t.Fatalf("install: %v\n%s", err, buf.String())
	}

	matches, _ := filepath.Glob(filepath.Join(launchdDir, "local.hebb.*.web.plist"))
	if len(matches) != 1 {
		t.Errorf("expected one web plist in %s, got %v", launchdDir, matches)
	}
}

func TestInstallCommandWiresSkills(t *testing.T) {
	vault := t.TempDir()
	if err := os.WriteFile(filepath.Join(vault, "note.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Asset root with a skills/ dir holding one skill.
	assetRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(assetRoot, "skills", "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()

	root := newRoot("test")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"install", "--vault", vault, "--home", home, "--data-dir", t.TempDir(), "--asset-root", assetRoot})
	if err := root.Execute(); err != nil {
		t.Fatalf("install: %v\n%s", err, buf.String())
	}

	link := filepath.Join(vault, ".claude", "skills", "build")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("skill not symlinked into vault: %v", err)
	}
	if target != filepath.Join(assetRoot, "skills", "build") {
		t.Errorf("link -> %s, want %s", target, filepath.Join(assetRoot, "skills", "build"))
	}
}
