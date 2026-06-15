package core

import (
	"crypto/sha256"
	"encoding/hex"
)

// contentHash returns a stable hash of a note's indexed content. The inputs are
// exactly the strings stored in the notes columns (title, body, the
// space-joined tags, and the JSON-encoded frontmatter), so the same note always
// hashes the same whether the hash is computed from a freshly parsed note or
// read back from the index for a migration backfill.
//
// It is the digest's change signal, not a byte hash of the file: a note whose
// bytes were rewritten with no meaningful change (a sync client re-downloading
// an identical version, a `touch`, a whitespace-only reformat the parser
// normalises away) produces the same hash and so does not register as activity.
func contentHash(title, body, tags, frontmatter string) string {
	h := sha256.New()
	// Zero-byte separators so that moving text across fields cannot collide.
	h.Write([]byte(title))
	h.Write([]byte{0})
	h.Write([]byte(body))
	h.Write([]byte{0})
	h.Write([]byte(tags))
	h.Write([]byte{0})
	h.Write([]byte(frontmatter))
	return hex.EncodeToString(h.Sum(nil))
}

// noteUpsertSQL inserts or updates a note while maintaining the content-change
// columns. On a first insert content_changed_at and first_indexed_at are seeded
// from the caller-supplied value (the file's mtime, clamped to now). On a
// conflicting update, first_indexed_at is left untouched (the note is not new),
// and content_changed_at advances only when the content_hash actually changes:
// a row whose hash matches keeps its prior content_changed_at, so a bare mtime
// bump from a bulk operation does not move the digest's change signal.
const noteUpsertSQL = `INSERT INTO notes (path, title, body, tags, frontmatter, mtime, content_hash, content_changed_at, first_indexed_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(path) DO UPDATE SET
  title = excluded.title,
  body = excluded.body,
  tags = excluded.tags,
  frontmatter = excluded.frontmatter,
  mtime = excluded.mtime,
  content_changed_at = CASE
    WHEN notes.content_hash = excluded.content_hash THEN notes.content_changed_at
    ELSE excluded.content_changed_at
  END,
  content_hash = excluded.content_hash`

// changedAtSeed returns the content_changed_at / first_indexed_at value to store
// for a note observed now with the given on-disk mtime. It is the mtime, clamped
// so it can never exceed the observation time: a future or bulk-skewed mtime is
// pulled back to now so a note is never reported as "changed" in a window it has
// not yet reached, and so it is not re-reported on every later run.
func changedAtSeed(mtimeMs, nowMs float64) float64 {
	if mtimeMs > nowMs {
		return nowMs
	}
	return mtimeMs
}
