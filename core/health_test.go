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

	// (iii) An oversized note: body long enough to exceed the default 4000-token
	// threshold (4000 * 4 = 16000 chars) and containing >= 3 H2/H3 sections,
	// each with non-trivial content.
	bigBody := strings.Builder{}
	bigBody.WriteString("# Big Note\n\n")
	for section := 0; section < 4; section++ {
		bigBody.WriteString("## Section\n\n")
		// Each section needs enough text to be considered substantial.
		for line := 0; line < 160; line++ {
			bigBody.WriteString("This is a line of body text in the section to pad out the token count.\n")
		}
		bigBody.WriteString("\n")
	}
	write("Notes/Big.md", bigBody.String())

	// (iv) Clean notes that must NOT be flagged.
	//   - A resolved link (target exists).
	write("Notes/Target.md", "# Target\n\nA real note.\n")
	write("Notes/Resolved.md", "# Resolved\n\nSee [[Target]] for details.\n")
	//   - A 1-Projects note with an active status and a link to the target note (so
	//     the stub detector does not flag it: it has an outbound resolved link).
	write("1-Projects/Active.md", "---\ntitle: Active\nstatus: in-progress\n---\n\nStill going. See [[Target]].\n")
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

	// [[Nonexistent]] is an unresolved link, suppressed by default, so enable
	// reporting to assert its per-link finding and wording.
	report, err := RunHealthFull(cfg, db, true)
	if err != nil {
		t.Fatalf("RunHealth: %v", err)
	}
	findings := report.Findings

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

	// Enable unresolved reporting so the dangling [[Ghost]] surfaces alongside
	// the ambiguous [[Note]]; the test asserts their distinct wording.
	report, err := RunHealthFull(cfg, db, true)
	if err != nil {
		t.Fatalf("RunHealth: %v", err)
	}
	findings := report.Findings

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

