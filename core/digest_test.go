package core

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// digestFixture builds a vault with an open, freshly migrated index.
func digestFixture(t *testing.T) (Config, *sql.DB, func(rel, content string)) {
	t.Helper()
	cfg, write := refreshFixture(t)
	db, err := OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return cfg, db, write
}

func ms(when time.Time) float64 { return float64(when.UnixNano()) / 1e6 }

func setMtime(t *testing.T, cfg Config, rel string, when time.Time) {
	t.Helper()
	full := filepath.Join(cfg.VaultPath, filepath.FromSlash(rel))
	if err := os.Chtimes(full, when, when); err != nil {
		t.Fatal(err)
	}
}

// setAllMtimes simulates a vault-wide bulk operation (sync client, restore,
// find/replace) that stamps every markdown file with the same recent mtime.
func setAllMtimes(t *testing.T, cfg Config, when time.Time) {
	t.Helper()
	files, err := enumerateMarkdown(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range files {
		setMtime(t, cfg, rel, when)
	}
}

func contentChangedAt(t *testing.T, db *sql.DB, rel string) float64 {
	t.Helper()
	var v sql.NullFloat64
	if err := db.QueryRow("SELECT content_changed_at FROM notes WHERE path = ?", rel).Scan(&v); err != nil {
		t.Fatalf("query content_changed_at for %s: %v", rel, err)
	}
	if !v.Valid {
		t.Fatalf("content_changed_at is NULL for %s", rel)
	}
	return v.Float64
}

func reindex(t *testing.T, cfg Config, db *sql.DB) {
	t.Helper()
	if _, err := FullReindex(cfg, db); err != nil {
		t.Fatal(err)
	}
}

func touchedSet(notes []touchedNote) map[string]bool {
	out := map[string]bool{}
	for _, n := range notes {
		out[n.path] = true
	}
	return out
}

// Criterion 1: a note whose content changed inside the window appears even if a
// later vault-wide rewrite bumps its mtime past the window. The change is
// recorded as content_changed_at at index time and is not moved by a bare mtime
// bump, so the note stays selected regardless of how far its mtime is pushed.
func TestChangedNotesContentChangeSurvivesMtimeBump(t *testing.T) {
	cfg, db, write := digestFixture(t)
	now := time.Now()
	write("2-Areas/edited.md", "# Edited\n\nversion one")
	write("2-Areas/stable.md", "# Stable\n\nnever touched again")
	setMtime(t, cfg, "2-Areas/edited.md", now.Add(-36*time.Hour)) // changed inside the window
	setMtime(t, cfg, "2-Areas/stable.md", now.Add(-200*time.Hour))
	reindex(t, cfg, db)

	editedCCA := contentChangedAt(t, db, "2-Areas/edited.md")

	// A bulk operation rewrites bytes and bumps mtimes far past the window; the
	// content is unchanged.
	setAllMtimes(t, cfg, now.Add(240*time.Hour))
	reindex(t, cfg, db)

	if got := contentChangedAt(t, db, "2-Areas/edited.md"); got != editedCCA {
		t.Errorf("content_changed_at moved on a bare mtime bump: got %v, want %v", got, editedCCA)
	}

	// Window cutoff sits between the two notes' change times.
	got, err := changedNotesSince(db, ms(now.Add(-100*time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	set := touchedSet(got)
	if !set["2-Areas/edited.md"] {
		t.Error("edited note must appear: its content changed in the window, even though a later rewrite bumped its mtime past it")
	}
	if set["2-Areas/stable.md"] {
		t.Error("stable note changed before the window and must not appear")
	}
}

// Criterion 2: a note whose bytes were rewritten by a bulk operation but whose
// content did not change does not appear; a genuine content edit still does.
func TestChangedNotesUnchangedContentNotReported(t *testing.T) {
	cfg, db, write := digestFixture(t)
	now := time.Now()
	write("1-Projects/a.md", "# A\n\nbody a")
	write("1-Projects/b.md", "# B\n\nbody b")
	setAllMtimes(t, cfg, now.Add(-100*time.Hour))
	reindex(t, cfg, db)

	watermark := contentChangedAt(t, db, "1-Projects/a.md")

	// Bulk no-op rewrite: every mtime bumped, content byte-identical.
	setAllMtimes(t, cfg, now.Add(-50*time.Hour))
	reindex(t, cfg, db)

	got, err := changedNotesSince(db, watermark)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("a no-op bulk rewrite must not register as activity, got %v", touchedSet(got))
	}

	// A real content edit does register.
	write("1-Projects/a.md", "# A\n\nbody a, now revised")
	setMtime(t, cfg, "1-Projects/a.md", now.Add(-10*time.Hour))
	reindex(t, cfg, db)

	got, err = changedNotesSince(db, watermark)
	if err != nil {
		t.Fatal(err)
	}
	set := touchedSet(got)
	if !set["1-Projects/a.md"] {
		t.Error("a genuine content edit must register as activity")
	}
	if set["1-Projects/b.md"] {
		t.Error("the untouched note must not register")
	}
}

// seedDigestWatermark fixes the selection window so a digest run is independent
// of the wall-clock date logic (which is exercised separately).
func seedDigestWatermark(t *testing.T, db *sql.DB, cutoff float64, date string) {
	t.Helper()
	if err := writeMeta(db, digestWindowDateKey, date); err != nil {
		t.Fatal(err)
	}
	if err := writeMetaFloat(db, digestWindowStartKey, cutoff); err != nil {
		t.Fatal(err)
	}
}

// Criterion 3: running the digest twice over the same logical window produces
// the same set regardless of mtime churn between the runs.
func TestGenerateDigestIdempotentAcrossMtimeChurn(t *testing.T) {
	cfg, db, write := digestFixture(t)
	now := time.Now()
	write("1-Projects/a.md", "# A\n\nbody a")
	write("2-Areas/b.md", "# B\n\nbody b")
	setAllMtimes(t, cfg, now.Add(-30*time.Hour))
	reindex(t, cfg, db)

	runDate := now.Add(-2 * time.Hour)
	seedDigestWatermark(t, db, ms(now.Add(-50*time.Hour)), runDate.Format("2006-01-02"))

	r1, err := GenerateDigest(cfg, db, DigestOptions{Now: runDate})
	if err != nil {
		t.Fatal(err)
	}
	doc1, err := os.ReadFile(filepath.Join(cfg.VaultPath, filepath.FromSlash(DefaultDigestOutput)))
	if err != nil {
		t.Fatal(err)
	}

	// Vault-wide mtime churn with no content change between the two runs.
	setAllMtimes(t, cfg, now)
	reindex(t, cfg, db)

	r2, err := GenerateDigest(cfg, db, DigestOptions{Now: runDate})
	if err != nil {
		t.Fatal(err)
	}
	doc2, err := os.ReadFile(filepath.Join(cfg.VaultPath, filepath.FromSlash(DefaultDigestOutput)))
	if err != nil {
		t.Fatal(err)
	}

	if r1.Count != 2 || r2.Count != 2 {
		t.Fatalf("expected 2 notes both runs, got %d then %d", r1.Count, r2.Count)
	}
	// The same-day entry is replaced, not duplicated, and is byte-identical: same
	// notes, same content_changed_at stamps, despite the mtime churn.
	if string(doc1) != string(doc2) {
		t.Errorf("same-window rerun produced a different digest:\n--- run 1 ---\n%s\n--- run 2 ---\n%s", doc1, doc2)
	}
}

// Criterion 4: a vault-wide find/replace (here a no-op byte rewrite that bumps
// every mtime) immediately before a digest run does not inflate the next run.
func TestGenerateDigestNextRunNotInflatedByBulkRewrite(t *testing.T) {
	cfg, db, write := digestFixture(t)
	now := time.Now()
	for _, n := range []struct{ rel, body string }{
		{"1-Projects/a.md", "# A\n\nalpha"},
		{"2-Areas/b.md", "# B\n\nbeta"},
		{"3-Resources/c.md", "# C\n\ngamma"},
	} {
		write(n.rel, n.body)
	}
	setAllMtimes(t, cfg, now.Add(-30*time.Hour))
	reindex(t, cfg, db)

	// Day 1: report the three notes and advance the watermark.
	day1 := now.Add(-2 * time.Hour)
	seedDigestWatermark(t, db, ms(now.Add(-50*time.Hour)), day1.Format("2006-01-02"))
	r1, err := GenerateDigest(cfg, db, DigestOptions{Now: day1})
	if err != nil {
		t.Fatal(err)
	}
	if r1.Count != 3 {
		t.Fatalf("day 1 should report all 3 notes, got %d", r1.Count)
	}

	// Immediately before the next run, a vault-wide rewrite bumps every mtime
	// (including the digest note just written) with no content change.
	setAllMtimes(t, cfg, now)
	reindex(t, cfg, db)

	// Day 2 (a different calendar day) starts its window at day 1's run, so the
	// unchanged notes fall before it and are not re-reported.
	day2 := now.Add(22 * time.Hour)
	r2, err := GenerateDigest(cfg, db, DigestOptions{Now: day2})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Count != 0 {
		t.Errorf("next run must not be inflated by a no-op bulk rewrite, got %d notes", r2.Count)
	}
	doc, err := os.ReadFile(filepath.Join(cfg.VaultPath, filepath.FromSlash(DefaultDigestOutput)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc), "_No vault activity in this window._") {
		t.Errorf("day 2 should report no activity:\n%s", doc)
	}
}

func TestDigestWindowLabel(t *testing.T) {
	// A Tuesday covers the prior day only.
	tue := time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC)
	if _, label := digestWindow(tue); label != "2026-06-15 (Mon)" {
		t.Errorf("Tuesday label = %q, want single prior day", label)
	}
	// A Monday spans the preceding Fri/Sat/Sun.
	mon := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	if _, label := digestWindow(mon); label != "2026-06-12 (Fri) to 2026-06-14 (Sun)" {
		t.Errorf("Monday label = %q, want Fri..Sun span", label)
	}
}

func TestDigestExcluded(t *testing.T) {
	cases := map[string]bool{
		"2-Areas/_DAILY-DIGEST.md":  true,
		"2-Areas/_ACTION-REVIEW.md": true,
		"1-Projects/_INGEST-LOG.md": true,
		"assets/diagram.md":         true,
		".obsidian/workspace.md":    true,
		"1-Projects/real-note.md":   false,
		"2-Areas/area/sub-note.md":  false,
	}
	for rel, want := range cases {
		if got := digestExcluded(rel); got != want {
			t.Errorf("digestExcluded(%q) = %v, want %v", rel, got, want)
		}
	}
}
