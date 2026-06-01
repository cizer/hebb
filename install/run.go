package install

import "path/filepath"

// Options configures an install run. Directory fields are explicit so install
// is fully testable and never hard-codes a home directory. An empty Home or
// AssetRoot disables the corresponding wiring (vault-local steps still run).
type Options struct {
	VaultPath  string
	MCPName    string
	MCPCommand string
	Home       string // base dir containing .claude (e.g. ~); "" disables skills wiring
	AssetRoot  string // hebb asset dir containing skills/; "" disables skills wiring
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
	return rep, nil
}

func wroteOrUnchanged(changed bool) string {
	if changed {
		return "wrote"
	}
	return "unchanged"
}
