package install

import (
	"path/filepath"
	"strings"
)

// defaultStableBinCandidates are the conventional stable symlink locations a
// Homebrew (or manual /usr/local) install exposes for the hebb binary. They are
// preferred over a versioned Cellar path so a launchd job's Program[0] survives
// an upgrade with its TCC grant intact.
func defaultStableBinCandidates() []string {
	return []string{"/opt/homebrew/bin/hebb", "/usr/local/bin/hebb"}
}

// StableHebbBin returns the binary path to embed in launchd jobs for the running
// hebb executable. When exePath resolves into a versioned Homebrew Cellar dir
// (so the path churns on every upgrade and the TCC Full Disk Access grant would
// reset), it prefers a stable symlink such as /opt/homebrew/bin/hebb that
// resolves to the same binary. Otherwise it returns exePath unchanged.
func StableHebbBin(exePath string) string {
	return StableBinPath(exePath, defaultStableBinCandidates())
}

// StableBinPath is the testable core of StableHebbBin: given the executable path
// and a list of candidate stable symlinks, it returns a candidate that resolves
// to the same binary as exePath when exePath sits under a versioned Cellar dir,
// else exePath. Only Cellar paths are rewritten, because only they churn across
// upgrades; a self-managed or go-install binary is left alone even if a symlink
// happens to point at it.
func StableBinPath(exePath string, candidates []string) string {
	if exePath == "" {
		return exePath
	}
	resolved := exePath
	if rp, err := filepath.EvalSymlinks(exePath); err == nil {
		resolved = rp
	}
	if !strings.Contains(resolved, "/Cellar/") {
		return exePath
	}
	for _, cand := range candidates {
		cr, err := filepath.EvalSymlinks(cand)
		if err != nil {
			continue
		}
		if cr == resolved {
			return cand
		}
	}
	return exePath
}
