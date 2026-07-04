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

// maxIndexFileBytes caps the size of a markdown file the indexer will read.
// This is a DoS guard: without it, a single huge file dropped into the vault
// (accidentally or otherwise) would be read fully into memory and hashed on
// every reindex/watch event, and its size is checked via os.Stat before any
// read is attempted. There is no config knob; a genuine note this large is not
// a realistic case the indexer needs to support.
const maxIndexFileBytes = 5 << 20 // 5 MiB

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

	nowMs := float64(time.Now().UnixNano()) / 1e6

	tx, err := db.Begin()
	if err != nil {
		return IndexResult{}, err
	}
	defer tx.Rollback()

	upsert, err := tx.Prepare(noteUpsertSQL)
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
		// DoS guard: a file over maxIndexFileBytes is skipped without being read.
		// It is treated like an unreadable file for indexing purposes, but unlike
		// one it must not be silently dropped from the index if a smaller version
		// was indexed previously, so it is removed from existing here to protect
		// any prior entry from the removal pass below.
		if info.Size() > maxIndexFileBytes {
			delete(existing, rel)
			// Poison the stored mtime so the fast path above can never skip this
			// file on a later run: if it shrinks back under the cap with its
			// original mtime restored (backup/restore tools preserve mtimes), the
			// stale pre-oversize entry must be re-read, not mtime-skipped forever.
			if _, known := stored[rel]; known {
				if _, err := tx.Exec(`UPDATE notes SET mtime = -1 WHERE path = ?`, rel); err != nil {
					return IndexResult{}, err
				}
			}
			continue
		}
		content, rerr := readFile(full)
		if rerr != nil {
			continue
		}
		n := ParseNote(string(content), rel)
		fmJSON, _ := json.Marshal(n.Frontmatter)
		tags := strings.Join(n.Tags, " ")
		hash := contentHash(n.Title, n.Body, tags, string(fmJSON))
		seed := changedAtSeed(mtime, nowMs)
		if _, err := upsert.Exec(rel, n.Title, n.Body, tags, string(fmJSON), mtime, hash, seed, seed); err != nil {
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

	// Pass 2: resolve every link's target_path now that all notes for this
	// reindex are present in the transaction. A linked-to note may have been
	// parsed after the file that links to it, so resolution must happen after
	// the whole notes set is written, not per file in the loop above. The
	// in-memory index is built once from the complete notes table and reused
	// for every link. NULL target_path means dangling or ambiguous.
	if err := resolveLinkTargets(tx); err != nil {
		return IndexResult{}, err
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
