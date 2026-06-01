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
		linked := 0
		for _, s := range vc.Skills {
			if isSymlink(filepath.Join(opts.Home, ".claude", "skills", s)) {
				linked++
			}
		}
		add("skills", okIf(linked == len(vc.Skills)), fmt.Sprintf("%d/%d linked", linked, len(vc.Skills)))
	}

	if opts.Home != "" {
		link := filepath.Join(opts.Home, ".claude", "projects", ClaudeProjectSlug(opts.VaultPath), "memory")
		if isSymlink(link) {
			add("memory", "ok", "linked")
		} else {
			add("memory", "warn", "not linked (run hebb install)")
		}
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

func checkLaunchd(add func(string, string, string), opts Options, vc core.VaultConfig) {
	dir := opts.LaunchdDir
	if dir == "" && opts.Home != "" {
		dir = filepath.Join(opts.Home, "Library", "LaunchAgents")
	}
	if dir == "" {
		return
	}
	assetDir := opts.AssetRoot
	if assetDir == "" {
		assetDir = opts.DataDir // read-only: do not materialise during doctor
	}
	jobs := VaultJobs(opts.VaultPath, Slugify(vc.Name), "hebb", assetDir, opts.Home, vc.WebPort, vc.Jobs)
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
