package install

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/cizer/hebb/core"
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

// MemoryDir is the in-vault home for this vault's Claude Code memory. It lives
// under .hebb so agent memory still syncs and travels with the vault, but is
// hidden from Obsidian (a dotfolder). The index carves it out of the .hebb
// exclusion: memory is searchable under the ".hebb/memory/" path prefix, but it
// is not treated as vault content (no links, graph or audit; see
// core.MemoryDirRel).
func MemoryDir(vaultPath string) string {
	return filepath.Join(vaultPath, filepath.FromSlash(core.MemoryDirRel))
}

// SymlinkMemory links <vault>/.hebb/memory into
// <claudeProjectsDir>/<projectSlug>/memory so Claude Code reads the vault's
// (synced) memory when opened there. The vault memory dir is created if absent.
// It is defensive: an existing real memory dir at the target is left untouched
// (status "conflict").
func SymlinkMemory(vaultPath, claudeProjectsDir, projectSlug string) (string, error) {
	src := MemoryDir(vaultPath)
	if err := os.MkdirAll(src, 0o755); err != nil {
		return "", err
	}
	projDir := filepath.Join(claudeProjectsDir, projectSlug)
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		return "", err
	}
	return linkOne(src, filepath.Join(projDir, "memory"))
}
