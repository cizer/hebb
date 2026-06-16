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
	JobEnv      JobEnv       `toml:"job_env"`
	Git         GitConfig    `toml:"git"`
	Update      UpdateConfig `toml:"update"`
	Index       IndexConfig  `toml:"index"`
	Ingest      IngestConfig `toml:"ingest"`
	Notify      NotifyConfig `toml:"notify"`
	Health      HealthConfig `toml:"health"`
}

// HealthConfig is the committed [health] block. It governs the thresholds used
// by the Phase 1 and Phase 2a vault-health detectors (hebb health). Zero values
// are replaced by sensible defaults via the accessor methods.
type HealthConfig struct {
	// ProjectStaleDays is the number of days without modification after which a
	// note under 1-Projects/ is flagged as a PARA-drift candidate. Default 180.
	ProjectStaleDays int `toml:"project_stale_days"`
	// SizeThreshold is the estimated token count (len(body)/4) above which a
	// note is a candidate for the oversized detector. Default 1200.
	SizeThreshold int `toml:"size_threshold"`

	// Phase 2a graph-health fields.

	// ConnectiveFolders is the set of vault-root-relative folder prefixes whose
	// notes are expected to be well-connected. Degree-0 (orphan) and degree-1
	// (leaf) notes in these folders that are also older than OrphanStaleDays are
	// flagged. Default: ["2-Areas", "3-Resources"].
	ConnectiveFolders []string `toml:"connective_folders"`
	// ExpectedOrphanFolders is the set of vault-root-relative folder prefixes
	// where sparse connectivity is normal (fresh capture, journals, archives).
	// Notes under these prefixes are never flagged as orphans or leaves.
	// Default: ["Journal", "Notes", "4-Archives"].
	ExpectedOrphanFolders []string `toml:"expected_orphan_folders"`
	// OrphanStaleDays is the minimum age in days before a degree-0 or degree-1
	// note in a connective folder is flagged. Fresh notes are not yet expected
	// to be linked. Default 90.
	OrphanStaleDays int `toml:"orphan_stale_days"`
	// IslandMaxSize is the maximum component size (inclusive) that is reported
	// as a small island finding, provided the island is not entirely under an
	// archive folder. Default 3.
	IslandMaxSize int `toml:"island_max_size"`
	// ArchiveFolders is the set of vault-root-relative folder prefixes that are
	// treated as archive storage. Small islands whose every member sits under an
	// archive folder are suppressed, because archived notes are intentionally
	// disconnected from the active vault. Default: ["4-Archives"]. This is
	// intentionally narrower than ExpectedOrphanFolders: Journal and Notes are
	// excluded from orphan/leaf checks but are NOT archive folders and so their
	// islands are still reported.
	ArchiveFolders []string `toml:"archive_folders"`

	// Dangling-link classification fields.

	// ReportUnresolvedLinks controls whether the dangling-link detector emits a
	// per-link finding for each unresolved wiki-link (a link to a note that does
	// not exist). Obsidian treats these as expected "unresolved links" (often an
	// intentional future note), not errors, so this is off by default: the
	// detector counts them but does not list them. The 'hebb health --unresolved'
	// flag forces it on for a single run. Default false.
	ReportUnresolvedLinks bool `toml:"report_unresolved_links"`
	// AttachmentExtensions is the set of file extensions (without the leading dot)
	// the dangling-link detector treats as attachment links rather than note
	// links, and so excludes entirely: hebb does not index non-note files and
	// cannot judge them broken. When empty the accessor returns the built-in
	// default list. Setting this replaces the default rather than extending it.
	AttachmentExtensions []string `toml:"attachment_extensions"`

	// ExcludeFromGraph is an optional list of glob patterns matched against each
	// note's title, basename-without-.md, and vault-relative path (any match
	// excludes the note). Matching uses path.Match semantics (shell-style globs
	// over the '/'-separated vault path; a malformed pattern fails the health run
	// rather than being silently ignored). Notes that match
	// are removed from the link graph BEFORE computing connected components,
	// k-core coreness, orphans, leaves, and islands, so a machine-generated hub
	// that would otherwise dominate those metrics is invisible to the graph
	// detectors. Content detectors (dangling_link, ambiguous_link, para_drift,
	// oversized) are unaffected: they still run over ALL notes, including excluded
	// ones. Default: empty (exclude nothing).
	ExcludeFromGraph []string `toml:"exclude_from_graph"`
}

