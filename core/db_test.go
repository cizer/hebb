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

// TestMigrationAddsAccessLogTable proves that opening a pre-Phase-3 index.db
// (no access_log table at all) gains it via the plain CREATE TABLE IF NOT
// EXISTS in schemaSQL, with no forced rebuild and no data loss for the
// pre-existing notes table (METABOLISM.md Phase 3).
func TestMigrationAddsAccessLogTable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")
	legacy := newLegacyNotesDB(t, dbPath)
	if _, err := legacy.Exec(
		`INSERT INTO notes (path, title, body, tags, frontmatter, mtime) VALUES (?, ?, ?, ?, ?, ?)`,
		"Note.md", "Note", "the body", "tag1 tag2", "{}", 1700000000000.0); err != nil {
		t.Fatal(err)
	}
	legacy.Close()

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB on legacy db: %v", err)
	}
	defer db.Close()

	var name string
	if err := db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'access_log'",
	).Scan(&name); err != nil {
		t.Fatalf("access_log table missing after migration: %v", err)
	}

	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM notes").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("pre-existing notes row lost across migration: count = %d, want 1", n)
	}

	var logged int
	if err := db.QueryRow("SELECT COUNT(*) FROM access_log").Scan(&logged); err != nil {
		t.Fatal(err)
	}
	if logged != 0 {
		t.Errorf("access_log should start empty on migration, got %d rows", logged)
	}
}

// TestLogReturnsAndFollowup exercises the write path directly: LogReturns
// bumps return_count/access_count/last_accessed for every path, a second call
// increments rather than overwrites, and LogFollowup bumps followup_count
// (not return_count) for the resolved seed of an expand_context call.
func TestLogReturnsAndFollowup(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")
	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	before := float64(time.Now().UnixNano()) / 1e6
	LogReturns(db, []string{"A.md", "B.md"})
	after := float64(time.Now().UnixNano()) / 1e6

	var accessCount, returnCount, followupCount int
	var lastAccessed float64
	if err := db.QueryRow(
		"SELECT access_count, return_count, followup_count, last_accessed FROM access_log WHERE path = 'A.md'",
	).Scan(&accessCount, &returnCount, &followupCount, &lastAccessed); err != nil {
		t.Fatal(err)
	}
	if accessCount != 1 || returnCount != 1 || followupCount != 0 {
		t.Errorf("after first LogReturns: access=%d return=%d followup=%d, want 1/1/0", accessCount, returnCount, followupCount)
	}
	if lastAccessed < before || lastAccessed > after {
		t.Errorf("last_accessed = %v, want within [%v, %v]", lastAccessed, before, after)
	}

	// A second call increments rather than overwrites.
	LogReturns(db, []string{"A.md"})
	if err := db.QueryRow(
		"SELECT access_count, return_count FROM access_log WHERE path = 'A.md'",
	).Scan(&accessCount, &returnCount); err != nil {
		t.Fatal(err)
	}
	if accessCount != 2 || returnCount != 2 {
		t.Errorf("after second LogReturns: access=%d return=%d, want 2/2", accessCount, returnCount)
	}

	// LogFollowup bumps followup_count, not return_count, and shares the same
	// access_count/last_accessed bookkeeping.
	LogFollowup(db, "A.md")
	if err := db.QueryRow(
		"SELECT access_count, return_count, followup_count FROM access_log WHERE path = 'A.md'",
	).Scan(&accessCount, &returnCount, &followupCount); err != nil {
		t.Fatal(err)
	}
	if accessCount != 3 || returnCount != 2 || followupCount != 1 {
		t.Errorf("after LogFollowup: access=%d return=%d followup=%d, want 3/2/1", accessCount, returnCount, followupCount)
	}

	// B.md only ever got one LogReturns bump.
	if err := db.QueryRow(
		"SELECT access_count, return_count, followup_count FROM access_log WHERE path = 'B.md'",
	).Scan(&accessCount, &returnCount, &followupCount); err != nil {
		t.Fatal(err)
	}
	if accessCount != 1 || returnCount != 1 || followupCount != 0 {
		t.Errorf("B.md: access=%d return=%d followup=%d, want 1/1/0", accessCount, returnCount, followupCount)
	}

	// Empty/blank input is a no-op: no row, no error.
	LogReturns(db, nil)
	LogFollowup(db, "")
	var total int
	if err := db.QueryRow("SELECT COUNT(*) FROM access_log").Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Errorf("access_log row count = %d, want 2 (A.md, B.md only)", total)
	}
}

