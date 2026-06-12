package core

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
	if len(vc.ExcludeDirs) == 0 || len(vc.Jobs) == 0 {
		t.Errorf("defaults should populate exclude_dirs/jobs, got %+v", vc)
	}
}

func TestVaultConfigRoundTrip(t *testing.T) {
	vault := t.TempDir()
	want := VaultConfig{
		Name:        "Work",
		ExcludeDirs: []string{".obsidian", ".git"},
		WebPort:     4399,
		Jobs:        []string{"web"},
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

func TestVaultConfigJobArgs(t *testing.T) {
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, ".hebb"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `name = "Work"

[job_args]
action-review = ["--owner", "Alex Doe", "--mine-output", "2-Areas/_MY-OPEN-ACTIONS.md"]
`
	if err := os.WriteFile(filepath.Join(vault, ".hebb", "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _, err := LoadVaultConfig(vault)
	if err != nil {
		t.Fatalf("LoadVaultConfig: %v", err)
	}
	want := []string{"--owner", "Alex Doe", "--mine-output", "2-Areas/_MY-OPEN-ACTIONS.md"}
	if !reflect.DeepEqual(got.JobArgs["action-review"], want) {
		t.Errorf("job_args[action-review] = %v, want %v", got.JobArgs["action-review"], want)
	}
}

func TestIndexConfigAutoRefreshDefault(t *testing.T) {
	// Absent [index] block: auto-refresh defaults on.
	if !(IndexConfig{}).AutoRefreshEnabled() {
		t.Error("absent [index]: AutoRefreshEnabled() = false, want true (default on)")
	}
	// Explicit false turns it off.
	off := false
	if (IndexConfig{AutoRefresh: &off}).AutoRefreshEnabled() {
		t.Error("auto_refresh = false: AutoRefreshEnabled() = true, want false")
	}
	on := true
	if !(IndexConfig{AutoRefresh: &on}).AutoRefreshEnabled() {
		t.Error("auto_refresh = true: AutoRefreshEnabled() = false, want true")
	}
}

func TestVaultConfigParsesIndexBlock(t *testing.T) {
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, ".hebb"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `name = "Work"

[index]
auto_refresh = false
`
	if err := os.WriteFile(filepath.Join(vault, ".hebb", "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _, err := LoadVaultConfig(vault)
	if err != nil {
		t.Fatalf("LoadVaultConfig: %v", err)
	}
	if got.Index.AutoRefreshEnabled() {
		t.Error("parsed auto_refresh = false but AutoRefreshEnabled() = true")
	}
}

func TestResolveVaultAutoRefreshDefaultsOn(t *testing.T) {
	// No config at all: cfg.AutoRefresh must default on.
	vault := t.TempDir()
	cfg, err := ResolveVault(vault, "")
	if err != nil {
		t.Fatalf("ResolveVault: %v", err)
	}
	if !cfg.AutoRefresh {
		t.Error("no config: cfg.AutoRefresh = false, want true (default on)")
	}

	// Explicit off propagates to the resolved Config.
	off := false
	vc := DefaultVaultConfig("T")
	vc.Index.AutoRefresh = &off
	if err := vc.Save(vault); err != nil {
		t.Fatal(err)
	}
	cfg, err = ResolveVault(vault, "")
	if err != nil {
		t.Fatalf("ResolveVault: %v", err)
	}
	if cfg.AutoRefresh {
		t.Error("auto_refresh = false in config: cfg.AutoRefresh = true, want false")
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

// TestIngestConfigStageDefault proves the IngestConfig accessor returns 1 when
// the [ingest] section is absent and clamps below-range values to 1.
func TestIngestConfigStageDefault(t *testing.T) {
	// Absent section (zero value): accessor returns 1.
	var ic IngestConfig
	if ic.GetStage() != 1 {
		t.Errorf("absent [ingest]: GetStage() = %d, want 1", ic.GetStage())
	}
	// Explicitly zero: clamps to 1.
	ic.Stage = 0
	if ic.GetStage() != 1 {
		t.Errorf("stage=0: GetStage() = %d, want 1 (below-range clamp)", ic.GetStage())
	}
	// Negative: clamps to 1.
	ic.Stage = -1
	if ic.GetStage() != 1 {
		t.Errorf("stage=-1: GetStage() = %d, want 1 (below-range clamp)", ic.GetStage())
	}
	// In range: returned as-is.
	for _, want := range []int{1, 2, 3, 4} {
		ic.Stage = want
		if ic.GetStage() != want {
			t.Errorf("stage=%d: GetStage() = %d, want %d", want, ic.GetStage(), want)
		}
	}
}

// TestIngestConfigParsesFromTOML proves that [ingest] stage and scratch_dirs
// round-trip through Save/LoadVaultConfig correctly.
func TestIngestConfigParsesFromTOML(t *testing.T) {
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, ".hebb"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `name = "Work"

[ingest]
stage = 2
scratch_dirs = ["Daily/Scratch", "Inbox/Staging"]
`
	if err := os.WriteFile(filepath.Join(vault, ".hebb", "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _, err := LoadVaultConfig(vault)
	if err != nil {
		t.Fatalf("LoadVaultConfig: %v", err)
	}
	if got.Ingest.GetStage() != 2 {
		t.Errorf("stage = %d, want 2", got.Ingest.GetStage())
	}
	wantDirs := []string{"Daily/Scratch", "Inbox/Staging"}
	if !reflect.DeepEqual(got.Ingest.ScratchDirs, wantDirs) {
		t.Errorf("scratch_dirs = %v, want %v", got.Ingest.ScratchDirs, wantDirs)
	}
}

// TestIngestConfigAbsentSection proves LoadVaultConfig returns stage 1 and no
// scratch_dirs when the [ingest] block is entirely absent.
func TestIngestConfigAbsentSection(t *testing.T) {
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, ".hebb"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, ".hebb", "config.toml"), []byte(`name = "Work"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _, err := LoadVaultConfig(vault)
	if err != nil {
		t.Fatalf("LoadVaultConfig: %v", err)
	}
	if got.Ingest.GetStage() != 1 {
		t.Errorf("absent [ingest]: GetStage() = %d, want 1", got.Ingest.GetStage())
	}
	if len(got.Ingest.ScratchDirs) != 0 {
		t.Errorf("absent [ingest]: scratch_dirs = %v, want empty", got.Ingest.ScratchDirs)
	}
}

// TestIngestConfigBelowRangeClamp proves that a stage value below 1 in the
// config file is clamped to 1 by the accessor.
func TestIngestConfigBelowRangeClamp(t *testing.T) {
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, ".hebb"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, ".hebb", "config.toml"), []byte("name = \"Work\"\n\n[ingest]\nstage = 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _, err := LoadVaultConfig(vault)
	if err != nil {
		t.Fatalf("LoadVaultConfig: %v", err)
	}
	if got.Ingest.GetStage() != 1 {
		t.Errorf("stage=0 in config: GetStage() = %d, want 1 (clamped)", got.Ingest.GetStage())
	}
}

// TestIngestConfigSaveRoundTrip proves that a VaultConfig with an [ingest]
// section survives a Save/Load round-trip.
func TestIngestConfigSaveRoundTrip(t *testing.T) {
	vault := t.TempDir()
	want := DefaultVaultConfig("RT")
	want.Ingest = IngestConfig{
		Stage:       3,
		ScratchDirs: []string{"Daily/Scratch", "Inbox/Staging"},
	}
	if err := want.Save(vault); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, _, err := LoadVaultConfig(vault)
	if err != nil {
		t.Fatalf("LoadVaultConfig: %v", err)
	}
	if got.Ingest.GetStage() != 3 {
		t.Errorf("round-trip stage = %d, want 3", got.Ingest.GetStage())
	}
	if !reflect.DeepEqual(got.Ingest.ScratchDirs, want.Ingest.ScratchDirs) {
		t.Errorf("round-trip scratch_dirs = %v, want %v", got.Ingest.ScratchDirs, want.Ingest.ScratchDirs)
	}
}

// TestGeneratedConfigDocumentsIngest proves that Save writes the [ingest] keys
// in commented form in the generated config.toml header comment.
func TestGeneratedConfigDocumentsIngest(t *testing.T) {
	vault := t.TempDir()
	vc := DefaultVaultConfig("Docs")
	if err := vc.Save(vault); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(vault, ".hebb", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(b)
	for _, want := range []string{"[ingest]", "scratch_dirs", "stage", "exclude_dirs"} {
		if !strings.Contains(content, want) {
			t.Errorf("generated config.toml missing %q in commented defaults", want)
		}
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
