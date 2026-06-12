package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

	if _, err := Run(Options{
		VaultPath:  vault,
		MCPName:    DefaultMCPServerName,
		MCPCommand: DefaultMCPCommand,
		Home:       home,
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
		LaunchdDir: launchdDir,
	})
	for _, c := range checks {
		if c.Status != "ok" {
			t.Errorf("check %q = %q (%s), want ok", c.Name, c.Status, c.Detail)
		}
	}
	// Confirm the expected checks are all present. Skills are no longer an
	// install concern (the plugin delivers them), so there is no skills check.
	for _, name := range []string{"config", "mcp.json", "index", "settings", "memory", "launchd"} {
		if _, ok := checkByName(checks, name); !ok {
			t.Errorf("missing check %q", name)
		}
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

// TestDoctorWarnsOnStaleIndex proves checkIndex flags a note newer on disk than
// anything indexed, and that excluded/symlinked files cannot cause a false
// staleness warning (they use the same walk as indexing).
func TestDoctorWarnsOnStaleIndex(t *testing.T) {
	vault := t.TempDir()
	writeNote := func(rel, body string) {
		p := filepath.Join(vault, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeNote("a.md", "# A\n\nbody")
	// Save a config so checkLaunchd and config check behave, and excludes resolve.
	if err := core.DefaultVaultConfig("T").Save(vault); err != nil {
		t.Fatal(err)
	}

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

	// Healthy: index is current.
	checks := Doctor(Options{VaultPath: vault, MCPName: DefaultMCPServerName})
	if c, _ := checkByName(checks, "index"); c.Status != "ok" {
		t.Errorf("fresh index: status=%q detail=%q, want ok", c.Status, c.Detail)
	}

	// Now write a newer note without reindexing: doctor should warn.
	future := time.Now().Add(time.Hour)
	writeNote("b.md", "# B\n\nnewer body")
	if err := os.Chtimes(filepath.Join(vault, "b.md"), future, future); err != nil {
		t.Fatal(err)
	}
	checks = Doctor(Options{VaultPath: vault, MCPName: DefaultMCPServerName})
	if c, _ := checkByName(checks, "index"); c.Status != "warn" || !strings.Contains(c.Detail, "stale") {
		t.Errorf("stale index: status=%q detail=%q, want warn + 'stale'", c.Status, c.Detail)
	}

	// A newer file under an excluded dir must NOT raise staleness. Reindex first
	// so the visible notes are current again.
	cfg, _ = core.ResolveVault(vault, "")
	db, _ = core.OpenDB(cfg.DBPath)
	if _, err := core.FullReindex(cfg, db); err != nil {
		t.Fatal(err)
	}
	db.Close()
	writeNote(".obsidian/excluded.md", "# X\n\nshould not count")
	if err := os.Chtimes(filepath.Join(vault, ".obsidian", "excluded.md"), future, future); err != nil {
		t.Fatal(err)
	}
	checks = Doctor(Options{VaultPath: vault, MCPName: DefaultMCPServerName})
	if c, _ := checkByName(checks, "index"); c.Status != "ok" {
		t.Errorf("newer excluded-dir note: status=%q detail=%q, want ok (must not trigger staleness)", c.Status, c.Detail)
	}
}

// TestDoctorLintsShellWrapperProgram proves the TCC lint warns when an installed
// plist's Program[0] is a shell wrapper (the field failure mode), names the Full
// Disk Access grant step, and stays read-only (no launchd job is spawned). A
// grantable binary Program[0] produces no such finding.
func TestDoctorLintsShellWrapperProgram(t *testing.T) {
	vault := t.TempDir()
	home := t.TempDir()
	launchdDir := t.TempDir()
	assetRoot := t.TempDir()
	autoDir := filepath.Join(assetRoot, "automation")
	if err := os.MkdirAll(autoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Present so the daily-digest job renders (and so its label is one doctor
	// looks for installed plists under).
	for _, f := range []string{"generate-vault-digest.py", "run-vault-digest.sh"} {
		if err := os.WriteFile(filepath.Join(autoDir, f), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := core.DefaultVaultConfig("Work").Save(vault); err != nil {
		t.Fatal(err)
	}

	opts := Options{
		VaultPath:  vault,
		MCPName:    DefaultMCPServerName,
		Home:       home,
		AssetRoot:  assetRoot,
		LaunchdDir: launchdDir,
	}

	// Write an old-style daily-digest plist whose Program[0] is the shell wrapper.
	slug := Slugify("Work")
	label := "local.hebb." + slug + ".daily-digest"
	wrapper := filepath.Join(autoDir, "run-vault-digest.sh")
	plist := "<?xml version=\"1.0\"?>\n<plist><dict>\n" +
		"<key>Label</key><string>" + label + "</string>\n" +
		"<key>ProgramArguments</key><array>\n" +
		"  <string>" + wrapper + "</string>\n" +
		"  <string>--vault-root</string>\n  <string>" + vault + "</string>\n" +
		"</array>\n</dict></plist>\n"
	if err := os.WriteFile(filepath.Join(launchdDir, label+".plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}

	checks := Doctor(opts)
	c, ok := checkByName(checks, "launchd-tcc")
	if !ok {
		t.Fatalf("expected a launchd-tcc check, got: %v", checks)
	}
	if c.Status != "warn" {
		t.Errorf("shell-wrapper Program[0]: status=%q, want warn (detail=%q)", c.Status, c.Detail)
	}
	if !strings.Contains(c.Detail, "Full Disk Access") {
		t.Errorf("lint detail must name the Full Disk Access grant step, got: %q", c.Detail)
	}
	if !strings.Contains(c.Detail, label) {
		t.Errorf("lint detail should name the offending job %q, got: %q", label, c.Detail)
	}
}

// TestDoctorTCCLintCleanOnGrantableProgram proves the lint does not false-positive
// when installed plists use a grantable binary Program[0], and that doctor's own
// expected-job rendering (which uses the "hebb" placeholder) never trips it.
func TestDoctorTCCLintCleanOnGrantableProgram(t *testing.T) {
	vault := t.TempDir()
	home := t.TempDir()
	launchdDir := t.TempDir()
	assetRoot := t.TempDir()
	autoDir := filepath.Join(assetRoot, "automation")
	if err := os.MkdirAll(autoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"generate-vault-digest.py", "generate-action-review.py"} {
		if err := os.WriteFile(filepath.Join(autoDir, f), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := core.DefaultVaultConfig("Work").Save(vault); err != nil {
		t.Fatal(err)
	}
	// Install renders the jobs (Program[0] = the binary path) and writes the plists.
	if _, err := Run(Options{
		VaultPath:  vault,
		MCPName:    DefaultMCPServerName,
		MCPCommand: DefaultMCPCommand,
		Home:       home,
		HebbBin:    "/usr/local/bin/hebb",
		AssetRoot:  assetRoot,
		LaunchdDir: launchdDir,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	checks := Doctor(Options{
		VaultPath:  vault,
		MCPName:    DefaultMCPServerName,
		Home:       home,
		AssetRoot:  assetRoot,
		LaunchdDir: launchdDir,
	})
	if c, ok := checkByName(checks, "launchd-tcc"); ok && c.Status != "ok" {
		t.Errorf("grantable Program[0]: launchd-tcc=%q (%s), want ok or absent", c.Status, c.Detail)
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
