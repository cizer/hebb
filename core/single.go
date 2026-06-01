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
	content, err := os.ReadFile(full)
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
		if _, err := db.Exec(`INSERT OR IGNORE INTO links (source_path, target) VALUES (?, ?)`, rel, l); err != nil {
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
