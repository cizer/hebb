package install

import "os"

// linkOne ensures dst is a symlink pointing at src. It is idempotent and
// defensive — it never deletes a real (non-symlink) entry. Status is one of:
//
//	symlinked  created a new link
//	exists     correct link already present
//	repointed  a stale link was updated to src
//	conflict   a real file/dir is in the way; left untouched
func linkOne(src, dst string) (string, error) {
	info, err := os.Lstat(dst)
	switch {
	case os.IsNotExist(err):
		if err := os.Symlink(src, dst); err != nil {
			return "", err
		}
		return "symlinked", nil
	case err != nil:
		return "", err
	case info.Mode()&os.ModeSymlink != 0:
		if cur, _ := os.Readlink(dst); cur == src {
			return "exists", nil
		}
		if err := os.Remove(dst); err != nil {
			return "", err
		}
		if err := os.Symlink(src, dst); err != nil {
			return "", err
		}
		return "repointed", nil
	default:
		return "conflict", nil
	}
}
