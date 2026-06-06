package hebb

import (
	"io/fs"
	"testing"
)

// TestAssetsShipAutomationScripts guards the contract between the embedded
// binary and install.VaultJobs: the launchd jobs are gated on these exact
// filenames existing under automation/, so a rename here would silently stop
// the daily-digest / action-review jobs from ever rendering.
func TestAssetsShipAutomationScripts(t *testing.T) {
	for _, name := range []string{
		"automation/run-vault-digest.sh",       // daily-digest entrypoint
		"automation/generate-vault-digest.py",  // called by the wrapper
		"automation/generate-action-review.py", // action-review job
	} {
		if _, err := fs.Stat(Assets, name); err != nil {
			t.Errorf("embedded assets missing %q: %v", name, err)
		}
	}
}

// TestAssetsExcludeSkills confirms skills are no longer embedded in the binary
// (B2): the plugin delivers them. A skills/ tree in the embed would mean a
// second, drifting copy.
func TestAssetsExcludeSkills(t *testing.T) {
	if _, err := fs.Stat(Assets, "skills"); err == nil {
		t.Error("embedded assets should not contain skills/ (the plugin ships skills)")
	}
}
