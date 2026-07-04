package core

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// consolidateFixture builds a vault with a stale pin, a cooled project (with
// several notes so citations have something to pick from), a recurring theme
// target linked >= 3 times from recently-changed notes, and a regulated note
// that must never surface in a proposal. Returns cfg + an open, fully indexed
// DB. Caller must defer db.Close().
func consolidateFixture(t *testing.T) (Config, *sql.DB) {
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

	// _NOW.md: one stale pin (past review-by).
	write("_NOW.md", "---\ntitle: Now\n---\n\n# Now\n\n"+
		"## Chase the venue booking\n\nreview-by: 2020-01-01\n\nBook the venue.\n")

	// A cooled 1-Projects folder with a few notes, all old.
	write("1-Projects/Offsite/Kickoff.md", "# Kickoff\n\nPlanning begins.\n")
	write("1-Projects/Offsite/Venue.md", "# Venue\n\nShortlist of venues.\n")
	write("1-Projects/Offsite/Budget.md", "# Budget\n\nCost estimate.\n")

	// A recurring theme target: linked from 3 distinct, recently-changed notes.
	write("3-Resources/Payment Provider.md", "# Payment Provider\n\nReference note.\n")
	write("2-Areas/Note A.md", "# Note A\n\nSee [[Payment Provider]].\n")
	write("2-Areas/Note B.md", "# Note B\n\nSee [[Payment Provider]].\n")
	write("2-Areas/Note C.md", "# Note C\n\nSee [[Payment Provider]].\n")

	// A regulated note that links to the theme target too: its own candidacy
	// (were it ever a target) must never surface, and it must not appear as a
	// cited source either.
	write("2-Areas/Regulated Note.md", "---\ntitle: Regulated Note\ntags: [regulated]\n---\n\n"+
		"# Regulated Note\n\nSee [[Payment Provider]].\n")

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

	// Backdate the cooled project's notes well past cooling_project_days
	// (default 30) and leave the theme/recent notes at their natural
	// (just-written, "now") content_changed_at so they fall inside the themes
	// window.
	old := float64(timeNowForTest().AddDate(0, 0, -40).UnixNano()) / 1e6
	for _, rel := range []string{
		"1-Projects/Offsite/Kickoff.md",
		"1-Projects/Offsite/Venue.md",
		"1-Projects/Offsite/Budget.md",
	} {
		backdateContentChangedAt(t, db, rel, old)
	}
	return cfg, db
}

func TestGenerateConsolidationRankedProposalsWithCitations(t *testing.T) {
	cfg, db := consolidateFixture(t)
	defer db.Close()

	res, err := GenerateConsolidation(cfg, db, ConsolidationOptions{Now: time.Now()})
	if err != nil {
		t.Fatalf("GenerateConsolidation: %v", err)
	}
	if res.Proposals == 0 {
		t.Fatal("expected at least one proposal")
	}
	if res.Proposals > maxConsolidationProposals {
		t.Errorf("proposals = %d, want <= %d", res.Proposals, maxConsolidationProposals)
	}

	raw, err := os.ReadFile(filepath.Join(cfg.VaultPath, DefaultConsolidationOutput))
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	doc := string(raw)

	if !strings.HasPrefix(doc, "# Consolidation review\n") {
		t.Errorf("document must start with H1 'Consolidation review'; got: %q", doc[:min(60, len(doc))])
	}
	if !strings.Contains(doc, "vault-gardener") {
		t.Error("document must mention the vault-gardener skill in the what-this-is note")
	}
	if !strings.Contains(doc, "## Proposals") {
		t.Error("document must have a '## Proposals' section")
	}

	// Stale pin proposal: cites _NOW.md and the pin name/date.
	if !strings.Contains(doc, "[[_NOW]]") {
		t.Error("stale pin proposal must cite [[_NOW]]")
	}
	if !strings.Contains(doc, `"Chase the venue booking"`) {
		t.Error("stale pin proposal must name the stale pin")
	}
	if !strings.Contains(doc, "2020-01-01") {
		t.Error("stale pin proposal must include the review-by date")
	}

	// Cooling project proposal: cites the project and recent notes as wiki-links.
	if !strings.Contains(doc, "[[1-Projects/Offsite/") && !strings.Contains(doc, "Offsite") {
		t.Error("cooling project proposal must reference the Offsite project")
	}
	if !strings.Contains(doc, "archive it or restate its next step") {
		t.Error("cooling project proposal must use the specified phrasing")
	}

	// Recurring theme proposal: cites the target and source notes.
	if !strings.Contains(doc, "Payment Provider") {
		t.Error("theme proposal must reference the recurring link target")
	}
	if !strings.Contains(doc, "keeps coming up") {
		t.Error("theme proposal must use the specified phrasing")
	}

	// Every proposal line ("N. ...") must cite at least one [[wiki-link]].
	for _, line := range strings.Split(doc, "\n") {
		if len(line) < 3 || line[1] != '.' && line[2] != '.' {
			continue
		}
		trimmed := strings.TrimLeft(line, "0123456789. ")
		if trimmed == line {
			continue // not a numbered proposal line
		}
		if !strings.Contains(line, "[[") {
			t.Errorf("proposal line missing a wiki-link citation: %q", line)
		}
	}

	// The regulated note must never appear anywhere in the document.
	if strings.Contains(doc, "Regulated Note") {
		t.Error("regulated-tagged note must never surface in the consolidation document")
	}
}

