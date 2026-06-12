package install

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/cizer/hebb/core"
	"github.com/cizer/hebb/launchd"
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

	// The binary path doctor compares wiring against: the running executable at
	// doctor-run time, resolved through the same stable-symlink rule install uses
	// (so a Cellar path is compared as its /opt/homebrew/bin symlink). Binary
	// paths legitimately churn across upgrades, so a difference that still points
	// at a working hebb is warn, not fail (see checkBinaryPath).
	bin := opts.HebbBin
	if bin == "" {
		if exe, err := os.Executable(); err == nil {
			bin = StableHebbBin(exe)
		}
	}

	checkMCPJSON(add, opts.VaultPath)
	checkIndex(add, opts.VaultPath, vc)
	checkSettings(add, opts.VaultPath, opts.MCPName)

	if opts.Home != "" {
		checkMemory(add, opts.Home, opts.VaultPath)
	}

	// Agent wiring drift: re-verify any entry pinned to this vault against what
	// install would write today. Never-wired stays silent (no config, or no entry
	// matching this vault's HEBB_VAULT).
	checkClaudeDesktop(add, opts, bin)
	checkCodex(add, opts)

	if existed {
		checkIngestStage(add, vc)
		checkLaunchd(add, opts, vc, bin)
		checkLaunchdTCC(add, opts, vc, bin)
	}
	return checks
}

// checkIngestStage warns when the vault config carries an ingest stage that is
// unsupported (>= 4; headless ingest is not yet implemented) or outside the
// valid 1-4 range. Stage 0 or negative is stored as-is and triggers the warning
// because GetStage clamps only the accessor, leaving the raw value visible here.
func checkIngestStage(add func(string, string, string), vc core.VaultConfig) {
	s := vc.Ingest.Stage
	if s == 0 {
		// Zero means the [ingest] section was absent or stage was not set:
		// GetStage() will return 1, which is the safe default. No warning.
		return
	}
	if s >= 1 && s <= 3 {
		return
	}
	if s == 4 {
		add("ingest-stage", "warn",
			"ingest stage 4 (headless) is not yet supported; the skill will not run headless. "+
				"Set [ingest] stage to 1-3 in .hebb/config.toml.")
		return
	}
	add("ingest-stage", "warn",
		fmt.Sprintf("ingest stage %d is outside the valid range 1-4. "+
			"Set [ingest] stage to 1-3 in .hebb/config.toml.", s))
}

// checkBinaryPath classifies a command field that differs from the binary path
// doctor would write today. Binary paths legitimately change across upgrades, so
// a difference that still resolves to a working hebb is warn (re-run hebb
// install), and only a command that points at nothing is fail. It never executes
// the command (read-only, fast): "working" means the path exists and is a
// regular executable file. Returns "ok" when path == want, "warn" when it
// resolves to a working binary, or "fail".
func checkBinaryPath(path, want string) string {
	if path == want {
		return "ok"
	}
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() || fi.Mode().Perm()&0o111 == 0 {
		return "fail"
	}
	return "warn"
}

