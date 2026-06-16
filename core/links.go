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
//
// Matching is case-insensitive to mirror Obsidian, which resolves [[foo]] to a
// note filed as "Foo.md". Every map is therefore keyed by the lowercased form of
// the path/basename/title, while the stored values remain the notes' real
// on-disk paths so a resolution returns the canonical (original-case) path.
type noteIndex struct {
	paths   map[string][]string // lowercased note path -> real paths (case-only collisions keep every hit, e.g. "foo.md" -> ["Foo.md", "foo.md"])
	byBase  map[string][]string // lowercased final segment without ".md" -> note paths
	byTitle map[string][]string // lowercased title -> note paths
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
		paths:   make(map[string][]string, len(paths)),
		byBase:  make(map[string][]string, len(paths)),
		byTitle: make(map[string][]string, len(titles)),
	}
	for _, p := range paths {
		// Key by the lowercased path so an exact-path lookup is case-insensitive;
		// keep every real path as the value so a case-only collision (two notes
		// whose paths differ only in case on a case-sensitive FS) is preserved as
		// an ambiguous match rather than silently overwriting one with the other.
		lp := strings.ToLower(p)
		ix.paths[lp] = append(ix.paths[lp], p)
		base := strings.ToLower(basenameNoExt(p))
		ix.byBase[base] = append(ix.byBase[base], p)
	}
	for p, title := range titles {
		if title != "" {
			ix.byTitle[strings.ToLower(title)] = append(ix.byTitle[strings.ToLower(title)], p)
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
	// Lowercase the lookup target once: every map is keyed by lowercase, and the
	// directory-anchored suffix comparisons below run against this form so case
	// never decides a match (mirrors Obsidian's case-insensitive resolution).
	lower := strings.ToLower(target)
	// Strip a single trailing ".md" for the basename/anchoring comparisons so a
	// target that already carries the extension (e.g. "sub/note.md") is not given
	// a second one ("sub/note.md.md") that could never match a real path.
	lowerNoExt := strings.TrimSuffix(lower, ".md")

	// (a) exact path match against target or target+".md". Both forms are gathered
	// from the (possibly multi-valued) path index: a single distinct hit Resolves,
	// while two or more (a case-only path collision) are Ambiguous and must not
	// fall through to basename/title.
	pathHits := distinct(append(append([]string(nil), ix.paths[lower]...), ix.paths[lower+".md"]...))
	if len(pathHits) == 1 {
		return pathHits[0], Resolved
	}
	if len(pathHits) > 1 {
		return "", Ambiguous
	}

	// (b) basename match. When the target itself contains a slash, require the
	// note path to end with target+".md" so a subpath target stays anchored to
	// its directory and cannot collide with a same-named note elsewhere. The
	// suffix comparison is done on the lowercased, extension-stripped path so it
	// stays case-insensitive like the rest of the resolver and never compares
	// against a doubled ".md".
	base := strings.ToLower(basenameNoExt(target))
	var baseHits []string
	for _, p := range ix.byBase[base] {
		if strings.Contains(lower, "/") {
			lp := strings.ToLower(p)
			if strings.HasSuffix(lp, "/"+lowerNoExt+".md") || lp == lowerNoExt+".md" {
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
	titleHits := ix.byTitle[lower]
	if r, ok := decide(titleHits); ok {
		return r, Resolved
	}
	if len(titleHits) > 1 {
		return "", Ambiguous
	}

	// The exact-path stage already returned for one or more path hits, so reaching
	// here means no precedence yielded a single match: the target is dangling.
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

// distinct returns hits with duplicate real paths removed, preserving first-seen
// order. The exact-path stage unions the lookups for "target" and "target.md",
// which can surface the same on-disk path twice; collapsing them keeps a lone
// note from reading as an ambiguous match while still counting two genuinely
// different paths (a case-only collision) as two.
func distinct(hits []string) []string {
	if len(hits) <= 1 {
		return hits
	}
	seen := make(map[string]bool, len(hits))
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		if seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, h)
	}
	return out
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
// is the per-call form used for one-off resolutions where building a full
// in-memory index would be wasteful, such as resolvePath's seed lookup in the
// context walk (one call per expansion, not per link). The indexing paths
// instead build a noteIndex once and call its resolve method, so they do not pay
// this query's per-call basename full scan for every link. It returns the
// resolved note path (empty for dangling or ambiguous) and the Resolution.
func ResolveTargetDB(q queryer, rawTarget string) (string, Resolution) {
	target := canonicalTarget(rawTarget)
	if target == "" {
		return "", Dangling
	}
	// Lowercase the target for the Go-side basename comparison; the SQL stages use
	// COLLATE NOCASE so all three precedences match case-insensitively, mirroring
	// the in-memory resolver and Obsidian.
	lower := strings.ToLower(target)
	// Strip a single trailing ".md" for the basename/anchoring comparisons so a
	// target that already carries the extension (e.g. "sub/note.md") is not given
	// a second one ("sub/note.md.md") that could never match a real path.
	lowerNoExt := strings.TrimSuffix(lower, ".md")

	// (a) exact path against the raw target or target+".md". The collation is
	// attached to the column operand (path COLLATE NOCASE) so the case-insensitive
	// comparison applies regardless of the parameter's own collation, making a
	// case-mismatched full path such as "SUB/NOTE.md" match "sub/Note.md".
	if hits := queryPaths(q, "SELECT path FROM notes WHERE path COLLATE NOCASE = ? OR path COLLATE NOCASE = ?", target, target+".md"); len(hits) > 0 {
		if len(hits) == 1 {
			return hits[0], Resolved
		}
		return "", Ambiguous
	}

	// (b) basename. Match by final segment, then anchor to the directory when
	// the target contains a slash. All comparisons are lowercased and use the
	// extension-stripped target so case never decides the match and a target that
	// already ends in ".md" is not anchored against a doubled ".md".
	base := strings.ToLower(basenameNoExt(target))
	var baseHits []string
	for _, p := range queryPaths(q, "SELECT path FROM notes") {
		if strings.ToLower(basenameNoExt(p)) != base {
			continue
		}
		if strings.Contains(lower, "/") {
			lp := strings.ToLower(p)
			if strings.HasSuffix(lp, "/"+lowerNoExt+".md") || lp == lowerNoExt+".md" {
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

	// (c) exact title (case-insensitive via COLLATE NOCASE).
	if hits := queryPaths(q, "SELECT path FROM notes WHERE title = ? COLLATE NOCASE", target); len(hits) > 0 {
		if len(hits) == 1 {
			return hits[0], Resolved
		}
		return "", Ambiguous
	}

	return "", Dangling
}

// execer is the read+write surface shared by *sql.DB and *sql.Tx, used to
// resolve and update link target_path. fullReindex passes its *sql.Tx; the
// incremental path passes the live *sql.DB.
type execer interface {
	Query(query string, args ...any) (*sql.Rows, error)
	Exec(query string, args ...any) (sql.Result, error)
}

// noteKeys returns the set of raw targets that should resolve to the note at
// rel with the given title: its exact path, every directory-suffix form of the
// path without ".md", and the exact title (when non-empty). It mirrors the match
// precedences in resolve (path, basename, title) so inbound re-resolution
// considers exactly the links this note could now satisfy, without re-running the
// resolver over the whole links table.
//
// The directory-suffix forms matter because the basename stage of the resolver
// accepts a slash-bearing target whose note path ends with "/"+target+".md". For
// a note at "x/dir/Note.md" the resolver would match the targets "x/dir/Note",
// "dir/Note" and "Note", so all three must be keys here; emitting only the full
// path and the bare basename (as an earlier version did) left a "[[dir/Note]]"
// link unresolved on the incremental path even though fullReindex resolved it.
func noteKeys(rel, title string) []string {
	keys := map[string]bool{rel: true}
	// Every directory-suffix of the path without ".md": "x/dir/Note", "dir/Note",
	// "Note". Walk the slash boundaries from the front, plus the bare basename.
	noExt := strings.TrimSuffix(rel, ".md")
	keys[noExt] = true
	for i := 0; i < len(noExt); i++ {
		if noExt[i] == '/' {
			keys[noExt[i+1:]] = true
		}
	}
	if title != "" {
		keys[title] = true
	}
	out := make([]string, 0, len(keys))
	for k := range keys {
		out = append(out, k)
	}
	return out
}

// reResolveForKeys re-resolves every link whose raw target matches one of the
// supplied note keys, regardless of its current target_path, and rewrites that
// target_path to the resolver's verdict against the supplied in-memory index. It
// is the incremental counterpart to fullReindex's full second pass: when the note
// graph changes, only the links that could match the changed note are revisited,
// so the work is proportional to the relevant links rather than the whole table,
// and the result converges to the same state a full reindex would produce.
//
// Candidate selection happens in SQL (target = ? OR target LIKE ?#... for the
// fragment forms of each key) so the read hot path does not scan every dangling
// target. Resolving against ix (built once per IndexFile invocation from one
// notes scan) avoids the previous per-link "SELECT path FROM notes" full scan.
//
// All matching rows are reconsidered, not only the NULL ones, so a target that
// has become ambiguous (a second same-named note appeared) flips from a stale
// non-NULL pointer back to NULL, and a target whose note was removed falls back
// to dangling or to another note. A still-unresolved target is set to NULL
// explicitly rather than left stale.
func reResolveForKeys(db execer, ix *noteIndex, keys []string) error {
	candidates, err := candidateTargets(db, keys)
	if err != nil {
		return err
	}
	for _, raw := range candidates {
		resolved, status := ix.resolve(raw)
		var tp any
		if status == Resolved {
			tp = resolved
		}
		if _, err := db.Exec("UPDATE links SET target_path = ? WHERE target = ?", tp, raw); err != nil {
			return err
		}
	}
	return nil
}

// candidateTargets returns the distinct raw targets in the links table that
// match any of keys, either exactly or as a "key#fragment" form (the fragment is
// stripped before matching by the resolver, so a "[[Note#Section]]" link must be
// considered a candidate for the note keyed "Note"). Selection is done in SQL so
// the caller never scans the whole links table in Go.
func candidateTargets(q queryer, keys []string) ([]string, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	// Build "target = ? COLLATE NOCASE OR target LIKE ? ESCAPE '\'" pairs per key:
	// the exact form and the "key#..." fragment form. Matching is case-insensitive
	// (COLLATE NOCASE on the exact form, a NOCASE LIKE pattern on the fragment
	// form) so a case-mismatched inbound link, e.g. an existing [[foo]] when
	// "Foo.md" is indexed, is re-resolved on the incremental path too, keeping it
	// convergent with a full reindex. LIKE wildcards in the key are escaped so a
	// title containing '%' or '_' cannot widen the match.
	var clauses []string
	var args []any
	for _, k := range keys {
		clauses = append(clauses, "target = ? COLLATE NOCASE", `target LIKE ? ESCAPE '\'`)
		args = append(args, k, escapeLike(k)+`#%`)
	}
	query := "SELECT DISTINCT target FROM links WHERE " + strings.Join(clauses, " OR ")
	rows, err := q.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		// A canonicalised "key#fragment" target must canonicalise back to one of
		// the keys; the LIKE pattern alone could admit "Other#..." when a key is a
		// prefix-free string, so confirm exactly.
		if matchesKey(raw, keys) {
			out = append(out, raw)
		}
	}
	return out, rows.Err()
}

// matchesKey reports whether raw, once its fragment is stripped, equals one of
// the keys (case-insensitively, mirroring the resolver). It is the Go-side
// confirmation of the SQL candidate filter.
func matchesKey(raw string, keys []string) bool {
	canon := strings.ToLower(canonicalTarget(raw))
	for _, k := range keys {
		if canon == strings.ToLower(k) {
			return true
		}
	}
	return false
}

// escapeLike escapes the LIKE wildcards ('%', '_') and the escape character
// itself in s so it can be embedded literally in a LIKE pattern with
// "ESCAPE '\'".
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// reResolveInbound re-resolves the links that could point at the just-indexed
// note (rel, title) against ix, so an inbound link written before this note
// existed now resolves to it, and a link that has become ambiguous flips back to
// NULL. It is called from IndexFile after the note is upserted.
func reResolveInbound(db execer, ix *noteIndex, rel, title string) error {
	return reResolveForKeys(db, ix, noteKeys(rel, title))
}

// reResolveForRemovedNote re-resolves the links that pointed at, or could have
// matched, a note being removed at rel (with title). Any link whose target_path
// equalled the removed path, and any link whose target matches the removed note's
// keys, is resolved afresh against ix (which no longer contains the removed
// note), so it falls back to dangling (NULL) or to another note that still
// matches. This keeps the incremental path convergent with a full reindex when a
// target disappears, instead of leaving a stale non-NULL pointer.
func reResolveForRemovedNote(db execer, ix *noteIndex, rel, title string) error {
	keys := noteKeys(rel, title)
	candidates, err := candidateTargets(db, keys)
	if err != nil {
		return err
	}
	// Also pick up links that resolved to the removed path by a key form not in
	// this set (defensive: the stored target_path is the authoritative pointer).
	byPath, err := distinctTargetsForPath(db, rel)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, raw := range candidates {
		seen[raw] = true
	}
	for _, raw := range byPath {
		if !seen[raw] {
			candidates = append(candidates, raw)
		}
	}
	for _, raw := range candidates {
		resolved, status := ix.resolve(raw)
		var tp any
		if status == Resolved {
			tp = resolved
		}
		if _, err := db.Exec("UPDATE links SET target_path = ? WHERE target = ?", tp, raw); err != nil {
			return err
		}
	}
	return nil
}

// distinctTargetsForPath returns the distinct raw targets of links whose stored
// target_path equals path, used to find links that resolved to a now-removed
// note even if its keys no longer describe them.
func distinctTargetsForPath(q queryer, path string) ([]string, error) {
	rows, err := q.Query("SELECT DISTINCT target FROM links WHERE target_path = ?", path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		out = append(out, raw)
	}
	return out, rows.Err()
}

// resolveLinkTargets resolves every distinct raw target to a canonical note
// path and writes it back to links.target_path. It builds one in-memory note
// index from the complete notes table, so it must be called only after all
// notes for the reindex are present. Each distinct target is resolved once and
// applied to every links row sharing it. A target that does not resolve to
// exactly one note (dangling or ambiguous) is set to NULL.
func resolveLinkTargets(tx execer) error {
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
