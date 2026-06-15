package core

import (
	"database/sql"

	_ "modernc.org/sqlite"
)

// OpenDB opens (creating if needed) the index database and ensures the schema.
func OpenDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // serialise watcher, search and reindex over one connection
	for _, pragma := range []string{"PRAGMA journal_mode=WAL", "PRAGMA synchronous=NORMAL", "PRAGMA busy_timeout=3000"} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, err
		}
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrateLinksTargetPath(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrateNotesContentChange(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// migrateLinksTargetPath adds the nullable links.target_path column (and its
// index) to an index.db created before Phase 0, so an existing index upgrades
// in place without a forced rebuild. New databases already get the column from
// schemaSQL, so this is a no-op for them. The column is the resolved canonical
// note path for a wiki-link (NULL when dangling or ambiguous).
func migrateLinksTargetPath(db *sql.DB) error {
	rows, err := db.Query("PRAGMA table_info(links)")
	if err != nil {
		return err
	}
	defer rows.Close()
	hasColumn := false
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == "target_path" {
			hasColumn = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !hasColumn {
		if _, err := db.Exec("ALTER TABLE links ADD COLUMN target_path TEXT"); err != nil {
			return err
		}
	}
	// Idempotent: safe to run whether the column was just added or already present.
	if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_links_target_path ON links(target_path)"); err != nil {
		return err
	}
	// One-time backfill on the upgrade path only. A legacy index.db (column newly
	// added) has every links row at NULL target_path, but an unchanged vault makes
	// RefreshChanged skip every file (mtimes match), so without this pass every
	// valid legacy link would be reported dangling and outgoing() (which joins on
	// target_path) would return nothing until a manual full reindex. Resolve every
	// existing link against the current notes/links tables now, using the same
	// logic as fullReindex's second pass. Gated strictly on the column having just
	// been added: a fresh DB already has the column (hasColumn true) and an empty
	// links table, so this never runs there, and it never runs on a later open.
	if !hasColumn {
		if err := resolveLinkTargets(db); err != nil {
			return err
		}
	}
	return nil
}

// migrateNotesContentChange adds the content-level change-detection columns to
// the notes table for an index.db created before this feature, so an existing
// index upgrades in place without a forced rebuild. New databases already get
// the columns from schemaSQL, so this is a no-op for them.
//
//   - content_hash: a stable hash of the note's indexed content (title, body,
//     tags, frontmatter). It changes only when the meaningful content changes,
//     so a note whose bytes were rewritten with no content change (a sync
//     re-download, a touch, a whitespace-only reformat the parser normalises)
//     keeps the same hash.
//   - content_changed_at: the millisecond timestamp (same units as mtime) at
//     which the index last observed this note's content_hash take its current
//     value. This is the digest's change signal, replacing st_mtime: a bulk
//     operation that bumps mtime without changing content does not move it.
//   - first_indexed_at: the timestamp the path first entered this index, used to
//     mark a note "new" vs "updated" in the digest.
//
// On the upgrade path the three columns are backfilled from the already-stored
// content: content_hash from the indexed columns, and both timestamps seeded
// from the stored mtime (the best available proxy for "last changed" before the
// index tracked it). Seeding from mtime rather than "now" is what stops the
// first digest after the upgrade from reporting the entire vault as activity.
func migrateNotesContentChange(db *sql.DB) error {
	rows, err := db.Query("PRAGMA table_info(notes)")
	if err != nil {
		return err
	}
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if cols["content_hash"] {
		return nil
	}
	for _, col := range []string{
		"ALTER TABLE notes ADD COLUMN content_hash TEXT",
		"ALTER TABLE notes ADD COLUMN content_changed_at REAL",
		"ALTER TABLE notes ADD COLUMN first_indexed_at REAL",
	} {
		if _, err := db.Exec(col); err != nil {
			return err
		}
	}
	// Backfill only when the legacy table actually has the source columns. A
	// standard pre-feature index.db always does; a non-standard or partially
	// repaired table does not, and there is nothing safe to seed from. Skipping
	// leaves content_hash NULL, which the digest treats as "no change observed
	// yet" (never reported) until the next index repopulates it, so the structural
	// migration succeeds without depending on a well-formed legacy body.
	for _, c := range []string{"body", "tags", "frontmatter", "mtime"} {
		if !cols[c] {
			return nil
		}
	}
	return backfillNotesContentHash(db)
}

// backfillNotesContentHash fills content_hash / content_changed_at /
// first_indexed_at for every legacy row that lacks them, computing the hash from
// the stored indexed columns and seeding both timestamps from the stored mtime.
// It runs once, on the migration that adds the columns.
func backfillNotesContentHash(db *sql.DB) error {
	rows, err := db.Query("SELECT path, title, body, tags, frontmatter, mtime FROM notes WHERE content_hash IS NULL")
	if err != nil {
		return err
	}
	type row struct {
		path  string
		hash  string
		mtime float64
	}
	var pending []row
	for rows.Next() {
		var path, title, body string
		var tags, frontmatter sql.NullString
		var mtime float64
		if err := rows.Scan(&path, &title, &body, &tags, &frontmatter, &mtime); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, row{
			path:  path,
			hash:  contentHash(title, body, tags.String, frontmatter.String),
			mtime: mtime,
		})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(pending) == 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare("UPDATE notes SET content_hash = ?, content_changed_at = ?, first_indexed_at = ? WHERE path = ?")
	if err != nil {
		return err
	}
	for _, r := range pending {
		if _, err := stmt.Exec(r.hash, r.mtime, r.mtime, r.path); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// schemaSQL defines the index: external-content FTS5 kept in sync by triggers.
// The links.target_path index is intentionally absent here and created by
// migrateLinksTargetPath instead: a legacy index.db predating Phase 0 has a
// links table without target_path, so CREATE TABLE IF NOT EXISTS is a no-op for
// it and the column is added by the migration. Creating the index here would
// reference a column that does not yet exist on such a database and fail before
// the migration runs.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS notes (
  path TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  body TEXT NOT NULL,
  tags TEXT,
  frontmatter TEXT,
  mtime REAL NOT NULL,
  content_hash TEXT,
  content_changed_at REAL,
  first_indexed_at REAL
);
CREATE TABLE IF NOT EXISTS links (
  source_path TEXT NOT NULL,
  target TEXT NOT NULL,
  target_path TEXT,
  PRIMARY KEY (source_path, target)
);
CREATE TABLE IF NOT EXISTS index_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE VIRTUAL TABLE IF NOT EXISTS notes_fts USING fts5(
  title, body, tags,
  content='notes', content_rowid='rowid',
  tokenize='porter unicode61'
);
CREATE TRIGGER IF NOT EXISTS notes_ai AFTER INSERT ON notes BEGIN
  INSERT INTO notes_fts(rowid, title, body, tags) VALUES (new.rowid, new.title, new.body, new.tags);
END;
CREATE TRIGGER IF NOT EXISTS notes_ad AFTER DELETE ON notes BEGIN
  INSERT INTO notes_fts(notes_fts, rowid, title, body, tags) VALUES ('delete', old.rowid, old.title, old.body, old.tags);
END;
CREATE TRIGGER IF NOT EXISTS notes_au AFTER UPDATE ON notes BEGIN
  INSERT INTO notes_fts(notes_fts, rowid, title, body, tags) VALUES ('delete', old.rowid, old.title, old.body, old.tags);
  INSERT INTO notes_fts(rowid, title, body, tags) VALUES (new.rowid, new.title, new.body, new.tags);
END;
CREATE INDEX IF NOT EXISTS idx_links_source ON links(source_path);
CREATE INDEX IF NOT EXISTS idx_links_target ON links(target);
`