func checkMCPJSON(add func(string, string, string), vaultPath string) {
	path := filepath.Join(vaultPath, ".mcp.json")
	b, err := os.ReadFile(path)
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
	// Content comparison, not just presence: a .mcp.json that parses but whose
	// command/args drifted from what install would write today is reported (the
	// old presence check waved it through). RenderMCPJSON is deliberately
	// machine-independent (no absolute paths), so this is exempt from the binary
	// path-churn warn/fail split: any drift is a flat warn.
	want, err := RenderMCPJSON(DefaultMCPServerName, DefaultMCPCommand)
	if err == nil && !bytes.Equal(b, want) {
		add("mcp.json", "warn", "differs from what install would write (run hebb install --mcp-json)")
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

// checkClaudeDesktop re-verifies any Claude Desktop MCP entry pinned to this
// vault against what install would write today. It scans for the entry by its
// HEBB_VAULT match (install records nowhere which agents were wired), so a
// machine where Desktop was never wired (no config file, or no entry for this
// vault) produces no finding: never-wired is silent. The Desktop entry pins the
// absolute binary path (the app launches servers with a minimal PATH), the field
// the observed round-trip dropped. Binary paths churn across upgrades, so a
// command differing but resolving to a working hebb is warn (re-run hebb
// install), and one pointing at nothing is fail. doctor never executes the
// command. Args and env (other than the path) are compared exactly.
func checkClaudeDesktop(add func(string, string, string), opts Options, bin string) {
	path := opts.ClaudeDesktopConfig
	if path == "" {
		if opts.Home == "" {
			return
		}
		path = DefaultClaudeDesktopConfigPath(opts.Home)
	}
	root, err := readJSONObject(path)
	if err != nil {
		// A malformed Desktop config we did wire into is worth surfacing, but only
		// once we know it holds an entry for this vault; we cannot tell from a parse
		// error, so stay silent (read-only, never-wired-is-silent posture).
		return
	}
	servers, _ := root["mcpServers"].(map[string]any)
	name, entry, found := findVaultPinnedServer(servers, opts.VaultPath)
	if !found {
		return
	}
	// "want" is what WriteClaudeDesktopConfig would write today with the doctor-run
	// binary path. Comparing the decoded entry to it isolates the command field for
	// the warn-vs-fail rule and compares args/env exactly.
	command, _ := entry["command"].(string)
	want := map[string]any{
		"command": bin,
		"args":    []any{"mcp"},
		"env":     map[string]any{"HEBB_VAULT": opts.VaultPath},
	}
	// Compare everything but the command exactly; classify the command separately.
	gotNoCmd := map[string]any{"args": entry["args"], "env": entry["env"]}
	wantNoCmd := map[string]any{"args": want["args"], "env": want["env"]}
	if !jsonEqual(gotNoCmd, wantNoCmd) {
		add("claude-desktop", "warn", fmt.Sprintf("entry %q differs from what install would write (re-run hebb install)", name))
		return
	}
	switch checkBinaryPath(command, bin) {
	case "ok":
		add("claude-desktop", "ok", "wired ("+name+")")
	case "warn":
		add("claude-desktop", "warn", fmt.Sprintf("command %q differs but resolves to a working hebb (re-run hebb install)", command))
	default:
		add("claude-desktop", "fail", fmt.Sprintf("command %q points at nothing (re-run hebb install)", command))
	}
}

// findVaultPinnedServer returns the first MCP server entry (from a decoded
// mcpServers map) whose env.HEBB_VAULT matches vaultPath, with its name. This is
// how doctor distinguishes "wired into this vault" (verify it) from "never wired
// / wired to a different vault" (silent).
func findVaultPinnedServer(servers map[string]any, vaultPath string) (string, map[string]any, bool) {
	for name, v := range servers {
		entry, ok := v.(map[string]any)
		if !ok {
			continue
		}
		env, _ := entry["env"].(map[string]any)
		if s, _ := env["HEBB_VAULT"].(string); s == vaultPath {
			return name, entry, true
		}
	}
	return "", nil, false
}

// checkCodex re-verifies any Codex MCP entry pinned to this vault against
// RenderCodexServer output. Like the Desktop check it scans by HEBB_VAULT match,
// so a machine where Codex was never wired produces no finding. Unlike Desktop,
// the Codex entry pins the bare "hebb" command (resolved on PATH), so it is
// machine-independent and exempt from the binary path-churn warn/fail split: any
// drift from RenderCodexServer is a flat warn with the fix `hebb codex`. The
// comparison decodes both the installed block and RenderCodexServer's output with
// the same TOML parser the writer uses, the single source of truth.
func checkCodex(add func(string, string, string), opts Options) {
	path := opts.CodexConfig
	if path == "" {
		if opts.Home == "" {
			return
		}
		path = filepath.Join(opts.Home, ".codex", "config.toml")
	}
	got, err := decodeCodexServers(path)
	if err != nil {
		return // absent or unreadable: never-wired is silent
	}
	name, gotEntry, found := findCodexVaultPinned(got, opts.VaultPath)
	if !found {
		return
	}
	// Decode RenderCodexServer's output with the same parser to get the canonical
	// entry, then compare values (whitespace/formatting independent).
	wantBlock := RenderCodexServer(name, DefaultMCPCommand, opts.VaultPath)
	wantServers, err := decodeCodexServersString(wantBlock)
	if err != nil {
		return
	}
	wantEntry := wantServers[name]
	if codexEntryEqual(gotEntry, wantEntry) {
		add("codex", "ok", "wired ("+name+")")
		return
	}
	add("codex", "warn", fmt.Sprintf("entry %q differs from what install would write (run hebb codex)", name))
}

// codexServer mirrors the fields RenderCodexServer writes, for value comparison.
type codexServer struct {
	Command string            `toml:"command"`
	Args    []string          `toml:"args"`
	Cwd     string            `toml:"cwd"`
	Env     map[string]string `toml:"env"`
	Timeout int               `toml:"startup_timeout_sec"`
}

func decodeCodexServers(path string) (map[string]codexServer, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decodeCodexServersString(string(b))
}

func decodeCodexServersString(s string) (map[string]codexServer, error) {
	var parsed struct {
		MCPServers map[string]codexServer `toml:"mcp_servers"`
	}
	if _, err := toml.Decode(s, &parsed); err != nil {
		return nil, err
	}
	return parsed.MCPServers, nil
}

func findCodexVaultPinned(servers map[string]codexServer, vaultPath string) (string, codexServer, bool) {
	for name, s := range servers {
		if s.Env["HEBB_VAULT"] == vaultPath {
			return name, s, true
		}
	}
	return "", codexServer{}, false
}

func codexEntryEqual(a, b codexServer) bool {
	if a.Command != b.Command || a.Cwd != b.Cwd || a.Timeout != b.Timeout {
		return false
	}
	if len(a.Args) != len(b.Args) {
		return false
	}
	for i := range a.Args {
		if a.Args[i] != b.Args[i] {
			return false
		}
	}
	if len(a.Env) != len(b.Env) {
		return false
	}
	for k, v := range a.Env {
		if b.Env[k] != v {
			return false
		}
	}
	return true
}

// resolvedAssetDir is the on-disk asset dir doctor compares against, read-only:
// the --asset-root override if set, otherwise the data dir. Never materialises.
func resolvedAssetDir(opts Options) string {
	if opts.AssetRoot != "" {
		return opts.AssetRoot
	}
	return opts.DataDir
}

// checkLaunchd compares each installed plist against what VaultJobs renders
// today, not just its presence: a plist whose [job_args], env, or schedule was
// hand-edited (or left behind by an old install) is reported. Expected jobs are
// rendered with the binary path at doctor-run time (bin) and the current
// pythonPath(), replacing the literal "hebb"/install-time path the installed
// plist embeds, so a naive diff cannot flag every healthy install. The warn-vs-
// fail rule applies to Program[0] (binary paths churn across upgrades, so a path
// resolving to a working hebb is warn, not fail); the rest of the plist (args,
// env, schedule) must match exactly.
func checkLaunchd(add func(string, string, string), opts Options, vc core.VaultConfig, bin string) {
	dir := opts.LaunchdDir
	if dir == "" && opts.Home != "" {
		dir = filepath.Join(opts.Home, "Library", "LaunchAgents")
	}
	if dir == "" {
		return
	}
	if bin == "" {
		bin = "hebb"
	}
	jobs := VaultJobs(opts.VaultPath, Slugify(vc.Name), bin, resolvedAssetDir(opts), opts.Home, vc.WebPort, vc.Jobs, vc.Update.Auto, vc.JobArgs)
	if len(jobs) == 0 {
		return
	}
	present := 0
	status := "ok"
	var notes []string
	worst := func(s string) {
		// Escalate ok -> warn -> fail, never downgrade.
		if status == "fail" || s == "ok" {
			return
		}
		if s == "fail" || status == "ok" {
			status = s
		}
	}
	for _, j := range jobs {
		plistPath := filepath.Join(dir, j.Label+".plist")
		installed, err := os.ReadFile(plistPath)
		if err != nil {
			worst("warn")
			notes = append(notes, j.Label+" missing")
			continue
		}
		present++
		s, note := compareLaunchdJob(j, installed)
		if s != "ok" {
			worst(s)
			if note != "" {
				notes = append(notes, j.Label+": "+note)
			}
		}
	}
	detail := fmt.Sprintf("%d/%d plists", present, len(jobs))
	if len(notes) > 0 {
		detail += " (" + strings.Join(notes, "; ") + ")"
	}
	add("launchd", status, detail)
}

// compareLaunchdJob compares an installed plist against the expected job render,
// applying the Program[0] warn-vs-fail rule. It returns "ok" when the plist
// matches exactly; "warn"/"fail" classified by checkBinaryPath when the only
// difference is Program[0]; and "warn" for any other content difference (args,
// env, schedule edited without re-running install). It re-renders the expected
// plist with the installed Program[0] substituted in to isolate a pure path
// difference from a content difference.
func compareLaunchdJob(want launchd.Job, installed []byte) (string, string) {
	wantBytes, err := launchd.Render(want)
	if err != nil {
		return "warn", "render error"
	}
	if bytes.Equal(installed, wantBytes) {
		return "ok", ""
	}
	installedProg0, ok := plistProgram0Bytes(installed)
	wantProg0 := ""
	if len(want.Program) > 0 {
		wantProg0 = want.Program[0]
	}
	if ok && installedProg0 != wantProg0 {
		// Re-render the expected plist with the installed Program[0] swapped in;
		// if it now matches, the only difference is the binary path, which the
		// warn-vs-fail rule classifies. Otherwise there is genuine content drift.
		probe := want
		probe.Program = append([]string{installedProg0}, want.Program[1:]...)
		if probeBytes, perr := launchd.Render(probe); perr == nil && bytes.Equal(installed, probeBytes) {
			s := checkBinaryPath(installedProg0, wantProg0)
			if s == "fail" {
				return "fail", "Program[0] points at nothing: " + installedProg0 + " (re-run hebb install)"
			}
			return "warn", "Program[0] differs but resolves to a working hebb: " + installedProg0 + " (re-run hebb install)"
		}
	}
	return "warn", "content differs from what install would write (re-run hebb install)"
}

// checkLaunchdTCC statically lints each of this vault's launchd jobs for a
// Program[0] that macOS TCC cannot attribute a Full Disk Access grant to: a
// shell script (.sh) or an interpreter shim (/bin/sh, /bin/bash, /usr/bin/env).
// Such a job's child interpreter blocks indefinitely on the first read into a
// protected vault folder, which is invisible (no error, no exit). It is a static
// lint only: it reads the rendered job specs and any installed plists, and never
// bootstraps a launchctl job, keeping doctor read-only and fast.
//
// The expected jobs render with the binary path at doctor-run time (bin), which
// is a grantable hebb binary, never a shell wrapper, so doctor's own rendering
// cannot trip the lint; the field failure mode is caught by inspecting the
// installed plist's Program[0]. (Item 3 replaced the literal "hebb" placeholder
// the lint previously assumed; substituting the real binary path keeps the lint
// free of false positives, since a hebb binary path is never a .sh or a shim.)
func checkLaunchdTCC(add func(string, string, string), opts Options, vc core.VaultConfig, bin string) {
	dir := opts.LaunchdDir
	if dir == "" && opts.Home != "" {
		dir = filepath.Join(opts.Home, "Library", "LaunchAgents")
	}
	if dir == "" {
		return
	}
	if bin == "" {
		bin = "hebb"
	}
	jobs := VaultJobs(opts.VaultPath, Slugify(vc.Name), bin, resolvedAssetDir(opts), opts.Home, vc.WebPort, vc.Jobs, vc.Update.Auto, vc.JobArgs)
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

// plistProgram0 reads the first ProgramArguments string from a launchd plist
// file, read-only. It returns ok=false when the file is absent or has no program.
func plistProgram0(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return plistProgram0Bytes(b)
}

// plistProgram0Bytes reads the first ProgramArguments string from plist bytes. It
// scans for the ProgramArguments key then the next <string>, which suffices for
// the plists launchd renders (no nested arrays before it).
func plistProgram0Bytes(b []byte) (string, bool) {
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
