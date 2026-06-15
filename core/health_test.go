package core

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildHealthVault writes a minimal temp vault and returns cfg + an open DB
// ready for FullReindex. Caller must defer db.Close().
func buildHealthVault(t *testing.T) (Config, *sql.DB) {
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

	// (i) A note with a dangling wiki-link.
	write("Notes/Linker.md", "# Linker\n\nSee [[Nonexistent]] for details.\n")

	// (ii) A 1-Projects note with status: done (PARA drift by status).
	write("1-Projects/Foo.md", "---\ntitle: Foo\nstatus: done\n---\n\nFinished project.\n")

	// (iii) An oversized note: body long enough to exceed the default 1200-token
	// threshold (1200 * 4 = 4800 chars) and containing >= 3 H2/H3 sections,
	// each with non-trivial content.
	bigBody := strings.Builder{}
	bigBody.WriteString("# Big Note\n\n")
	for section := 0; section < 4; section++ {
		bigBody.WriteString("## Section\n\n")
		// Each section needs enough text to be considered substantial.
		for line := 0; line < 40; line++ {
			bigBody.WriteString("This is a line of body text in the section to pad out the token count.\n")
		}
		bigBody.WriteString("\n")
	}
	write("Notes/Big.md", bigBody.String())

	// (iv) Clean notes that must NOT be flagged.
	//   - A resolved link (target exists).
	write("Notes/Target.md", "# Target\n\nA real note.\n")
	write("Notes/Resolved.md", "# Resolved\n\nSee [[Target]] for details.\n")
	//   - A 1-Projects note with an active status.
	write("1-Projects/Active.md", "---\ntitle: Active\nstatus: in-progress\n---\n\nStill going.\n")
	//   - A small note (well under the token threshold).
	write("Notes/Small.md", "# Small\n\nJust a tiny note.\n")

	cfg := Config{
		VaultPath:   vault,
		DBPath:      filepath.Join(vault, ".hebb", "index.db"),
		ExcludeDirs: defaultExcludeDirs,
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
	return cfg, db
}

func TestRunHealthDanglingLink(t *testing.T) {
	cfg, db := buildHealthVault(t)
	defer db.Close()

	findings, err := RunHealth(cfg, db)
	if err != nil {
		t.Fatalf("RunHealth: %v", err)
	}

	var dangling []Finding
	for _, f := range findings {
		if f.Type == "dangling_link" {
			dangling = append(dangling, f)
		}
	}
	if len(dangling) != 1 {
		t.Fatalf("dangling_link findings = %d, want 1; all findings: %+v", len(dangling), findings)
	}
	got := dangling[0]
	if got.Path != "Notes/Linker.md" {
		t.Errorf("dangling_link path = %q, want Notes/Linker.md", got.Path)
	}
	if !strings.Contains(got.Detail, "Nonexistent") {
		t.Errorf("dangling_link detail %q missing target name", got.Detail)
	}
	if got.Severity == "" {
		t.Error("dangling_link severity must be set")
	}
}

// TestRunHealthDanglingVsAmbiguous is review finding D: a NULL target_path can
// mean either dangling (no note) or ambiguous (more than one note). The two must
// carry distinct, accurate finding types and wording, not a single "resolves to
// no note" message for both.
func TestRunHealthDanglingVsAmbiguous(t *testing.T) {
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
	// Linker has one genuinely dangling link and one ambiguous link.
	write("Linker.md", "# Linker\n\nSee [[Ghost]] and [[Note]].")
	write("one/Note.md", "# Note One\n\nFirst.")
	write("two/Note.md", "# Note Two\n\nSecond.")

	cfg := Config{VaultPath: vault, DBPath: filepath.Join(vault, ".hebb", "index.db"), ExcludeDirs: defaultExcludeDirs}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := FullReindex(cfg, db); err != nil {
		t.Fatal(err)
	}

	findings, err := RunHealth(cfg, db)
	if err != nil {
		t.Fatalf("RunHealth: %v", err)
	}

	var dangling, ambiguous []Finding
	for _, f := range findings {
		switch f.Type {
		case "dangling_link":
			dangling = append(dangling, f)
		case "ambiguous_link":
			ambiguous = append(ambiguous, f)
		}
	}
	if len(dangling) != 1 {
		t.Fatalf("dangling_link findings = %d, want 1; all: %+v", len(dangling), findings)
	}
	if len(ambiguous) != 1 {
		t.Fatalf("ambiguous_link findings = %d, want 1; all: %+v", len(ambiguous), findings)
	}
	if !strings.Contains(dangling[0].Detail, "Ghost") || !strings.Contains(dangling[0].Detail, "resolves to no note") {
		t.Errorf("dangling detail %q should name Ghost and say it resolves to no note", dangling[0].Detail)
	}
	if !strings.Contains(ambiguous[0].Detail, "Note") || !strings.Contains(strings.ToLower(ambiguous[0].Detail), "ambiguous") {
		t.Errorf("ambiguous detail %q should name Note and say it is ambiguous", ambiguous[0].Detail)
	}
}

func TestRunHealthPARADriftByStatus(t *testing.T) {
	cfg, db := buildHealthVault(t)
	defer db.Close()

	findings, err := RunHealth(cfg, db)
	if err != nil {
		t.Fatalf("RunHealth: %v", err)
	}

	var drift []Finding
	for _, f := range findings {
		if f.Type == "para_drift" {
			drift = append(drift, f)
		}
	}

	// Exactly one: Foo.md (done) -- Active.md must NOT appear.
	if len(drift) != 1 {
		t.Fatalf("para_drift findings = %d, want 1; all: %+v", len(drift), findings)
	}
	got := drift[0]
	if got.Path != "1-Projects/Foo.md" {
		t.Errorf("para_drift path = %q, want 1-Projects/Foo.md", got.Path)
	}
	if !strings.Contains(strings.ToLower(got.Detail), "done") {
		t.Errorf("para_drift detail %q should mention status", got.Detail)
	}
}

func TestRunHealthOversized(t *testing.T) {
	cfg, db := buildHealthVault(t)
	defer db.Close()

	findings, err := RunHealth(cfg, db)
	if err != nil {
		t.Fatalf("RunHealth: %v", err)
	}

	var oversized []Finding
	for _, f := range findings {
		if f.Type == "oversized" {
			oversized = append(oversized, f)
		}
	}
	if len(oversized) != 1 {
		t.Fatalf("oversized findings = %d, want 1; all: %+v", len(oversized), findings)
	}
	got := oversized[0]
	if got.Path != "Notes/Big.md" {
		t.Errorf("oversized path = %q, want Notes/Big.md", got.Path)
	}
	if !strings.Contains(got.Detail, "tokens") {
		t.Errorf("oversized detail %q should mention tokens", got.Detail)
	}
	if !strings.Contains(got.Detail, "sections") {
		t.Errorf("oversized detail %q should mention sections", got.Detail)
	}
}

func TestRunHealthCleanNotesNotFlagged(t *testing.T) {
	cfg, db := buildHealthVault(t)
	defer db.Close()

	findings, err := RunHealth(cfg, db)
	if err != nil {
		t.Fatalf("RunHealth: %v", err)
	}

	// Phase 1 detectors must not flag these notes. Phase 2a graph detectors
	// (orphan, leaf, island) may legitimately flag some notes (e.g. isolated
	// notes become island findings), so we only check Phase 1 finding types here.
	phase1Types := map[string]bool{
		"dangling_link": true,
		"para_drift":    true,
		"oversized":     true,
	}
	clean := []string{"Notes/Target.md", "Notes/Resolved.md", "1-Projects/Active.md", "Notes/Small.md"}
	for _, path := range clean {
		for _, f := range findings {
			if f.Path == path && phase1Types[f.Type] {
				t.Errorf("clean note %q incorrectly flagged by Phase 1 detector: %+v", path, f)
			}
		}
	}
}

func TestRunHealthResolvedLinkNotDangling(t *testing.T) {
	cfg, db := buildHealthVault(t)
	defer db.Close()

	findings, err := RunHealth(cfg, db)
	if err != nil {
		t.Fatalf("RunHealth: %v", err)
	}

	for _, f := range findings {
		if f.Type == "dangling_link" && f.Path == "Notes/Resolved.md" {
			t.Errorf("Notes/Resolved.md (resolved link) incorrectly flagged as dangling: %+v", f)
		}
	}
}

func TestRunHealthPARADriftStaleDays(t *testing.T) {
	// Build a vault where a 1-Projects note has an active status but a very old
	// mtime. Force the mtime by touching the file with an old timestamp.
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
	write("1-Projects/Stale.md", "---\ntitle: Stale\nstatus: in-progress\n---\n\nStill going but ancient.\n")

	cfg := Config{
		VaultPath:   vault,
		DBPath:      filepath.Join(vault, ".hebb", "index.db"),
		ExcludeDirs: defaultExcludeDirs,
		Health: HealthConfig{
			ProjectStaleDays: 1, // anything older than 1 day triggers
		},
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		t.Fatal(err)
	}

	// Age the file by setting mtime to 2 days ago.
	staleTime := timeNowForTest().AddDate(0, 0, -2)
	notePath := filepath.Join(vault, "1-Projects/Stale.md")
	if err := os.Chtimes(notePath, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}

	db, err := OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := FullReindex(cfg, db); err != nil {
		t.Fatal(err)
	}

	findings, err := RunHealth(cfg, db)
	if err != nil {
		t.Fatalf("RunHealth: %v", err)
	}

	var drift []Finding
	for _, f := range findings {
		if f.Type == "para_drift" && f.Path == "1-Projects/Stale.md" {
			drift = append(drift, f)
		}
	}
	if len(drift) == 0 {
		t.Fatalf("expected para_drift for stale project (mtime 2 days, threshold 1 day), got none; all: %+v", findings)
	}
	if !strings.Contains(drift[0].Detail, "days") {
		t.Errorf("para_drift stale detail %q should mention days", drift[0].Detail)
	}
}

func TestHealthConfigDefaults(t *testing.T) {
	hc := HealthConfig{}
	if hc.GetProjectStaleDays() != 180 {
		t.Errorf("ProjectStaleDays default = %d, want 180", hc.GetProjectStaleDays())
	}
	if hc.GetSizeThreshold() != 1200 {
		t.Errorf("SizeThreshold default = %d, want 1200", hc.GetSizeThreshold())
	}
}

func TestHealthConfigCustom(t *testing.T) {
	hc := HealthConfig{ProjectStaleDays: 30, SizeThreshold: 500}
	if hc.GetProjectStaleDays() != 30 {
		t.Errorf("ProjectStaleDays = %d, want 30", hc.GetProjectStaleDays())
	}
	if hc.GetSizeThreshold() != 500 {
		t.Errorf("SizeThreshold = %d, want 500", hc.GetSizeThreshold())
	}
}
