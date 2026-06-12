package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cizer/hebb/launchd"
)

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Team Vault":    "team-vault",
		"My Work Vault": "my-work-vault",
		"work_2025":     "work-2025",
		"  Spaces  ":    "spaces",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func jobByLabel(jobs []launchd.Job, label string) (launchd.Job, bool) {
	for _, j := range jobs {
		if j.Label == label {
			return j, true
		}
	}
	return launchd.Job{}, false
}

func TestVaultJobsWebIsBuiltIn(t *testing.T) {
	home := t.TempDir()
	jobs := VaultJobs("/vaults/work", "work", "/usr/local/bin/hebb", t.TempDir(), home, 4399, []string{"web"}, false, nil, nil)
	j, ok := jobByLabel(jobs, "local.hebb.work.web")
	if !ok {
		t.Fatalf("web job not built; got %d jobs", len(jobs))
	}
	prog := strings.Join(j.Program, " ")
	for _, want := range []string{"/usr/local/bin/hebb", "serve", "--vault", "/vaults/work", "--port", "4399"} {
		if !strings.Contains(prog, want) {
			t.Errorf("web program %q missing %q", prog, want)
		}
	}
	if !j.KeepAlive || !j.RunAtLoad {
		t.Error("web job should KeepAlive and RunAtLoad")
	}
	if filepath.Dir(j.LogPath) != filepath.Join(home, "Library", "Logs") {
		t.Errorf("log path %q not under ~/Library/Logs", j.LogPath)
	}
}

