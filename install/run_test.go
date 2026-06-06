package install

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestRunWiresEverythingLocal(t *testing.T) {
	vault := t.TempDir()
	home := t.TempDir()
	assets := makeSkills(t, "build", "vault-ingest") // assetRoot/skills/* would be the real layout
	// makeSkills returns a dir that IS the skills dir; Run expects assetRoot to
	// contain a skills/ subdir, so nest it.
	assetRoot := t.TempDir()
	if err := os.Rename(assets, filepath.Join(assetRoot, "skills")); err != nil {
		t.Fatal(err)
	}

	rep, err := Run(Options{
		VaultPath:  vault,
		MCPName:    DefaultMCPServerName,
		MCPCommand: DefaultMCPCommand,
		Home:       home,
		AssetRoot:  assetRoot,
		MCPJSON:    true, // exercise the plugin-less wiring (.mcp.json + settings)
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	mustExist(t, filepath.Join(vault, ".hebb", "config.toml"))
	mustExist(t, filepath.Join(vault, ".mcp.json"))
	mustExist(t, filepath.Join(vault, ".claude", "settings.json"))
	// Skills symlinked into <vault>/.claude/skills (project-scoped)
	for _, n := range []string{"build", "vault-ingest"} {
		link := filepath.Join(vault, ".claude", "skills", n)
		if _, err := os.Lstat(link); err != nil {
			t.Errorf("skill %s not linked into vault: %v", n, err)
		}
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

func TestRunStandaloneMaterialisesAndLinksSkills(t *testing.T) {
	vault := t.TempDir()
	home := t.TempDir()
	dataDir := t.TempDir()
	// No AssetRoot: assets come from the embedded FS, materialised to dataDir.
	assets := fstest.MapFS{
		"skills/build/SKILL.md":    {Data: []byte("build")},
		"skills/vault-ingest/S.md": {Data: []byte("ingest")},
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
	// Assets materialised to the data dir...
	if _, err := os.Stat(filepath.Join(dataDir, "skills", "build", "SKILL.md")); err != nil {
		t.Errorf("assets not materialised: %v", err)
	}
	// ...and skills linked from there into the vault's project skills dir.
	link := filepath.Join(vault, ".claude", "skills", "build")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("skill not linked: %v", err)
	}
	if target != filepath.Join(dataDir, "skills", "build") {
		t.Errorf("skill -> %s, want it under the data dir %s", target, dataDir)
	}
	if statusOf(rep, "assets") == "" {
		t.Error("report missing assets step")
	}
}

func TestRunSkipsSkillsWithoutAssetRoot(t *testing.T) {
	vault := t.TempDir()
	rep, err := Run(Options{
		VaultPath:  vault,
		MCPName:    DefaultMCPServerName,
		MCPCommand: DefaultMCPCommand,
		// no Home, no AssetRoot -> vault-local only
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustExist(t, filepath.Join(vault, ".hebb", "config.toml"))
	// Default install writes no .mcp.json/settings (plugin provides MCP).
	if _, err := os.Stat(filepath.Join(vault, ".mcp.json")); err == nil {
		t.Error("default install should not write .mcp.json")
	}
	for _, s := range rep.Steps {
		if s.Name == "build" {
			t.Error("skills should be skipped when AssetRoot is empty")
		}
	}
}

func TestRunRendersLaunchdWebJob(t *testing.T) {
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
	slug := Slugify(filepath.Base(vault))
	plist := filepath.Join(launchdDir, "local.hebb."+slug+".web.plist")
	if _, err := os.Stat(plist); err != nil {
		t.Fatalf("web plist not rendered at %s: %v", plist, err)
	}
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected %s: %v", path, err)
	}
}
