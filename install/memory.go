package install

import (
	"os"
	"path/filepath"
	"strings"
)

// ClaudeProjectSlug reproduces Claude Code's per-project directory naming:
// every non-alphanumeric character in the absolute path becomes '-', with case
// preserved and no collapsing. So /Users/a.b/v becomes -Users-a-b-v.
func ClaudeProjectSlug(absPath string) string {
	var b strings.Builder
	b.Grow(len(absPath))
	for _, r := range absPath {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

// SymlinkMemory links <vault>/memory into
// <claudeProjectsDir>/<projectSlug>/memory so Claude Code reads the vault's
// (synced) memory when opened there. The vault memory dir is created if absent.
// It is defensive: an existing real memory dir at the target is left untouched
// (status "conflict").
func SymlinkMemory(vaultPath, claudeProjectsDir, projectSlug string) (string, error) {
	src := filepath.Join(vaultPath, "memory")
	if err := os.MkdirAll(src, 0o755); err != nil {
		return "", err
	}
	projDir := filepath.Join(claudeProjectsDir, projectSlug)
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		return "", err
	}
	return linkOne(src, filepath.Join(projDir, "memory"))
}
