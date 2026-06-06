package install

import (
	"path/filepath"

	"github.com/cizer/hebb/core"
)

// Step records the outcome of one install action for reporting.
type Step struct {
	Name   string
	Status string // created | exists | wrote | unchanged | symlinked | loaded ...
}

// Report is the ordered list of actions an install performed.
type Report struct {
	Steps []Step
}

func (r *Report) add(name, status string) {
	r.Steps = append(r.Steps, Step{Name: name, Status: status})
}

// VaultLocal initialises the per-vault config (.hebb/config.toml, created with
// defaults if absent, never clobbered). It is idempotent. The project-scoped
// .mcp.json is written separately and only on request (Options.MCPJSON): the
// hebb plugin normally provides the MCP server.
func VaultLocal(vaultPath string) (Report, error) {
	var r Report
	_, existed, err := core.LoadVaultConfig(vaultPath)
	if err != nil {
		return r, err
	}
	if !existed {
		vc := core.DefaultVaultConfig(filepath.Base(vaultPath))
		if err := vc.Save(vaultPath); err != nil {
			return r, err
		}
		r.add("config.toml", "created")
	} else {
		r.add("config.toml", "exists")
	}
	return r, nil
}
