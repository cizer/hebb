package core

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// IndexResult summarises a reindex.
type IndexResult struct {
	Indexed int
	Removed int
}

// FullReindex walks the vault, parses changed markdown files and upserts the
// index, removing entries whose files no longer exist on disk. Files whose
// on-disk mtime matches the stored mtime are skipped without being re-parsed,
// so a routine reindex over an unchanged vault is nearly free. Use
// FullReindexForce to re-parse every file regardless (the manual escape hatch
// for a suspected-corrupt index).
func FullReindex(cfg Config, db *sql.DB) (IndexResult, error) {
	return fullReindex(cfg, db, false)
}

// FullReindexForce re-parses every markdown file, ignoring stored mtimes. It is
// the manual escape hatch behind the reindex_vault tool and the web reindex
// endpoint: use it to rebuild from scratch when the index is suspected stale or
// corrupt, not on the hot read path.
func FullReindexForce(cfg Config, db *sql.DB) (IndexResult, error) {
	return fullReindex(cfg, db, true)
}

func fullReindex(cfg Config, db *sql.DB, force bool) (IndexResult, error) {
	files, err := enumerateMarkdown(cfg)
	if err != nil {
		return IndexResult{}, err
	}

	existing, err := existingPaths(db)
	if err != nil {
		return IndexResult{}, err
	}
	var stored map[string]float64
	if !force {
		if stored, err = indexedMtimes(db); err != nil {
			return IndexResult{}, err
		}
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
		info, serr := os.Stat(full)
		if serr != nil {
			continue
		}
		mtime := float64(info.ModTime().UnixNano()) / 1e6
		// Skip files whose on-disk mtime matches the stored mtime: nothing changed,
		// so there is nothing to re-parse. existing is left untouched so the file
		// is not treated as removed below.
		if !force {
			if prev, known := stored[rel]; known && prev == mtime {
				delete(existing, rel)
				continue
			}
		}
		content, rerr := readFile(full)
		if rerr != nil {
			continue
		}
		n := ParseNote(string(content), rel)
		fmJSON, _ := json.Marshal(n.Frontmatter)
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

	// Record the moment the index was last walked end-to-end. vault_stats and
	// doctor read this back to report freshness; it is written on every
	// FullReindex (changed-only or forced) because both fully reconcile the walk
	// against disk.
	if _, err := tx.Exec(
		`INSERT OR REPLACE INTO index_meta (key, value) VALUES ('last_full_reindex_at', ?)`,
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return IndexResult{}, err
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
