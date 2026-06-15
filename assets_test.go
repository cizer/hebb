package hebb

import (
	"io/fs"
	"testing"
)

// TestAssetsShipAutomationScripts guards the contract between the embedded
// binary and install.VaultJobs: the action-review launchd job is gated on this
// exact filename existing under automation/, so a rename here would silently
// stop the action-review job from ever rendering. The daily-digest job is no
// longer gated on a script: `hebb digest` is built into the binary.
func TestAssetsShipAutomationScripts(t *testing.T) {
	for _, name := range []string{
		"automation/generate-action-review.py", // action-review job
	} {
		if _, err := fs.Stat(Assets, name); err != nil {
			t.Errorf("embedded assets missing %q: %v", name, err)
		}
	}
}

// TestAssetsExcludeRetiredDigestScripts confirms the Python digest generator and
// its shell wrapper are gone from the binary: the digest is reimplemented in Go
// (`hebb digest`), so a re-embedded copy would be dead, drifting code.
func TestAssetsExcludeRetiredDigestScripts(t *testing.T) {
	for _, name := range []string{
		"automation/generate-vault-digest.py",
		"automation/run-vault-digest.sh",
	} {
		if _, err := fs.Stat(Assets, name); err == nil {
			t.Errorf("embedded assets should no longer contain %q (digest is Go now)", name)
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
