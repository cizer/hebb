package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cizer/hebb/core"
)

func TestVaultLocalCreatesContracts(t *testing.T) {
	vault := t.TempDir()
	rep, err := VaultLocal(vault, DefaultMCPServerName, DefaultMCPCommand)
	if err != nil {
		t.Fatalf("VaultLocal: %v", err)
	}
	// config.toml created and loadable
	if _, existed, err := core.LoadVaultConfig(vault); err != nil || !existed {
		t.Fatalf("config.toml not created (existed=%v, err=%v)", existed, err)
	}
	// .mcp.json present
	if _, err := os.Stat(filepath.Join(vault, ".mcp.json")); err != nil {
		t.Fatalf(".mcp.json not created: %v", err)
	}
	if statusOf(rep, "config.toml") != "created" {
		t.Errorf("config.toml status = %q, want created", statusOf(rep, "config.toml"))
	}
	if statusOf(rep, ".mcp.json") != "wrote" {
		t.Errorf(".mcp.json status = %q, want wrote", statusOf(rep, ".mcp.json"))
	}
}

func TestVaultLocalIdempotent(t *testing.T) {
	vault := t.TempDir()
	if _, err := VaultLocal(vault, DefaultMCPServerName, DefaultMCPCommand); err != nil {
		t.Fatal(err)
	}
	rep, err := VaultLocal(vault, DefaultMCPServerName, DefaultMCPCommand)
	if err != nil {
		t.Fatal(err)
	}
	if statusOf(rep, "config.toml") != "exists" {
		t.Errorf("2nd run config.toml = %q, want exists (must not clobber)", statusOf(rep, "config.toml"))
	}
	if statusOf(rep, ".mcp.json") != "unchanged" {
		t.Errorf("2nd run .mcp.json = %q, want unchanged", statusOf(rep, ".mcp.json"))
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