// TestLogEditAfterSurface verifies the edit-after-surface follow-up signal:
// it bumps followup_count only for a note already surfaced (an existing
// access_log row) within followupEditWindow, leaving access_count/return_count
// untouched; it is a no-op for a never-surfaced note and for a surface older
// than the window.
func TestLogEditAfterSurface(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Surfaced, then edited: followup_count bumps; return_count/access_count do not.
	LogReturns(db, []string{"A.md"})
	LogEditAfterSurface(db, "A.md")
	var access, ret, follow int
	if err := db.QueryRow(
		"SELECT access_count, return_count, followup_count FROM access_log WHERE path = 'A.md'",
	).Scan(&access, &ret, &follow); err != nil {
		t.Fatal(err)
	}
	if access != 1 || ret != 1 || follow != 1 {
		t.Errorf("edited surfaced note: access=%d return=%d followup=%d, want 1/1/1", access, ret, follow)
	}

	// Never surfaced: an edit is only a follow-up after a surface, so no row is created.
	LogEditAfterSurface(db, "Never.md")
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM access_log WHERE path = 'Never.md'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("never-surfaced note gained %d access_log rows, want 0", n)
	}

	// A surface older than the window does not count.
	LogReturns(db, []string{"Old.md"})
	stale := float64(time.Now().Add(-followupEditWindow-24*time.Hour).UnixNano()) / 1e6
	if _, err := db.Exec("UPDATE access_log SET last_accessed = ? WHERE path = 'Old.md'", stale); err != nil {
		t.Fatal(err)
	}
	LogEditAfterSurface(db, "Old.md")
	if err := db.QueryRow("SELECT followup_count FROM access_log WHERE path = 'Old.md'").Scan(&follow); err != nil {
		t.Fatal(err)
	}
	if follow != 0 {
		t.Errorf("stale-surface edit: followup=%d, want 0 (outside window)", follow)
	}

	// Empty path is a no-op.
	LogEditAfterSurface(db, "")
}

// TestLogEditAfterSurfaceCountsUseDays proves the follow-up signal counts
// distinct use-days, not raw edit events: repeated edits on the same day bump
// followup_count once, and a note whose last follow-up was an earlier day bumps
// again. This stops one iterative editing burst dominating the signal (the
// 2026-08-11 re-measure had a single note holding 58% of all follow-ups).
func TestLogEditAfterSurfaceCountsUseDays(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	LogReturns(db, []string{"A.md"})

	// An editing burst on one day counts once, not five times.
	for i := 0; i < 5; i++ {
		LogEditAfterSurface(db, "A.md")
	}
	var follow int
	var day string
	if err := db.QueryRow(
		"SELECT followup_count, COALESCE(last_followup_day, '') FROM access_log WHERE path = 'A.md'",
	).Scan(&follow, &day); err != nil {
		t.Fatal(err)
	}
	if follow != 1 {
		t.Errorf("five edits in one day: followup=%d, want 1 (one use-day)", follow)
	}
	if day != time.Now().Format("2006-01-02") {
		t.Errorf("last_followup_day = %q, want today", day)
	}

	// A follow-up recorded on an earlier day allows one more bump: a second
	// distinct use-day is genuine repeat use.
	if _, err := db.Exec("UPDATE access_log SET last_followup_day = '2000-01-01' WHERE path = 'A.md'"); err != nil {
		t.Fatal(err)
	}
	LogEditAfterSurface(db, "A.md")
	LogEditAfterSurface(db, "A.md") // same day again: still only one
	if err := db.QueryRow("SELECT followup_count FROM access_log WHERE path = 'A.md'").Scan(&follow); err != nil {
		t.Fatal(err)
	}
	if follow != 2 {
		t.Errorf("second use-day: followup=%d, want 2", follow)
	}
}

// TestMigrationAddsFollowupDayColumn verifies a pre-use-day access_log (no
// last_followup_day column) gains it on open, with existing rows left NULL so
// the next edit is still allowed to record one follow-up.
func TestMigrationAddsFollowupDayColumn(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")
	legacy, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`CREATE TABLE access_log (
		path TEXT PRIMARY KEY,
		last_accessed REAL NOT NULL,
		access_count INTEGER NOT NULL DEFAULT 0,
		return_count INTEGER NOT NULL DEFAULT 0,
		followup_count INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(
		`INSERT INTO access_log (path, last_accessed, access_count, return_count, followup_count)
		 VALUES ('Legacy.md', ?, 3, 3, 0)`,
		float64(time.Now().UnixNano())/1e6); err != nil {
		t.Fatal(err)
	}
	legacy.Close()

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB must migrate a pre-use-day access_log: %v", err)
	}
	defer db.Close()

	var day sql.NullString
	if err := db.QueryRow("SELECT last_followup_day FROM access_log WHERE path = 'Legacy.md'").Scan(&day); err != nil {
		t.Fatalf("last_followup_day missing after migration: %v", err)
	}
	if day.Valid {
		t.Errorf("legacy row last_followup_day = %q, want NULL", day.String)
	}

	// The migrated row still records a follow-up on its next edit.
	LogEditAfterSurface(db, "Legacy.md")
	var follow int
	if err := db.QueryRow("SELECT followup_count FROM access_log WHERE path = 'Legacy.md'").Scan(&follow); err != nil {
		t.Fatal(err)
	}
	if follow != 1 {
		t.Errorf("migrated row followup=%d, want 1", follow)
	}
}
