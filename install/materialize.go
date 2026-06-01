package install

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// MaterializeAssets writes the embedded function assets (fsys) onto disk under
// dataDir, preserving the tree. It is idempotent (unchanged files are skipped)
// and returns the number of files written or updated. Files under automation/
// are made executable, since go:embed does not preserve the executable bit.
func MaterializeAssets(fsys fs.FS, dataDir string) (int, error) {
	written := 0
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "." {
			return nil
		}
		target := filepath.Join(dataDir, path)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		if existing, err := os.ReadFile(target); err == nil && bytes.Equal(existing, data) {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if strings.HasPrefix(path, "automation/") {
			mode = 0o755
		}
		if err := os.WriteFile(target, data, mode); err != nil {
			return err
		}
		written++
		return nil
	})
	return written, err
}
