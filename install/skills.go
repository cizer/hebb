package install

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
)

// hebb ships one set of agent skills (a SKILL.md per skill directory) and
// installs the same files into every place an agent looks for them: Claude
// Code's personal skills dir (~/.claude/skills) and Codex's skills dir
// (~/.agents/skills). The Claude Code plugin publishes the same skills via the
// marketplace, but that only reaches plugin-enabled Claude Code; installing into
// the skills dirs directly makes them work everywhere. InstallSkills is the one
// materialiser shared by every caller; only the target directory differs.

// ClaudeSkillsDir is Claude Code's user-global (personal) skills directory.
func ClaudeSkillsDir(home string) string {
	return filepath.Join(home, ".claude", "skills")
}

// CodexSkillsDir is Codex's user-global skills directory.
func CodexSkillsDir(home string) string {
	return filepath.Join(home, ".agents", "skills")
}

// InstallSkills materialises the bundled skills (skillsFS rooted at the skills
// parent, each immediate subdirectory a skill with a SKILL.md) into dir. hebb
// owns the skills it ships: it writes and updates their files and leaves any
// other skill already in dir untouched. Unchanged files are skipped, so it is
// idempotent and cheap to re-run (e.g. after `hebb update`). Returns the names
// of the skills delivered.
func InstallSkills(skillsFS fs.FS, dir string) ([]string, error) {
	entries, err := fs.ReadDir(skillsFS, ".")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub, err := fs.Sub(skillsFS, e.Name())
		if err != nil {
			return nil, err
		}
		if err := copyTree(sub, filepath.Join(dir, e.Name())); err != nil {
			return nil, err
		}
		names = append(names, e.Name())
	}
	return names, nil
}

// UpdateManagedSkills re-applies the bundled skills to dir, but only if hebb
// already manages skills there (at least one bundled skill is present). When the
// dir is managed it installs the full bundle, so a new skill in a release is
// deployed and changed skills are refreshed; when it is not (an agent the user
// doesn't use, or one they installed with --no-skills) it does nothing, so an
// upgrade never forces skills onto an opted-out dir. Returns the skills applied.
func UpdateManagedSkills(skillsFS fs.FS, dir string) ([]string, error) {
	entries, err := fs.ReadDir(skillsFS, ".")
	if err != nil {
		return nil, err
	}
	managed := false
	for _, e := range entries {
		if e.IsDir() {
			if _, err := os.Stat(filepath.Join(dir, e.Name())); err == nil {
				managed = true
				break
			}
		}
	}
	if !managed {
		return nil, nil
	}
	return InstallSkills(skillsFS, dir)
}

// copyTree writes every file in src under dst, creating directories as needed
// and skipping files whose content already matches.
func copyTree(src fs.FS, dst string) error {
	return fs.WalkDir(src, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dst, path)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(src, path)
		if err != nil {
			return err
		}
		if existing, err := os.ReadFile(target); err == nil && bytes.Equal(existing, data) {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
