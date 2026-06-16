package install

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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

// VaultJobs builds launchd job specs for the named jobs of a vault. The web,
// update-check and daily-digest jobs are built in (they run the hebb binary;
// `hebb digest` no longer needs a materialised script). The action-review job
// is only included when its script exists under <assetRoot>/automation, so no
// broken plist is written if the automation script is absent. updateAuto makes
// the update-check job install updates
// rather than only reporting them. jobArgs carries the per-job extra arguments
// from config.toml's [job_args]; they are appended to the matching job's
// program. jobEnv carries per-job extra environment variables from config.toml's
// [job_env]; they are merged into the job's EnvVars after built-in env, with
// user-supplied keys overriding built-in keys of the same name (user wins). The
// merge produces a deterministic slice: built-in env order is preserved for
// unoverridden keys, then user-supplied extra keys sorted alphabetically.
// Unknown job names are skipped.
func VaultJobs(vaultPath, slug, hebbBin, assetRoot, home string, port int, names []string, updateAuto bool, jobArgs map[string][]string, jobEnv map[string]map[string]string) []launchd.Job {
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
			// No script gate: `hebb digest` is built into the binary now (it selects
			// changed notes from the index and writes the digest in Go), so the job
			// needs only the hebb binary, never a materialised generator.
			var days []launchd.CalInterval
			for wd := 1; wd <= 5; wd++ {
				days = append(days, launchd.CalInterval{Weekday: wd, Hour: 8, Minute: 0})
			}
			jobs = append(jobs, launchd.Job{
				Label: label("daily-digest"),
				// Program[0] is the grantable hebb binary: macOS TCC attributes
				// file-access permission to Program[0], and a shell interpreter
				// (env/bash) has no grantable identity, so a wrapper's child reads into
				// a protected vault folder block indefinitely. Running `hebb digest`
				// makes Program[0] a binary the user can grant Full Disk Access to. No
				// PYTHON env: the digest is pure Go now, with no interpreter to find.
				Program:    []string{hebbBin, "digest", "--vault-root", vaultPath},
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
				Label:   label("action-review"),
				Program: []string{pythonPath(), script, "--vault-root", vaultPath},
				// HEBB_BIN is pinned so the script can invoke `hebb notify` after
				// writing its note: launchd's minimal PATH may not resolve the bare
				// "hebb" name. The script reads $HEBB_BIN (defaulting to "hebb" when
				// absent) and shells out non-destructively, so it works unchanged when
				// hebb or the notify config is absent.
				EnvVars:    []launchd.EnvVar{{Key: "HEBB_BIN", Value: hebbBin}},
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
			// Per-job extra env from config.toml's [job_env] is merged after built-in
			// env. A user-supplied key that matches a built-in key overrides it (user
			// wins). The result is deterministic: built-in keys in original order
			// (minus any overridden), then user-supplied extra keys sorted
			// alphabetically. A plist EnvironmentVariables dict cannot hold duplicate
			// keys, so we dedupe before building the slice.
			if extra := jobEnv[name]; len(extra) > 0 {
				job := &jobs[len(jobs)-1]
				job.EnvVars = mergeEnvVars(job.EnvVars, extra)
			}
		}
	}
	return jobs
}

// mergeEnvVars merges built-in env (a slice) with user-supplied env (a map).
// User-supplied keys override built-in keys of the same name (user wins). The
// result is deterministic: built-in keys that are not overridden appear first in
// their original order, followed by user-supplied extra keys (not present in
// built-in) sorted alphabetically. This guarantees idempotent plist output.
func mergeEnvVars(builtin []launchd.EnvVar, extra map[string]string) []launchd.EnvVar {
	// Build a set of overridden built-in keys.
	overrides := make(map[string]struct{}, len(extra))
	for k := range extra {
		overrides[k] = struct{}{}
	}

	// Collect built-in keys that are present in extra (for detecting new vs
	// override) and keep original keys that are not overridden.
	var result []launchd.EnvVar
	builtinKeys := make(map[string]struct{}, len(builtin))
	for _, e := range builtin {
		builtinKeys[e.Key] = struct{}{}
		if _, overridden := overrides[e.Key]; overridden {
			// Replace with the user value.
			result = append(result, launchd.EnvVar{Key: e.Key, Value: extra[e.Key]})
		} else {
			result = append(result, e)
		}
	}

	// Append truly new user keys (not present in built-in) sorted alphabetically
	// for determinism.
	var newKeys []string
	for k := range extra {
		if _, inBuiltin := builtinKeys[k]; !inBuiltin {
			newKeys = append(newKeys, k)
		}
	}
	sort.Strings(newKeys)
	for _, k := range newKeys {
		result = append(result, launchd.EnvVar{Key: k, Value: extra[k]})
	}
	return result
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
