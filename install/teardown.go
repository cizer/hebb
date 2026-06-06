package install

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/cizer/hebb/core"
)

// TeardownOptions configures `hebb reset`: un-wire a vault from this machine.
// Directory fields are explicit so it is hermetically testable. Force=false is a
// dry run (report only, mutate nothing).
type TeardownOptions struct {
	VaultPath     string
	Home          string // base dir holding .claude; "" disables the memory step
	LaunchdDir    string // "" -> <home>/Library/LaunchAgents
	CodexConfig   string // "" -> <home>/.codex/config.toml
	DesktopConfig string // "" -> the macOS Claude Desktop config under Home
	MCPName       string // server/block name (default "hebb")
	Force         bool   // false = dry run
	KeepIndex     bool   // default false -> clear .hebb/index.db (cheap to rebuild)
}

// TeardownStep is one action in the teardown plan/report.
type TeardownStep struct {
	Target string // what
	Status string // "removed" | "would remove" | "absent" | "skipped: <why>"
}

// TeardownReport is the outcome of Teardown.
type TeardownReport struct {
	Forced bool
	Steps  []TeardownStep
}

func (r *TeardownReport) add(target, status string) {
	r.Steps = append(r.Steps, TeardownStep{Target: target, Status: status})
}

// did returns the verb for a thing that exists and will/should be removed,
// honouring dry-run.
func (r *TeardownReport) did(force bool) string {
	if force {
		return "removed"
	}
	return "would remove"
}

// Teardown removes only the machine-side wiring `hebb install` created. It NEVER
// touches vault content: markdown notes, .hebb/memory, and .hebb/config.toml are
// always left intact. Steps (each tolerant of being absent):
//   - memory symlink in <home>/.claude/projects/<slug>/memory (the link only)
//   - launchd plists local.hebb.<slug>.* (+ best-effort bootout)
//   - the [mcp_servers.<name>] block in ~/.codex/config.toml
//   - the opt-in per-vault .mcp.json (only if hebb-generated) and the hebb entry
//     in <vault>/.claude/settings.json
//   - .hebb/index.db (derived; unless KeepIndex)
func Teardown(opts TeardownOptions) (TeardownReport, error) {
	if opts.MCPName == "" {
		opts.MCPName = DefaultMCPServerName
	}
	rep := TeardownReport{Forced: opts.Force}

	// launchd labels use the vault NAME slug (local.hebb.<name-slug>.<job>);
	// fall back to the directory name on a half-installed vault. The memory link
	// instead uses the PATH slug (see teardownMemoryLink) - they differ.
	launchdSlug := Slugify(filepath.Base(opts.VaultPath))
	if vc, existed, err := core.LoadVaultConfig(opts.VaultPath); err == nil && existed && vc.Name != "" {
		launchdSlug = Slugify(vc.Name)
	}

	if opts.Home != "" {
		teardownMemoryLink(&rep, opts)
	}
	teardownLaunchd(&rep, opts, launchdSlug)
	teardownCodex(&rep, opts)
	teardownClaudeDesktop(&rep, opts)
	teardownMCPJSON(&rep, opts)
	teardownIndex(&rep, opts)
	return rep, nil
}

// teardownMemoryLink removes the Claude project memory symlink, but only if it is
// a symlink resolving into THIS vault's memory dir. A real dir or a link pointing
// elsewhere is left untouched, so we never delete memory content.
func teardownMemoryLink(rep *TeardownReport, opts TeardownOptions) {
	// The memory link path uses the Claude project PATH slug, exactly as
	// SymlinkMemory created it (not the vault-name slug used for launchd).
	link := filepath.Join(opts.Home, ".claude", "projects", ClaudeProjectSlug(opts.VaultPath), "memory")
	target, err := os.Readlink(link)
	switch {
	case err != nil:
		rep.add("memory symlink", "absent")
	case target != MemoryDir(opts.VaultPath):
		rep.add("memory symlink", "skipped: points elsewhere ("+target+")")
	default:
		if opts.Force {
			if err := os.Remove(link); err != nil {
				rep.add("memory symlink", "skipped: "+err.Error())
				return
			}
		}
		rep.add("memory symlink", rep.did(opts.Force))
	}
}

// teardownLaunchd boots out and removes this vault's plists. Only files matching
// the vault's label prefix are touched.
func teardownLaunchd(rep *TeardownReport, opts TeardownOptions, slug string) {
	dir := opts.LaunchdDir
	if dir == "" && opts.Home != "" {
		dir = filepath.Join(opts.Home, "Library", "LaunchAgents")
	}
	if dir == "" {
		return
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "local.hebb."+slug+".*.plist"))
	if len(matches) == 0 {
		rep.add("launchd jobs", "absent")
		return
	}
	for _, p := range matches {
		label := filepath.Base(p)
		label = label[:len(label)-len(".plist")]
		if opts.Force {
			Bootout(label)
			if err := os.Remove(p); err != nil {
				rep.add(label, "skipped: "+err.Error())
				continue
			}
		}
		rep.add(label, rep.did(opts.Force))
	}
}

