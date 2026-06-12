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
	jobs := VaultJobs("/vaults/work", "work", "/usr/local/bin/hebb", t.TempDir(), home, 4399, []string{"web"}, false, nil)
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
		[]string{"daily-digest", "action-review"}, false, nil)
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
		[]string{"daily-digest", "action-review"}, false, nil)

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
		[]string{"action-review", "web"}, false, jobArgs)

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
	jobs := VaultJobs("/v", "v", "hebb", t.TempDir(), t.TempDir(), 4321, []string{"web", "bogus"}, false, nil)
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
	jobs := VaultJobs("/vaults/work", "work", "/opt/homebrew/bin/hebb", assetRoot, home, 4321, names, false, nil)
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
		[]string{"daily-digest"}, false, jobArgs)
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

func TestVaultJobsUpdateCheck(t *testing.T) {
	// Default: the scheduled job only checks (notifies).
	jobs := VaultJobs("/v", "v", "hebb", t.TempDir(), t.TempDir(), 4321, []string{"update-check"}, false, nil)
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
	jobs = VaultJobs("/v", "v", "hebb", t.TempDir(), t.TempDir(), 4321, []string{"update-check"}, true, nil)
	j, _ = jobByLabel(jobs, "local.hebb.v.update-check")
	if got := strings.Join(j.Program, " "); !strings.Contains(got, "update") || strings.Contains(got, "--check") {
		t.Errorf("auto update-check should run 'update' without --check, got %q", got)
	}
}
