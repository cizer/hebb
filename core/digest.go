package core

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DefaultDigestOutput is the rolling digest note, relative to the vault root.
const DefaultDigestOutput = "2-Areas/_DAILY-DIGEST.md"

// maxDigestEntries caps how many dated sections the rolling digest note keeps.
const maxDigestEntries = 30

// index_meta keys holding the per-vault digest watermark. The window the digest
// reports is "content changed since the last successful run", not a wall-clock
// span, so an mtime reset cannot shift notes across a fixed boundary. Three keys
// keep same-day reruns reproducible while still advancing day to day:
//   - digestWindowStartKey: the lower bound (exclusive) used to select this
//     window. Stable across reruns on the same date.
//   - digestWindowDateKey: the run date that digestWindowStartKey belongs to.
//   - digestLastRunKey: the timestamp of the most recent run; it becomes the
//     next day's window start, so a new day reports only what changed since the
//     last run, with no gap and no double-reporting.
const (
	digestWindowStartKey = "digest_window_start"
	digestWindowDateKey  = "digest_window_date"
	digestLastRunKey     = "digest_last_run_at"
)

// digestExcludeBasenames are auto-generated system notes that would otherwise be
// daily noise. They are still indexed and searchable; they are only kept out of
// the digest's activity list.
var digestExcludeBasenames = map[string]bool{
	"_DAILY-DIGEST.md":  true,
	"_ACTION-REVIEW.md": true,
	"_INGEST-LOG.md":    true,
}

// digestParaOrder lists the top-level folders shown first and in this order;
// everything else follows alphabetically, with "Vault root" last.
var digestParaOrder = []string{"1-Projects", "2-Areas", "3-Resources", "4-Archives"}

// DigestOptions configures a digest run. The zero value runs against the default
// output path at the current time.
type DigestOptions struct {
	Output string    // digest note path relative to the vault root (default DefaultDigestOutput)
	Now    time.Time // run time, for testing; defaults to time.Now()
}

// DigestResult summarises a digest run.
type DigestResult struct {
	OutputPath string // the path written, as supplied (relative to vault, unless overridden absolute)
	Count      int    // notes reported in this window
	Label      string // human-readable window label
}

// touchedNote is one note reported in the digest.
type touchedNote struct {
	path      string // vault-relative, slash-separated
	title     string
	changedAt float64 // content_changed_at, in milliseconds
	isNew     bool
}

func (t touchedNote) group() string {
	if i := strings.IndexByte(t.path, '/'); i != -1 {
		return t.path[:i]
	}
	return "Vault root"
}

// GenerateDigest selects the notes whose content changed since the last digest
// run and writes a dated section to the rolling digest note. Selection is driven
// entirely by the index's content_changed_at watermark, never by filesystem
// mtime, so a bulk operation that rewrites bytes or bumps mtimes without
// changing content does not register, and a genuine edit is reported even if a
// later rewrite bumps its mtime. Callers should refresh the index (FullReindex)
// before calling so content_changed_at reflects the current vault.
func GenerateDigest(cfg Config, db *sql.DB, opts DigestOptions) (DigestResult, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	output := opts.Output
	if output == "" {
		output = DefaultDigestOutput
	}

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	start, label := digestWindow(today)
	nowMs := float64(now.UnixNano()) / 1e6
	todayStr := today.Format("2006-01-02")

	cutoff := resolveDigestCutoff(db, todayStr, float64(start.UnixNano())/1e6)
	touched, err := changedNotesSince(db, cutoff)
	if err != nil {
		return DigestResult{}, err
	}

	entry := renderDigestEntry(today, label, touched)
	outPath := output
	if !filepath.IsAbs(outPath) {
		outPath = filepath.Join(cfg.VaultPath, filepath.FromSlash(output))
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return DigestResult{}, err
	}
	doc := buildDigestDocument(entry, outPath, today)
	if err := os.WriteFile(outPath, []byte(doc), 0o644); err != nil {
		return DigestResult{}, err
	}

	// Advance the watermark only after a successful write so a failed run does
	// not silently skip the window. Re-running the same date rewrites these with
	// the same values (the cutoff is unchanged), so it is idempotent.
	if err := writeDigestMeta(db, cutoff, todayStr, nowMs); err != nil {
		return DigestResult{}, err
	}

	return DigestResult{OutputPath: output, Count: len(touched), Label: label}, nil
}

