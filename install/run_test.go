package install

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/cizer/hebb/core"
)

func TestRunWiresEverythingLocal(t *testing.T) {
	vault := t.TempDir()
	home := t.TempDir()

	rep, err := Run(Options{
		VaultPath:  vault,
		MCPName:    DefaultMCPServerName,
		MCPCommand: DefaultMCPCommand,
		Home:       home,
		MCPJSON:    true, // exercise the plugin-less wiring (.mcp.json + settings)
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	mustExist(t, filepath.Join(vault, ".hebb", "config.toml"))
	mustExist(t, filepath.Join(vault, ".mcp.json"))
	mustExist(t, filepath.Join(vault, ".claude", "settings.json"))
	// Install never touches .claude/skills: the plugin delivers skills.
	if _, err := os.Lstat(filepath.Join(vault, ".claude", "skills")); err == nil {
		t.Error("install should not create <vault>/.claude/skills (the plugin ships skills)")
	}
	if statusOf(rep, "settings.json") == "" {
		t.Error("report missing settings.json step")
	}
	// Memory linked into the project dir.
	memLink := filepath.Join(home, ".claude", "projects", ClaudeProjectSlug(vault), "memory")
	if _, err := os.Lstat(memLink); err != nil {
		t.Errorf("memory not linked into project dir: %v", err)
	}
	if statusOf(rep, "memory") != "symlinked" {
		t.Errorf("memory step = %q, want symlinked", statusOf(rep, "memory"))
	}
}

func TestRunRegistersVault(t *testing.T) {
	vault := t.TempDir()
	registry := filepath.Join(t.TempDir(), "vaults.toml")

	rep, err := Run(Options{
		VaultPath:    vault,
		MCPName:      DefaultMCPServerName,
		MCPCommand:   DefaultMCPCommand,
		RegistryPath: registry,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if statusOf(rep, "registry") != "registered" {
		t.Errorf("expected a registry step, got steps %+v", rep.Steps)
	}
	r, err := core.LoadRegistry(registry)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Vaults) != 1 || r.Vaults[0].Name != filepath.Base(vault) {
		t.Fatalf("vault not registered: %+v", r.Vaults)
	}

	// Empty RegistryPath skips registration entirely.
	rep2, err := Run(Options{VaultPath: t.TempDir(), MCPName: DefaultMCPServerName, MCPCommand: DefaultMCPCommand})
	if err != nil {
		t.Fatal(err)
	}
	if statusOf(rep2, "registry") != "" {
		t.Error("registration should be skipped when RegistryPath is empty")
	}
}

func TestRunStandaloneMaterialisesAutomation(t *testing.T) {
	vault := t.TempDir()
	home := t.TempDir()
	dataDir := t.TempDir()
	// No AssetRoot: assets come from the embedded FS, materialised to dataDir so
	// launchd jobs can find their automation scripts. Skills are not materialised.
	assets := fstest.MapFS{
		"automation/run-digest.sh": {Data: []byte("#!/bin/sh\n")},
	}

	rep, err := Run(Options{
		VaultPath:  vault,
		MCPName:    DefaultMCPServerName,
		MCPCommand: DefaultMCPCommand,
		Home:       home,
		Assets:     assets,
		DataDir:    dataDir,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Automation materialised to the data dir.
	if _, err := os.Stat(filepath.Join(dataDir, "automation", "run-digest.sh")); err != nil {
		t.Errorf("automation not materialised: %v", err)
	}
	// Nothing skill-related lands in the vault.
	if _, err := os.Lstat(filepath.Join(vault, ".claude", "skills")); err == nil {
		t.Error("install should not create <vault>/.claude/skills")
	}
	if statusOf(rep, "assets") == "" {
		t.Error("report missing assets step")
	}
}

func TestRunVaultLocalOnly(t *testing.T) {
	vault := t.TempDir()
	_, err := Run(Options{
		VaultPath:  vault,
		MCPName:    DefaultMCPServerName,
		MCPCommand: DefaultMCPCommand,
		// no Home, no AssetRoot, no MCPJSON -> vault-local config only
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustExist(t, filepath.Join(vault, ".hebb", "config.toml"))
	// Default install writes no .mcp.json/settings (plugin provides MCP).
	if _, err := os.Stat(filepath.Join(vault, ".mcp.json")); err == nil {
		t.Error("default install should not write .mcp.json")
	}
}

func TestRunRendersGlobalWebJob(t *testing.T) {
	vault := t.TempDir()
	home := t.TempDir()
	launchdDir := t.TempDir()

	_, err := Run(Options{
		VaultPath:  vault,
		MCPName:    DefaultMCPServerName,
		MCPCommand: DefaultMCPCommand,
		Home:       home,
		HebbBin:    "/usr/local/bin/hebb",
		LaunchdDir: launchdDir,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The web UI is one machine-global job (no vault slug), not per-vault.
	if _, err := os.Stat(filepath.Join(launchdDir, GlobalWebLabel+".plist")); err != nil {
		t.Fatalf("global web plist not rendered: %v", err)
	}
	slug := Slugify(filepath.Base(vault))
	if _, err := os.Stat(filepath.Join(launchdDir, "local.hebb."+slug+".web.plist")); err == nil {
		t.Error("a per-vault web plist should no longer be rendered")
	}
}

func TestRunRetiresStalePerVaultWebPlist(t *testing.T) {
	vault := t.TempDir()
	home := t.TempDir()
	launchdDir := t.TempDir()
	// A leftover per-vault web plist from a previous version.
	stale := filepath.Join(launchdDir, "local.hebb.work.web.plist")
	if err := os.WriteFile(stale, []byte("<plist/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Run(Options{
		VaultPath:  vault,
		MCPName:    DefaultMCPServerName,
		MCPCommand: DefaultMCPCommand,
		Home:       home,
		HebbBin:    "/usr/local/bin/hebb",
		LaunchdDir: launchdDir,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("stale per-vault web plist should be removed on install (web consolidated)")
	}
	if _, err := os.Stat(filepath.Join(launchdDir, GlobalWebLabel+".plist")); err != nil {
		t.Errorf("global web plist should remain: %v", err)
	}
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected %s: %v", path, err)
	}
}