func TestGenerateConsolidationEmptyVaultWritesNothingNeedsAttention(t *testing.T) {
	cfg, write := refreshFixture(t)
	db, err := OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	write("2-Areas/Quiet.md", "# Quiet\n\nNothing notable.\n")
	if _, err := FullReindex(cfg, db); err != nil {
		t.Fatal(err)
	}

	res, err := GenerateConsolidation(cfg, db, ConsolidationOptions{Now: time.Now()})
	if err != nil {
		t.Fatalf("GenerateConsolidation: %v", err)
	}
	if res.Proposals != 0 {
		t.Errorf("proposals = %d, want 0 for an empty/quiet vault", res.Proposals)
	}

	raw, err := os.ReadFile(filepath.Join(cfg.VaultPath, DefaultConsolidationOutput))
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	doc := string(raw)
	if !strings.Contains(doc, "Nothing needs attention this week.") {
		t.Errorf("empty-proposal document must say nothing needs attention; got: %q", doc)
	}
	if !strings.HasPrefix(doc, "# Consolidation review\n") {
		t.Error("document must still have the H1 even when empty")
	}
}

func TestGenerateConsolidationCapsAtFiveProposals(t *testing.T) {
	vault := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(vault, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Six cooled projects: more raw findings than the cap allows.
	for i := 0; i < 6; i++ {
		folder := "Project" + string(rune('A'+i))
		write("1-Projects/"+folder+"/Note.md", "# Note\n\nBody.\n")
	}

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
	old := float64(timeNowForTest().AddDate(0, 0, -40).UnixNano()) / 1e6
	for i := 0; i < 6; i++ {
		folder := "Project" + string(rune('A'+i))
		backdateContentChangedAt(t, db, "1-Projects/"+folder+"/Note.md", old)
	}

	res, err := GenerateConsolidation(cfg, db, ConsolidationOptions{Now: time.Now()})
	if err != nil {
		t.Fatalf("GenerateConsolidation: %v", err)
	}
	if res.Proposals != maxConsolidationProposals {
		t.Errorf("proposals = %d, want exactly the cap %d with 6 cooled projects available", res.Proposals, maxConsolidationProposals)
	}
}

func TestGenerateConsolidationOverwritesEachRun(t *testing.T) {
	cfg, db := consolidateFixture(t)
	defer db.Close()

	if _, err := GenerateConsolidation(cfg, db, ConsolidationOptions{Now: time.Now()}); err != nil {
		t.Fatalf("first run: %v", err)
	}
	firstRaw, err := os.ReadFile(filepath.Join(cfg.VaultPath, DefaultConsolidationOutput))
	if err != nil {
		t.Fatal(err)
	}

	// Resolve the stale pin (no more stale_pin finding) and rerun; the document
	// must reflect only the current state, not accumulate history.
	nowPath := filepath.Join(cfg.VaultPath, "_NOW.md")
	if err := os.WriteFile(nowPath, []byte("---\ntitle: Now\n---\n\n# Now\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := FullReindex(cfg, db); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateConsolidation(cfg, db, ConsolidationOptions{Now: time.Now()}); err != nil {
		t.Fatalf("second run: %v", err)
	}
	secondRaw, err := os.ReadFile(filepath.Join(cfg.VaultPath, DefaultConsolidationOutput))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(secondRaw), "Chase the venue booking") {
		t.Error("second run must not carry over the resolved stale pin; the file is overwritten, not appended")
	}
	if string(firstRaw) == string(secondRaw) {
		t.Error("expected the document to change once the stale pin was resolved")
	}
}

func TestGenerateConsolidationCustomOutput(t *testing.T) {
	cfg, db := consolidateFixture(t)
	defer db.Close()

	res, err := GenerateConsolidation(cfg, db, ConsolidationOptions{Output: "2-Areas/_CUSTOM-CONSOLIDATION.md", Now: time.Now()})
	if err != nil {
		t.Fatalf("GenerateConsolidation: %v", err)
	}
	if res.OutputPath != "2-Areas/_CUSTOM-CONSOLIDATION.md" {
		t.Errorf("OutputPath = %q, want the custom path", res.OutputPath)
	}
	if _, err := os.Stat(filepath.Join(cfg.VaultPath, "2-Areas/_CUSTOM-CONSOLIDATION.md")); err != nil {
		t.Errorf("custom output not written: %v", err)
	}
}
