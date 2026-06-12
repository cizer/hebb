package core

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// VaultConfig is the committed, per-vault contract stored at
// <vault>/.hebb/config.toml. It self-identifies a vault and configures the
// parts of hebb that vary per vault (excludes, web port, enabled jobs).
// Skills are not configured here: install delivers the full bundle to the
// user-global skills dirs, shared across vaults.
type VaultConfig struct {
	Name        string       `toml:"name"`
	ExcludeDirs []string     `toml:"exclude_dirs"`
	WebPort     int          `toml:"web_port"`
	Jobs        []string     `toml:"jobs"`
	JobArgs     JobArgs      `toml:"job_args"`
	Git         GitConfig    `toml:"git"`
	Update      UpdateConfig `toml:"update"`
	Index       IndexConfig  `toml:"index"`
	Ingest      IngestConfig `toml:"ingest"`
}

// IngestConfig is the committed [ingest] block. It records ingest policy that
// must travel with the vault, not live in per-user agent memory, so a cloned
// or second-machine vault inherits the same behaviour.
//
// Stage is the automation trust level: 1 (approve every write) through 4
// (headless; not yet supported). The accessor GetStage() defaults to 1 when the
// section is absent (Stage = 0) and clamps any below-range value to 1.
//
// ScratchDirs is a list of vault-root-relative directory path prefixes matched
// case-sensitively against a note's vault-relative path. Notes under these
// paths are indexed and searchable as normal, but ingest skills never treat
// them as ingest sources. This is distinct from exclude_dirs: exclude_dirs
// removes a directory from the index walk entirely (notes become invisible to
// search), while scratch_dirs keeps notes searchable but marks them off-limits
// as ingest sources. Use scratch_dirs for transient pads (daily scratch, paste
// staging) that hold real-looking content you do not want filed automatically.
type IngestConfig struct {
	Stage       int      `toml:"stage"`
	ScratchDirs []string `toml:"scratch_dirs"`
}

// GetStage returns the resolved ingest stage, defaulting to 1 when Stage is 0
// (section absent or not set) and clamping any below-range value to 1.
func (ic IngestConfig) GetStage() int {
	if ic.Stage < 1 {
		return 1
	}
	return ic.Stage
}

// IndexConfig is the committed [index] block. auto_refresh governs only the
// read-time staleness pass (RefreshChanged on a search, context or stats read):
// on by default, an explicit false leaves reads to the watcher alone. It does
// not affect watcher health reporting, which is unconditional. AutoRefresh is a
// pointer so an unset value defaults to on while an explicit false turns it off,
// mirroring the GitConfig pattern.
type IndexConfig struct {
	AutoRefresh *bool `toml:"auto_refresh"` // default true
}

// AutoRefreshEnabled reports whether read-time RefreshChanged should run (on by
// default; only an explicit auto_refresh = false disables it).
func (i IndexConfig) AutoRefreshEnabled() bool {
	return i.AutoRefresh == nil || *i.AutoRefresh
}

// JobArgs is the committed [job_args] block: extra command-line arguments
// appended to a job's rendered launchd program, keyed by job name. Entries for
// job names not listed under jobs (or unknown to hebb) are ignored, e.g.
//
//	[job_args]
//	action-review = ["--owner", "Alex Doe"]
type JobArgs map[string][]string

// UpdateConfig is the committed [update] block. The scheduled update-check job
// reports a newer release by default; with auto = true it installs it (opt-in,
// since self-replacing a binary unattended is a deliberate choice).
type UpdateConfig struct {
	Auto bool `toml:"auto"`
}

// GitConfig is the committed [git] block. Git mode keeps the vault's markdown in
// sync with a remote (pull before work, commit + push after). It is off unless
// enabled is true. AutoPull/AutoPush are pointers so an unset value defaults to
// on (only meaningful when enabled), while an explicit false turns that half
// off.
type GitConfig struct {
	Enabled         bool   `toml:"enabled"`
	AutoPull        *bool  `toml:"auto_pull"`        // default true when enabled
	AutoPush        *bool  `toml:"auto_push"`        // default true when enabled
	DebounceSeconds int    `toml:"debounce_seconds"` // watcher quiet-period before a sync; default 10
	CommitMessage   string `toml:"commit_message"`   // default "hebb: sync vault"
}

const defaultGitDebounceSeconds = 10

