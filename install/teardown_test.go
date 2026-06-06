package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cizer/hebb/core"
)

// wiredVault builds a vault that looks fully installed: config, a note, memory
// (with content), an index, plus the machine-side wiring (memory symlink,
// launchd plist, codex block, hebb .mcp.json). Returns the opts to tear it down.
func wiredVault(t *testing.T) (TeardownOptions, string) {
	t.Helper()
	vault := t.TempDir()
	home := t.TempDir()
	launchd := t.TempDir()
	codex := filepath.Join(home, ".codex", "config.toml")

	// Vault content (must survive teardown).
	if err := os.WriteFile(filepath.Join(vault, "note.md"), []byte("# Note\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	vc := core.DefaultVaultConfig("Reset Vault")
	if err := vc.Save(vault); err != nil {
		t.Fatal(err)
	}
	slug := Slugify(vc.Name)
	mem := MemoryDir(vault)
	if err := os.MkdirAll(mem, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mem, "seed.md"), []byte("precious memory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, ".hebb", "index.db"), []byte("idx"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Machine-side wiring (must be removed). The memory link uses the PATH slug
	// (as SymlinkMemory does); launchd below uses the NAME slug.
	projMem := filepath.Join(home, ".claude", "projects", ClaudeProjectSlug(vault), "memory")
	if err := os.MkdirAll(filepath.Dir(projMem), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(mem, projMem); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(launchd, "local.hebb."+slug+".web.plist"), []byte("<plist/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteCodexConfig(codex, DefaultMCPServerName, DefaultMCPCommand, vault); err != nil {
		t.Fatal(err)
	}
	// A second codex server that must survive.
	appendFile(t, codex, "\n[mcp_servers.other]\ncommand = \"other\"\n")
	mcp, _ := RenderMCPJSON(DefaultMCPServerName, DefaultMCPCommand)
	if err := os.WriteFile(filepath.Join(vault, ".mcp.json"), mcp, 0o644); err != nil {
		t.Fatal(err)
	}

	return TeardownOptions{
		VaultPath:   vault,
		Home:        home,
		LaunchdDir:  launchd,
		CodexConfig: codex,
	}, slug
}

func appendFile(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

func TestTeardownDryRunChangesNothing(t *testing.T) {
	opts, slug := wiredVault(t)
	opts.Force = false

	rep, err := Teardown(opts)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Forced {
		t.Error("dry run should not be marked forced")
	}
	// Everything still on disk.
	mustExistT(t, filepath.Join(opts.Home, ".claude", "projects", ClaudeProjectSlug(opts.VaultPath), "memory"))
	mustExistT(t, filepath.Join(opts.LaunchdDir, "local.hebb."+slug+".web.plist"))
	mustExistT(t, filepath.Join(opts.VaultPath, ".mcp.json"))
	mustExistT(t, filepath.Join(opts.VaultPath, ".hebb", "index.db"))
	if b, _ := os.ReadFile(opts.CodexConfig); !strings.Contains(string(b), "[mcp_servers.hebb]") {
		t.Error("dry run removed the codex block")
	}
	// And the plan flags them as would-remove.
	if !AnyRemoved(rep) {
		t.Error("dry run should still report would-remove actions")
	}
}

func TestTeardownForceRemovesWiringKeepsContent(t *testing.T) {
	opts, slug := wiredVault(t)
	opts.Force = true

	if _, err := Teardown(opts); err != nil {
		t.Fatal(err)
	}

	// Wiring gone.
	if _, err := os.Lstat(filepath.Join(opts.Home, ".claude", "projects", ClaudeProjectSlug(opts.VaultPath), "memory")); !os.IsNotExist(err) {
		t.Error("memory symlink should be removed")
	}
	if _, err := os.Stat(filepath.Join(opts.LaunchdDir, "local.hebb."+slug+".web.plist")); !os.IsNotExist(err) {
		t.Error("launchd plist should be removed")
	}
	if _, err := os.Stat(filepath.Join(opts.VaultPath, ".mcp.json")); !os.IsNotExist(err) {
		t.Error(".mcp.json should be removed")
	}
	if _, err := os.Stat(filepath.Join(opts.VaultPath, ".hebb", "index.db")); !os.IsNotExist(err) {
		t.Error("index.db should be cleared")
	}
	cb, _ := os.ReadFile(opts.CodexConfig)
	// The block (and its vault path) is gone; the sibling server survives.
	if strings.Contains(string(cb), opts.VaultPath) {
		t.Errorf("hebb codex block should be removed (vault path lingered):\n%s", cb)
	}
	if !strings.Contains(string(cb), "[mcp_servers.other]") {
		t.Error("the other codex server must survive")
	}

	// Content intact - the whole point.
	mustExistT(t, filepath.Join(opts.VaultPath, "note.md"))
	mustExistT(t, filepath.Join(opts.VaultPath, ".hebb", "config.toml"))
	seed := filepath.Join(MemoryDir(opts.VaultPath), "seed.md")
	if b, err := os.ReadFile(seed); err != nil || string(b) != "precious memory" {
		t.Errorf("memory content must be preserved, got err=%v b=%q", err, b)
	}
}

func TestTeardownKeepIndex(t *testing.T) {
	opts, _ := wiredVault(t)
	opts.Force = true
	opts.KeepIndex = true
	if _, err := Teardown(opts); err != nil {
		t.Fatal(err)
	}
	mustExistT(t, filepath.Join(opts.VaultPath, ".hebb", "index.db"))
}

func TestTeardownLeavesForeignMemoryLink(t *testing.T) {
	opts, _ := wiredVault(t)
	// Repoint the memory link at somewhere outside this vault.
	link := filepath.Join(opts.Home, ".claude", "projects", ClaudeProjectSlug(opts.VaultPath), "memory")
	other := t.TempDir()
	os.Remove(link)
	if err := os.Symlink(other, link); err != nil {
		t.Fatal(err)
	}
	opts.Force = true
	if _, err := Teardown(opts); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Error("a memory link pointing elsewhere must NOT be removed")
	}
}

func TestTeardownLeavesForeignMCPJSON(t *testing.T) {
	opts, _ := wiredVault(t)
	// Replace .mcp.json with a hand-written one (not hebb's).
	custom := `{"mcpServers":{"mine":{"command":"x"}}}`
	if err := os.WriteFile(filepath.Join(opts.VaultPath, ".mcp.json"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	opts.Force = true
	if _, err := Teardown(opts); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(opts.VaultPath, ".mcp.json")); err != nil || string(b) != custom {
		t.Error("a non-hebb .mcp.json must be left untouched")
	}
}

func TestTeardownTolerantOfBareVault(t *testing.T) {
	vault := t.TempDir()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(vault, "n.md"), []byte("# n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Never installed: no wiring anywhere. Force must not error.
	rep, err := Teardown(TeardownOptions{VaultPath: vault, Home: home, LaunchdDir: t.TempDir(), Force: true})
	if err != nil {
		t.Fatalf("teardown of a bare vault should not error: %v", err)
	}
	if AnyRemoved(rep) {
		t.Errorf("nothing should be removed from a bare vault, got %+v", rep.Steps)
	}
	mustExistT(t, filepath.Join(vault, "n.md"))
}

func mustExistT(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err != nil {
		t.Errorf("expected %s to still exist: %v", path, err)
	}
}
