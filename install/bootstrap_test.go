package install

import (
	"strings"
	"testing"
)

func TestBootstrapDryRunPlansCommands(t *testing.T) {
	plists := []string{"/L/local.hebb.work.web.plist"}
	planned, err := Bootstrap(plists, false)
	if err != nil {
		t.Fatalf("Bootstrap(dry): %v", err)
	}
	joined := strings.Join(planned, "\n")
	if !strings.Contains(joined, "bootout") || !strings.Contains(joined, "bootstrap") {
		t.Errorf("dry run should plan bootout+bootstrap, got:\n%s", joined)
	}
	if !strings.Contains(joined, "/L/local.hebb.work.web.plist") {
		t.Errorf("plan should reference the plist path, got:\n%s", joined)
	}
}

func TestBootstrapDryRunDoesNotExecute(t *testing.T) {
	// A bogus path must not error in dry-run mode (nothing is executed).
	if _, err := Bootstrap([]string{"/does/not/exist.plist"}, false); err != nil {
		t.Errorf("dry run must not execute or error: %v", err)
	}
}