func TestVaultJobsAutomationGatedOnScript(t *testing.T) {
	home := t.TempDir()
	assetRoot := t.TempDir()

	// Without the scripts present, automation jobs are skipped.
	jobs := VaultJobs("/vaults/work", "work", "hebb", assetRoot, home, 4321,
		[]string{"daily-digest", "action-review"}, false, nil, nil)
	if len(jobs) != 0 {
		t.Errorf("expected automation jobs skipped when scripts absent, got %d", len(jobs))
	}

	// Create the migrated scripts; now they should be built.
	autoDir := filepath.Join(assetRoot, "automation")
	if err := os.MkdirAll(autoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"generate-vault-digest.py", "generate-action-review.py"} {
		if err := os.WriteFile(filepath.Join(autoDir, f), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	jobs = VaultJobs("/vaults/work", "work", "hebb", assetRoot, home, 4321,
		[]string{"daily-digest", "action-review"}, false, nil, nil)

	digest, ok := jobByLabel(jobs, "local.hebb.work.daily-digest")
	if !ok {
		t.Fatal("daily-digest job not built once script exists")
	}
	if len(digest.Schedule) != 5 {
		t.Errorf("daily-digest should run 5 weekdays, got %d entries", len(digest.Schedule))
	}
	// Program[0] is the grantable hebb binary running the digest subcommand, not
	// the run-vault-digest.sh wrapper: macOS TCC attributes Full Disk Access to
	// Program[0], and a shell interpreter has no grantable identity.
	if digest.Program[0] != "hebb" {
		t.Errorf("digest Program[0] should be the hebb binary, got %q", digest.Program[0])
	}
	if !strings.Contains(strings.Join(digest.Program, " "), "digest --vault-root /vaults/work") {
		t.Errorf("digest program should run `digest --vault-root <vault>`: %v", digest.Program)
	}
	// PYTHON stays (launchd's minimal PATH resolves python3 to the Xcode shim,
	// which has no Full Disk Access); HEBB_BIN goes (hebb is now Program[0] and
	// invokes itself).
	env := map[string]string{}
	for _, e := range digest.EnvVars {
		env[e.Key] = e.Value
	}
	if env["PYTHON"] == "" || !filepath.IsAbs(env["PYTHON"]) {
		t.Errorf("digest job should pin an absolute PYTHON, got %q", env["PYTHON"])
	}
	if _, ok := env["HEBB_BIN"]; ok {
		t.Errorf("digest job should no longer pin HEBB_BIN; hebb is Program[0], got %v", env)
	}

	review, ok := jobByLabel(jobs, "local.hebb.work.action-review")
	if !ok {
		t.Fatal("action-review job not built once script exists")
	}
	if len(review.Schedule) != 1 || review.Schedule[0].Hour != 7 || review.Schedule[0].Minute != 3 {
		t.Errorf("action-review should be daily 07:03, got %+v", review.Schedule)
	}
	// HEBB_BIN is pinned so the script can invoke `hebb notify` after writing
	// its note: launchd's minimal PATH may not resolve the bare "hebb" name.
	reviewEnv := map[string]string{}
	for _, e := range review.EnvVars {
		reviewEnv[e.Key] = e.Value
	}
	if reviewEnv["HEBB_BIN"] != "hebb" {
		t.Errorf("action-review should pin HEBB_BIN = hebb, got %q", reviewEnv["HEBB_BIN"])
	}
}

func TestVaultJobsAppendsPerJobArgs(t *testing.T) {
	home := t.TempDir()
	assetRoot := t.TempDir()
	autoDir := filepath.Join(assetRoot, "automation")
	if err := os.MkdirAll(autoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(autoDir, "generate-action-review.py"), []byte("#!/usr/bin/env python3\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	jobArgs := map[string][]string{
		"action-review": {"--owner", "Alex Doe", "--mine-output", "2-Areas/_MY-OPEN-ACTIONS.md"},
		"bogus":         {"--ignored"},
	}
	jobs := VaultJobs("/vaults/work", "work", "hebb", assetRoot, home, 4321,
		[]string{"action-review", "web"}, false, jobArgs, nil)

	review, ok := jobByLabel(jobs, "local.hebb.work.action-review")
	if !ok {
		t.Fatal("action-review job not built")
	}
	prog := strings.Join(review.Program, " ")
	for _, want := range []string{"--vault-root /vaults/work", "--owner Alex Doe", "--mine-output 2-Areas/_MY-OPEN-ACTIONS.md"} {
		if !strings.Contains(prog, want) {
			t.Errorf("action-review program %q missing %q", prog, want)
		}
	}

	// Jobs without configured args are untouched.
	web, ok := jobByLabel(jobs, "local.hebb.work.web")
	if !ok {
		t.Fatal("web job not built")
	}
	if got := strings.Join(web.Program, " "); strings.Contains(got, "--owner") || strings.Contains(got, "--ignored") {
		t.Errorf("web program %q should not pick up other jobs' args", got)
	}
}

func TestVaultJobsSkipsUnknown(t *testing.T) {
	jobs := VaultJobs("/v", "v", "hebb", t.TempDir(), t.TempDir(), 4321, []string{"web", "bogus"}, false, nil, nil)
	if len(jobs) != 1 {
		t.Errorf("unknown job name should be skipped, got %d jobs", len(jobs))
	}
}

// TestVaultJobsNoShellProgram asserts the TCC-hardening invariant over the full
// default jobs list: no rendered stock job may have a shell script or interpreter
// shim as Program[0], because macOS TCC attributes file-access permission to
// Program[0] and a shell wrapper has no grantable identity (SPEC item 2).
func TestVaultJobsNoShellProgram(t *testing.T) {
	home := t.TempDir()
	assetRoot := t.TempDir()
	autoDir := filepath.Join(assetRoot, "automation")
	if err := os.MkdirAll(autoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Both automation scripts present, so daily-digest and action-review render.
	for _, f := range []string{"run-vault-digest.sh", "generate-vault-digest.py", "generate-action-review.py"} {
		if err := os.WriteFile(filepath.Join(autoDir, f), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// The default jobs list from DefaultVaultConfig.
	names := []string{"daily-digest", "action-review", "web", "update-check"}
	jobs := VaultJobs("/vaults/work", "work", "/opt/homebrew/bin/hebb", assetRoot, home, 4321, names, false, nil, nil)
	if len(jobs) != len(names) {
		t.Fatalf("expected %d default jobs to render, got %d", len(names), len(jobs))
	}

	badPrefixes := []string{"/bin/sh", "/bin/bash", "/usr/bin/env"}
	for _, j := range jobs {
		prog0 := j.Program[0]
		if strings.HasSuffix(prog0, ".sh") {
			t.Errorf("job %s has a shell script as Program[0]: %q", j.Label, prog0)
		}
		for _, p := range badPrefixes {
			if prog0 == p || strings.HasPrefix(prog0, p+" ") {
				t.Errorf("job %s has an interpreter shim as Program[0]: %q", j.Label, prog0)
			}
		}
	}
}

func TestVaultJobsDigestAppendsArgs(t *testing.T) {
	home := t.TempDir()
	assetRoot := t.TempDir()
	autoDir := filepath.Join(assetRoot, "automation")
	if err := os.MkdirAll(autoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(autoDir, "generate-vault-digest.py"), []byte("#!/usr/bin/env python3\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	jobArgs := map[string][]string{"daily-digest": {"--output", "2-Areas/_DIGEST.md"}}
	jobs := VaultJobs("/vaults/work", "work", "hebb", assetRoot, home, 4321,
		[]string{"daily-digest"}, false, jobArgs, nil)
	digest, ok := jobByLabel(jobs, "local.hebb.work.daily-digest")
	if !ok {
		t.Fatal("daily-digest job not built")
	}
	// The built-in flags come first, the [job_args] extras last.
	want := []string{"hebb", "digest", "--vault-root", "/vaults/work", "--output", "2-Areas/_DIGEST.md"}
	if got := strings.Join(digest.Program, " "); got != strings.Join(want, " ") {
		t.Errorf("digest program = %q, want %q", got, strings.Join(want, " "))
	}
}

// TestVaultJobsJobEnvAppendsAfterBuiltIn proves that [job_env] entries render
// into that job's EnvironmentVariables after the built-in env, deterministically
// ordered. This mirrors the [job_args] test pattern.
func TestVaultJobsJobEnvAppendsAfterBuiltIn(t *testing.T) {
	home := t.TempDir()
	assetRoot := t.TempDir()
	autoDir := filepath.Join(assetRoot, "automation")
	if err := os.MkdirAll(autoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(autoDir, "generate-vault-digest.py"), []byte("#!/usr/bin/env python3\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	jobEnv := map[string]map[string]string{
		"daily-digest": {"HEBB_NOTIFY_URL": "https://hooks.example.com/abc", "EXTRA_KEY": "extra-val"},
		"bogus":        {"IGNORED": "yes"},
	}
	jobs := VaultJobs("/vaults/work", "work", "hebb", assetRoot, home, 4321,
		[]string{"daily-digest"}, false, nil, jobEnv)

	digest, ok := jobByLabel(jobs, "local.hebb.work.daily-digest")
	if !ok {
		t.Fatal("daily-digest job not built")
	}
	env := map[string]string{}
	for _, e := range digest.EnvVars {
		env[e.Key] = e.Value
	}
	// Built-in PYTHON must still be present.
	if env["PYTHON"] == "" {
		t.Error("built-in PYTHON env var missing after job_env injection")
	}
	// [job_env] extras must also be present.
	if env["HEBB_NOTIFY_URL"] != "https://hooks.example.com/abc" {
		t.Errorf("HEBB_NOTIFY_URL = %q, want url", env["HEBB_NOTIFY_URL"])
	}
	if env["EXTRA_KEY"] != "extra-val" {
		t.Errorf("EXTRA_KEY = %q, want extra-val", env["EXTRA_KEY"])
	}
	// Built-in env must appear before user-supplied env (PYTHON is first).
	if len(digest.EnvVars) < 2 || digest.EnvVars[0].Key != "PYTHON" {
		t.Errorf("built-in env (PYTHON) should precede user env, got %v", digest.EnvVars)
	}
}

// TestVaultJobsJobEnvOverridesBuiltIn proves that a [job_env] key matching a
// built-in env key (e.g. PATH) replaces it with no duplicate key in the plist.
// The spec says "user wins" and a plist EnvironmentVariables dict cannot hold
// duplicate keys.
func TestVaultJobsJobEnvOverridesBuiltIn(t *testing.T) {
	home := t.TempDir()
	assetRoot := t.TempDir()
	autoDir := filepath.Join(assetRoot, "automation")
	if err := os.MkdirAll(autoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(autoDir, "generate-vault-digest.py"), []byte("#!/usr/bin/env python3\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Override the built-in PYTHON env var.
	jobEnv := map[string]map[string]string{
		"daily-digest": {"PYTHON": "/custom/python3"},
	}
	jobs := VaultJobs("/vaults/work", "work", "hebb", assetRoot, home, 4321,
		[]string{"daily-digest"}, false, nil, jobEnv)

	digest, ok := jobByLabel(jobs, "local.hebb.work.daily-digest")
	if !ok {
		t.Fatal("daily-digest job not built")
	}
	// Count PYTHON keys: must be exactly 1 (no duplicate).
	count := 0
	overrideValue := ""
	for _, e := range digest.EnvVars {
		if e.Key == "PYTHON" {
			count++
			overrideValue = e.Value
		}
	}
	if count != 1 {
		t.Errorf("PYTHON appears %d times in EnvVars, want exactly 1 (no duplicate keys)", count)
	}
	if overrideValue != "/custom/python3" {
		t.Errorf("PYTHON override = %q, want /custom/python3 (user wins)", overrideValue)
	}
}

// TestVaultJobsJobEnvNoEntryIsIdentical proves that jobs without a [job_env]
// entry render byte-identically to today (no regression for absent config).
func TestVaultJobsJobEnvNoEntryIsIdentical(t *testing.T) {
	home := t.TempDir()
	jobs1 := VaultJobs("/v", "v", "hebb", t.TempDir(), home, 4321, []string{"web"}, false, nil, nil)
	jobs2 := VaultJobs("/v", "v", "hebb", t.TempDir(), home, 4321, []string{"web"}, false, nil, map[string]map[string]string{"other-job": {"KEY": "val"}})

	if len(jobs1) != 1 || len(jobs2) != 1 {
		t.Fatalf("expected 1 job each, got %d and %d", len(jobs1), len(jobs2))
	}

	b1, err1 := launchd.Render(jobs1[0])
	b2, err2 := launchd.Render(jobs2[0])
	if err1 != nil || err2 != nil {
		t.Fatalf("render errors: %v %v", err1, err2)
	}
	if string(b1) != string(b2) {
		t.Errorf("job without job_env entry should render byte-identically;\ngot  %q\nwant %q", b2, b1)
	}
}

// TestVaultJobsJobEnvDeterministicOrder proves the env vars in a rendered plist
// appear in deterministic order: built-in env first (in original order), then
// user env sorted by key.
func TestVaultJobsJobEnvDeterministicOrder(t *testing.T) {
	home := t.TempDir()
	assetRoot := t.TempDir()
	autoDir := filepath.Join(assetRoot, "automation")
	if err := os.MkdirAll(autoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(autoDir, "generate-vault-digest.py"), []byte("#!/usr/bin/env python3\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	jobEnv := map[string]map[string]string{
		"daily-digest": {"Z_KEY": "z", "A_KEY": "a", "M_KEY": "m"},
	}
	// Run twice to confirm order is stable across calls.
	for i := 0; i < 2; i++ {
		jobs := VaultJobs("/vaults/work", "work", "hebb", assetRoot, home, 4321,
			[]string{"daily-digest"}, false, nil, jobEnv)
		digest, ok := jobByLabel(jobs, "local.hebb.work.daily-digest")
		if !ok {
			t.Fatal("daily-digest job not built")
		}
		// Built-in PYTHON first, then A_KEY, M_KEY, Z_KEY (sorted).
		wantOrder := []string{"PYTHON", "A_KEY", "M_KEY", "Z_KEY"}
		if len(digest.EnvVars) != len(wantOrder) {
			t.Fatalf("run %d: want %d env vars, got %d: %v", i+1, len(wantOrder), len(digest.EnvVars), digest.EnvVars)
		}
		for j, k := range wantOrder {
			if digest.EnvVars[j].Key != k {
				t.Errorf("run %d: EnvVars[%d].Key = %q, want %q", i+1, j, digest.EnvVars[j].Key, k)
			}
		}
	}
}

func TestVaultJobsUpdateCheck(t *testing.T) {
	// Default: the scheduled job only checks (notifies).
	jobs := VaultJobs("/v", "v", "hebb", t.TempDir(), t.TempDir(), 4321, []string{"update-check"}, false, nil, nil)
	j, ok := jobByLabel(jobs, "local.hebb.v.update-check")
	if !ok {
		t.Fatal("update-check job not built")
	}
	if got := strings.Join(j.Program, " "); !strings.Contains(got, "update --check") {
		t.Errorf("default update-check should run 'update --check', got %q", got)
	}
	if len(j.Schedule) != 1 {
		t.Errorf("update-check should have one schedule entry, got %d", len(j.Schedule))
	}

	// auto = true: the job applies the update instead.
	jobs = VaultJobs("/v", "v", "hebb", t.TempDir(), t.TempDir(), 4321, []string{"update-check"}, true, nil, nil)
	j, _ = jobByLabel(jobs, "local.hebb.v.update-check")
	if got := strings.Join(j.Program, " "); !strings.Contains(got, "update") || strings.Contains(got, "--check") {
		t.Errorf("auto update-check should run 'update' without --check, got %q", got)
	}
}