// PullEnabled reports whether git mode should pull (on by default when enabled).
func (g GitConfig) PullEnabled() bool {
	return g.Enabled && (g.AutoPull == nil || *g.AutoPull)
}

// PushEnabled reports whether git mode should commit+push (on by default when enabled).
func (g GitConfig) PushEnabled() bool {
	return g.Enabled && (g.AutoPush == nil || *g.AutoPush)
}

// Debounce is the resolved quiet-period in seconds before the watcher syncs.
func (g GitConfig) Debounce() int {
	if g.DebounceSeconds > 0 {
		return g.DebounceSeconds
	}
	return defaultGitDebounceSeconds
}

// Message is the resolved auto-commit message.
func (g GitConfig) Message() string {
	if g.CommitMessage != "" {
		return g.CommitMessage
	}
	return defaultCommitMessage
}

// DefaultVaultConfig returns the baseline config for a vault of the given name.
func DefaultVaultConfig(name string) VaultConfig {
	return VaultConfig{
		Name:        name,
		ExcludeDirs: append([]string(nil), defaultExcludeDirs...),
		WebPort:     defaultWebPort,
		Jobs:        []string{"daily-digest", "action-review", "web", "update-check"},
	}
}

const defaultWebPort = 4321

// vaultConfigPath is the canonical location of the committed config file.
func vaultConfigPath(vaultPath string) string {
	return filepath.Join(vaultPath, ".hebb", "config.toml")
}

// LoadVaultConfig reads <vault>/.hebb/config.toml. The boolean reports whether
// the file existed: when it does not, defaults named after the vault directory
// are returned so callers always receive a usable config. A malformed file is
// an error.
func LoadVaultConfig(vaultPath string) (VaultConfig, bool, error) {
	path := vaultConfigPath(vaultPath)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return DefaultVaultConfig(filepath.Base(vaultPath)), false, nil
	}
	if err != nil {
		return VaultConfig{}, false, err
	}
	var vc VaultConfig
	if err := toml.Unmarshal(data, &vc); err != nil {
		return VaultConfig{}, false, fmt.Errorf("parse %s: %w", path, err)
	}
	return vc, true, nil
}

// Save writes the config to <vault>/.hebb/config.toml, creating .hebb/ if
// needed. It is idempotent.
func (vc VaultConfig) Save(vaultPath string) error {
	path := vaultConfigPath(vaultPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	buf.WriteString("# hebb vault config - committed; identifies and configures this vault.\n")
	buf.WriteString("# Generated by 'hebb install'. Safe to edit.\n")
	buf.WriteString("#\n")
	buf.WriteString("# Key reference:\n")
	buf.WriteString("#   name          - human name for this vault (used in launchd labels)\n")
	buf.WriteString("#   exclude_dirs  - directory names skipped entirely during the index walk:\n")
	buf.WriteString("#                   notes become invisible to search. Matched at any depth.\n")
	buf.WriteString("#   web_port      - port for 'hebb serve' (default 4321)\n")
	buf.WriteString("#   jobs          - launchd jobs to render (daily-digest, action-review, web, update-check)\n")
	buf.WriteString("#\n")
	buf.WriteString("#   [ingest]      - ingest policy; travels with the vault so all machines share it\n")
	buf.WriteString("#     stage       - automation trust level 1-3 (default 1: approve every write).\n")
	buf.WriteString("#                   Stage 4 (headless) is not yet supported.\n")
	buf.WriteString("#                   Stage changes are the user's call; never advance automatically.\n")
	buf.WriteString("#     scratch_dirs - vault-root-relative path prefixes, case-sensitive.\n")
	buf.WriteString("#                   Notes under these paths are indexed and searchable as normal,\n")
	buf.WriteString("#                   but ingest skills never treat them as ingest sources.\n")
	buf.WriteString("#                   Distinct from exclude_dirs: scratch_dirs keeps notes visible\n")
	buf.WriteString("#                   in search, exclude_dirs removes them from the index entirely.\n")
	buf.WriteString("#                   Example: scratch_dirs = [\"Daily/Scratch\", \"Inbox/Staging\"]\n")
	buf.WriteString("#\n")
	buf.WriteString("#   [git]         - git-sync settings (enabled, auto_pull, auto_push, ...)\n")
	buf.WriteString("#   [update]      - auto-update settings (auto = false by default)\n")
	buf.WriteString("#   [index]       - index settings (auto_refresh = true by default)\n")
	buf.WriteString("\n")
	if err := toml.NewEncoder(&buf).Encode(vc); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
