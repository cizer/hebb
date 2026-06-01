package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cizer/hebb/core"
)

// Check is the result of one diagnostic. Status is "ok", "warn", or "fail".
type Check struct {
	Name   string
	Status string
	Detail string
}

// AnyFailed reports whether any check has status "fail".
func AnyFailed(checks []Check) bool {
	for _, c := range checks {
		if c.Status == "fail" {
			return true
		}
	}
	return false
}

// Doctor inspects a vault and its install and returns a check per facet. It is
// read-only: it never creates or repairs anything. Home/AssetRoot/LaunchdDir
// gate the home-side checks; when unset, those checks are omitted.
func Doctor(opts Options) []Check {
	if opts.MCPName == "" {
		opts.MCPName = DefaultMCPServerName
	}
	var checks []Check
	add := func(name, status, detail string) {
		checks = append(checks, Check{Name: name, Status: status, Detail: detail})
	}

	vc, existed, err := core.LoadVaultConfig(opts.VaultPath)
	switch {
	case err != nil:
		add("config", "fail", err.Error())
	case !existed:
		add("config", "fail", "no .hebb/config.toml (run hebb install)")
	default:
		add("config", "ok", vc.Name)
	}

	checkMCPJSON(add, opts.VaultPath)
	checkIndex(add, opts.VaultPath)
	checkSettings(add, opts.VaultPath, opts.MCPName)

	if opts.Home != "" && existed {
		checkSkills(add, opts.Home, resolvedAssetDir(opts), vc.Skills)
	}

	if opts.Home != "" {
		checkMemory(add, opts.Home, opts.VaultPath)
	}

	if existed {
		checkLaunchd(add, opts, vc)
	}
	return checks
}

func checkMCPJSON(add func(string, string, string), vaultPath string) {
	b, err := os.ReadFile(filepath.Join(vaultPath, ".mcp.json"))
	if err != nil {
		add("mcp.json", "fail", "missing (run hebb install)")
		return
	}
	var m struct {
		MCPServers map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &m); err != nil || len(m.MCPServers) == 0 {
		add("mcp.json", "fail", "present but has no servers")
		return
	}
	add("mcp.json", "ok", fmt.Sprintf("%d server(s)", len(m.MCPServers)))
}

func checkIndex(add func(string, string, string), vaultPath string) {
	dbPath := filepath.Join(vaultPath, ".hebb", "index.db")
	if _, err := os.Stat(dbPath); err != nil {
		add("index", "warn", "no index (run hebb index)")
		return
	}
	db, err := core.OpenDB(dbPath)
	if err != nil {
		add("index", "warn", err.Error())
		return
	}
	defer db.Close()
	notes, _, _, err := core.Stats(db)
	switch {
	case err != nil:
		add("index", "warn", err.Error())
	case notes == 0:
		add("index", "warn", "index is empty (run hebb index)")
	default:
		add("index", "ok", fmt.Sprintf("%d notes", notes))
	}
}

func checkSettings(add func(string, string, string), vaultPath, mcpName string) {
	b, err := os.ReadFile(filepath.Join(vaultPath, ".claude", "settings.json"))
	if err != nil {
		add("settings", "warn", "no .claude/settings.json")
		return
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		add("settings", "warn", "invalid JSON")
		return
	}
	enabled, _ := m["enabledMcpjsonServers"].([]any)
	for _, x := range enabled {
		if s, ok := x.(string); ok && s == mcpName {
			add("settings", "ok", "MCP server enabled")
			return
		}
	}
	add("settings", "warn", "MCP server not enabled")
}

// checkSkills counts only skills hebb actually manages: a skill is "linked"
// only if ~/.claude/skills/<name> is a symlink resolving to <assetDir>/skills/<name>.
// Symlinks pointing elsewhere (another tool or checkout) are reported separately
// rather than silently counted, since hebb does not own them.
func checkSkills(add func(string, string, string), home, assetDir string, skills []string) {
	linked, elsewhere := 0, 0
	for _, s := range skills {
		p := filepath.Join(home, ".claude", "skills", s)
		fi, err := os.Lstat(p)
		if err != nil || fi.Mode()&os.ModeSymlink == 0 {
			continue // absent or a real dir: not a hebb link
		}
		target, _ := os.Readlink(p)
		if assetDir != "" && target == filepath.Join(assetDir, "skills", s) {
			linked++
		} else {
			elsewhere++
		}
	}
	detail := fmt.Sprintf("%d/%d linked", linked, len(skills))
	if elsewhere > 0 {
		detail += fmt.Sprintf(" (%d symlinked elsewhere)", elsewhere)
	}
	add("skills", okIf(linked == len(skills)), detail)
}

// checkMemory verifies the Claude project memory symlink resolves to this
// vault's MemoryDir. A symlink pointing elsewhere (e.g. a stale link to an old
// memory location) is flagged rather than passed.
func checkMemory(add func(string, string, string), home, vaultPath string) {
	link := filepath.Join(home, ".claude", "projects", ClaudeProjectSlug(vaultPath), "memory")
	want := MemoryDir(vaultPath)
	target, _ := os.Readlink(link)
	switch {
	case target == want:
		add("memory", "ok", "linked")
	case isSymlink(link):
		add("memory", "warn", "linked elsewhere: "+target)
	default:
		add("memory", "warn", "not linked (run hebb install)")
	}
}

// resolvedAssetDir is the on-disk asset dir doctor compares against, read-only:
// the --asset-root override if set, otherwise the data dir. Never materialises.
func resolvedAssetDir(opts Options) string {
	if opts.AssetRoot != "" {
		return opts.AssetRoot
	}
	return opts.DataDir
}

func checkLaunchd(add func(string, string, string), opts Options, vc core.VaultConfig) {
	dir := opts.LaunchdDir
	if dir == "" && opts.Home != "" {
		dir = filepath.Join(opts.Home, "Library", "LaunchAgents")
	}
	if dir == "" {
		return
	}
	jobs := VaultJobs(opts.VaultPath, Slugify(vc.Name), "hebb", resolvedAssetDir(opts), opts.Home, vc.WebPort, vc.Jobs)
	if len(jobs) == 0 {
		return
	}
	present := 0
	for _, j := range jobs {
		if _, err := os.Stat(filepath.Join(dir, j.Label+".plist")); err == nil {
			present++
		}
	}
	add("launchd", okIf(present == len(jobs)), fmt.Sprintf("%d/%d plists", present, len(jobs)))
}

func isSymlink(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.Mode()&os.ModeSymlink != 0
}

func okIf(cond bool) string {
	if cond {
		return "ok"
	}
	return "warn"
}