// digestWindow returns the window start and a human-readable label for a run
// date. A Monday covers the preceding Fri/Sat/Sun; any other day covers the
// single prior calendar day. The start is used only as the fallback selection
// cutoff for the very first run (before any watermark exists); the label is
// always shown.
func digestWindow(today time.Time) (start time.Time, label string) {
	var startDate time.Time
	if today.Weekday() == time.Monday {
		startDate = today.AddDate(0, 0, -3) // Friday
	} else {
		startDate = today.AddDate(0, 0, -1)
	}
	prevDay := today.AddDate(0, 0, -1)
	if startDate.Equal(prevDay) {
		label = fmt.Sprintf("%s (%s)", startDate.Format("2006-01-02"), startDate.Format("Mon"))
	} else {
		label = fmt.Sprintf("%s (%s) to %s (%s)",
			startDate.Format("2006-01-02"), startDate.Format("Mon"),
			prevDay.Format("2006-01-02"), prevDay.Format("Mon"))
	}
	return startDate, label
}

// resolveDigestCutoff picks the selection lower bound for this run. A rerun on
// the same date reuses the committed window start (reproducible). A new date
// starts the window at the last run's timestamp (changed-since-last-digest). The
// very first run, with no watermark at all, falls back to the wall-clock window
// start so the digest is not empty out of the box.
func resolveDigestCutoff(db *sql.DB, todayStr string, wallStartMs float64) float64 {
	if wmDate, ok := readMeta(db, digestWindowDateKey); ok && wmDate == todayStr {
		if v, ok := readMetaFloat(db, digestWindowStartKey); ok {
			return v
		}
	}
	if v, ok := readMetaFloat(db, digestLastRunKey); ok {
		return v
	}
	return wallStartMs
}

func writeDigestMeta(db *sql.DB, cutoff float64, todayStr string, nowMs float64) error {
	if err := writeMetaFloat(db, digestWindowStartKey, cutoff); err != nil {
		return err
	}
	if err := writeMeta(db, digestWindowDateKey, todayStr); err != nil {
		return err
	}
	return writeMetaFloat(db, digestLastRunKey, nowMs)
}

