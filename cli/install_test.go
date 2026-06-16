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
	// Keep the machine-global vault registry write hermetic (install registers
	// the vault under $XDG_CONFIG_HOME).
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
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

	// Default install is data-side: config + index, no per-vault .mcp.json
	// (the plugin provides the MCP server).
	for _, want := range []string{
		filepath.Join(vault, ".hebb", "config.toml"),
		filepath.Join(vault, ".hebb", "index.db"),
	} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("expected %s to exist after install: %v", want, err)
		}
	}
	if _, err := os.Stat(filepath.Join(vault, ".mcp.json")); err == nil {
		t.Error("default install should not write .mcp.json")
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
}

func TestInstallCommandMCPJSON(t *testing.T) {
	vault := t.TempDir()
	if err := os.WriteFile(filepath.Join(vault, "note.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runInstall(t, vault, "--mcp-json")
	for _, want := range []string{
		filepath.Join(vault, ".mcp.json"),
		filepath.Join(vault, ".claude", "settings.json"),
	} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("--mcp-json should write %s: %v", want, err)
		}
	}
}

func TestInstallCommandCodexFlag(t *testing.T) {
	vault := t.TempDir()
	if err := os.WriteFile(filepath.Join(vault, "note.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	codexCfg := filepath.Join(t.TempDir(), "config.toml")
	runInstall(t, vault, "--codex", "--codex-config", codexCfg)
	b, err := os.ReadFile(codexCfg)
	if err != nil {
		t.Fatalf("--codex should write the codex config: %v", err)
	}
	if !regexp.MustCompile(`\[mcp_servers\.hebb\]`).Match(b) {
		t.Errorf("codex config missing hebb block:\n%s", b)
	}
}

func TestInstallCommandClaudeDesktopFlag(t *testing.T) {
	vault := t.TempDir()
	if err := os.WriteFile(filepath.Join(vault, "note.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	desktopCfg := filepath.Join(t.TempDir(), "claude_desktop_config.json")
	runInstall(t, vault, "--claude-desktop", "--claude-desktop-config", desktopCfg)
	b, err := os.ReadFile(desktopCfg)
	if err != nil {
		t.Fatalf("--claude-desktop should write the desktop config: %v", err)
	}
	if !regexp.MustCompile(`"hebb"`).Match(b) || !regexp.MustCompile(`"HEBB_VAULT"`).Match(b) {
		t.Errorf("desktop config missing pinned hebb server:\n%s", b)
	}
}

func TestInstallCommandNoAgentsByDefault(t *testing.T) {
	// Non-interactive (test stdin is not a TTY) + no agent flags => no agent
	// wiring is written, and the prompt never blocks.
	vault := t.TempDir()
	if err := os.WriteFile(filepath.Join(vault, "note.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	runInstall(t, vault, "--home", home)
	if _, err := os.Stat(filepath.Join(home, ".codex", "config.toml")); err == nil {
		t.Error("default install must not write a codex config")
	}
	if _, err := os.Stat(filepath.Join(vault, ".mcp.json")); err == nil {
		t.Error("default install must not write .mcp.json")
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

	// The web UI is one machine-global service (local.hebb.web), not per-vault.
	if _, err := os.Stat(filepath.Join(launchdDir, "local.hebb.web.plist")); err != nil {
		t.Errorf("expected the global web plist in %s: %v", launchdDir, err)
	}
}
