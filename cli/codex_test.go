package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexCommandWritesConfig(t *testing.T) {
	vault := t.TempDir()
	if err := os.WriteFile(filepath.Join(vault, "note.md"), []byte("# A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	codexCfg := filepath.Join(home, ".codex", "config.toml") // parent absent

	root := newRoot("test")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"codex", "--vault", vault, "--home", home})
	if err := root.Execute(); err != nil {
		t.Fatalf("codex: %v\n%s", err, buf.String())
	}

	b, err := os.ReadFile(codexCfg)
	if err != nil {
		t.Fatalf("codex config not written: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, "[mcp_servers.hebb]") {
		t.Errorf("config missing hebb server block:\n%s", s)
	}
	// The vault path is pinned (absolute) so Codex resolves the right vault.
	abs, _ := filepath.Abs(vault)
	if !strings.Contains(s, "HEBB_VAULT = \""+abs+"\"") {
		t.Errorf("config does not pin the vault via HEBB_VAULT:\n%s", s)
	}
	if !strings.Contains(buf.String(), "config.toml") {
		t.Errorf("expected a report line, got:\n%s", buf.String())
	}
}

func TestCodexCommandCustomName(t *testing.T) {
	vault := t.TempDir()
	home := t.TempDir()
	root := newRoot("test")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"codex", "--vault", vault, "--home", home, "--mcp-name", "hebb-work"})
	if err := root.Execute(); err != nil {
		t.Fatalf("codex: %v\n%s", err, buf.String())
	}
	b, _ := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if !strings.Contains(string(b), "[mcp_servers.hebb-work]") {
		t.Errorf("custom server name not used:\n%s", b)
	}
}