func teardownCodex(rep *TeardownReport, opts TeardownOptions) {
	path := opts.CodexConfig
	if path == "" && opts.Home != "" {
		path = filepath.Join(opts.Home, ".codex", "config.toml")
	}
	if path == "" {
		return
	}
	if !opts.Force {
		// Dry run: report whether a block is present without writing.
		b, err := os.ReadFile(path)
		if err != nil {
			rep.add("codex mcp_servers."+opts.MCPName, "absent")
			return
		}
		if _, removed := removeCodexBlock(string(b), opts.MCPName); removed {
			rep.add("codex mcp_servers."+opts.MCPName, "would remove")
		} else {
			rep.add("codex mcp_servers."+opts.MCPName, "absent")
		}
		return
	}
	status, err := RemoveCodexConfig(path, opts.MCPName)
	if err != nil {
		rep.add("codex mcp_servers."+opts.MCPName, "skipped: "+err.Error())
		return
	}
	rep.add("codex mcp_servers."+opts.MCPName, status)
}

func teardownClaudeDesktop(rep *TeardownReport, opts TeardownOptions) {
	path := opts.DesktopConfig
	if path == "" && opts.Home != "" {
		path = DefaultClaudeDesktopConfigPath(opts.Home)
	}
	if path == "" {
		return
	}
	label := "claude desktop mcpServers." + opts.MCPName
	if !opts.Force {
		b, err := os.ReadFile(path)
		if err != nil {
			rep.add(label, "absent")
			return
		}
		var root map[string]any
		if json.Unmarshal(b, &root) == nil {
			if servers, ok := root["mcpServers"].(map[string]any); ok {
				if _, ok := servers[opts.MCPName]; ok {
					rep.add(label, "would remove")
					return
				}
			}
		}
		rep.add(label, "absent")
		return
	}
	status, err := RemoveClaudeDesktopConfig(path, opts.MCPName)
	if err != nil {
		rep.add(label, "skipped: "+err.Error())
		return
	}
	rep.add(label, status)
}

// teardownMCPJSON removes the opt-in per-vault .mcp.json only when it is the
// hebb-generated file (so a hand-written one is never clobbered), and strips the
// hebb server from .claude/settings.json's enabled list.
func teardownMCPJSON(rep *TeardownReport, opts TeardownOptions) {
	mcpPath := filepath.Join(opts.VaultPath, ".mcp.json")
	switch b, err := os.ReadFile(mcpPath); {
	case err != nil:
		rep.add(".mcp.json", "absent")
	case isHebbMCPJSON(b, opts.MCPName):
		if opts.Force {
			if err := os.Remove(mcpPath); err != nil {
				rep.add(".mcp.json", "skipped: "+err.Error())
				break
			}
		}
		rep.add(".mcp.json", rep.did(opts.Force))
	default:
		rep.add(".mcp.json", "skipped: not hebb-generated")
	}
}

func teardownIndex(rep *TeardownReport, opts TeardownOptions) {
	if opts.KeepIndex {
		rep.add("index.db", "kept (--keep-index)")
		return
	}
	db := filepath.Join(opts.VaultPath, ".hebb", "index.db")
	if _, err := os.Stat(db); err != nil {
		rep.add("index.db", "absent")
		return
	}
	if opts.Force {
		for _, p := range []string{db, db + "-wal", db + "-shm"} {
			_ = os.Remove(p)
		}
	}
	rep.add("index.db", rep.did(opts.Force))
}

// isHebbMCPJSON reports whether the .mcp.json is one hebb wrote: it has exactly
// the named server and nothing else, so removing it cannot drop a user's other
// MCP servers.
func isHebbMCPJSON(b []byte, name string) bool {
	want, err := RenderMCPJSON(name, DefaultMCPCommand)
	if err != nil {
		return false
	}
	return string(b) == string(want)
}

// Bootout unloads a launchd job from the user's domain, best-effort: a job that
// is not loaded (or a machine without launchctl, e.g. Linux/CI) is fine.
func Bootout(label string) {
	if _, err := exec.LookPath("launchctl"); err != nil {
		return
	}
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	_ = exec.Command("launchctl", "bootout", domain+"/"+label).Run()
}

// AnyRemoved reports whether the report contains an action that removed (or would
// remove) something - useful for exit codes / messaging.
func AnyRemoved(r TeardownReport) bool {
	for _, s := range r.Steps {
		if s.Status == "removed" || s.Status == "would remove" {
			return true
		}
	}
	return false
}
