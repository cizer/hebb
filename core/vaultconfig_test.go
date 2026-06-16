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

// TestVaultConfigJobEnv proves [job_env] parses from TOML and round-trips through
// Save/LoadVaultConfig correctly, mirroring the [job_args] tests.
func TestVaultConfigJobEnv(t *testing.T) {
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, ".hebb"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `name = "Work"

[job_env]
action-review = { HEBB_NOTIFY_URL = "https://hooks.example.com/abc", MY_KEY = "my-value" }
`
	if err := os.WriteFile(filepath.Join(vault, ".hebb", "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _, err := LoadVaultConfig(vault)
	if err != nil {
		t.Fatalf("LoadVaultConfig: %v", err)
	}
	env := got.JobEnv["action-review"]
	if env == nil {
		t.Fatal("job_env[action-review] = nil, want a map")
	}
	if env["HEBB_NOTIFY_URL"] != "https://hooks.example.com/abc" {
		t.Errorf("HEBB_NOTIFY_URL = %q, want https://hooks.example.com/abc", env["HEBB_NOTIFY_URL"])
	}
	if env["MY_KEY"] != "my-value" {
		t.Errorf("MY_KEY = %q, want my-value", env["MY_KEY"])
	}
}

// TestVaultConfigJobEnvSaveRoundTrip proves a VaultConfig with a [job_env]
// section survives a Save/Load round-trip.
func TestVaultConfigJobEnvSaveRoundTrip(t *testing.T) {
	vault := t.TempDir()
	want := DefaultVaultConfig("RT")
	want.JobEnv = JobEnv{
		"action-review": {"HEBB_NOTIFY_URL": "https://hooks.example.com/abc"},
	}
	if err := want.Save(vault); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, _, err := LoadVaultConfig(vault)
	if err != nil {
		t.Fatalf("LoadVaultConfig: %v", err)
	}
	if got.JobEnv["action-review"]["HEBB_NOTIFY_URL"] != "https://hooks.example.com/abc" {
		t.Errorf("round-trip job_env[action-review][HEBB_NOTIFY_URL] = %q, want url",
			got.JobEnv["action-review"]["HEBB_NOTIFY_URL"])
	}
}

// TestVaultConfigJobEnvAbsent proves LoadVaultConfig returns a nil/empty JobEnv
// when the [job_env] block is absent, and the round-trip with no JobEnv works.
func TestVaultConfigJobEnvAbsent(t *testing.T) {
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
	if len(got.JobEnv) != 0 {
		t.Errorf("absent [job_env]: JobEnv = %v, want empty", got.JobEnv)
	}
}

// TestGeneratedConfigDocumentsJobEnv proves that Save writes the [job_env] keys
// in commented form in the generated config.toml header comment.
func TestGeneratedConfigDocumentsJobEnv(t *testing.T) {
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
	for _, want := range []string{"[job_env]", "HEBB_NOTIFY_URL", "job_args"} {
		if !strings.Contains(content, want) {
			t.Errorf("generated config.toml missing %q in commented defaults", want)
		}
	}
}

// TestNotifyConfigParsesFromTOML proves [notify] parses correctly from TOML.
func TestNotifyConfigParsesFromTOML(t *testing.T) {
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, ".hebb"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `name = "Work"

[notify]
enabled = true
url = "https://hooks.example.com/abc"
`
	if err := os.WriteFile(filepath.Join(vault, ".hebb", "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _, err := LoadVaultConfig(vault)
	if err != nil {
		t.Fatalf("LoadVaultConfig: %v", err)
	}
	if !got.Notify.Enabled {
		t.Error("[notify] enabled = false, want true")
	}
	if got.Notify.URL != "https://hooks.example.com/abc" {
		t.Errorf("[notify] url = %q, want url", got.Notify.URL)
	}
}

// TestNotifyConfigAbsent proves that an absent [notify] section results in
// disabled notify with an empty URL.
func TestNotifyConfigAbsent(t *testing.T) {
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
	if got.Notify.Enabled {
		t.Error("absent [notify]: enabled = true, want false")
	}
	if got.Notify.URL != "" {
		t.Errorf("absent [notify]: url = %q, want empty", got.Notify.URL)
	}
}

// TestNotifyConfigResolveURLEnvOverride proves that $HEBB_NOTIFY_URL takes
// priority over the committed [notify] url value.
func TestNotifyConfigResolveURLEnvOverride(t *testing.T) {
	// Patch envGet for this test only.
	orig := envGet
	defer func() { envGet = orig }()
	envGet = func(key string) string {
		if key == "HEBB_NOTIFY_URL" {
			return "https://env-override.example.com/hook"
		}
		return ""
	}

	nc := NotifyConfig{Enabled: true, URL: "https://committed.example.com/hook"}
	got := nc.ResolveURL()
	if got != "https://env-override.example.com/hook" {
		t.Errorf("ResolveURL = %q, want env override", got)
	}
}

// TestNotifyConfigResolveURLFallsBackToConfig proves that when $HEBB_NOTIFY_URL
// is absent the committed url is used.
func TestNotifyConfigResolveURLFallsBackToConfig(t *testing.T) {
	orig := envGet
	defer func() { envGet = orig }()
	envGet = func(string) string { return "" }

	nc := NotifyConfig{Enabled: true, URL: "https://committed.example.com/hook"}
	if nc.ResolveURL() != "https://committed.example.com/hook" {
		t.Errorf("ResolveURL should fall back to committed url when env is absent")
	}
}

// TestNotifyConfigSaveRoundTrip proves a VaultConfig with a [notify] section
// survives a Save/Load round-trip.
func TestNotifyConfigSaveRoundTrip(t *testing.T) {
	vault := t.TempDir()
	want := DefaultVaultConfig("RT")
	want.Notify = NotifyConfig{Enabled: true, URL: "https://hooks.example.com/xyz"}
	if err := want.Save(vault); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, _, err := LoadVaultConfig(vault)
	if err != nil {
		t.Fatalf("LoadVaultConfig: %v", err)
	}
	if !got.Notify.Enabled {
		t.Error("round-trip notify.enabled = false, want true")
	}
	if got.Notify.URL != "https://hooks.example.com/xyz" {
		t.Errorf("round-trip notify.url = %q, want url", got.Notify.URL)
	}
}

// TestGeneratedConfigDocumentsNotify proves Save writes [notify] keys in
// commented form in the generated config.toml header comment.
func TestGeneratedConfigDocumentsNotify(t *testing.T) {
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
	for _, want := range []string{"[notify]", "HEBB_NOTIFY_URL", "webhook"} {
		if !strings.Contains(content, want) {
			t.Errorf("generated config.toml missing %q in commented defaults", want)
		}
	}
}

// TestHealthConfigReportUnresolvedDefault proves report_unresolved_links
// defaults to false (unresolved links are expected future notes, not errors).
func TestHealthConfigReportUnresolvedDefault(t *testing.T) {
	if (HealthConfig{}).ReportUnresolvedLinks {
		t.Error("absent [health]: ReportUnresolvedLinks = true, want false (default off)")
	}
}

// TestHealthConfigAttachmentExtensionsDefault proves the accessor returns the
// built-in attachment extension list when the config leaves it empty.
func TestHealthConfigAttachmentExtensionsDefault(t *testing.T) {
	got := (HealthConfig{}).GetAttachmentExtensions()
	if len(got) == 0 {
		t.Fatal("GetAttachmentExtensions() on empty config returned no defaults")
	}
	want := map[string]bool{}
	for _, e := range got {
		want[e] = true
	}
	for _, ext := range []string{"png", "pdf", "pptx", "canvas", "excalidraw"} {
		if !want[ext] {
			t.Errorf("default attachment extensions missing %q; got %v", ext, got)
		}
	}
}

// TestHealthConfigAttachmentExtensionsCustom proves a configured list overrides
// the default rather than being merged with it.
func TestHealthConfigAttachmentExtensionsCustom(t *testing.T) {
	hc := HealthConfig{AttachmentExtensions: []string{"xyz"}}
	got := hc.GetAttachmentExtensions()
	if len(got) != 1 || got[0] != "xyz" {
		t.Errorf("GetAttachmentExtensions() = %v, want [xyz] (config overrides default)", got)
	}
}

// TestHealthConfigParsesFromTOML proves the new [health] keys parse from TOML.
func TestHealthConfigParsesFromTOML(t *testing.T) {
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, ".hebb"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `name = "Work"

[health]
report_unresolved_links = true
attachment_extensions = ["png", "pdf"]
`
	if err := os.WriteFile(filepath.Join(vault, ".hebb", "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _, err := LoadVaultConfig(vault)
	if err != nil {
		t.Fatalf("LoadVaultConfig: %v", err)
	}
	if !got.Health.ReportUnresolvedLinks {
		t.Error("[health] report_unresolved_links = false, want true")
	}
	if !reflect.DeepEqual(got.Health.AttachmentExtensions, []string{"png", "pdf"}) {
		t.Errorf("attachment_extensions = %v, want [png pdf]", got.Health.AttachmentExtensions)
	}
}

// TestHealthConfigSaveRoundTrip proves the [health] block survives Save/Load.
func TestHealthConfigSaveRoundTrip(t *testing.T) {
	vault := t.TempDir()
	want := DefaultVaultConfig("RT")
	want.Health = HealthConfig{
		ProjectStaleDays:      30,
		SizeThreshold:         500,
		ReportUnresolvedLinks: true,
		AttachmentExtensions:  []string{"png", "pdf"},
	}
	if err := want.Save(vault); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, _, err := LoadVaultConfig(vault)
	if err != nil {
		t.Fatalf("LoadVaultConfig: %v", err)
	}
	if !got.Health.ReportUnresolvedLinks {
		t.Error("round-trip report_unresolved_links = false, want true")
	}
	if !reflect.DeepEqual(got.Health.AttachmentExtensions, want.Health.AttachmentExtensions) {
		t.Errorf("round-trip attachment_extensions = %v, want %v",
			got.Health.AttachmentExtensions, want.Health.AttachmentExtensions)
	}
}

// TestGeneratedConfigDocumentsHealth proves Save documents the new [health]
// keys in the generated config.toml header comment.
func TestGeneratedConfigDocumentsHealth(t *testing.T) {
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
	for _, want := range []string{"[health]", "report_unresolved_links", "attachment_extensions"} {
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
