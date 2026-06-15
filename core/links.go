package core

import (
	"database/sql"
	"strings"
)

// Resolution is the outcome of resolving a raw wiki-link target to a note path.
type Resolution int

const (
	// Dangling means no note matched the target (store NULL target_path).
	Dangling Resolution = iota
	// Resolved means exactly one note matched (store its path as target_path).
	Resolved
	// Ambiguous means more than one note matched (store NULL target_path, but
	// the ambiguity is a real data-quality signal Phase 1 will surface).
	Ambiguous
)

// noteIndex is an in-memory lookup over the notes table, built once so a whole
// reindex can resolve every link without a per-link query. The maps mirror the
// three match precedences in ResolveTarget: exact path, basename, and title.
type noteIndex struct {
	paths   map[string]bool     // exact note path (e.g. "sub/Note.md")
	byBase  map[string][]string // final segment without ".md" -> note paths
	byTitle map[string][]string // exact title -> note paths
}

// canonicalTarget strips any "#fragment" (everything from the first '#') and
// trims surrounding whitespace, yielding the text we match against notes. The
// alias (text after '|') is already dropped by the parser, so only the fragment
// remains to be removed here.
func canonicalTarget(raw string) string {
	t := raw
	if i := strings.IndexByte(t, '#'); i != -1 {
		t = t[:i]
	}
	return strings.TrimSpace(t)
}

// basenameNoExt returns the final path segment of a slash path with any ".md"
// suffix removed. Used for both note paths and link targets so a target like
// "sub/Note" matches a note at "sub/Note.md" or "other/Note.md" by basename.
func basenameNoExt(p string) string {
	if i := strings.LastIndexByte(p, '/'); i != -1 {
		p = p[i+1:]
	}
	return strings.TrimSuffix(p, ".md")
}

// newNoteIndex builds the in-memory lookup maps from a notes snapshot. paths is
// the full set of vault-relative note paths; titles maps each path to its
// stored title. Both come from a single scan of the notes table.
func newNoteIndex(paths []string, titles map[string]string) *noteIndex {
	ix := &noteIndex{
		paths:   make(map[string]bool, len(paths)),
		byBase:  make(map[string][]string, len(paths)),
		byTitle: make(map[string][]string, len(titles)),
	}
	for _, p := range paths {
		ix.paths[p] = true
		base := basenameNoExt(p)
		ix.byBase[base] = append(ix.byBase[base], p)
	}
	for p, title := range titles {
		if title != "" {
			ix.byTitle[title] = append(ix.byTitle[title], p)
		}
	}
	return ix
}

// resolve matches a raw target against the in-memory note index using exact
// precedence (no substring matching):
//
//	(a) exact path: a note whose path equals the target or target+".md";
//	(b) basename: a note whose final segment without ".md" equals the target's
//	    final segment, and when the target contains "/" the note path must end
//	    with target+".md" (so "sub/Note" cannot match "other/Note.md");
//	(c) exact title.
//
// The first precedence that yields candidates decides the outcome: exactly one
// candidate is Resolved (with its path), zero falls through to the next
// precedence, and more than one is Ambiguous. If no precedence matches, the
// target is Dangling.
func (ix *noteIndex) resolve(rawTarget string) (string, Resolution) {
	target := canonicalTarget(rawTarget)
	if target == "" {
		return "", Dangling
	}

	// (a) exact path match against target or target+".md".
	var pathHits []string
	if ix.paths[target] {
		pathHits = append(pathHits, target)
	}
	if withMD := target + ".md"; withMD != target && ix.paths[withMD] {
		pathHits = append(pathHits, withMD)
	}
	if r, ok := decide(pathHits); ok {
		return r, Resolved
	}

	// (b) basename match. When the target itself contains a slash, require the
	// note path to end with target+".md" so a subpath target stays anchored to
	// its directory and cannot collide with a same-named note elsewhere.
	base := basenameNoExt(target)
	var baseHits []string
	for _, p := range ix.byBase[base] {
		if strings.Contains(target, "/") {
			if strings.HasSuffix(p, "/"+target+".md") || p == target+".md" {
				baseHits = append(baseHits, p)
			}
			continue
		}
		baseHits = append(baseHits, p)
	}
	if r, ok := decide(baseHits); ok {
		return r, Resolved
	}
	if len(baseHits) > 1 {
		return "", Ambiguous
	}

	// (c) exact title match.
	titleHits := ix.byTitle[target]
	if r, ok := decide(titleHits); ok {
		return r, Resolved
	}
	if len(titleHits) > 1 {
		return "", Ambiguous
	}

	// A single path hit short-circuited above; reaching here means zero path
	// hits and zero/one basename or title hits already handled, so report the
	// remaining ambiguity from the path stage if any, else dangling.
	if len(pathHits) > 1 {
		return "", Ambiguous
	}
	return "", Dangling
}

