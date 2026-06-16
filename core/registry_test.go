package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegistryAddListRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "hebb", "vaults.toml")

	if err := RegisterVault(path, "Personal", t.TempDir()); err != nil {
		t.Fatalf("register: %v", err)
	}
	work := t.TempDir()
	if err := RegisterVault(path, "Work", work); err != nil {
		t.Fatalf("register: %v", err)
	}

	r, err := LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Vaults) != 2 {
		t.Fatalf("got %d vaults, want 2: %+v", len(r.Vaults), r.Vaults)
	}

	// Re-registering the same path with the same name is a no-op (no change).
	r2, _ := LoadRegistry(path)
	if r2.Add("Work", work) {
		t.Error("re-adding an identical entry should report no change")
	}
	// Re-registering with a new name updates in place, not appends.
	if !r2.Add("Work Vault", work) {
		t.Error("changing the name should report a change")
	}
	if len(r2.Vaults) != 2 {
		t.Errorf("name update should not append: got %d", len(r2.Vaults))
	}

	// Deregister.
	if err := DeregisterVault(path, work); err != nil {
		t.Fatal(err)
	}
	r3, _ := LoadRegistry(path)
	if len(r3.Vaults) != 1 || r3.Vaults[0].Name != "Personal" {
		t.Fatalf("after deregister: %+v", r3.Vaults)
	}
}

func TestLoadRegistryMissingIsEmpty(t *testing.T) {
	r, err := LoadRegistry(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatalf("missing registry should not error: %v", err)
	}
	if len(r.Vaults) != 0 {
		t.Errorf("missing registry should be empty, got %+v", r.Vaults)
	}
}

func TestRegistryPathHonoursXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	if got := RegistryPath("/home/x"); got != filepath.Join("/tmp/xdg", "hebb", "vaults.toml") {
		t.Errorf("RegistryPath with XDG = %q", got)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	if got := RegistryPath("/home/x"); got != filepath.Join("/home/x", ".config", "hebb", "vaults.toml") {
		t.Errorf("RegistryPath default = %q", got)
	}
}

func TestRegistryDedupesByCanonicalPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vaults.toml")
	vault := t.TempDir()
	// Same vault via a trailing-dot-slash spelling resolves to the same canonical
	// path, so it must not create a second entry.
	if err := RegisterVault(path, "A", vault); err != nil {
		t.Fatal(err)
	}
	if err := RegisterVault(path, "A", filepath.Join(vault, ".")); err != nil {
		t.Fatal(err)
	}
	r, _ := LoadRegistry(path)
	if len(r.Vaults) != 1 {
		t.Fatalf("duplicate spellings should dedupe to 1, got %d: %+v", len(r.Vaults), r.Vaults)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("registry file not written: %v", err)
	}
}
