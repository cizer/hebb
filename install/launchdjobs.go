package install

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cizer/hebb/launchd"
)

// Slugify turns a vault name into a label-safe token (lowercase, alphanumerics
// and single hyphens), used in launchd labels and the memory symlink path.
func Slugify(s string) string {
	var b strings.Builder
	lastHyphen := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastHyphen = false
		default:
			if !lastHyphen && b.Len() > 0 {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// VaultJobs builds launchd job specs for the named jobs of a vault. The web and
// update-check jobs are built in (they run the hebb binary). The daily-digest
// and action-review jobs are only included when their script exists under
// <assetRoot>/automation, so no broken plists are written if the automation
// scripts are absent. updateAuto makes the update-check job install updates
// rather than only reporting them. Unknown names are skipped.
func VaultJobs(vaultPath, slug, hebbBin, assetRoot, home string, port int, names []string, updateAuto bool) []launchd.Job {
	logDir := filepath.Join(home, "Library", "Logs")
	logPath := func(job string) string {
		return filepath.Join(logDir, "hebb-"+slug+"-"+job+".log")
	}
	label := func(job string) string { return "local.hebb." + slug + "." + job }
	autoScript := func(name string) (string, bool) {
		p := filepath.Join(assetRoot, "automation", name)
		if _, err := os.Stat(p); err != nil {
			return "", false
		}
		return p, true
	}

	var jobs []launchd.Job
	for _, name := range names {
		switch name {
		case "web":
			jobs = append(jobs, launchd.Job{
				Label:      label("web"),
				Program:    []string{hebbBin, "serve", "--vault", vaultPath, "--port", strconv.Itoa(port)},
				WorkingDir: vaultPath,
				EnvVars:    []launchd.EnvVar{{Key: "HEBB_WEB_PORT", Value: strconv.Itoa(port)}},
				RunAtLoad:  true,
				KeepAlive:  true,
				Throttle:   10,
				LogPath:    logPath("web"),
			})
		case "daily-digest":
			script, ok := autoScript("run-vault-digest.sh")
			if !ok {
				continue
			}
			var days []launchd.CalInterval
			for wd := 1; wd <= 5; wd++ {
				days = append(days, launchd.CalInterval{Weekday: wd, Hour: 8, Minute: 0})
			}
			jobs = append(jobs, launchd.Job{
				Label:      label("daily-digest"),
				Program:    []string{script, "--vault-root", vaultPath},
				WorkingDir: vaultPath,
				Schedule:   days,
				LogPath:    logPath("daily-digest"),
			})
		case "action-review":
			script, ok := autoScript("generate-action-review.py")
			if !ok {
				continue
			}
			jobs = append(jobs, launchd.Job{
				Label:      label("action-review"),
				Program:    []string{pythonPath(), script, "--vault-root", vaultPath},
				WorkingDir: vaultPath,
				Schedule:   []launchd.CalInterval{{Weekday: -1, Hour: 7, Minute: 3}},
				LogPath:    logPath("action-review"),
			})
		case "update-check":
			args := []string{hebbBin, "update", "--check"}
			if updateAuto {
				args = []string{hebbBin, "update"}
			}
			jobs = append(jobs, launchd.Job{
				Label:      label("update-check"),
				Program:    args,
				WorkingDir: vaultPath,
				Schedule:   []launchd.CalInterval{{Weekday: 1, Hour: 9, Minute: 0}}, // Mondays 09:00
				LogPath:    logPath("update-check"),
			})
		}
	}
	return jobs
}

// pythonPath resolves an absolute python3 (launchd has a minimal PATH), falling
// back to the bare name if it cannot be located.
func pythonPath() string {
	if p, err := exec.LookPath("python3"); err == nil {
		return p
	}
	return "python3"
}
