package core

import (
	"path/filepath"
	"strings"
)

// tccProtectedDirs are the home-relative folders macOS guards behind TCC
// (Transparency, Consent, and Control). A non-interactive launchd agent that
// reads files under these needs Full Disk Access, and because the ad-hoc-signed
// hebb binary's grant is keyed to its cdhash, every in-place update silently
// invalidates it (see NOTE-codesigning-fda-persistence.md). A vault kept outside
// these folders needs no grant at all, so the scheduled jobs never break on
// update. iCloud Drive is included: equally protected, and equally hostile to a
// git-synced vault.
var tccProtectedDirs = []string{
	"Documents",
	"Desktop",
	"Downloads",
	filepath.Join("Library", "Mobile Documents", "com~apple~CloudDocs"),
}

// ProtectedVaultRoot reports whether vaultPath sits inside a macOS TCC-protected
// location under home, returning the protected root when it does (e.g.
// "/Users/me/Documents"). The match is on the literal (un-resolved) absolute
// path on purpose: a vault referenced through a protected path still makes the
// launchd job's --vault-root protected even if it symlinks out. The check is
// pure path logic and OS-agnostic so it is unit-testable; callers gate on darwin
// since TCC only applies there. An empty home yields no match.
func ProtectedVaultRoot(vaultPath, home string) (string, bool) {
	if home == "" {
		return "", false
	}
	abs, err := filepath.Abs(vaultPath)
	if err != nil {
		return "", false
	}
	for _, d := range tccProtectedDirs {
		root := filepath.Join(home, d)
		if abs == root || strings.HasPrefix(abs, root+string(filepath.Separator)) {
			return root, true
		}
	}
	return "", false
}
