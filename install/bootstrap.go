package install

import (
	"fmt"
	"os"
	"os/exec"
)

// Bootstrap (re)loads the given plists into the user's launchd domain. When run
// is false it returns the commands it would execute without running them, so an
// install can preview the bootstrap. A failed bootout (the job was not loaded)
// is ignored; only bootstrap failures are returned as errors.
func Bootstrap(plistPaths []string, run bool) ([]string, error) {
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	var planned []string
	for _, p := range plistPaths {
		planned = append(planned,
			fmt.Sprintf("launchctl bootout %s %s", domain, p),
			fmt.Sprintf("launchctl bootstrap %s %s", domain, p),
		)
	}
	if !run {
		return planned, nil
	}
	for _, p := range plistPaths {
		_ = exec.Command("launchctl", "bootout", domain, p).Run() // may not be loaded; ignore
		if out, err := exec.Command("launchctl", "bootstrap", domain, p).CombinedOutput(); err != nil {
			return planned, fmt.Errorf("launchctl bootstrap %s: %w: %s", p, err, out)
		}
	}
	return planned, nil
}
