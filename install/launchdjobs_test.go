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
	jobs := VaultJobs("/vaults/work", "work", "/usr/local/bin/hebb", t.TempDir(), home, 4399, []string{"web"})
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
		[]string{"daily-digest", "action-review"})
	if len(jobs) != 0 {
		t.Errorf("expected automation jobs skipped when scripts absent, got %d", len(jobs))
	}

	// Create the migrated scripts; now they should be built.
	autoDir := filepath.Join(assetRoot, "automation")
	if err := os.MkdirAll(autoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"run-vault-digest.sh", "generate-action-review.py"} {
		if err := os.WriteFile(filepath.Join(autoDir, f), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	jobs = VaultJobs("/vaults/work", "work", "hebb", assetRoot, home, 4321,
		[]string{"daily-digest", "action-review"})

	digest, ok := jobByLabel(jobs, "local.hebb.work.daily-digest")
	if !ok {
		t.Fatal("daily-digest job not built once script exists")
	}
	if len(digest.Schedule) != 5 {
		t.Errorf("daily-digest should run 5 weekdays, got %d entries", len(digest.Schedule))
	}
	if !strings.Contains(strings.Join(digest.Program, " "), "--vault-root /vaults/work") {
		t.Errorf("digest program missing vault: %v", digest.Program)
	}

	review, ok := jobByLabel(jobs, "local.hebb.work.action-review")
	if !ok {
		t.Fatal("action-review job not built once script exists")
	}
	if len(review.Schedule) != 1 || review.Schedule[0].Hour != 7 || review.Schedule[0].Minute != 3 {
		t.Errorf("action-review should be daily 07:03, got %+v", review.Schedule)
	}
}

func TestVaultJobsSkipsUnknown(t *testing.T) {
	jobs := VaultJobs("/v", "v", "hebb", t.TempDir(), t.TempDir(), 4321, []string{"web", "bogus"})
	if len(jobs) != 1 {
		t.Errorf("unknown job name should be skipped, got %d jobs", len(jobs))
	}
}
