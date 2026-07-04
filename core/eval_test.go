package core

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// buildEvalVault writes a minimal temp vault with a few notes and returns cfg
// plus an open, indexed DB. Caller must defer db.Close().
func buildEvalVault(t *testing.T) (Config, *sql.DB) {
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

	write("2-Areas/Board/Board pack process.md", "# Board pack process\n\nHow the quarterly board pack is assembled.\n")
	write("Notes/Unrelated.md", "# Unrelated\n\nSomething about groceries.\n")

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

func TestRunEvalHit(t *testing.T) {
	_, db := buildEvalVault(t)
	defer db.Close()

	cases := []EvalCase{
		{Query: "quarterly board pack", Expect: []string{"2-Areas/Board/Board pack process.md"}},
	}

	report, err := RunEval(db, cases)
	if err != nil {
		t.Fatalf("RunEval: %v", err)
	}
	if !report.Passed() {
		t.Fatalf("expected report to pass, got misses: %+v", report.Cases)
	}
	if report.TotalExpected != 1 || report.TotalFound != 1 {
		t.Fatalf("expected 1/1 found, got %d/%d", report.TotalFound, report.TotalExpected)
	}
	if report.Recall != 1.0 {
		t.Errorf("expected recall 1.0, got %f", report.Recall)
	}
	if len(report.Cases) != 1 || len(report.Cases[0].Hits) != 1 {
		t.Fatalf("expected exactly one hit recorded, got %+v", report.Cases)
	}
	if report.Cases[0].Hits[0].Rank < 1 {
		t.Errorf("expected a 1-based rank, got %d", report.Cases[0].Hits[0].Rank)
	}
	if report.MRR <= 0 {
		t.Errorf("expected a positive MRR, got %f", report.MRR)
	}
}

func TestRunEvalMiss(t *testing.T) {
	_, db := buildEvalVault(t)
	defer db.Close()

	cases := []EvalCase{
		{Query: "quarterly board pack", Expect: []string{"2-Areas/Board/Nonexistent note.md"}},
	}

	report, err := RunEval(db, cases)
	if err != nil {
		t.Fatalf("RunEval: %v", err)
	}
	if report.Passed() {
		t.Fatal("expected report to fail (missing note), but it passed")
	}
	if len(report.Cases) != 1 || len(report.Cases[0].Misses) != 1 {
		t.Fatalf("expected exactly one miss recorded, got %+v", report.Cases)
	}
	if report.Cases[0].Misses[0] != "2-Areas/Board/Nonexistent note.md" {
		t.Errorf("miss should name the missing path, got %q", report.Cases[0].Misses[0])
	}
	if report.Recall != 0.0 {
		t.Errorf("expected recall 0.0, got %f", report.Recall)
	}
}

func TestRunEvalDefaultLimit(t *testing.T) {
	_, db := buildEvalVault(t)
	defer db.Close()

	cases := []EvalCase{
		{Query: "quarterly board pack", Expect: []string{"2-Areas/Board/Board pack process.md"}},
	}
	report, err := RunEval(db, cases)
	if err != nil {
		t.Fatalf("RunEval: %v", err)
	}
	if report.Cases[0].Limit != 10 {
		t.Errorf("expected default limit of 10, got %d", report.Cases[0].Limit)
	}
}

func TestLoadGolden(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "golden.toml")
	content := `
[[case]]
query = "quarterly board pack"
expect = ["2-Areas/Board/Board pack process.md"]

[[case]]
query = "something else"
expect = ["Notes/Foo.md", "Notes/Bar.md"]
limit = 5
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cases, err := LoadGolden(path)
	if err != nil {
		t.Fatalf("LoadGolden: %v", err)
	}
	if len(cases) != 2 {
		t.Fatalf("expected 2 cases, got %d", len(cases))
	}
	if cases[0].Query != "quarterly board pack" {
		t.Errorf("case 0 query mismatch: %q", cases[0].Query)
	}
	if len(cases[0].Expect) != 1 || cases[0].Expect[0] != "2-Areas/Board/Board pack process.md" {
		t.Errorf("case 0 expect mismatch: %+v", cases[0].Expect)
	}
	if cases[1].Limit != 5 {
		t.Errorf("case 1 limit mismatch: got %d, want 5", cases[1].Limit)
	}
}

func TestLoadGoldenMissingFile(t *testing.T) {
	_, err := LoadGolden(filepath.Join(t.TempDir(), "does-not-exist.toml"))
	if err == nil {
		t.Fatal("expected an error for a missing golden file")
	}
	if !os.IsNotExist(err) {
		t.Errorf("expected an os.IsNotExist error so callers can special-case it, got: %v", err)
	}
}

func TestLoadGoldenMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "golden.toml")
	if err := os.WriteFile(path, []byte("not valid = = toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadGolden(path); err == nil {
		t.Fatal("expected an error for a malformed golden file")
	}
}
