package install

import (
	"io/fs"
	"path/filepath"

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
	Home       string // base dir containing .claude (e.g. ~); "" disables skills/memory wiring
	HebbBin    string // path to the hebb binary, for the web launchd job
	LaunchdDir string // target LaunchAgents dir; "" disables launchd rendering
	Load       bool   // if true, bootstrap rendered jobs via launchctl

	// Asset source. The binary is standalone: Assets carries the embedded
	// function content (skills/, automation/, vault-template/), materialised to
	// DataDir on install. AssetRoot is a development override - point it at a
	// repo checkout to symlink skills straight from source (live editing) and
	// skip materialisation.
	Assets    fs.FS
	DataDir   string
	AssetRoot string
}

// Run performs the file-level install for a vault and returns a report of every
// action. It does not build the index (the caller owns the engine/db). Steps:
//   - vault-local contracts: .hebb/config.toml, .mcp.json
//   - project settings:      <vault>/.claude/settings.json (MCP enable + allow)
//   - assets:                materialise embedded function content to DataDir
//     (unless --asset-root points at a live repo checkout)
//   - skills (if Home):      symlink <assetDir>/skills/* into <Home>/.claude/skills
//   - memory (if Home):      symlink <vault>/memory into the Claude project dir
//   - launchd (if requested): render the vault's jobs
//
// Every step is idempotent.
func Run(opts Options) (Report, error) {
	rep, err := VaultLocal(opts.VaultPath, opts.MCPName, opts.MCPCommand)
	if err != nil {
		return rep, err
	}

	changed, err := WriteProjectSettings(opts.VaultPath, opts.MCPName)
	if err != nil {
		return rep, err
	}
	rep.add("settings.json", wroteOrUnchanged(changed))

	assetDir, err := resolveAssetDir(&rep, opts)
	if err != nil {
		return rep, err
	}

	if opts.Home != "" && assetDir != "" {
		skillsSrc := filepath.Join(assetDir, "skills")
		claudeSkills := filepath.Join(opts.Home, ".claude", "skills")
		sr, err := SymlinkSkills(skillsSrc, claudeSkills)
		if err != nil {
			return rep, err
		}
		rep.Steps = append(rep.Steps, sr.Steps...)
	}

	if opts.Home != "" {
		projects := filepath.Join(opts.Home, ".claude", "projects")
		status, err := SymlinkMemory(opts.VaultPath, projects, ClaudeProjectSlug(opts.VaultPath))
		if err != nil {
			return rep, err
		}
		rep.add("memory", status)
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
	jobs := VaultJobs(opts.VaultPath, slug, opts.HebbBin, assetDir, opts.Home, vc.WebPort, vc.Jobs)
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
