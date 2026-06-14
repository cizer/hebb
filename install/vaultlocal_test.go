package install

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cizer/hebb/core"
)

func TestVaultLocalCreatesConfig(t *testing.T) {
	vault := t.TempDir()
	rep, err := VaultLocal(vault)
	if err != nil {
		t.Fatalf("VaultLocal: %v", err)
	}
	// config.toml created and loadable
	if _, existed, err := core.LoadVaultConfig(vault); err != nil || !existed {
		t.Fatalf("config.toml not created (existed=%v, err=%v)", existed, err)
	}
	if statusOf(rep, "config.toml") != "created" {
		t.Errorf("config.toml status = %q, want created", statusOf(rep, "config.toml"))
	}
	// VaultLocal does not write .mcp.json (the plugin provides the MCP server).
	if _, err := os.Stat(filepath.Join(vault, ".mcp.json")); err == nil {
		t.Error("VaultLocal should not write .mcp.json")
	}
}

func TestVaultLocalIdempotent(t *testing.T) {
	vault := t.TempDir()
	if _, err := VaultLocal(vault); err != nil {
		t.Fatal(err)
	}
	rep, err := VaultLocal(vault)
	if err != nil {
		t.Fatal(err)
	}
	if statusOf(rep, "config.toml") != "exists" {
		t.Errorf("2nd run config.toml = %q, want exists (must not clobber)", statusOf(rep, "config.toml"))
	}
}

func TestVaultLocalEnablesGitInRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	vault := t.TempDir()
	if err := exec.Command("git", "-C", vault, "init").Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	rep, err := VaultLocal(vault)
	if err != nil {
		t.Fatalf("VaultLocal: %v", err)
	}
	vc, _, err := core.LoadVaultConfig(vault)
	if err != nil {
		t.Fatal(err)
	}
	if !vc.Git.Enabled {
		t.Error("expected [git] enabled = true when the vault is a git repo")
	}
	if got := statusOf(rep, "config.toml"); got != "created (git-sync on)" {
		t.Errorf("config.toml status = %q, want \"created (git-sync on)\"", got)
	}
}

func statusOf(r Report, name string) string {
	for _, s := range r.Steps {
		if s.Name == name {
			return s.Status
		}
	}
	return ""
}
