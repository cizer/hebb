package install

import (
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/cizer/hebb/core"
	"github.com/cizer/hebb/launchd"
)

// Options configures an install run. Directory fields are explicit so install
// is fully testable and never hard-codes a home directory. An empty Home
// disables the home-side wiring (vault-local steps still run).
type Options struct {
	VaultPath  string
	MCPName    string
	MCPCommand string
	Home       string // base dir containing .claude (e.g. ~); "" disables memory wiring
	HebbBin    string // path to the hebb binary, for the web launchd job
	LaunchdDir string // target LaunchAgents dir; "" disables launchd rendering
	Load       bool   // if true, bootstrap rendered jobs via launchctl
	MCPJSON    bool   // if true, write a per-vault .mcp.json + settings (plugin-less wiring)
	SkipSkills bool   // if true, do not install agent skills into ~/.claude/skills

	// Agent config paths, used only by Doctor to re-verify wiring drift
	// read-only. Empty means "use the conventional default under Home"; when
	// that default file is absent the check stays silent (never-wired is silent).
	CodexConfig         string // Codex config.toml (default: <home>/.codex/config.toml)
	ClaudeDesktopConfig string // Claude Desktop config (default: macOS app support dir)

	// Asset source. The binary is standalone: Assets carries the embedded
	// function content (automation/, vault-template/), materialised to DataDir on
	// install so launchd jobs can find their scripts. AssetRoot is a development
	// override - point it at a repo checkout to use its automation/ straight from
	// source and skip materialisation.
	Assets    fs.FS
	DataDir   string
	AssetRoot string
}

// Run performs the file-level install for a vault and returns a report of every
// action. It does not build the index (the caller owns the engine/db). Steps:
//   - vault config:          .hebb/config.toml (always)
//   - plugin-less wiring (if MCPJSON): .mcp.json + <vault>/.claude/settings.json
//     (the hebb plugin normally provides the MCP server instead)
//   - assets:                materialise embedded automation scripts to DataDir
//     (unless --asset-root points at a live repo checkout), for launchd jobs
//   - memory (if Home):      symlink <vault>/.hebb/memory into the Claude project dir
//   - launchd (if requested): render the vault's jobs
//
// Skills are delivered by the hebb Claude Code plugin (see plugin/), not by
// install. Every step is idempotent.
func Run(opts Options) (Report, error) {
	rep, err := VaultLocal(opts.VaultPath)
	if err != nil {
		return rep, err
	}

	if opts.MCPJSON {
		// Plugin-less wiring: write the per-vault MCP server + tool allow-list.
		// Mutually exclusive with the hebb plugin, which provides a "hebb"
		// server of the same name.
		changed, err := WriteMCPJSON(opts.VaultPath, opts.MCPName, opts.MCPCommand)
		if err != nil {
			return rep, err
		}
		rep.add(".mcp.json", wroteOrUnchanged(changed))
		sc, err := WriteProjectSettings(opts.VaultPath, opts.MCPName)
		if err != nil {
			return rep, err
		}
		rep.add("settings.json", wroteOrUnchanged(sc))
	}

	// Materialise automation scripts so launchd jobs can find them. Skills are
	// no longer materialised or linked: the plugin ships them.
	assetDir, err := resolveAssetDir(&rep, opts)
	if err != nil {
		return rep, err
	}

	if opts.Home != "" {
		projects := filepath.Join(opts.Home, ".claude", "projects")
		status, err := SymlinkMemory(opts.VaultPath, projects, ClaudeProjectSlug(opts.VaultPath))
		if err != nil {
			return rep, err
		}
		rep.add("memory", status)
	}

	// Install the agent skills into Claude Code's personal skills dir. The plugin
	// also publishes them, but only reaches plugin-enabled Claude Code; a direct
	// install makes them work everywhere. Skipped when the assets carry no skills
	// (e.g. a test FS) so other install steps are unaffected.
	if opts.Home != "" && !opts.SkipSkills && opts.Assets != nil {
		if _, err := fs.Stat(opts.Assets, "plugin/skills"); err == nil {
			skillsFS, _ := fs.Sub(opts.Assets, "plugin/skills")
			names, err := InstallSkills(skillsFS, ClaudeSkillsDir(opts.Home))
			if err != nil {
				return rep, err
			}
			if len(names) > 0 {
				rep.add("skills", strings.Join(names, ", "))
			}
		}
	}

	if opts.LaunchdDir != "" && opts.HebbBin != "" && opts.Home != "" {
		if err := renderLaunchd(&rep, opts, assetDir); err != nil {
			return rep, err
		}
	}
	return rep, nil
}

// resolveAssetDir returns the on-disk directory holding skills/ and automation/.
// With --asset-root it is that repo checkout (dev: live source, no copy).
// Otherwise the embedded assets are materialised to DataDir and that is used,
// so the binary needs no repo checkout. Returns "" if neither is available.
func resolveAssetDir(rep *Report, opts Options) (string, error) {
	if opts.AssetRoot != "" {
		rep.add("assets", "source: "+opts.AssetRoot)
		return opts.AssetRoot, nil
	}
	if opts.Assets != nil && opts.DataDir != "" {
		n, err := MaterializeAssets(opts.Assets, opts.DataDir)
		if err != nil {
			return "", err
		}
		if n > 0 {
			rep.add("assets", "materialised")
		} else {
			rep.add("assets", "up to date")
		}
		return opts.DataDir, nil
	}
	return "", nil
}

// renderLaunchd builds the vault's launchd jobs from its config, writes them to
// the target dir, and optionally bootstraps them.
func renderLaunchd(rep *Report, opts Options, assetDir string) error {
	vc, _, err := core.LoadVaultConfig(opts.VaultPath) // exists: VaultLocal ran
	if err != nil {
		return err
	}
	slug := Slugify(vc.Name)
	jobs := VaultJobs(opts.VaultPath, slug, opts.HebbBin, assetDir, opts.Home, vc.WebPort, vc.Jobs, vc.Update.Auto, vc.JobArgs)
	changed, err := launchd.WriteJobs(jobs, opts.LaunchdDir)
	if err != nil {
		return err
	}
	changedSet := map[string]bool{}
	for _, l := range changed {
		changedSet[l] = true
	}
	var plistPaths []string
	for _, j := range jobs {
		rep.add(j.Label, wroteOrUnchanged(changedSet[j.Label]))
		plistPaths = append(plistPaths, filepath.Join(opts.LaunchdDir, j.Label+".plist"))
	}
	if opts.Load && len(plistPaths) > 0 {
		if _, err := Bootstrap(plistPaths, true); err != nil {
			return err
		}
	}
	return nil
}

func wroteOrUnchanged(changed bool) string {
	if changed {
		return "wrote"
	}
	return "unchanged"
}
