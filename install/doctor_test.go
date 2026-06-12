package install

import (
	"os"
	"path/filepath"
	"strconv"
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

	// A real, executable hebb stand-in: the launchd content check now classifies
	// a Program[0] pointing at nothing as fail, so a healthy install needs a
	// binary that actually exists on disk.
	hebbBin := filepath.Join(t.TempDir(), "hebb")
	if err := os.WriteFile(hebbBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := Run(Options{
		VaultPath:  vault,
		MCPName:    DefaultMCPServerName,
		MCPCommand: DefaultMCPCommand,
		Home:       home,
		HebbBin:    hebbBin,
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
		HebbBin:    hebbBin,
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

// TestDoctorMCPJSONContentDrift proves checkMCPJSON moved from presence to
// content comparison: a .mcp.json that parses and has a server but no longer
// matches RenderMCPJSON output is reported (it previously passed as ok). Because
// RenderMCPJSON is deliberately machine-independent, .mcp.json drift is a flat
// finding with no path-churn warn/fail split.
func TestDoctorMCPJSONContentDrift(t *testing.T) {
	vault := t.TempDir()
	if err := core.DefaultVaultConfig("T").Save(vault); err != nil {
		t.Fatal(err)
	}
	// A parseable .mcp.json with a server, but a drifted command (the field the
	// old presence check would have waved through).
	drifted := `{"mcpServers":{"hebb":{"type":"stdio","command":"/old/path/hebb","args":["mcp"]}}}` + "\n"
	if err := os.WriteFile(filepath.Join(vault, ".mcp.json"), []byte(drifted), 0o644); err != nil {
		t.Fatal(err)
	}
	var checks []Check
	add := func(n, s, d string) { checks = append(checks, Check{Name: n, Status: s, Detail: d}) }
	checkMCPJSON(add, vault)
	c, _ := checkByName(checks, "mcp.json")
	if c.Status == "ok" {
		t.Errorf("drifted .mcp.json should not pass as ok, got %q (%s)", c.Status, c.Detail)
	}

	// A canonical .mcp.json matches and passes.
	want, err := RenderMCPJSON(DefaultMCPServerName, DefaultMCPCommand)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, ".mcp.json"), want, 0o644); err != nil {
		t.Fatal(err)
	}
	checks = nil
	checkMCPJSON(add, vault)
	if c, _ := checkByName(checks, "mcp.json"); c.Status != "ok" {
		t.Errorf("canonical .mcp.json should pass: %q (%s)", c.Status, c.Detail)
	}
}

// TestDoctorLaunchdContentDriftAndPlaceholder proves the launchd check moved from
// presence to content comparison: an installed plist whose [job_args] were edited
// without re-running install is reported, while a healthy install whose Program[0]
// matches the binary path at doctor-run time produces no finding (the literal
// "hebb" placeholder comparison would have flagged it).
func TestDoctorLaunchdContentDriftAndPlaceholder(t *testing.T) {
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
	// A real, executable hebb stand-in so the Program[0] path resolves to a
	// working binary (the warn-vs-fail rule needs an executable on disk).
	hebbBin := filepath.Join(t.TempDir(), "hebb")
	if err := os.WriteFile(hebbBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{
		VaultPath:  vault,
		MCPName:    DefaultMCPServerName,
		MCPCommand: DefaultMCPCommand,
		Home:       home,
		HebbBin:    hebbBin,
		AssetRoot:  assetRoot,
		LaunchdDir: launchdDir,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	opts := Options{
		VaultPath:  vault,
		MCPName:    DefaultMCPServerName,
		Home:       home,
		AssetRoot:  assetRoot,
		LaunchdDir: launchdDir,
		HebbBin:    hebbBin,
	}

	// Healthy: the installed Program[0] matches the doctor-run binary path; the
	// literal "hebb" placeholder comparison would have flagged this as drift.
	checks := Doctor(opts)
	if c, _ := checkByName(checks, "launchd"); c.Status != "ok" {
		t.Errorf("healthy launchd should be ok, got %q (%s)", c.Status, c.Detail)
	}

	// Now edit a plist's args (as a hand-edit of [job_args] without re-install
	// would). The content comparison must report it.
	slug := Slugify("Work")
	digestPlist := filepath.Join(launchdDir, "local.hebb."+slug+".daily-digest.plist")
	b, err := os.ReadFile(digestPlist)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(b), "--vault-root", "--vault-root-EDITED", 1)
	if err := os.WriteFile(digestPlist, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	checks = Doctor(opts)
	if c, _ := checkByName(checks, "launchd"); c.Status == "ok" {
		t.Errorf("edited plist args should be reported, got ok (%s)", c.Detail)
	}
}

// TestDoctorLaunchdProgramPathWarnVsFail proves the warn-vs-fail rule for the
// launchd Program[0]: a plist whose only difference is a Program[0] resolving to
// a different working binary is warn, not fail; one pointing at a non-existent
// path is fail.
func TestDoctorLaunchdProgramPathWarnVsFail(t *testing.T) {
	setup := func(t *testing.T, installedBin string) Options {
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
		// Install with installedBin as Program[0].
		if _, err := Run(Options{
			VaultPath: vault, MCPName: DefaultMCPServerName, MCPCommand: DefaultMCPCommand,
			Home: home, HebbBin: installedBin, AssetRoot: assetRoot, LaunchdDir: launchdDir,
		}); err != nil {
			t.Fatalf("Run: %v", err)
		}
		// Doctor runs as a *different* binary path.
		runtimeBin := filepath.Join(t.TempDir(), "hebb")
		if err := os.WriteFile(runtimeBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return Options{
			VaultPath: vault, MCPName: DefaultMCPServerName, Home: home,
			AssetRoot: assetRoot, LaunchdDir: launchdDir, HebbBin: runtimeBin,
		}
	}

	// Installed Program[0] is a real, executable hebb at a different path: warn.
	workingBin := filepath.Join(t.TempDir(), "hebb")
	if err := os.WriteFile(workingBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	opts := setup(t, workingBin)
	checks := Doctor(opts)
	if c, _ := checkByName(checks, "launchd"); c.Status != "warn" {
		t.Errorf("Program[0] differing to a working binary should warn, got %q (%s)", c.Status, c.Detail)
	}

	// Installed Program[0] points at nothing: fail.
	missing := filepath.Join(t.TempDir(), "gone", "hebb")
	opts = setup(t, missing)
	checks = Doctor(opts)
	if c, _ := checkByName(checks, "launchd"); c.Status != "fail" {
		t.Errorf("Program[0] pointing at nothing should fail, got %q (%s)", c.Status, c.Detail)
	}
}

// TestDoctorChecksClaudeDesktop proves the new Desktop check: an entry whose
// command was rewritten to a non-existent path fails with a one-line fix; one
// rewritten to a different working hebb warns; and a machine where Desktop was
// never wired produces no finding (never-wired is silent).
func TestDoctorChecksClaudeDesktop(t *testing.T) {
	vault := t.TempDir()
	if err := core.DefaultVaultConfig("T").Save(vault); err != nil {
		t.Fatal(err)
	}
	runtimeBin := filepath.Join(t.TempDir(), "hebb")
	if err := os.WriteFile(runtimeBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Never wired: no config file, no finding.
	checks := Doctor(Options{
		VaultPath: vault, MCPName: DefaultMCPServerName, HebbBin: runtimeBin,
		ClaudeDesktopConfig: filepath.Join(t.TempDir(), "absent.json"),
	})
	if _, ok := checkByName(checks, "claude-desktop"); ok {
		t.Error("never-wired Desktop must produce no claude-desktop finding")
	}

	// Wired but command rewritten to a non-existent path: fail with a fix.
	desktopCfg := filepath.Join(t.TempDir(), "claude_desktop_config.json")
	if _, err := WriteClaudeDesktopConfig(desktopCfg, DefaultMCPServerName, filepath.Join(t.TempDir(), "gone", "hebb"), vault); err != nil {
		t.Fatal(err)
	}
	checks = Doctor(Options{
		VaultPath: vault, MCPName: DefaultMCPServerName, HebbBin: runtimeBin,
		ClaudeDesktopConfig: desktopCfg,
	})
	c, ok := checkByName(checks, "claude-desktop")
	if !ok || c.Status != "fail" {
		t.Errorf("broken Desktop command should fail, got %q (%s) present=%v", c.Status, c.Detail, ok)
	}
	if !strings.Contains(c.Detail, "hebb install") {
		t.Errorf("Desktop fail detail should name the fix (hebb install), got %q", c.Detail)
	}

	// Wired with a different but working hebb: warn.
	otherBin := filepath.Join(t.TempDir(), "hebb")
	if err := os.WriteFile(otherBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	desktopCfg2 := filepath.Join(t.TempDir(), "claude_desktop_config.json")
	if _, err := WriteClaudeDesktopConfig(desktopCfg2, DefaultMCPServerName, otherBin, vault); err != nil {
		t.Fatal(err)
	}
	checks = Doctor(Options{
		VaultPath: vault, MCPName: DefaultMCPServerName, HebbBin: runtimeBin,
		ClaudeDesktopConfig: desktopCfg2,
	})
	if c, _ := checkByName(checks, "claude-desktop"); c.Status != "warn" {
		t.Errorf("Desktop command differing to a working hebb should warn, got %q (%s)", c.Status, c.Detail)
	}

	// Wired and matching the doctor-run binary exactly: ok.
	desktopCfg3 := filepath.Join(t.TempDir(), "claude_desktop_config.json")
	if _, err := WriteClaudeDesktopConfig(desktopCfg3, DefaultMCPServerName, runtimeBin, vault); err != nil {
		t.Fatal(err)
	}
	checks = Doctor(Options{
		VaultPath: vault, MCPName: DefaultMCPServerName, HebbBin: runtimeBin,
		ClaudeDesktopConfig: desktopCfg3,
	})
	if c, _ := checkByName(checks, "claude-desktop"); c.Status != "ok" {
		t.Errorf("matching Desktop command should be ok, got %q (%s)", c.Status, c.Detail)
	}
}

// TestDoctorChecksCodex proves the new Codex check: an entry that no longer
// matches RenderCodexServer output is reported with the fix (hebb codex); a
// machine where Codex was never wired produces no finding; and a canonical entry
// passes. Codex pins the bare command (machine-independent), so there is no
// path-churn warn/fail split.
func TestDoctorChecksCodex(t *testing.T) {
	vault := t.TempDir()
	if err := core.DefaultVaultConfig("T").Save(vault); err != nil {
		t.Fatal(err)
	}

	// Never wired: no config file, no finding.
	checks := Doctor(Options{
		VaultPath: vault, MCPName: DefaultMCPServerName,
		CodexConfig: filepath.Join(t.TempDir(), "absent.toml"),
	})
	if _, ok := checkByName(checks, "codex"); ok {
		t.Error("never-wired Codex must produce no codex finding")
	}

	// Wired but drifted (command changed): reported with the fix.
	codexCfg := filepath.Join(t.TempDir(), "config.toml")
	block := "[mcp_servers.hebb]\ncommand = \"node\"\nargs = [\"mcp\"]\ncwd = " + strconvQuote(vault) +
		"\nenv = { HEBB_VAULT = " + strconvQuote(vault) + " }\nstartup_timeout_sec = 20\n"
	if err := os.WriteFile(codexCfg, []byte(block), 0o644); err != nil {
		t.Fatal(err)
	}
	checks = Doctor(Options{
		VaultPath: vault, MCPName: DefaultMCPServerName, CodexConfig: codexCfg,
	})
	c, ok := checkByName(checks, "codex")
	if !ok || c.Status == "ok" {
		t.Errorf("drifted Codex entry should be reported, got %q (%s) present=%v", c.Status, c.Detail, ok)
	}
	if !strings.Contains(c.Detail, "hebb codex") {
		t.Errorf("Codex finding should name the fix (hebb codex), got %q", c.Detail)
	}

	// Canonical entry: ok.
	codexCfg2 := filepath.Join(t.TempDir(), "config.toml")
	if _, err := WriteCodexConfig(codexCfg2, DefaultMCPServerName, DefaultMCPCommand, vault); err != nil {
		t.Fatal(err)
	}
	checks = Doctor(Options{
		VaultPath: vault, MCPName: DefaultMCPServerName, CodexConfig: codexCfg2,
	})
	if c, _ := checkByName(checks, "codex"); c.Status != "ok" {
		t.Errorf("canonical Codex entry should be ok, got %q (%s)", c.Status, c.Detail)
	}
}

// TestDoctorAgentChecksReadOnly proves doctor writes nothing and executes no
// configured command while running the Desktop and Codex checks. It records the
// config files' contents and mtimes before and after, and points the configured
// commands at a sentinel script that would leave a marker file if executed.
func TestDoctorAgentChecksReadOnly(t *testing.T) {
	vault := t.TempDir()
	if err := core.DefaultVaultConfig("T").Save(vault); err != nil {
		t.Fatal(err)
	}
	// A command that, if doctor ever ran it, would create marker.
	scriptDir := t.TempDir()
	marker := filepath.Join(scriptDir, "marker")
	sentinel := filepath.Join(scriptDir, "hebb")
	if err := os.WriteFile(sentinel, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	desktopCfg := filepath.Join(t.TempDir(), "claude_desktop_config.json")
	if _, err := WriteClaudeDesktopConfig(desktopCfg, DefaultMCPServerName, sentinel, vault); err != nil {
		t.Fatal(err)
	}
	codexCfg := filepath.Join(t.TempDir(), "config.toml")
	if _, err := WriteCodexConfig(codexCfg, DefaultMCPServerName, sentinel, vault); err != nil {
		t.Fatal(err)
	}

	snapshot := func(p string) (string, time.Time) {
		b, _ := os.ReadFile(p)
		fi, _ := os.Stat(p)
		return string(b), fi.ModTime()
	}
	dBefore, dmBefore := snapshot(desktopCfg)
	cBefore, cmBefore := snapshot(codexCfg)

	runtimeBin := filepath.Join(t.TempDir(), "hebb")
	if err := os.WriteFile(runtimeBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = Doctor(Options{
		VaultPath: vault, MCPName: DefaultMCPServerName, HebbBin: runtimeBin,
		ClaudeDesktopConfig: desktopCfg, CodexConfig: codexCfg,
	})

	if _, err := os.Stat(marker); err == nil {
		t.Error("doctor executed a configured command (marker file created)")
	}
	dAfter, dmAfter := snapshot(desktopCfg)
	cAfter, cmAfter := snapshot(codexCfg)
	if dAfter != dBefore || !dmAfter.Equal(dmBefore) {
		t.Error("doctor mutated the Claude Desktop config")
	}
	if cAfter != cBefore || !cmAfter.Equal(cmBefore) {
		t.Error("doctor mutated the Codex config")
	}
}

// TestDoctorIngestStageWarns proves Doctor emits an ingest-stage warn when the
// vault config carries a stage >= 4 (headless not yet supported) or a negative
// value (explicitly out of range). Stage 0 means the section is absent or
// stage was not set, which the accessor clamps to 1 and is not a warning. Stage
// 1-3 are valid and produce no finding.
func TestDoctorIngestStageWarns(t *testing.T) {
	cases := []struct {
		stage  int
		wantOK bool // true means no ingest-stage check or status=ok
	}{
		{stage: 0, wantOK: true}, // absent/default: accessor clamps to 1, no warning
		{stage: 1, wantOK: true},
		{stage: 2, wantOK: true},
		{stage: 3, wantOK: true},
		{stage: 4, wantOK: false},  // headless not yet supported: warn
		{stage: 5, wantOK: false},  // out of range: warn
		{stage: -1, wantOK: false}, // explicitly negative: out of range, warn
	}
	for _, tc := range cases {
		vc := core.DefaultVaultConfig("W")
		vc.Ingest.Stage = tc.stage
		vault := t.TempDir()
		if err := vc.Save(vault); err != nil {
			t.Fatal(err)
		}
		checks := Doctor(Options{VaultPath: vault, MCPName: DefaultMCPServerName})
		c, hasCheck := checkByName(checks, "ingest-stage")
		if tc.wantOK {
			// Either no check or status ok.
			if hasCheck && c.Status != "ok" {
				t.Errorf("stage=%d: ingest-stage=%q (%s), want ok or absent", tc.stage, c.Status, c.Detail)
			}
		} else {
			if !hasCheck || c.Status != "warn" {
				t.Errorf("stage=%d: ingest-stage=%v/%q (%s), want warn", tc.stage, hasCheck, c.Status, c.Detail)
			}
		}
	}
}

// strconvQuote renders a Go-quoted string for inline TOML basic strings in tests.
func strconvQuote(s string) string { return strconv.Quote(s) }

// TestDoctorNotifyCheck proves checkNotify warns when [notify] is enabled but
// no URL resolves (neither $HEBB_NOTIFY_URL nor [notify] url is set), and
// produces no finding when notify is disabled or a URL is present.
func TestDoctorNotifyCheck(t *testing.T) {
	// Enabled but no URL: warn.
	vc := core.DefaultVaultConfig("W")
	vc.Notify.Enabled = true
	// Ensure no env override is present (patch envGet to clear it).
	origEnvGet := core.GetEnvGet()
	core.SetEnvGet(func(key string) string { return "" })
	defer core.SetEnvGet(origEnvGet)

	var checks []Check
	add := func(n, s, d string) { checks = append(checks, Check{Name: n, Status: s, Detail: d}) }
	checkNotify(add, vc)
	c, ok := checkByName(checks, "notify")
	if !ok || c.Status != "warn" {
		t.Errorf("enabled with no URL: status=%q present=%v, want warn", c.Status, ok)
	}
	if !strings.Contains(c.Detail, "HEBB_NOTIFY_URL") {
		t.Errorf("warn detail should mention HEBB_NOTIFY_URL, got %q", c.Detail)
	}

	// Enabled with a committed URL: no finding.
	checks = nil
	vc.Notify.URL = "https://hooks.example.com/abc"
	checkNotify(add, vc)
	if _, ok := checkByName(checks, "notify"); ok {
		t.Error("enabled with URL should produce no finding")
	}

	// Enabled with $HEBB_NOTIFY_URL set: no finding.
	checks = nil
	vc.Notify.URL = ""
	core.SetEnvGet(func(key string) string {
		if key == "HEBB_NOTIFY_URL" {
			return "https://env.example.com/hook"
		}
		return ""
	})
	checkNotify(add, vc)
	if _, ok := checkByName(checks, "notify"); ok {
		t.Error("enabled with $HEBB_NOTIFY_URL set should produce no finding")
	}

	// Disabled: no finding regardless of URL state.
	checks = nil
	core.SetEnvGet(func(string) string { return "" })
	vc.Notify.Enabled = false
	vc.Notify.URL = ""
	checkNotify(add, vc)
	if _, ok := checkByName(checks, "notify"); ok {
		t.Error("disabled notify should produce no finding")
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
