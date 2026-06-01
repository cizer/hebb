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

// VaultLocal writes the per-vault contracts that live inside the vault
// directory only, making no changes to the home directory or system: it
// initialises .hebb/config.toml (created with defaults if absent, never
// clobbered) and the project-scoped .mcp.json. It is idempotent.
func VaultLocal(vaultPath, serverName, command string) (Report, error) {
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

	changed, err := WriteMCPJSON(vaultPath, serverName, command)
	if err != nil {
		return r, err
	}
	if changed {
		r.add(".mcp.json", "wrote")
	} else {
		r.add(".mcp.json", "unchanged")
	}
	return r, nil
}
