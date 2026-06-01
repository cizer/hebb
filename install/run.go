package install

import (
	"path/filepath"

	"github.com/cizer/hebb/core"
	"github.com/cizer/hebb/launchd"
)

// Options configures an install run. Directory fields are explicit so install
// is fully testable and never hard-codes a home directory. An empty Home or
// AssetRoot disables the corresponding wiring (vault-local steps still run).
type Options struct {
	VaultPath  string
	MCPName    string
	MCPCommand string
	Home       string // base dir containing .claude (e.g. ~); "" disables skills wiring
	AssetRoot  string // hebb asset dir containing skills/ and automation/; "" disables skills wiring
	HebbBin    string // path to the hebb binary, for the web launchd job
	LaunchdDir string // target LaunchAgents dir; "" disables launchd rendering
	Load       bool   // if true, bootstrap rendered jobs via launchctl
}

// Run performs the file-level install for a vault and returns a report of every
// action. It does not build the index (the caller owns the engine/db). Steps:
//   - vault-local contracts: .hebb/config.toml, .mcp.json
//   - project settings:      <vault>/.claude/settings.json (MCP enable + allow)
//   - skills (if Home and AssetRoot set): symlink <AssetRoot>/skills/* into
//     <Home>/.claude/skills
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

	if opts.Home != "" && opts.AssetRoot != "" {
		skillsSrc := filepath.Join(opts.AssetRoot, "skills")
		claudeSkills := filepath.Join(opts.Home, ".claude", "skills")
		sr, err := SymlinkSkills(skillsSrc, claudeSkills)
		if err != nil {
			return rep, err
		}
		rep.Steps = append(rep.Steps, sr.Steps...)
	}

	if opts.LaunchdDir != "" && opts.HebbBin != "" && opts.Home != "" {
		if err := renderLaunchd(&rep, opts); err != nil {
			return rep, err
		}
	}
	return rep, nil
}

// renderLaunchd builds the vault's launchd jobs from its config, writes them to
// the target dir, and optionally bootstraps them.
func renderLaunchd(rep *Report, opts Options) error {
	vc, _, err := core.LoadVaultConfig(opts.VaultPath) // exists: VaultLocal ran
	if err != nil {
		return err
	}
	slug := Slugify(vc.Name)
	jobs := VaultJobs(opts.VaultPath, slug, opts.HebbBin, opts.AssetRoot, opts.Home, vc.WebPort, vc.Jobs)
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
