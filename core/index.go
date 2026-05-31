package core

import (
	"database/sql"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// IndexResult summarises a reindex.
type IndexResult struct {
	Indexed int
	Removed int
}

// FullReindex walks the vault, parses every markdown file and upserts the
// index, removing entries whose files no longer exist on disk.
func FullReindex(cfg Config, db *sql.DB) (IndexResult, error) {
	excluded := map[string]bool{}
	for _, d := range cfg.ExcludeDirs {
		excluded[d] = true
	}

	var files []string
	walkErr := filepath.WalkDir(cfg.VaultPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			if excluded[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".md") {
			if rel, rerr := filepath.Rel(cfg.VaultPath, p); rerr == nil {
				files = append(files, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	if walkErr != nil {
		return IndexResult{}, walkErr
	}

	existing, err := existingPaths(db)
	if err != nil {
		return IndexResult{}, err
	}

	tx, err := db.Begin()
	if err != nil {
		return IndexResult{}, err
	}
	defer tx.Rollback()

	upsert, err := tx.Prepare(`INSERT OR REPLACE INTO notes (path, title, body, tags, frontmatter, mtime) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return IndexResult{}, err
	}
	delLinks, err := tx.Prepare(`DELETE FROM links WHERE source_path = ?`)
	if err != nil {
		return IndexResult{}, err
	}
	insLink, err := tx.Prepare(`INSERT OR IGNORE INTO links (source_path, target) VALUES (?, ?)`)
	if err != nil {
		return IndexResult{}, err
	}

	indexed := 0
	for _, rel := range files {
		full := filepath.Join(cfg.VaultPath, filepath.FromSlash(rel))
		content, rerr := os.ReadFile(full)
		if rerr != nil {
			continue
		}
		info, serr := os.Stat(full)
		if serr != nil {
			continue
		}
		n := ParseNote(string(content), rel)
		fmJSON, _ := json.Marshal(n.Frontmatter)
		mtime := float64(info.ModTime().UnixNano()) / 1e6
		if _, err := upsert.Exec(rel, n.Title, n.Body, strings.Join(n.Tags, " "), string(fmJSON), mtime); err != nil {
			return IndexResult{}, err
		}
		if _, err := delLinks.Exec(rel); err != nil {
			return IndexResult{}, err
		}
		for _, l := range n.Links {
			if _, err := insLink.Exec(rel, l); err != nil {
				return IndexResult{}, err
			}
		}
		delete(existing, rel)
		indexed++
	}

	removed := 0
	for p := range existing {
		if _, err := tx.Exec("DELETE FROM notes WHERE path = ?", p); err != nil {
			return IndexResult{}, err
		}
		if _, err := tx.Exec("DELETE FROM links WHERE source_path = ?", p); err != nil {
			return IndexResult{}, err
		}
		removed++
	}

	if err := tx.Commit(); err != nil {
		return IndexResult{}, err
	}
	return IndexResult{Indexed: indexed, Removed: removed}, nil
}

func existingPaths(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query("SELECT path FROM notes")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out[p] = true
	}
	return out, rows.Err()
}
