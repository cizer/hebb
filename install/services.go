package install

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Restarting services is needed because hebb's only long-running launchd job,
// the web server (KeepAlive), keeps executing the old binary inode after the
// binary is replaced (by `hebb update`, a dev `go build`, brew, or go install);
// launchctl relaunches the same process, so it does not pick up the new path
// until the job is restarted. Scheduled jobs (daily-digest, action-review,
// update-check) re-exec the binary on their next run, so they are deliberately
// left alone here: kickstarting them would fire them immediately.

// isHebbWebLabel reports whether a launchd label is a hebb web service
// (local.hebb.<slug>.web), the only long-running job hebb defines.
func isHebbWebLabel(label string) bool {
	return strings.HasPrefix(label, "local.hebb.") && strings.HasSuffix(label, ".web")
}

// parseHebbWebLabels extracts hebb web-service labels from `launchctl list`
// output. Each line is "PID\tStatus\tLabel"; the label is the last field. The
// header line (last field "Label") and every non-web or non-hebb job are
// skipped.
func parseHebbWebLabels(listOutput string) []string {
	var out []string
	for _, line := range strings.Split(listOutput, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		label := f[len(f)-1]
		if isHebbWebLabel(label) {
			out = append(out, label)
		}
	}
	return out
}

// RestartServices restarts every loaded hebb web service across all vaults on
// this machine via `launchctl kickstart -k` (kill and relaunch), so a replaced
// binary takes effect. Only the long-running web jobs are restarted; scheduled
// jobs pick up the new binary on their next run. It is a safe no-op (returns
// nil, nil) where launchctl is unavailable (e.g. Linux), so callers can invoke
// it unconditionally. Returns the labels restarted.
func RestartServices() ([]string, error) {
	if _, err := exec.LookPath("launchctl"); err != nil {
		return nil, nil // not macOS / no launchctl: nothing to restart
	}
	return restartServices(launchctlListOutput, kickstartLabel)
}

// restartServices is the testable core: list returns `launchctl list` output,
// restart restarts one label. It restarts each hebb web service in turn and
// stops at the first restart error, returning the labels restarted so far.
func restartServices(list func() (string, error), restart func(label string) error) ([]string, error) {
	out, err := list()
	if err != nil {
		return nil, err
	}
	var restarted []string
	for _, label := range parseHebbWebLabels(out) {
		if err := restart(label); err != nil {
			return restarted, fmt.Errorf("restart %s: %w", label, err)
		}
		restarted = append(restarted, label)
	}
	return restarted, nil
}

func launchctlListOutput() (string, error) {
	out, err := exec.Command("launchctl", "list").Output()
	return string(out), err
}

func kickstartLabel(label string) error {
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	return exec.Command("launchctl", "kickstart", "-k", domain+"/"+label).Run()
}
