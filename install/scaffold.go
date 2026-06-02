package install

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Scaffold materialises the vault template tree (tmpl) into vaultPath, creating
// the directory if needed. It is defensive: it refuses to scaffold into a
// directory that already has contents, so it never clobbers an existing vault.
// An absent or empty target is fine. Returns a report of the action.
//
// tmpl is the template filesystem rooted at its contents (e.g. the embedded
// assets sub-FS for "vault-template", or os.DirFS of a checkout's
// vault-template/). The per-vault contracts (.hebb/config.toml, .mcp.json) are
// not part of the template; install writes those afterwards.
func Scaffold(tmpl fs.FS, vaultPath string) (Report, error) {
	var r Report
	if err := ensureScaffoldable(vaultPath); err != nil {
		return r, err
	}
	written := 0
	err := fs.WalkDir(tmpl, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == "." {
			return nil
		}
		target := filepath.Join(vaultPath, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, rerr := fs.ReadFile(tmpl, p)
		if rerr != nil {
			return rerr
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
		written++
		return nil
	})
	if err != nil {
		return r, err
	}
	r.add("scaffold", fmt.Sprintf("%d files from template", written))
	return r, nil
}

// ensureScaffoldable creates vaultPath if absent and verifies it is an empty
// directory, returning an error otherwise so an existing vault is never
// overwritten.
func ensureScaffoldable(vaultPath string) error {
	entries, err := os.ReadDir(vaultPath)
	if errors.Is(err, fs.ErrNotExist) {
		return os.MkdirAll(vaultPath, 0o755)
	}
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("refusing to scaffold into a non-empty directory: %s", vaultPath)
	}
	return nil
}
