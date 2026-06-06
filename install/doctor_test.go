package install

import (
	"os"
	"path/filepath"
	"strings"
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
	// mcp.json absent is fine now (the plugin provides the MCP server).
	if c, _ := checkByName(checks, "mcp.json"); c.Status != "ok" {
		t.Errorf("mcp.json on empty vault = %q, want ok (plugin mode)", c.Status)
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
		MCPJSON:    true, // so the mcp.json + settings checks have something to verify
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

func TestDoctorSkillsCountsOnlyHebbManagedLinks(t *testing.T) {
	vault := t.TempDir()
	home := t.TempDir() // empty: no personal skills to shadow
	assetDir := t.TempDir()
	skillsDst := filepath.Join(vault, ".claude", "skills")
	if err := os.MkdirAll(skillsDst, 0o755); err != nil {
		t.Fatal(err)
	}
	// build: a genuine hebb-managed link (-> <assetDir>/skills/build)
	if err := os.Symlink(filepath.Join(assetDir, "skills", "build"), filepath.Join(skillsDst, "build")); err != nil {
		t.Fatal(err)
	}
	// publish-artifact: a symlink owned by some other tool
	if err := os.Symlink("/somewhere/else/publish-artifact", filepath.Join(skillsDst, "publish-artifact")); err != nil {
		t.Fatal(err)
	}
	// vault-ingest: a real dir, not a link
	if err := os.MkdirAll(filepath.Join(skillsDst, "vault-ingest"), 0o755); err != nil {
		t.Fatal(err)
	}

	var checks []Check
	add := func(n, s, d string) { checks = append(checks, Check{Name: n, Status: s, Detail: d}) }
	checkSkills(add, vault, home, assetDir, []string{"build", "publish-artifact", "vault-ingest"})

	c, ok := checkByName(checks, "skills")
	if !ok {
		t.Fatal("no skills check emitted")
	}
	if !strings.Contains(c.Detail, "1/3") {
		t.Errorf("detail = %q, want 1/3 (only the hebb-managed link counts)", c.Detail)
	}
	if !strings.Contains(c.Detail, "elsewhere") {
		t.Errorf("detail = %q, want a note about the foreign symlink", c.Detail)
	}
	if c.Status != "warn" {
		t.Errorf("status = %q, want warn", c.Status)
	}
}

func TestDoctorSkillsFlagsPersonalShadow(t *testing.T) {
	vault := t.TempDir()
	home := t.TempDir()
	assetDir := t.TempDir()
	// A correct project-level link...
	skillsDst := filepath.Join(vault, ".claude", "skills")
	if err := os.MkdirAll(skillsDst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(assetDir, "skills", "vault-ingest"), filepath.Join(skillsDst, "vault-ingest")); err != nil {
		t.Fatal(err)
	}
	// ...but a personal skill of the same name exists, which Claude prefers.
	personal := filepath.Join(home, ".claude", "skills", "vault-ingest")
	if err := os.MkdirAll(personal, 0o755); err != nil {
		t.Fatal(err)
	}

	var checks []Check
	add := func(n, s, d string) { checks = append(checks, Check{Name: n, Status: s, Detail: d}) }
	checkSkills(add, vault, home, assetDir, []string{"vault-ingest"})

	c, _ := checkByName(checks, "skills")
	if c.Status != "warn" {
		t.Errorf("status = %q, want warn (project link shadowed by personal)", c.Status)
	}
	if !strings.Contains(c.Detail, "shadowed") {
		t.Errorf("detail = %q, want a note about the personal shadow", c.Detail)
	}
}

func TestDoctorMemoryFlagsStaleLink(t *testing.T) {
	home := t.TempDir()
	vault := t.TempDir()
	link := filepath.Join(home, ".claude", "projects", ClaudeProjectSlug(vault), "memory")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	// Stale: points at the old root memory location, not .hebb/memory.
	if err := os.Symlink(filepath.Join(vault, "memory"), link); err != nil {
		t.Fatal(err)
	}
	var checks []Check
	add := func(n, s, d string) { checks = append(checks, Check{Name: n, Status: s, Detail: d}) }
	checkMemory(add, home, vault)

	c, _ := checkByName(checks, "memory")
	if c.Status != "warn" || !strings.Contains(c.Detail, "elsewhere") {
		t.Errorf("stale link: status=%q detail=%q, want warn + 'elsewhere'", c.Status, c.Detail)
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
