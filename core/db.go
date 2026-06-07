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
	return db, nil
}

// schemaSQL defines the index: external-content FTS5 kept in sync by triggers.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS notes (
  path TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  body TEXT NOT NULL,
  tags TEXT,
  frontmatter TEXT,
  mtime REAL NOT NULL
);
CREATE TABLE IF NOT EXISTS links (
  source_path TEXT NOT NULL,
  target TEXT NOT NULL,
  PRIMARY KEY (source_path, target)
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