// defaultAttachmentExtensions is the built-in set of file extensions the
// dangling-link detector treats as attachment links (not note links). These are
// the non-markdown file types Obsidian commonly embeds or links.
var defaultAttachmentExtensions = []string{
	"png", "jpg", "jpeg", "gif", "svg", "webp", "bmp",
	"pdf", "ppt", "pptx", "doc", "docx", "xls", "xlsx", "csv",
	"mp4", "mov", "webm", "mp3", "wav", "m4a", "vtt",
	"html", "htm", "zip", "excalidraw", "canvas",
}

// GetAttachmentExtensions returns the configured attachment extensions, or the
// built-in default list when none are set. A configured list replaces the
// default rather than extending it.
func (h HealthConfig) GetAttachmentExtensions() []string {
	if len(h.AttachmentExtensions) > 0 {
		return h.AttachmentExtensions
	}
	return append([]string(nil), defaultAttachmentExtensions...)
}

// GetProjectStaleDays returns the configured stale-days threshold, defaulting
// to 180 when the field is zero (section absent or not set).
func (h HealthConfig) GetProjectStaleDays() int {
	if h.ProjectStaleDays <= 0 {
		return 180
	}
	return h.ProjectStaleDays
}

// GetSizeThreshold returns the configured token-count threshold, defaulting to
// 1200 when the field is zero (section absent or not set).
func (h HealthConfig) GetSizeThreshold() int {
	if h.SizeThreshold <= 0 {
		return 1200
	}
	return h.SizeThreshold
}

// GetConnectiveFolders returns the configured connective-folder prefixes,
// defaulting to ["2-Areas", "3-Resources"] when the slice is empty.
func (h HealthConfig) GetConnectiveFolders() []string {
	if len(h.ConnectiveFolders) == 0 {
		return []string{"2-Areas", "3-Resources"}
	}
	return h.ConnectiveFolders
}

// GetExpectedOrphanFolders returns the configured expected-orphan folder
// prefixes, defaulting to ["Journal", "Notes", "4-Archives"] when the slice is
// empty.
func (h HealthConfig) GetExpectedOrphanFolders() []string {
	if len(h.ExpectedOrphanFolders) == 0 {
		return []string{"Journal", "Notes", "4-Archives"}
	}
	return h.ExpectedOrphanFolders
}

// GetOrphanStaleDays returns the configured minimum age threshold for orphan
// and leaf flagging, defaulting to 90 when the field is zero.
func (h HealthConfig) GetOrphanStaleDays() int {
	if h.OrphanStaleDays <= 0 {
		return 90
	}
	return h.OrphanStaleDays
}

// GetIslandMaxSize returns the configured maximum size of a small island that
// is reported as a finding, defaulting to 3 when the field is zero.
func (h HealthConfig) GetIslandMaxSize() int {
	if h.IslandMaxSize <= 0 {
		return 3
	}
	return h.IslandMaxSize
}

// GetArchiveFolders returns the configured archive folder prefixes, defaulting
// to ["4-Archives"] when the slice is empty. Only islands whose every member
// sits under an archive folder are suppressed; Journal and Notes are excluded
// from orphan/leaf checks via ExpectedOrphanFolders but are not archive
// folders, so their islands are reported.
func (h HealthConfig) GetArchiveFolders() []string {
	if len(h.ArchiveFolders) == 0 {
		return []string{"4-Archives"}
	}
	return h.ArchiveFolders
}

