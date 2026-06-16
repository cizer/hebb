package core

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// newLegacyNotesDB creates a pre-feature index.db at dbPath: a notes table
// without the content-change columns, plus the FTS table and triggers in sync
// (as a real legacy index.db has them, so a migration UPDATE on notes stays
// consistent). It returns the open handle for seeding rows.
func newLegacyNotesDB(t *testing.T, dbPath string) *sql.DB {
	t.Helper()
	legacy, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`
CREATE TABLE notes (path TEXT PRIMARY KEY, title TEXT NOT NULL, body TEXT NOT NULL, tags TEXT, frontmatter TEXT, mtime REAL NOT NULL);
CREATE TABLE links (source_path TEXT NOT NULL, target TEXT NOT NULL, PRIMARY KEY (source_path, target));
CREATE VIRTUAL TABLE notes_fts USING fts5(title, body, tags, content='notes', content_rowid='rowid', tokenize='porter unicode61');
CREATE TRIGGER notes_ai AFTER INSERT ON notes BEGIN
  INSERT INTO notes_fts(rowid, title, body, tags) VALUES (new.rowid, new.title, new.body, new.tags);
END;
CREATE TRIGGER notes_ad AFTER DELETE ON notes BEGIN
  INSERT INTO notes_fts(notes_fts, rowid, title, body, tags) VALUES ('delete', old.rowid, old.title, old.body, old.tags);
END;
CREATE TRIGGER notes_au AFTER UPDATE ON notes BEGIN
  INSERT INTO notes_fts(notes_fts, rowid, title, body, tags) VALUES ('delete', old.rowid, old.title, old.body, old.tags);
  INSERT INTO notes_fts(rowid, title, body, tags) VALUES (new.rowid, new.title, new.body, new.tags);
END;`); err != nil {
		legacy.Close()
		t.Fatal(err)
	}
	return legacy
}

// TestMigrationBackfillsContentChangeColumns proves that opening a pre-feature
// index.db (notes without the content-change columns) adds them and seeds them
// from the stored content and mtime. Seeding content_changed_at from mtime
// rather than "now" is what stops the first digest after an upgrade from
// reporting the whole vault, and backfilling content_hash is what stops the
// first no-op rewrite after an upgrade from masquerading as a content change.
func TestMigrationBackfillsContentChangeColumns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")
	legacy := newLegacyNotesDB(t, dbPath)
	// A past mtime (well before now) is seeded unchanged: the clamp only pulls
	// future mtimes back to the observation time.
	const mtime = 1700000000000.0
	if _, err := legacy.Exec(
		`INSERT INTO notes (path, title, body, tags, frontmatter, mtime) VALUES (?, ?, ?, ?, ?, ?)`,
		"Note.md", "Note", "the body", "tag1 tag2", "{}", mtime); err != nil {
		t.Fatal(err)
	}
	legacy.Close()

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB on legacy db: %v", err)
	}
	defer db.Close()

	var hash sql.NullString
	var cca, fia sql.NullFloat64
	if err := db.QueryRow(
		"SELECT content_hash, content_changed_at, first_indexed_at FROM notes WHERE path = 'Note.md'",
	).Scan(&hash, &cca, &fia); err != nil {
		t.Fatal(err)
	}
	if want := contentHash("Note", "the body", "tag1 tag2", "{}"); !hash.Valid || hash.String != want {
		t.Errorf("content_hash = %v, want %q", hash, want)
	}
	if !cca.Valid || cca.Float64 != mtime {
		t.Errorf("content_changed_at = %v, want the stored mtime %v (seeded, not now)", cca, mtime)
	}
	if !fia.Valid || fia.Float64 != mtime {
		t.Errorf("first_indexed_at = %v, want the stored mtime %v", fia, mtime)
	}

	// Reopening must not re-run the migration (the column already exists).
	db.Close()
	again, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer again.Close()
	var n int
	if err := again.QueryRow("SELECT COUNT(*) FROM notes WHERE content_hash IS NOT NULL").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("after reopen, backfilled rows = %d, want 1", n)
	}
}

// TestMigrationBackfillClampsFutureMtime guards the clamp on the upgrade path: a
// legacy row whose stored mtime is in the future (clock skew, a restore) must be
// seeded at the observation time, not the future mtime, so the first digest
// after the upgrade does not report it as changed in a window it has not reached
// and does not re-report it on every later run. This mirrors changedAtSeed on
// the index path.
func TestMigrationBackfillClampsFutureMtime(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")
	legacy := newLegacyNotesDB(t, dbPath)
	future := float64(time.Now().Add(72*time.Hour).UnixNano()) / 1e6
	if _, err := legacy.Exec(
		`INSERT INTO notes (path, title, body, tags, frontmatter, mtime) VALUES (?, ?, ?, ?, ?, ?)`,
		"Future.md", "Future", "body", "", "{}", future); err != nil {
		t.Fatal(err)
	}
	legacy.Close()

	before := float64(time.Now().UnixNano()) / 1e6
	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB on legacy db: %v", err)
	}
	defer db.Close()
	after := float64(time.Now().UnixNano()) / 1e6

	var cca, fia sql.NullFloat64
	if err := db.QueryRow(
		"SELECT content_changed_at, first_indexed_at FROM notes WHERE path = 'Future.md'",
	).Scan(&cca, &fia); err != nil {
		t.Fatal(err)
	}
	// The future mtime must be clamped to the observation time, not stored raw.
	if !cca.Valid || cca.Float64 >= future {
		t.Errorf("content_changed_at = %v, want clamped below the future mtime %v", cca, future)
	}
	if cca.Float64 < before || cca.Float64 > after {
		t.Errorf("content_changed_at = %v, want within the observation window [%v, %v]", cca.Float64, before, after)
	}
	if !fia.Valid || fia.Float64 != cca.Float64 {
		t.Errorf("first_indexed_at = %v, want the same clamped seed as content_changed_at %v", fia, cca)
	}
}
