package core

import (
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// readFile is the content read used by the indexing paths (IndexFile,
// fullReindex). It is a package var only so tests can count reads and assert
// that a staleness pass over an unchanged vault opens no files beyond its
// stat() calls; production code never reassigns it.
var readFile = os.ReadFile

// enumerateMarkdown walks the vault and returns the vault-relative slash paths
// of every indexable markdown file. It is the single source of "which files
// count" so exclude_dirs skipping and the symlink filter apply identically to
// indexing, refresh and staleness detection. A second walk without these rules
// would index excluded content (defeating the privacy posture the symlink
// comment documents) or loop forever on a symlinked .md that IndexFile removes
// on every pass.
func enumerateMarkdown(cfg Config) ([]string, error) {
	excluded := map[string]bool{}
	for _, d := range cfg.ExcludeDirs {
		excluded[d] = true
	}
	var files []string
	err := filepath.WalkDir(cfg.VaultPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		// Never follow symlinks. WalkDir does not descend into symlinked dirs,
		// but a symlinked .md file would otherwise be read and indexed - a note
		// pointing at, say, ~/.ssh/id_rsa would pull host files into the
		// searchable index. This matters most for synced or shared vaults.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
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
	if err != nil {
		return nil, err
	}
	return files, nil
}

// indexedMtimes returns the stored mtime (UnixNano/1e6 milliseconds) for every
// indexed note, keyed by vault-relative path. It reads back the column the
// engine writes on every index, so a staleness pass can compare against disk
// without re-parsing.
func indexedMtimes(db *sql.DB) (map[string]float64, error) {
	rows, err := db.Query("SELECT path, mtime FROM notes")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]float64{}
	for rows.Next() {
		var p string
		var m float64
		if err := rows.Scan(&p, &m); err != nil {
			return nil, err
		}
		out[p] = m
	}
	return out, rows.Err()
}

// diskMtimeMillis returns a file's mtime in the same units stored in notes.mtime
// (UnixNano / 1e6). The bool is false if the file cannot be stat'd.
func diskMtimeMillis(full string) (float64, bool) {
	info, err := os.Stat(full)
	if err != nil {
		return 0, false
	}
	return float64(info.ModTime().UnixNano()) / 1e6, true
}

// NewestMarkdownMtime returns the most recent on-disk mtime (UnixNano/1e6
// milliseconds) across the vault's indexable markdown, using the same walk as
// indexing so excluded and symlinked files cannot inflate it. ok is false when
// the vault holds no indexable markdown. Doctor uses this to detect staleness
// without false positives from content the index would never hold.
func NewestMarkdownMtime(cfg Config) (newest float64, ok bool) {
	files, err := enumerateMarkdown(cfg)
	if err != nil {
		return 0, false
	}
	for _, rel := range files {
		full := filepath.Join(cfg.VaultPath, filepath.FromSlash(rel))
		if m, present := diskMtimeMillis(full); present && (!ok || m > newest) {
			newest, ok = m, true
		}
	}
	return newest, ok
}

// MaxIndexedMtime returns the largest stored notes.mtime. ok is false when the
// index is empty.
func MaxIndexedMtime(db *sql.DB) (max float64, ok bool) {
	var v sql.NullFloat64
	if err := db.QueryRow("SELECT MAX(mtime) FROM notes").Scan(&v); err != nil {
		return 0, false
	}
	return v.Float64, v.Valid
}

// RefreshChanged is a cheap staleness pass: it enumerates the vault (reusing the
// indexing walk so excluded and symlinked files are treated identically) and,
// comparing on-disk mtimes against the stored notes.mtime column, reindexes only
// changed or new files and removes deleted ones. Unchanged files are never
// re-parsed: the only filesystem reads beyond the directory walk are the stat()
// calls behind the mtime comparison. SetMaxOpenConns(1) serialises this with the
// watcher, so it must stay this cheap when called on every read.
func RefreshChanged(cfg Config, db *sql.DB) (IndexResult, error) {
	files, err := enumerateMarkdown(cfg)
	if err != nil {
		return IndexResult{}, err
	}
	stored, err := indexedMtimes(db)
	if err != nil {
		return IndexResult{}, err
	}

	indexed := 0
	for _, rel := range files {
		full := filepath.Join(cfg.VaultPath, filepath.FromSlash(rel))
		dm, ok := diskMtimeMillis(full)
		prev, known := stored[rel]
		delete(stored, rel)
		// Unchanged: present, stat'd, and the on-disk mtime matches. Skip without
		// reading or parsing the file.
		if known && ok && dm == prev {
			continue
		}
		if err := IndexFile(cfg, db, rel); err != nil {
			return IndexResult{}, err
		}
		indexed++
	}

	removed := 0
	for rel := range stored {
		if err := RemoveFile(db, rel); err != nil {
			return IndexResult{}, err
		}
		removed++
	}
	return IndexResult{Indexed: indexed, Removed: removed}, nil
}
