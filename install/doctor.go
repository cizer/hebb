package install

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	checkIndex(add, opts.VaultPath, vc)
	checkSettings(add, opts.VaultPath, opts.MCPName)

	if opts.Home != "" {
		checkMemory(add, opts.Home, opts.VaultPath)
	}

	if existed {
		checkLaunchd(add, opts, vc)
		checkLaunchdTCC(add, opts, vc)
	}
	return checks
}

func checkMCPJSON(add func(string, string, string), vaultPath string) {
	b, err := os.ReadFile(filepath.Join(vaultPath, ".mcp.json"))
	if err != nil {
		// No per-vault .mcp.json is normal: the hebb plugin provides the MCP
		// server. Only validate one if it exists (plugin-less / --mcp-json).
		add("mcp.json", "ok", "none (plugin provides the MCP server)")
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

func checkIndex(add func(string, string, string), vaultPath string, vc core.VaultConfig) {
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
		return
	case notes == 0:
		add("index", "warn", "index is empty (run hebb index)")
		return
	}
	// Staleness: the newest .md on disk is newer than anything indexed. The walk
	// uses the same exclude_dirs and symlink filter as indexing, so a newer file
	// under an excluded dir or behind a symlink (which the index would never
	// hold) cannot raise a false warning.
	if stale, detail := indexStale(vaultPath, vc, db); stale {
		add("index", "warn", detail)
		return
	}
	add("index", "ok", fmt.Sprintf("%d notes", notes))
}

// staleMtimeEpsilonMillis is the slack allowed before calling the index stale,
// guarding against sub-millisecond float jitter between a file's stored mtime
// and its re-stat'd value. A genuine edit moves the mtime by far more.
const staleMtimeEpsilonMillis = 1.0

// indexStale reports whether the newest indexable .md on disk post-dates the
// newest indexed note. Returns a one-line detail for the warning when true.
func indexStale(vaultPath string, vc core.VaultConfig, db *sql.DB) (bool, string) {
	excludes := vc.ExcludeDirs
	if len(excludes) == 0 {
		excludes = core.DefaultExcludeDirs()
	}
	cfg := core.Config{VaultPath: vaultPath, ExcludeDirs: excludes}
	newest, ok := core.NewestMarkdownMtime(cfg)
	if !ok {
		return false, ""
	}
	indexedMax, ok := core.MaxIndexedMtime(db)
	if !ok {
		return false, ""
	}
	if newest > indexedMax+staleMtimeEpsilonMillis {
		return true, "index stale: a newer note exists on disk (run hebb index, or it refreshes on next search)"
	}
	return false, ""
}

func checkSettings(add func(string, string, string), vaultPath, mcpName string) {
	b, err := os.ReadFile(filepath.Join(vaultPath, ".claude", "settings.json"))
	if err != nil {
		// No per-vault settings is normal in plugin mode; nothing to check.
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
	jobs := VaultJobs(opts.VaultPath, Slugify(vc.Name), "hebb", resolvedAssetDir(opts), opts.Home, vc.WebPort, vc.Jobs, vc.Update.Auto, vc.JobArgs)
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

// checkLaunchdTCC statically lints each of this vault's launchd jobs for a
// Program[0] that macOS TCC cannot attribute a Full Disk Access grant to: a
// shell script (.sh) or an interpreter shim (/bin/sh, /bin/bash, /usr/bin/env).
// Such a job's child interpreter blocks indefinitely on the first read into a
// protected vault folder, which is invisible (no error, no exit). It is a static
// lint only: it reads the rendered job specs and any installed plists, and never
// bootstraps a launchctl job, keeping doctor read-only and fast.
//
// The expected jobs render with the literal "hebb" placeholder for the binary,
// which is never a shell wrapper, so doctor's own rendering cannot trip the lint;
// the field failure mode is caught by inspecting the installed plist's Program[0].
func checkLaunchdTCC(add func(string, string, string), opts Options, vc core.VaultConfig) {
	dir := opts.LaunchdDir
	if dir == "" && opts.Home != "" {
		dir = filepath.Join(opts.Home, "Library", "LaunchAgents")
	}
	if dir == "" {
		return
	}
	jobs := VaultJobs(opts.VaultPath, Slugify(vc.Name), "hebb", resolvedAssetDir(opts), opts.Home, vc.WebPort, vc.Jobs, vc.Update.Auto, vc.JobArgs)
	if len(jobs) == 0 {
		return
	}
	var offenders []string
	for _, j := range jobs {
		// Lint the rendered program (defensive; the placeholder never trips it),
		// then the installed plist's program, which is where a stale shell-wrapper
		// install shows up.
		prog0 := ""
		if len(j.Program) > 0 {
			prog0 = j.Program[0]
		}
		if installed, ok := plistProgram0(filepath.Join(dir, j.Label+".plist")); ok {
			prog0 = installed
		}
		if isShellWrapperProgram(prog0) {
			offenders = append(offenders, j.Label)
		}
	}
	if len(offenders) == 0 {
		add("launchd-tcc", "ok", "no shell-wrapper job programs")
		return
	}
	add("launchd-tcc", "warn", fmt.Sprintf(
		"shell-wrapper Program[0] in %s: launchd cannot grant it Full Disk Access, so the job hangs on protected folders. "+
			"Re-run hebb install (the daily-digest job now runs the hebb binary), then grant the binary access in "+
			"System Settings > Privacy & Security > Full Disk Access.",
		strings.Join(offenders, ", ")))
}

// isShellWrapperProgram reports whether prog is a shell script or interpreter
// shim that macOS TCC cannot attribute a file-access grant to.
func isShellWrapperProgram(prog string) bool {
	if prog == "" {
		return false
	}
	if strings.HasSuffix(prog, ".sh") {
		return true
	}
	switch prog {
	case "/bin/sh", "/bin/bash", "/usr/bin/env":
		return true
	}
	return false
}

// plistProgram0 reads the first ProgramArguments string from a launchd plist,
// read-only. It returns ok=false when the file is absent or has no program. It
// scans for the ProgramArguments key then the next <string>, which suffices for
// the plists launchd renders (no nested arrays before it).
func plistProgram0(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	s := string(b)
	i := strings.Index(s, "<key>ProgramArguments</key>")
	if i < 0 {
		return "", false
	}
	rest := s[i:]
	start := strings.Index(rest, "<string>")
	if start < 0 {
		return "", false
	}
	rest = rest[start+len("<string>"):]
	end := strings.Index(rest, "</string>")
	if end < 0 {
		return "", false
	}
	return xmlUnescape(rest[:end]), true
}

// xmlUnescape reverses the five XML metacharacter escapes the launchd template
// applies, so a Program[0] containing them lints against its real value.
var xmlUnescaper = strings.NewReplacer(
	"&amp;", "&",
	"&lt;", "<",
	"&gt;", ">",
	"&quot;", `"`,
	"&apos;", "'",
)

func xmlUnescape(s string) string { return xmlUnescaper.Replace(s) }

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
