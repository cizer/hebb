package core

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// IndexFile indexes or updates a single note by vault-relative path. If the
// file no longer exists it is removed from the index.
func IndexFile(cfg Config, db *sql.DB, rel string) error {
	full := filepath.Join(cfg.VaultPath, filepath.FromSlash(rel))
	// Never index a symlinked note: it could point outside the vault (e.g. at a
	// host secret), and following it would leak that content into the index.
	// Drop any prior entry so a file that becomes a symlink stops being served.
	if fi, err := os.Lstat(full); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return RemoveFile(db, rel)
	}
	content, err := readFile(full)
	if err != nil {
		return RemoveFile(db, rel)
	}
	info, err := os.Stat(full)
	if err != nil {
		return RemoveFile(db, rel)
	}
	n := ParseNote(string(content), rel)
	fmJSON, _ := json.Marshal(n.Frontmatter)
	mtime := float64(info.ModTime().UnixNano()) / 1e6
	if _, err := db.Exec(`INSERT OR REPLACE INTO notes (path, title, body, tags, frontmatter, mtime) VALUES (?, ?, ?, ?, ?, ?)`,
		rel, n.Title, n.Body, strings.Join(n.Tags, " "), string(fmJSON), mtime); err != nil {
		return err
	}
	if _, err := db.Exec(`DELETE FROM links WHERE source_path = ?`, rel); err != nil {
		return err
	}
	for _, l := range n.Links {
		// The notes table already holds the full corpus on this incremental
		// path, so each link is resolved against the live DB at write time.
		// target_path is the canonical note path, or NULL when the target is
		// dangling or ambiguous.
		resolved, status := ResolveTargetDB(db, l)
		var tp any
		if status == Resolved {
			tp = resolved
		}
		if _, err := db.Exec(`INSERT OR IGNORE INTO links (source_path, target, target_path) VALUES (?, ?, ?)`, rel, l, tp); err != nil {
			return err
		}
	}
	return nil
}

// RemoveFile drops a note and its links from the index.
func RemoveFile(db *sql.DB, rel string) error {
	if _, err := db.Exec("DELETE FROM notes WHERE path = ?", rel); err != nil {
		return err
	}
	_, err := db.Exec("DELETE FROM links WHERE source_path = ?", rel)
	return err
}
