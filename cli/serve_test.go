package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cizer/hebb/core"
)

// TestBuildServeTargetsSkipsMissingVault proves a registry entry whose directory
// is gone never stops the healthy vaults being served, and is reported as
// skipped rather than dropped silently. This is the failure mode a stale
// registry entry creates: a throwaway vault is registered, its directory is
// later deleted, and the entry remains behind.
func TestBuildServeTargetsSkipsMissingVault(t *testing.T) {
	// Keep the "current vault" resolution out of it: with no HEBB_VAULT and a cwd
	// that is not a vault, only the registry contributes targets.
	t.Setenv("HEBB_VAULT", "")
	origVault, origDB := flagVault, flagDB
	flagVault, flagDB = "", ""
	defer func() { flagVault, flagDB = origVault, origDB }()

	home, _ := os.UserHomeDir()
	regPath := core.RegistryPath(home)

	good := t.TempDir()
	if err := os.MkdirAll(filepath.Join(good, ".hebb"), 0o755); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(t.TempDir(), "deleted-vault") // never created

	if err := core.RegisterVault(regPath, "Good", good); err != nil {
		t.Fatal(err)
	}
	if err := core.RegisterVault(regPath, "Gone", gone); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = core.DeregisterVault(regPath, good)
		_ = core.DeregisterVault(regPath, gone)
	}()

	targets, skipped, err := buildServeTargets()
	if err != nil {
		t.Fatalf("a missing vault must not fail serve: %v", err)
	}

	// Compare by registry name, not path: on macOS t.TempDir() hands back a
	// symlinked /var/... path that resolves to /private/var/..., so path equality
	// is not a reliable assertion here.
	var served bool
	for _, tg := range targets {
		if tg.Name == "Good" {
			served = true
		}
		if tg.Name == "Gone" {
			t.Error("the missing vault must not be served")
		}
	}
	if !served {
		names := make([]string, 0, len(targets))
		for _, tg := range targets {
			names = append(names, tg.Name)
		}
		t.Errorf("the healthy vault must still be served; got %d targets %v", len(targets), names)
	}

	var reported bool
	for _, s := range skipped {
		if s.Path == gone {
			reported = true
			if s.Name != "Gone" {
				t.Errorf("skipped name = %q, want %q", s.Name, "Gone")
			}
			if s.Err == nil {
				t.Error("a skipped vault must carry the reason it was skipped")
			}
		}
	}
	if !reported {
		t.Error("the missing vault must be reported as skipped, not dropped silently")
	}
}