// GetExcludeFromGraph returns the configured list of glob patterns used to drop
// notes from the link graph before computing graph metrics (connected
// components, k-core coreness, orphans, leaves, islands). Returns an empty
// (non-nil) slice when the field is not set; the default behaviour is to
// exclude nothing.
func (h HealthConfig) GetExcludeFromGraph() []string {
	if h.ExcludeFromGraph == nil {
		return []string{}
	}
	return h.ExcludeFromGraph
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

// JobEnv is the committed [job_env] block: extra environment variables injected
// into a job's rendered launchd EnvironmentVariables, keyed by job name. Each
// value is a string-to-string map of env key/value pairs.
//
// A [job_env] key matching a built-in env key overrides it (user wins). Entries
// for job names not listed under jobs (or unknown to hebb) are ignored, e.g.
//
//	[job_env]
//	action-review = { HEBB_NOTIFY_URL = "https://hooks.example.com/abc" }
type JobEnv map[string]map[string]string

// UpdateConfig is the committed [update] block. The scheduled update-check job
// reports a newer release by default; with auto = true it installs it (opt-in,
// since self-replacing a binary unattended is a deliberate choice).
type UpdateConfig struct {
	Auto bool `toml:"auto"`
}

// NotifyConfig is the committed [notify] block. When enabled is true, hebb
// posts a short summary to a webhook URL after headless job runs (daily-digest,
// action-review, update-check) so output reaches the user without requiring an
// interactive session.
//
// Secret placement trade-off: config.toml is committed by design, so the URL
// is resolved from the environment first ($HEBB_NOTIFY_URL), then [notify] url.
// Committing the URL is the vault owner's call and is fine for a private vault;
// use $HEBB_NOTIFY_URL (injectable per job via [job_env]) to keep the URL out
// of the committed file when the vault is shared or public.
//
// The URL is never echoed to logs or standard output at any level of verbosity.
type NotifyConfig struct {
	Enabled bool   `toml:"enabled"`
	URL     string `toml:"url"`
}

// ResolveURL returns the webhook URL, preferring the $HEBB_NOTIFY_URL env var
// over the committed [notify] url. Returns "" when neither is set.
func (n NotifyConfig) ResolveURL() string {
	if v := resolveNotifyURLFromEnv(); v != "" {
		return v
	}
	return n.URL
}

// resolveNotifyURLFromEnv is split out so tests can avoid os.Getenv coupling.
func resolveNotifyURLFromEnv() string {
	return envGet("HEBB_NOTIFY_URL")
}

// envGet is a thin wrapper so the notify tests can swap it out without patching
// os.Getenv. The real implementation just calls os.Getenv.
var envGet = func(key string) string {
	return os.Getenv(key)
}

// GetEnvGet returns the current envGet function (for test save/restore).
func GetEnvGet() func(string) string { return envGet }

// SetEnvGet replaces the envGet function (for test injection only).
func SetEnvGet(f func(string) string) { envGet = f }

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
	buf.WriteString("#   [job_args]    - extra CLI arguments per job, appended to the rendered launchd\n")
	buf.WriteString("#                   program after built-in flags. Keys are job names.\n")
	buf.WriteString("#                   Example: action-review = [\"--owner\", \"Alex Doe\"]\n")
	buf.WriteString("#\n")
	buf.WriteString("#   [job_env]     - extra environment variables per job, injected into the\n")
	buf.WriteString("#                   rendered launchd EnvironmentVariables after built-in env.\n")
	buf.WriteString("#                   A key matching a built-in env key overrides it (user wins).\n")
	buf.WriteString("#                   Keys are job names; values are key=value string maps.\n")
	buf.WriteString("#                   The env var $HEBB_NOTIFY_URL (item 6) is the primary use:\n")
	buf.WriteString("#                   Example: action-review = { HEBB_NOTIFY_URL = \"https://hooks.example.com/abc\" }\n")
	buf.WriteString("#                   Committing the URL is the vault owner's call (fine for a\n")
	buf.WriteString("#                   private vault); use this env table to keep it out of the\n")
	buf.WriteString("#                   committed file when the vault is shared or public.\n")
	buf.WriteString("#\n")
	buf.WriteString("#   [notify]      - headless notification delivery via an incoming webhook\n")
	buf.WriteString("#     enabled     - set to true to enable (default false)\n")
	buf.WriteString("#     url         - webhook URL (POST application/json, body {\"text\": \"...\"})\n")
	buf.WriteString("#                   Resolution order: $HEBB_NOTIFY_URL env var first, then this\n")
	buf.WriteString("#                   field. Committing the URL here is fine for a private vault;\n")
	buf.WriteString("#                   use [job_env] to inject $HEBB_NOTIFY_URL per job to keep\n")
	buf.WriteString("#                   it out of the committed file when the vault is shared.\n")
	buf.WriteString("#                   The URL is never echoed to logs or standard output.\n")
	buf.WriteString("#\n")
	buf.WriteString("#   [git]         - git-sync settings (enabled, auto_pull, auto_push, ...)\n")
	buf.WriteString("#   [update]      - auto-update settings (auto = false by default)\n")
	buf.WriteString("#   [index]       - index settings (auto_refresh = true by default)\n")
	buf.WriteString("#\n")
	buf.WriteString("#   [health]      - vault-health detector thresholds (hebb health)\n")
	buf.WriteString("#     project_stale_days    - days without modification before a 1-Projects/ note\n")
	buf.WriteString("#                             is flagged as PARA drift (default 180)\n")
	buf.WriteString("#     size_threshold        - estimated token count (len(body)/4) above which a\n")
	buf.WriteString("#                             note is checked for multiple sections (default 1200)\n")
	buf.WriteString("#     connective_folders    - folder prefixes where sparse connectivity is flagged\n")
	buf.WriteString("#                             (default [\"2-Areas\", \"3-Resources\"])\n")
	buf.WriteString("#     expected_orphan_folders - folder prefixes where sparse connectivity is normal\n")
	buf.WriteString("#                             (default [\"Journal\", \"Notes\", \"4-Archives\"])\n")
	buf.WriteString("#     orphan_stale_days     - minimum note age in days before an orphan/leaf in a\n")
	buf.WriteString("#                             connective folder is flagged (default 90)\n")
	buf.WriteString("#     island_max_size       - maximum component size reported as a small island\n")
	buf.WriteString("#                             finding (default 3)\n")
	buf.WriteString("#     archive_folders       - folder prefixes whose islands are suppressed (default\n")
	buf.WriteString("#                             [\"4-Archives\"]); narrower than expected_orphan_folders:\n")
	buf.WriteString("#                             Journal/Notes orphans are exempt but their islands are\n")
	buf.WriteString("#                             still reported\n")
	buf.WriteString("#     report_unresolved_links - list each unresolved wiki-link (a link to a\n")
	buf.WriteString("#                             note that does not exist) as a dangling_link finding.\n")
	buf.WriteString("#                             Obsidian treats these as expected future notes, so\n")
	buf.WriteString("#                             they are counted but not listed by default (false).\n")
	buf.WriteString("#                             'hebb health --unresolved' forces listing for one run.\n")
	buf.WriteString("#     attachment_extensions - file extensions (no leading dot) treated as\n")
	buf.WriteString("#                             attachment links and excluded from dangling checks\n")
	buf.WriteString("#                             (hebb does not index non-note files). Empty uses the\n")
	buf.WriteString("#                             built-in default (png pdf pptx canvas excalidraw ...);\n")
	buf.WriteString("#                             setting it replaces the default rather than extending.\n")
	buf.WriteString("#     exclude_from_graph    - glob patterns matched against a note's title, basename\n")
	buf.WriteString("#                             without .md, and vault-relative path. A note is dropped\n")
	buf.WriteString("#                             from the link graph (and thus from coreness, components,\n")
	buf.WriteString("#                             orphan, leaf, and island metrics) when ANY pattern\n")
	buf.WriteString("#                             matches ANY of those three candidates via path.Match\n")
	buf.WriteString("#                             (a malformed glob fails the run, not silently ignored).\n")
	buf.WriteString("#                             Content detectors (dangling_link, oversized, ...) are\n")
	buf.WriteString("#                             unaffected and still run over ALL notes. Default: empty\n")
	buf.WriteString("#                             (exclude nothing). Use for machine-generated scaffolding\n")
	buf.WriteString("#                             that would otherwise dominate graph-centrality metrics.\n")
	buf.WriteString("#                             Example:\n")
	buf.WriteString("#                             exclude_from_graph = [\"Vault Daily Digest\", \"Ingest Log\",\n")
	buf.WriteString("#                                                   \"Action Review\", \"My Open Actions\",\n")
	buf.WriteString("#                                                   \"Open Actions*\"]\n")
	buf.WriteString("\n")
	if err := toml.NewEncoder(&buf).Encode(vc); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
