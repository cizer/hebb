package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cizer/hebb/core"
)

func checkByName(checks []Check, name string) (Check, bool) {
	for _, c := range checks {
		if c.Name == name {
			return c, true
		}
	}
	return Check{}, false
}

func TestDoctorEmptyVault(t *testing.T) {
	vault := t.TempDir()
	checks := Doctor(Options{VaultPath: vault, MCPName: DefaultMCPServerName})

	if c, _ := checkByName(checks, "config"); c.Status != "fail" {
		t.Errorf("config on empty vault = %q, want fail", c.Status)
	}
	if c, _ := checkByName(checks, "mcp.json"); c.Status != "fail" {
		t.Errorf("mcp.json on empty vault = %q, want fail", c.Status)
	}
	if c, _ := checkByName(checks, "index"); c.Status != "warn" {
		t.Errorf("index on empty vault = %q, want warn", c.Status)
	}
}

func TestDoctorHealthyAfterInstall(t *testing.T) {
	vault := t.TempDir()
	home := t.TempDir()
	launchdDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(vault, "note.md"), []byte("# A\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Asset root holding every default skill so all link cleanly.
	assetRoot := t.TempDir()
	for _, s := range core.DefaultVaultConfig("x").Skills {
		if err := os.MkdirAll(filepath.Join(assetRoot, "skills", s), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := Run(Options{
		VaultPath:  vault,
		MCPName:    DefaultMCPServerName,
		MCPCommand: DefaultMCPCommand,
		Home:       home,
		AssetRoot:  assetRoot,
		HebbBin:    "/usr/local/bin/hebb",
		LaunchdDir: launchdDir,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Build the index so the index check is healthy.
	cfg, err := core.ResolveVault(vault, "")
	if err != nil {
		t.Fatal(err)
	}
	db, err := core.OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.FullReindex(cfg, db); err != nil {
		t.Fatal(err)
	}
	db.Close()

	checks := Doctor(Options{
		VaultPath:  vault,
		MCPName:    DefaultMCPServerName,
		Home:       home,
		AssetRoot:  assetRoot,
		LaunchdDir: launchdDir,
	})
	for _, c := range checks {
		if c.Status != "ok" {
			t.Errorf("check %q = %q (%s), want ok", c.Name, c.Status, c.Detail)
		}
	}
	// Confirm the expected checks are all present.
	for _, name := range []string{"config", "mcp.json", "index", "settings", "skills", "memory", "launchd"} {
		if _, ok := checkByName(checks, name); !ok {
			t.Errorf("missing check %q", name)
		}
	}
}

func TestAnyFailed(t *testing.T) {
	if AnyFailed([]Check{{Status: "ok"}, {Status: "warn"}}) {
		t.Error("warn/ok should not count as failed")
	}
	if !AnyFailed([]Check{{Status: "ok"}, {Status: "fail"}}) {
		t.Error("a fail should be detected")
	}
}
