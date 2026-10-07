package core

import (
	"path/filepath"
	"strings"
)

// MemoryDirRel is the vault-relative home of the vault's agent memory: the
// Claude Code auto-memory files that 'hebb install' links into
// ~/.claude/projects/<slug>/memory. It lives under .hebb so it travels with the
// vault but stays hidden from Obsidian.
//
// Memory is indexed for search under this path prefix even though .hebb itself
// is excluded, so search_vault / hebb search with path prefix ".hebb/memory/"
// finds what the agent has learned. It is not part of the note graph: memory
// notes record no links, are never a link target or context seed, and are left
// out of the graph metrics and the audit. Adding "memory" to exclude_dirs stops
// indexing it.
const MemoryDirRel = ".hebb/memory"

// IsMemoryNote reports whether a vault-relative slash path is an agent memory
// note (anything under MemoryDirRel).
func IsMemoryNote(rel string) bool {
	return strings.HasPrefix(rel, MemoryDirRel+"/")
}

// MemoryIndexed reports whether agent memory is part of the index walk for the
// given exclude_dirs. It always is, unless "memory" itself is excluded: the
// ".hebb" exclusion deliberately does not reach it (see indexRoots).
func MemoryIndexed(excludeDirs []string) bool {
	for _, d := range excludeDirs {
		if d == filepath.Base(MemoryDirRel) {
			return false
		}
	}
	return true
}

// indexRoots returns the directories the index walk and the watcher start from:
// the vault itself, plus the agent memory dir when the vault walk would skip it
// because ".hebb" is excluded (the default). That carve-out is what keeps
// memory searchable while index.db, config.toml and any other markdown under
// .hebb stay out of the index. Only the vault's own .hebb/memory is added; a
// nested vault's .hebb is still skipped whole by the walk.
//
// The memory root is walked by the same rules as the vault: a symlinked memory
// dir is not followed, and an excluded "memory" name skips it entirely.
func indexRoots(cfg Config) []string {
	roots := []string{cfg.VaultPath}
	for _, d := range cfg.ExcludeDirs {
		if d == ".hebb" {
			return append(roots, filepath.Join(cfg.VaultPath, filepath.FromSlash(MemoryDirRel)))
		}
	}
	return roots
}
