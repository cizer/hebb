package install

import (
	"os"
	"path/filepath"
)

// SymlinkSkills links each immediate subdirectory of srcDir (each a skill) into
// dstDir (typically ~/.claude/skills), creating dstDir if needed. Non-directory
// entries in srcDir (e.g. README.md) are ignored.
//
// It is idempotent and defensive:
//   - missing target          -> create symlink   (symlinked)
//   - correct symlink present  -> leave as-is      (exists)
//   - symlink to elsewhere     -> repoint          (repointed)
//   - real file/dir present    -> leave untouched  (conflict)
//
// It never deletes a real (non-symlink) entry, so it is safe even if pointed at
// a directory that already holds hand-maintained skills.
func SymlinkSkills(srcDir, dstDir string) (Report, error) {
	var r Report
	srcAbs, err := filepath.Abs(srcDir)
	if err != nil {
		return r, err
	}
	entries, err := os.ReadDir(srcAbs)
	if err != nil {
		return r, err
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return r, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		want := filepath.Join(srcAbs, name)
		link := filepath.Join(dstDir, name)

		info, err := os.Lstat(link)
		switch {
		case os.IsNotExist(err):
			if err := os.Symlink(want, link); err != nil {
				return r, err
			}
			r.add(name, "symlinked")
		case err != nil:
			return r, err
		case info.Mode()&os.ModeSymlink != 0:
			if cur, _ := os.Readlink(link); cur == want {
				r.add(name, "exists")
				continue
			}
			if err := os.Remove(link); err != nil {
				return r, err
			}
			if err := os.Symlink(want, link); err != nil {
				return r, err
			}
			r.add(name, "repointed")
		default:
			r.add(name, "conflict")
		}
	}
	return r, nil
}
