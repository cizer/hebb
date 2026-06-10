package core

import (
	"fmt"
	"os"
	"path/filepath"
)

// Config locates a vault and its index database.
type Config struct {
	VaultPath   string
	DBPath      string
	ExcludeDirs []string
	Git         GitConfig // git-mode settings from the vault's [git] block
}

// defaultExcludeDirs are directory names skipped when walking a vault. These
// hold tool machinery (Obsidian/git/hebb state, agent config), not notes, so
// indexing their markdown would pollute search and context results.
var defaultExcludeDirs = []string{".obsidian", ".trash", ".hebb", ".git", ".claude"}

// ResolveVault determines the vault path (flag, then $HEBB_VAULT, then the
// nearest ancestor of the cwd containing .hebb/) and the index db path.
func ResolveVault(flagVault, flagDB string) (Config, error) {
	vault := flagVault
	if vault == "" {
		vault = os.Getenv("HEBB_VAULT")
	}
	if vault == "" {
		if cwd, err := os.Getwd(); err == nil {
			vault = findVaultUp(cwd)
		}
	}
	if vault == "" {
		return Config{}, fmt.Errorf("no vault found: pass --vault, set HEBB_VAULT, or run inside a vault (create one with 'hebb new')")
	}
	abs, err := filepath.Abs(vault)
	if err != nil {
		return Config{}, err
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return Config{}, fmt.Errorf("vault path is not a directory: %s", abs)
	}
	dbPath := flagDB
	if dbPath == "" {
		dbPath = filepath.Join(abs, ".hebb", "index.db")
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return Config{}, err
	}
	cfg := Config{VaultPath: abs, DBPath: dbPath, ExcludeDirs: defaultExcludeDirs}
	vc, existed, err := LoadVaultConfig(abs)
	if err != nil {
		return Config{}, err
	}
	if existed && len(vc.ExcludeDirs) > 0 {
		cfg.ExcludeDirs = vc.ExcludeDirs
	}
	if existed {
		cfg.Git = vc.Git
	}
	return cfg, nil
}

func findVaultUp(start string) string {
	dir := start
	for {
		if info, err := os.Stat(filepath.Join(dir, ".hebb")); err == nil && info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