// changedNotesSince returns the notes whose content_changed_at is strictly after
// cutoff, oldest change first, excluding the auto-generated system notes. A note
// is "new" when it first entered the index after the cutoff; otherwise it is an
// update to a note that already existed.
func changedNotesSince(db *sql.DB, cutoff float64) ([]touchedNote, error) {
	rows, err := db.Query(
		`SELECT path, title, content_changed_at, first_indexed_at
		   FROM notes
		  WHERE content_changed_at > ?
		  ORDER BY content_changed_at ASC, path ASC`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []touchedNote
	for rows.Next() {
		var path, title string
		var changedAt, firstIndexedAt sql.NullFloat64
		if err := rows.Scan(&path, &title, &changedAt, &firstIndexedAt); err != nil {
			return nil, err
		}
		if !changedAt.Valid || digestExcluded(path) {
			continue
		}
		out = append(out, touchedNote{
			path:      path,
			title:     title,
			changedAt: changedAt.Float64,
			isNew:     firstIndexedAt.Valid && firstIndexedAt.Float64 > cutoff,
		})
	}
	return out, rows.Err()
}

// digestExcluded reports whether a vault-relative path should be kept out of the
// digest: the auto-generated system notes, anything under an assets/ folder, and
// any dotted path component (the index already skips dotted dirs, but this keeps
// the digest correct independent of index configuration).
func digestExcluded(rel string) bool {
	parts := strings.Split(rel, "/")
	for _, p := range parts {
		if p == "assets" || strings.HasPrefix(p, ".") {
			return true
		}
	}
	return digestExcludeBasenames[parts[len(parts)-1]]
}

func renderDigestEntry(today time.Time, label string, touched []touchedNote) string {
	heading := fmt.Sprintf("## %s — activity for %s", today.Format("2006-01-02"), label)
	if len(touched) == 0 {
		return heading + "\n\n_No vault activity in this window._\n"
	}

	groups := map[string][]touchedNote{}
	for _, item := range touched {
		g := item.group()
		groups[g] = append(groups[g], item)
	}
	names := make([]string, 0, len(groups))
	for g := range groups {
		names = append(names, g)
	}
	sort.Slice(names, func(i, j int) bool { return digestGroupLess(names[i], names[j]) })

	lines := []string{heading, "", fmt.Sprintf("**Notes touched:** %d", len(touched)), ""}
	for _, g := range names {
		items := groups[g]
		sort.SliceStable(items, func(i, j int) bool { return items[i].changedAt < items[j].changedAt })
		lines = append(lines, fmt.Sprintf("### %s (%d)", g, len(items)))
		for _, item := range items {
			marker := "updated"
			if item.isNew {
				marker = "new"
			}
			stamp := time.UnixMilli(int64(item.changedAt)).Format("2006-01-02 15:04")
			lines = append(lines, fmt.Sprintf("- %s — %s, %s", digestWikiLink(item.path, item.title), marker, stamp))
		}
		lines = append(lines, "")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
}

// digestGroupLess orders groups: PARA folders first in digestParaOrder order,
// then any other folder alphabetically, then "Vault root" last.
func digestGroupLess(a, b string) bool {
	ra, sa := digestGroupRank(a)
	rb, sb := digestGroupRank(b)
	if ra != rb {
		return ra < rb
	}
	return sa < sb
}

func digestGroupRank(g string) (int, string) {
	for i, p := range digestParaOrder {
		if g == p {
			return i, ""
		}
	}
	if g == "Vault root" {
		return len(digestParaOrder) + 1, ""
	}
	return len(digestParaOrder), strings.ToLower(g)
}

func digestWikiLink(rel, label string) string {
	return fmt.Sprintf("[[%s|%s]]", strings.TrimSuffix(rel, ".md"), label)
}

const digestHeader = "# Vault Daily Digest\n\n" +
	"Automated digest of vault activity, newest first. " +
	"Generated by hebb's `hebb digest` on weekdays.\n"

func buildDigestDocument(newEntry, outPath string, today time.Time) string {
	var existing []string
	if b, err := os.ReadFile(outPath); err == nil {
		existing = splitDigestEntries(string(b))
	}
	// Drop any prior entry for the same run date so reruns replace, not duplicate.
	sameDayPrefix := fmt.Sprintf("## %s ", today.Format("2006-01-02"))
	kept := make([]string, 0, len(existing)+1)
	kept = append(kept, strings.TrimSpace(newEntry))
	for _, e := range existing {
		if !strings.HasPrefix(e, sameDayPrefix) {
			kept = append(kept, e)
		}
	}
	if len(kept) > maxDigestEntries {
		kept = kept[:maxDigestEntries]
	}
	return digestHeader + "\n---\n\n" + strings.Join(kept, "\n\n---\n\n") + "\n"
}

func splitDigestEntries(text string) []string {
	const marker = "\n## "
	idx := strings.Index(text, marker)
	if idx == -1 {
		return nil
	}
	body := text[idx+1:] // drop leading newline, keep "## ..."
	var entries []string
	// Split on the full "\n## " heading delimiter, not "## ": an entry's body
	// contains "### " H3 group headings, and splitting on the bare "## " would
	// tear those apart ("### " contains "## " at an offset).
	for i, chunk := range strings.Split(body, marker) {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		if i == 0 {
			entries = append(entries, chunk)
		} else {
			entries = append(entries, "## "+chunk)
		}
	}
	return entries
}

// readMeta returns an index_meta value and whether the key exists.
func readMeta(db *sql.DB, key string) (string, bool) {
	var v string
	if err := db.QueryRow("SELECT value FROM index_meta WHERE key = ?", key).Scan(&v); err != nil {
		return "", false
	}
	return v, true
}

func readMetaFloat(db *sql.DB, key string) (float64, bool) {
	s, ok := readMeta(db, key)
	if !ok {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func writeMeta(db *sql.DB, key, value string) error {
	_, err := db.Exec("INSERT OR REPLACE INTO index_meta (key, value) VALUES (?, ?)", key, value)
	return err
}

func writeMetaFloat(db *sql.DB, key string, value float64) error {
	return writeMeta(db, key, strconv.FormatFloat(value, 'f', -1, 64))
}
