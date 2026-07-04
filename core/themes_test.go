package core

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// buildThemesVault writes a vault exercising every themes rule, indexes it,
// and backdates content_changed_at so the windows are unambiguous relative to
// the returned now:
//
//	recent (2 days ago):    Recent1 (links Target A, Target B, _MACHINE; tag theme-x)
//	                        Recent2 (links Target A; tag theme-x)
//	                        _MACHINE.md (links Target A; underscore basename -> excluded)
//	                        Daily/Digest.md "Vault Daily Digest" (links Target A, Target B;
//	                                         matches exclude_from_graph -> excluded)
//	previous (40 days ago): Old1 (links Target B; tags theme-x theme-y)
//	outside (100 days ago): Target A, Target B (targets only, never sources)
//
// Config sets exclude_from_graph = ["Vault Daily Digest"] and days = 30.
func buildThemesVault(t *testing.T) (Config, *sql.DB, time.Time) {
	t.Helper()
	vault := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(vault, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("Notes/Target A.md", "# Target A\n\nA hub the work returns to.\n")
	write("Notes/Target B.md", "# Target B\n\nAnother recurring topic.\n")
	write("Notes/Recent1.md", "---\ntags: [theme-x]\n---\n\n# Recent1\n\nSee [[Target A]] and [[Target B]], plus [[_MACHINE]].\n")
	write("Notes/Recent2.md", "---\ntags: [theme-x]\n---\n\n# Recent2\n\nOnly [[Target A]] here.\n")
	write("Notes/Old1.md", "---\ntags: [theme-x, theme-y]\n---\n\n# Old1\n\nPrevious-window note: [[Target B]].\n")
	// Machine note: underscore basename. Recent, links out; must count nowhere.
	write("Notes/_MACHINE.md", "# Machine log\n\n[[Target A]]\n")
	// Generated hub matched by exclude_from_graph via its title. Recent, links
	// to both targets; must count nowhere.
	write("Daily/Digest.md", "# Vault Daily Digest\n\n[[Target A]] [[Target B]]\n")

	cfg := Config{
		VaultPath:   vault,
		DBPath:      filepath.Join(vault, ".hebb", "index.db"),
		ExcludeDirs: defaultExcludeDirs,
		Health:      HealthConfig{ExcludeFromGraph: []string{"Vault Daily Digest"}},
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FullReindex(cfg, db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	now := time.Now()
	stamp := func(rel string, daysAgo int) {
		backdateContentChangedAt(t, db, rel,
			float64(now.AddDate(0, 0, -daysAgo).UnixNano())/1e6)
	}
	stamp("Notes/Recent1.md", 2)
	stamp("Notes/Recent2.md", 2)
	stamp("Notes/_MACHINE.md", 2)
	stamp("Daily/Digest.md", 2)
	stamp("Notes/Old1.md", 40)
	stamp("Notes/Target A.md", 100)
	stamp("Notes/Target B.md", 100)
	return cfg, db, now
}

func TestThemesLinkTargets(t *testing.T) {
	cfg, db, now := buildThemesVault(t)

	report, err := Themes(db, cfg, 30, now)
	if err != nil {
		t.Fatalf("Themes: %v", err)
	}

	// Only Recent1 and Recent2 are countable recent notes: the machine note and
	// the excluded hub changed recently too but must not be counted.
	if report.NotesChanged != 2 {
		t.Errorf("NotesChanged = %d, want 2 (machine + excluded hub not counted)", report.NotesChanged)
	}
	if report.Days != 30 {
		t.Errorf("Days = %d, want 30", report.Days)
	}

	if len(report.LinkTargets) != 2 {
		t.Fatalf("LinkTargets = %+v, want exactly Target A and Target B", report.LinkTargets)
	}
	a := report.LinkTargets[0]
	if a.Path != "Notes/Target A.md" || a.Links != 2 || a.Sources != 2 {
		t.Errorf("top target = %+v, want Notes/Target A.md with 2 links from 2 sources (machine/hub links not counted)", a)
	}
	if a.Title != "Target A" {
		t.Errorf("top target title = %q, want Target A", a.Title)
	}
	b := report.LinkTargets[1]
	if b.Path != "Notes/Target B.md" || b.Links != 1 || b.Sources != 1 {
		t.Errorf("second target = %+v, want Notes/Target B.md with 1 link from 1 source (Old1 changed before the window)", b)
	}
	for _, lt := range report.LinkTargets {
		if lt.Path == "Notes/_MACHINE.md" {
			t.Errorf("underscore machine note must not appear as a target: %+v", lt)
		}
	}
}

func TestThemesTagMomentum(t *testing.T) {
	cfg, db, now := buildThemesVault(t)

	report, err := Themes(db, cfg, 30, now)
	if err != nil {
		t.Fatalf("Themes: %v", err)
	}

	if len(report.TagMomentum) != 1 {
		t.Fatalf("TagMomentum = %+v, want exactly [theme-x] (theme-y appears only on a previous-window note)", report.TagMomentum)
	}
	tm := report.TagMomentum[0]
	if tm.Tag != "theme-x" {
		t.Fatalf("TagMomentum[0].Tag = %q, want theme-x", tm.Tag)
	}
	// Window: Recent1 + Recent2 carry theme-x. Previous window: only Old1 does.
	if tm.Count != 2 || tm.Previous != 1 || tm.Delta != 1 {
		t.Errorf("theme-x momentum = count %d, previous %d, delta %d; want 2/1/+1",
			tm.Count, tm.Previous, tm.Delta)
	}
}

func TestThemesCoOccurrence(t *testing.T) {
	cfg, db, now := buildThemesVault(t)

	report, err := Themes(db, cfg, 30, now)
	if err != nil {
		t.Fatalf("Themes: %v", err)
	}

	// Recent1 links both targets -> one shared source. The excluded hub also
	// links both but must not add a second. Pair paths are ordered A < B.
	if len(report.CoOccurrence) != 1 {
		t.Fatalf("CoOccurrence = %+v, want exactly one pair (from Recent1)", report.CoOccurrence)
	}
	co := report.CoOccurrence[0]
	if co.A != "Notes/Target A.md" || co.B != "Notes/Target B.md" {
		t.Errorf("pair = (%q, %q), want (Notes/Target A.md, Notes/Target B.md) in lexicographic order", co.A, co.B)
	}
	if co.ATitle != "Target A" || co.BTitle != "Target B" {
		t.Errorf("pair titles = (%q, %q), want (Target A, Target B)", co.ATitle, co.BTitle)
	}
	if co.SharedSources != 1 {
		t.Errorf("SharedSources = %d, want 1 (Recent1 only; excluded hub must not count)", co.SharedSources)
	}
}

// TestThemesWindowNarrowing proves the days parameter is honoured: with a
// 1-day window nothing qualifies, and the report is empty rather than an
// error.
func TestThemesWindowNarrowing(t *testing.T) {
	cfg, db, now := buildThemesVault(t)

	report, err := Themes(db, cfg, 1, now)
	if err != nil {
		t.Fatalf("Themes: %v", err)
	}
	if report.NotesChanged != 0 {
		t.Errorf("NotesChanged = %d, want 0 (all notes changed 2+ days ago)", report.NotesChanged)
	}
	if len(report.LinkTargets) != 0 || len(report.TagMomentum) != 0 || len(report.CoOccurrence) != 0 {
		t.Errorf("1-day window must yield empty sections: %+v", report)
	}
	// The empty sections must be non-nil so JSON emits [] rather than null.
	if report.LinkTargets == nil || report.TagMomentum == nil || report.CoOccurrence == nil {
		t.Error("empty report sections must be non-nil (JSON [] contract)")
	}
}

// TestThemesDaysDefault proves days <= 0 falls back to the 30-day default.
func TestThemesDaysDefault(t *testing.T) {
	cfg, db, now := buildThemesVault(t)

	report, err := Themes(db, cfg, 0, now)
	if err != nil {
		t.Fatalf("Themes: %v", err)
	}
	if report.Days != 30 {
		t.Errorf("Days = %d, want the 30-day default when the caller passes 0", report.Days)
	}
	if report.NotesChanged != 2 {
		t.Errorf("NotesChanged = %d, want 2 under the defaulted window", report.NotesChanged)
	}
}

// TestThemesMalformedExcludePatternFails proves a malformed exclude_from_graph
// glob fails the run loudly instead of silently reporting over the unfiltered
// vault (same posture as the graph detectors).
func TestThemesMalformedExcludePatternFails(t *testing.T) {
	cfg, db, now := buildThemesVault(t)
	cfg.Health.ExcludeFromGraph = []string{"[unclosed"}

	if _, err := Themes(db, cfg, 30, now); err == nil {
		t.Fatal("Themes with a malformed exclude pattern must return an error")
	}
}