// decide returns the single candidate when exactly one matched. ok is false for
// zero matches (fall through to the next precedence) and for more than one
// match (the caller reports Ambiguous).
func decide(hits []string) (string, bool) {
	if len(hits) == 1 {
		return hits[0], true
	}
	return "", false
}

// loadNoteIndex reads the full notes table into an in-memory index for batch
// resolution. Used by fullReindex once all notes are present.
func loadNoteIndex(q queryer) (*noteIndex, error) {
	rows, err := q.Query("SELECT path, title FROM notes")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var paths []string
	titles := map[string]string{}
	for rows.Next() {
		var p, t string
		if err := rows.Scan(&p, &t); err != nil {
			return nil, err
		}
		paths = append(paths, p)
		titles[p] = t
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return newNoteIndex(paths, titles), nil
}

// ResolveTargetDB resolves a single raw target against the live notes table. It
// is the per-call form used by IndexFile, where the notes table already holds
// the complete corpus, so a query per link is acceptable. It returns the
// resolved note path (empty for dangling or ambiguous) and the Resolution.
func ResolveTargetDB(q queryer, rawTarget string) (string, Resolution) {
	target := canonicalTarget(rawTarget)
	if target == "" {
		return "", Dangling
	}

	// (a) exact path.
	if hits := queryPaths(q, "SELECT path FROM notes WHERE path = ? OR path = ?", target, target+".md"); len(hits) > 0 {
		if len(hits) == 1 {
			return hits[0], Resolved
		}
		return "", Ambiguous
	}

	// (b) basename. Match by final segment, then anchor to the directory when
	// the target contains a slash.
	base := basenameNoExt(target)
	var baseHits []string
	for _, p := range queryPaths(q, "SELECT path FROM notes") {
		if basenameNoExt(p) != base {
			continue
		}
		if strings.Contains(target, "/") {
			if strings.HasSuffix(p, "/"+target+".md") || p == target+".md" {
				baseHits = append(baseHits, p)
			}
			continue
		}
		baseHits = append(baseHits, p)
	}
	if len(baseHits) == 1 {
		return baseHits[0], Resolved
	}
	if len(baseHits) > 1 {
		return "", Ambiguous
	}

	// (c) exact title.
	if hits := queryPaths(q, "SELECT path FROM notes WHERE title = ?", target); len(hits) > 0 {
		if len(hits) == 1 {
			return hits[0], Resolved
		}
		return "", Ambiguous
	}

	return "", Dangling
}

// txExecer is the read+write surface shared by *sql.Tx, used to resolve and
// update link target_path in bulk inside the reindex transaction.
type txExecer interface {
	Query(query string, args ...any) (*sql.Rows, error)
	Exec(query string, args ...any) (sql.Result, error)
}

// resolveLinkTargets resolves every distinct raw target to a canonical note
// path and writes it back to links.target_path. It builds one in-memory note
// index from the complete notes table, so it must be called only after all
// notes for the reindex are present. Each distinct target is resolved once and
// applied to every links row sharing it. A target that does not resolve to
// exactly one note (dangling or ambiguous) is set to NULL.
func resolveLinkTargets(tx txExecer) error {
	ix, err := loadNoteIndex(tx)
	if err != nil {
		return err
	}
	targets, err := distinctTargets(tx)
	if err != nil {
		return err
	}
	for _, raw := range targets {
		resolved, status := ix.resolve(raw)
		if status == Resolved {
			if _, err := tx.Exec("UPDATE links SET target_path = ? WHERE target = ?", resolved, raw); err != nil {
				return err
			}
			continue
		}
		// Dangling or ambiguous: NULL the column. Setting it explicitly (rather
		// than leaving a stale value) keeps a re-resolution after content
		// changes correct, e.g. when a previously matching note is removed.
		if _, err := tx.Exec("UPDATE links SET target_path = NULL WHERE target = ?", raw); err != nil {
			return err
		}
	}
	return nil
}

// distinctTargets returns every distinct raw target currently in the links
// table, so each is resolved once regardless of how many notes link to it.
func distinctTargets(q queryer) ([]string, error) {
	rows, err := q.Query("SELECT DISTINCT target FROM links")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// queryer is the read surface shared by *sql.DB and *sql.Tx, letting the
// resolver run against either a live connection or an open transaction.
type queryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

// queryPaths runs a path-returning query and collects the results. On error it
// returns nil so callers fall through to the next precedence rather than
// fabricating a match.
func queryPaths(q queryer, query string, args ...any) []string {
	rows, err := q.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if rows.Scan(&p) == nil {
			out = append(out, p)
		}
	}
	return out
}
