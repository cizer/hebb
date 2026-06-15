package core

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// TestMigrationBackfillsContentChangeColumns proves that opening a pre-feature
// index.db (notes without the content-change columns) adds them and seeds them
// from the stored content and mtime. Seeding content_changed_at from mtime
// rather than "now" is what stops the first digest after an upgrade from
// reporting the whole vault, and backfilling content_hash is what stops the
// first no-op rewrite after an upgrade from masquerading as a content change.
func TestMigrationBackfillsContentChangeColumns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")
	legacy, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	// Pre-feature schema: notes without the content columns, with the FTS table
	// and triggers in sync (as a real legacy index.db has them).
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
		t.Fatal(err)
	}
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
