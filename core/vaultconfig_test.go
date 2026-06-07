package core

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultVaultConfig(t *testing.T) {
	vc := DefaultVaultConfig("Work Vault")
	if vc.Name != "Work Vault" {
		t.Errorf("name = %q, want Work Vault", vc.Name)
	}
	if vc.WebPort != 4321 {
		t.Errorf("web port = %d, want 4321", vc.WebPort)
	}
	if len(vc.ExcludeDirs) == 0 || len(vc.Jobs) == 0 || len(vc.Skills) == 0 {
		t.Errorf("defaults should populate exclude_dirs/jobs/skills, got %+v", vc)
	}
}

func TestVaultConfigRoundTrip(t *testing.T) {
	vault := t.TempDir()
	want := VaultConfig{
		Name:        "Work",
		ExcludeDirs: []string{".obsidian", ".git"},
		WebPort:     4399,
		Jobs:        []string{"web"},
		Skills:      []string{"build"},
	}
	if err := want.Save(vault); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// File lands at <vault>/.hebb/config.toml
	if _, err := os.Stat(filepath.Join(vault, ".hebb", "config.toml")); err != nil {
		t.Fatalf("config.toml not written: %v", err)
	}
	got, existed, err := LoadVaultConfig(vault)
	if err != nil {
		t.Fatalf("LoadVaultConfig: %v", err)
	}
	if !existed {
		t.Error("existed = false, want true after Save")
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round-trip mismatch:\n got = %+v\nwant = %+v", got, want)
	}
}

func TestLoadVaultConfigAbsent(t *testing.T) {
	vault := t.TempDir()
	got, existed, err := LoadVaultConfig(vault)
	if err != nil {
		t.Fatalf("LoadVaultConfig: %v", err)
	}
	if existed {
		t.Error("existed = true, want false for a vault with no config")
	}
	// Falls back to defaults named after the vault directory.
	if got.Name != filepath.Base(vault) {
		t.Errorf("default name = %q, want %q", got.Name, filepath.Base(vault))
	}
	if got.WebPort != 4321 {
		t.Errorf("default web port = %d, want 4321", got.WebPort)
	}
}

func TestLoadVaultConfigInvalid(t *testing.T) {
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, ".hebb"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, ".hebb", "config.toml"), []byte("name = \"unterminated"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadVaultConfig(vault); err == nil {
		t.Error("expected error for malformed TOML, got nil")
	}
}

func TestResolveVaultHonorsConfigExcludeDirs(t *testing.T) {
	vault := t.TempDir()
	vc := DefaultVaultConfig("T")
	vc.ExcludeDirs = []string{".obsidian", "Archive", "node_modules"}
	if err := vc.Save(vault); err != nil {
		t.Fatal(err)
	}
	cfg, err := ResolveVault(vault, "")
	if err != nil {
		t.Fatalf("ResolveVault: %v", err)
	}
	if !reflect.DeepEqual(cfg.ExcludeDirs, vc.ExcludeDirs) {
		t.Errorf("exclude dirs = %v, want %v (from config.toml)", cfg.ExcludeDirs, vc.ExcludeDirs)
	}
}