// buildLinkClassVault writes a vault exercising every dangling-link
// classification branch: an attachment link, a folder link (trailing slash and
// a real directory), an ambiguous link, and a genuinely unresolved link. It
// returns cfg + an open, reindexed DB.
func buildLinkClassVault(t *testing.T) (Config, *sql.DB) {
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

	// A real directory the folder link points at.
	if err := os.MkdirAll(filepath.Join(vault, "Area"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Source note with one link of each unresolved kind:
	//   [[diagram.png]]   attachment    -> excluded entirely
	//   [[Area/]]         folder (slash) -> excluded
	//   [[Area]]          folder (real dir, no slash) -> excluded
	//   [[Note]]          ambiguous     -> ambiguous_link, reported by default
	//   [[Nonexistent]]   unresolved    -> dangling_link, suppressed by default
	write("Linker.md", "# Linker\n\n"+
		"See [[diagram.png]], [[Area/]], [[Area]], [[Note]], and [[Nonexistent]].\n")
	write("one/Note.md", "# Note One\n\nFirst.")
	write("two/Note.md", "# Note Two\n\nSecond.")

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

func findingsByType(fs []Finding, typ string) []Finding {
	var out []Finding
	for _, f := range fs {
		if f.Type == typ {
			out = append(out, f)
		}
	}
	return out
}

func detailContaining(fs []Finding, needle string) bool {
	for _, f := range fs {
		if strings.Contains(f.Detail, needle) {
			return true
		}
	}
	return false
}

// TestRunHealthAttachmentNotDangling proves an attachment link target
// ([[diagram.png]]) is never reported as a dangling or ambiguous link: hebb does
// not index non-note files, so it cannot judge them broken.
func TestRunHealthAttachmentNotDangling(t *testing.T) {
	cfg, db := buildLinkClassVault(t)
	defer db.Close()

	// Even with unresolved reporting on, the attachment must not surface.
	report, err := RunHealthFull(cfg, db, true)
	if err != nil {
		t.Fatalf("RunHealth: %v", err)
	}
	if detailContaining(report.Findings, "diagram.png") {
		t.Errorf("attachment link [[diagram.png]] must not be a finding; findings: %+v", report.Findings)
	}
}

// TestRunHealthFolderNotDangling proves a folder link is excluded both when the
// raw target ends with '/' ([[Area/]]) and when it names a real directory with
// no slash ([[Area]]).
func TestRunHealthFolderNotDangling(t *testing.T) {
	cfg, db := buildLinkClassVault(t)
	defer db.Close()

	report, err := RunHealthFull(cfg, db, true)
	if err != nil {
		t.Fatalf("RunHealth: %v", err)
	}
	if detailContaining(report.Findings, "Area/") {
		t.Errorf("folder link [[Area/]] must not be a finding; findings: %+v", report.Findings)
	}
	for _, f := range findingsByType(report.Findings, "dangling_link") {
		if strings.Contains(f.Detail, "[[Area]]") {
			t.Errorf("folder link [[Area]] (real dir) must not be a dangling finding: %+v", f)
		}
	}
}

// TestRunHealthAmbiguousReportedByDefault proves an ambiguous link is reported
// even when unresolved reporting is off (the default): ambiguity is a real
// data-quality issue, not an expected future note.
func TestRunHealthAmbiguousReportedByDefault(t *testing.T) {
	cfg, db := buildLinkClassVault(t)
	defer db.Close()

	report, err := RunHealthFull(cfg, db, false)
	if err != nil {
		t.Fatalf("RunHealth: %v", err)
	}
	amb := findingsByType(report.Findings, "ambiguous_link")
	if len(amb) != 1 {
		t.Fatalf("ambiguous_link findings = %d, want 1; findings: %+v", len(amb), report.Findings)
	}
	if !strings.Contains(amb[0].Detail, "Note") {
		t.Errorf("ambiguous detail %q should name the target", amb[0].Detail)
	}
}

// TestRunHealthUnresolvedSuppressedByDefault proves a genuinely unresolved link
// ([[Nonexistent]]) is NOT reported by default but IS counted as suppressed.
func TestRunHealthUnresolvedSuppressedByDefault(t *testing.T) {
	cfg, db := buildLinkClassVault(t)
	defer db.Close()

	report, err := RunHealthFull(cfg, db, false)
	if err != nil {
		t.Fatalf("RunHealth: %v", err)
	}
	if dl := findingsByType(report.Findings, "dangling_link"); len(dl) != 0 {
		t.Errorf("unresolved links must not be reported by default, got %d: %+v", len(dl), dl)
	}
	if report.SuppressedUnresolved != 1 {
		t.Errorf("SuppressedUnresolved = %d, want 1 (the [[Nonexistent]] link)", report.SuppressedUnresolved)
	}
}

// TestRunHealthUnresolvedReportedWhenEnabled proves enabling unresolved
// reporting (the --unresolved flag / config) surfaces the dangling link as a
// per-link finding, and the suppressed count then drops to zero.
func TestRunHealthUnresolvedReportedWhenEnabled(t *testing.T) {
	cfg, db := buildLinkClassVault(t)
	defer db.Close()

	report, err := RunHealthFull(cfg, db, true)
	if err != nil {
		t.Fatalf("RunHealth: %v", err)
	}
	dl := findingsByType(report.Findings, "dangling_link")
	if len(dl) != 1 {
		t.Fatalf("dangling_link findings = %d, want 1 with reporting on; findings: %+v", len(dl), report.Findings)
	}
	if !strings.Contains(dl[0].Detail, "Nonexistent") {
		t.Errorf("dangling detail %q should name the target", dl[0].Detail)
	}
	if report.SuppressedUnresolved != 0 {
		t.Errorf("SuppressedUnresolved = %d, want 0 when reporting is on", report.SuppressedUnresolved)
	}
}

// TestRunHealthReportUnresolvedFromConfig proves the config default
// (report_unresolved_links = true) is honoured when the caller passes the
// effective setting through.
func TestRunHealthReportUnresolvedFromConfig(t *testing.T) {
	cfg, db := buildLinkClassVault(t)
	defer db.Close()
	cfg.Health.ReportUnresolvedLinks = true

	report, err := RunHealthFull(cfg, db, cfg.Health.ReportUnresolvedLinks)
	if err != nil {
		t.Fatalf("RunHealth: %v", err)
	}
	if len(findingsByType(report.Findings, "dangling_link")) != 1 {
		t.Errorf("config report_unresolved_links=true should surface the dangling link; findings: %+v", report.Findings)
	}
}

func TestRunHealthPARADriftByStatus(t *testing.T) {
	cfg, db := buildHealthVault(t)
	defer db.Close()

	report, err := RunHealthFull(cfg, db, false)
	if err != nil {
		t.Fatalf("RunHealth: %v", err)
	}
	findings := report.Findings

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

	report, err := RunHealthFull(cfg, db, false)
	if err != nil {
		t.Fatalf("RunHealth: %v", err)
	}
	findings := report.Findings

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

	report, err := RunHealthFull(cfg, db, false)
	if err != nil {
		t.Fatalf("RunHealth: %v", err)
	}
	findings := report.Findings

	// Phase 1 detectors must not flag these notes. Phase 2a graph detectors
	// (orphan, leaf, island) may legitimately flag some notes (e.g. isolated
	// notes become island findings), so we only check Phase 1 finding types here.
	phase1Types := map[string]bool{
		"dangling_link": true,
		"para_drift":    true,
		"oversized":     true,
		"stub":          true,
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

	report, err := RunHealthFull(cfg, db, false)
	if err != nil {
		t.Fatalf("RunHealth: %v", err)
	}
	findings := report.Findings

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

	report, err := RunHealthFull(cfg, db, false)
	if err != nil {
		t.Fatalf("RunHealth: %v", err)
	}
	findings := report.Findings

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
	if hc.GetSizeThreshold() != 4000 {
		t.Errorf("SizeThreshold default = %d, want 4000", hc.GetSizeThreshold())
	}
	if hc.GetStubThreshold() != 20 {
		t.Errorf("StubThreshold default = %d, want 20", hc.GetStubThreshold())
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

// buildStubVault creates a minimal vault for stub-detector tests and returns
// cfg + an open, reindexed DB. Caller must defer db.Close().
//
// Notes written:
//   - 2-Areas/Stub.md         -- 5 words, no outbound links -> should fire
//   - 2-Areas/ThinLinker.md   -- 5 words but has an outbound link -> should NOT fire
//   - Journal/ShortJournal.md -- 5 words, no links, but under Journal/ -> NOT fired
//   - 2-Areas/LongNote.md     -- many words, no outbound links -> NOT fired (token count too high)
//   - 2-Areas/Target.md       -- target for the ThinLinker resolved link
//
// Note: "Notes/" is in the default expected_orphan_folders, so stub notes are
// placed under "2-Areas/" which is NOT an expected-orphan folder.
func buildStubVault(t *testing.T) (Config, *sql.DB) {
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

	// A near-empty note with no outbound links and not in an expected-orphan folder:
	// should be flagged as a stub. Under 2-Areas/ which is not an orphan folder.
	write("2-Areas/Stub.md", "# Stub\n\nhello world tiny note\n")

	// A thin note that has an outbound link: intentional index stub, NOT flagged.
	write("2-Areas/Target.md", "# Target\n\nA real note with content.\n")
	write("2-Areas/ThinLinker.md", "# Thin\n\nSee [[Target]] here\n")

	// A thin note under Journal/: expected-orphan folder, NOT flagged.
	write("Journal/ShortJournal.md", "# Entry\n\nhello world entry\n")

	// A longer note with no outbound links: token count above stub threshold, NOT flagged.
	longBody := strings.Builder{}
	longBody.WriteString("# Long\n\n")
	for i := 0; i < 30; i++ {
		longBody.WriteString("This is a longer line of text to push the token estimate above the stub threshold.\n")
	}
	write("2-Areas/LongNote.md", longBody.String())

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

// TestStubDetectorFlagsNearEmptyNote proves a near-empty note (few tokens) with
// no outbound resolved links and not under an expected-orphan folder is flagged
// as a stub finding.
func TestStubDetectorFlagsNearEmptyNote(t *testing.T) {
	cfg, db := buildStubVault(t)
	defer db.Close()

	report, err := RunHealthFull(cfg, db, false)
	if err != nil {
		t.Fatalf("RunHealthFull: %v", err)
	}

	stubs := findingsByType(report.Findings, "stub")
	var found bool
	for _, f := range stubs {
		if f.Path == "2-Areas/Stub.md" {
			found = true
			if f.Severity != "warn" {
				t.Errorf("stub finding severity = %q, want warn", f.Severity)
			}
			if !strings.Contains(f.Detail, "tokens") {
				t.Errorf("stub detail %q should mention tokens", f.Detail)
			}
			if !strings.Contains(strings.ToLower(f.Detail), "merge") && !strings.Contains(strings.ToLower(f.Detail), "archive") {
				t.Errorf("stub detail %q should mention merge or archive", f.Detail)
			}
		}
	}
	if !found {
		t.Errorf("2-Areas/Stub.md should be flagged as a stub; stubs: %+v, all findings: %+v", stubs, report.Findings)
	}
}

// TestStubDetectorDoesNotFlagNoteWithOutboundLink proves a thin note that has
// at least one resolved outbound link is NOT flagged: it is an intentional
// index/map stub, not a dead stub.
func TestStubDetectorDoesNotFlagNoteWithOutboundLink(t *testing.T) {
	cfg, db := buildStubVault(t)
	defer db.Close()

	report, err := RunHealthFull(cfg, db, false)
	if err != nil {
		t.Fatalf("RunHealthFull: %v", err)
	}

	for _, f := range findingsByType(report.Findings, "stub") {
		if f.Path == "2-Areas/ThinLinker.md" {
			t.Errorf("2-Areas/ThinLinker.md has an outbound link and must NOT be flagged as a stub: %+v", f)
		}
	}
}

// TestStubDetectorDoesNotFlagJournalNote proves a short note under Journal/
// (an expected-orphan folder) is NOT flagged even if it has no outbound links.
func TestStubDetectorDoesNotFlagJournalNote(t *testing.T) {
	cfg, db := buildStubVault(t)
	defer db.Close()

	report, err := RunHealthFull(cfg, db, false)
	if err != nil {
		t.Fatalf("RunHealthFull: %v", err)
	}

	for _, f := range findingsByType(report.Findings, "stub") {
		if f.Path == "Journal/ShortJournal.md" {
			t.Errorf("Journal/ShortJournal.md is in an expected-orphan folder and must NOT be flagged as a stub: %+v", f)
		}
	}
}

// TestStubDetectorDoesNotFlagLongNote proves a note whose token estimate
// exceeds the stub threshold is NOT flagged even if it has no outbound links.
func TestStubDetectorDoesNotFlagLongNote(t *testing.T) {
	cfg, db := buildStubVault(t)
	defer db.Close()

	report, err := RunHealthFull(cfg, db, false)
	if err != nil {
		t.Fatalf("RunHealthFull: %v", err)
	}

	for _, f := range findingsByType(report.Findings, "stub") {
		if f.Path == "2-Areas/LongNote.md" {
			t.Errorf("2-Areas/LongNote.md is long enough and must NOT be flagged as a stub: %+v", f)
		}
	}
}

// TestOversizedThresholdRecalibrated proves the default 4000-token threshold
// means a ~1500-token note with sections is NOT flagged, but a ~5000-token
// note with sections IS flagged.
func TestOversizedThresholdRecalibrated(t *testing.T) {
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

	// ~1500-token note with 3 substantial H2 sections.
	// 1500 tokens * 4 chars/token = 6000 chars of body content.
	// We use 1400 tokens worth of body so it is clearly below 4000.
	smallSections := strings.Builder{}
	smallSections.WriteString("# Moderate Note\n\n")
	for section := 0; section < 3; section++ {
		smallSections.WriteString("## Section\n\n")
		for line := 0; line < 22; line++ {
			// ~75 chars per line; 22 lines per section * 3 sections = ~1500 tokens
			smallSections.WriteString("This is a line of body text in the section to pad the token count here.\n")
		}
		smallSections.WriteString("\n")
	}
	write("Notes/Moderate.md", smallSections.String())

	// ~5000-token note with 4 substantial H2 sections (above the 4000 threshold).
	hugeSections := strings.Builder{}
	hugeSections.WriteString("# Huge Note\n\n")
	for section := 0; section < 4; section++ {
		hugeSections.WriteString("## Section\n\n")
		for line := 0; line < 130; line++ {
			hugeSections.WriteString("This is a line of body text in the section to pad the token count here.\n")
		}
		hugeSections.WriteString("\n")
	}
	write("Notes/Huge.md", hugeSections.String())

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
	defer db.Close()
	if _, err := FullReindex(cfg, db); err != nil {
		t.Fatal(err)
	}

	report, err := RunHealthFull(cfg, db, false)
	if err != nil {
		t.Fatalf("RunHealthFull: %v", err)
	}

	oversized := findingsByType(report.Findings, "oversized")

	// The ~1500-token note must NOT be flagged.
	for _, f := range oversized {
		if f.Path == "Notes/Moderate.md" {
			t.Errorf("Notes/Moderate.md (~1500 tokens) must NOT be flagged with default threshold 4000; got: %+v", f)
		}
	}

	// The ~5000-token note MUST be flagged.
	var hugeFound bool
	for _, f := range oversized {
		if f.Path == "Notes/Huge.md" {
			hugeFound = true
		}
	}
	if !hugeFound {
		t.Errorf("Notes/Huge.md (~5000 tokens, 4 sections) MUST be flagged with default threshold 4000; oversized: %+v", oversized)
	}
}
