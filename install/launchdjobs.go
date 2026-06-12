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
// rather than only reporting them. jobArgs carries the per-job extra arguments
// from config.toml's [job_args]; they are appended to the matching job's
// program. Unknown names are skipped.
func VaultJobs(vaultPath, slug, hebbBin, assetRoot, home string, port int, names []string, updateAuto bool, jobArgs map[string][]string) []launchd.Job {
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
		before := len(jobs)
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
			// Gate on the generator the `hebb digest` subcommand runs, so no broken
			// plist is written when the automation scripts are absent.
			if _, ok := autoScript("generate-vault-digest.py"); !ok {
				continue
			}
			var days []launchd.CalInterval
			for wd := 1; wd <= 5; wd++ {
				days = append(days, launchd.CalInterval{Weekday: wd, Hour: 8, Minute: 0})
			}
			jobs = append(jobs, launchd.Job{
				Label: label("daily-digest"),
				// Program[0] is the grantable hebb binary, not the run-vault-digest.sh
				// wrapper: macOS TCC attributes file-access permission to Program[0],
				// and a shell interpreter (env/bash) has no grantable identity, so the
				// child python's open() into a protected vault folder blocks
				// indefinitely. Running `hebb digest` makes Program[0] a binary the
				// user can grant Full Disk Access to, then the binary invokes python
				// itself. This was the one stock job that silently failed for weeks in
				// the field (SPEC item 2).
				Program:    []string{hebbBin, "digest", "--vault-root", vaultPath},
				WorkingDir: vaultPath,
				// PYTHON stays: launchd's minimal PATH resolves python3 to the Xcode
				// shim (no Full Disk Access), so `hebb digest` reads this pin to find a
				// real interpreter. HEBB_BIN is gone now that hebb is Program[0].
				EnvVars: []launchd.EnvVar{
					{Key: "PYTHON", Value: pythonPath()},
				},
				Schedule: days,
				LogPath:  logPath("daily-digest"),
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
		// Per-job extra args from config.toml's [job_args] go at the end of the
		// rendered program, after the built-in flags.
		if len(jobs) > before {
			if extra := jobArgs[name]; len(extra) > 0 {
				job := &jobs[len(jobs)-1]
				job.Program = append(job.Program, extra...)
			}
		}
	}
	return jobs
}

// pythonPath resolves an absolute python3 (launchd has a minimal PATH), falling
// back to the bare name if it cannot be located.
func pythonPath() string {
	return PythonPath()
}

// PythonPath resolves an absolute python3 (launchd has a minimal PATH), falling
// back to the bare name if it cannot be located. Exported so `hebb digest` can
// resolve the interpreter the same way the rendered launchd jobs do.
func PythonPath() string {
	if p, err := exec.LookPath("python3"); err == nil {
		return p
	}
	return "python3"
}
