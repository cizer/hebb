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
	// Build the in-memory note index once from a single notes scan and reuse it
	// for this file's outgoing links and the inbound re-resolution below. This
	// replaces the previous per-link "SELECT path FROM notes" full scan, so a file
	// with k links costs one notes scan rather than k of them (review finding F).
	// The notes table already holds the full corpus on this incremental path
	// (the upsert above is included), so the index reflects the current graph.
	ix, err := loadNoteIndex(db)
	if err != nil {
		return err
	}
	for _, l := range n.Links {
		// target_path is the canonical note path, or NULL when the target is
		// dangling or ambiguous.
		resolved, status := ix.resolve(l)
		var tp any
		if status == Resolved {
			tp = resolved
		}
		if _, err := db.Exec(`INSERT OR IGNORE INTO links (source_path, target, target_path) VALUES (?, ?, ?)`, rel, l, tp); err != nil {
			return err
		}
	}
	// Re-resolve INBOUND links now that this note is present. Resolving this
	// note's own outgoing links (above) only fixes one direction; this lets the
	// incremental path self-correct so a link written before this note existed
	// now points at it, and a link whose target has just become ambiguous (a
	// second same-named note) flips back to NULL. fullReindex does this via a full
	// second pass; doing it here keeps the no-op read-time refresh free, because
	// RefreshChanged only calls IndexFile for files that actually changed.
	if err := reResolveInbound(db, ix, rel, n.Title); err != nil {
		return err
	}
	return nil
}

// RemoveFile drops a note and its links from the index, then re-resolves the
// links that pointed at the removed note so they fall back to dangling (NULL) or
// to another note that still matches, keeping the incremental path convergent
// with a full reindex (review finding C). The removed note's title is read
// before deletion so its keys can be computed.
func RemoveFile(db *sql.DB, rel string) error {
	var title string
	// A missing row (already absent) leaves title empty; the key-based and
	// target_path-based candidate selection below still corrects any link that
	// pointed at rel by path.
	_ = db.QueryRow("SELECT title FROM notes WHERE path = ?", rel).Scan(&title)
	if _, err := db.Exec("DELETE FROM notes WHERE path = ?", rel); err != nil {
		return err
	}
	if _, err := db.Exec("DELETE FROM links WHERE source_path = ?", rel); err != nil {
		return err
	}
	// Re-resolve against the corpus with this note gone so any link that resolved
	// to it (or whose keys matched it) is corrected rather than left stale.
	ix, err := loadNoteIndex(db)
	if err != nil {
		return err
	}
	return reResolveForRemovedNote(db, ix, rel, title)
}
