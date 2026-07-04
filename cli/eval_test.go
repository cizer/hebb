package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cizer/hebb/core"
)

// runEval executes `hebb eval` against a temp vault and returns the combined
// stdout/stderr and any execution error.
func runEval(t *testing.T, vault string, extra ...string) (string, error) {
	t.Helper()
	root := newRoot("test")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	args := append([]string{"eval", "--vault", vault}, extra...)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

// buildEvalVaultCLI sets up a minimal indexed vault with one findable note and
// returns the vault path. No golden file is written; tests add their own.
func buildEvalVaultCLI(t *testing.T) string {
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

	// Vault config so ResolveVault finds the vault.
	if err := core.DefaultVaultConfig("test").Save(vault); err != nil {
		t.Fatal(err)
	}

	write("2-Areas/Board/Board pack process.md", "# Board pack process\n\nHow the quarterly board pack is assembled.\n")
	write("Notes/Unrelated.md", "# Unrelated\n\nSomething about groceries.\n")

	// Build the initial index so hebb eval has something to query.
	cfg, err := core.ResolveVault(vault, "")
	if err != nil {
		t.Fatal(err)
	}
	db, err := core.OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.FullReindex(cfg, db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()
	return vault
}

// writeGolden writes golden TOML content to the vault's default location.
func writeGolden(t *testing.T, vault, content string) {
	t.Helper()
	p := filepath.Join(vault, ".hebb", "golden.toml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEvalCommandPass(t *testing.T) {
	vault := buildEvalVaultCLI(t)
	writeGolden(t, vault, `
[[case]]
query = "quarterly board pack"
expect = ["2-Areas/Board/Board pack process.md"]
`)

	out, err := runEval(t, vault)
	if err != nil {
		t.Fatalf("hebb eval must exit 0 when all expected notes are found: %v\n%s", err, out)
	}
	if !bytes.Contains([]byte(out), []byte("PASS")) {
		t.Errorf("expected a PASS line:\n%s", out)
	}
	// The hit must be reported with its rank (path@rank form).
	if !bytes.Contains([]byte(out), []byte("2-Areas/Board/Board pack process.md@")) {
		t.Errorf("expected the found path with its rank (path@rank):\n%s", out)
	}
	// The summary line must be present.
	if !bytes.Contains([]byte(out), []byte("recall")) || !bytes.Contains([]byte(out), []byte("MRR")) {
		t.Errorf("expected a summary line with recall and MRR:\n%s", out)
	}
}

func TestEvalCommandMissExitsNonZero(t *testing.T) {
	vault := buildEvalVaultCLI(t)
	writeGolden(t, vault, `
[[case]]
query = "quarterly board pack"
expect = ["2-Areas/Board/Missing note.md"]
`)

	out, err := runEval(t, vault)
	if err == nil {
		t.Fatalf("hebb eval must exit non-zero when an expected note is missing\n%s", out)
	}
	// The missing note must be named in the output so the regression is
	// actionable, not just a failed exit code.
	if !bytes.Contains([]byte(out), []byte("MISS")) {
		t.Errorf("expected a MISS line:\n%s", out)
	}
	if !bytes.Contains([]byte(out), []byte("2-Areas/Board/Missing note.md")) {
		t.Errorf("the missing note must be named in the output:\n%s", out)
	}
}

func TestEvalCommandNoGoldenFileExitsZero(t *testing.T) {
	// A vault with no golden file is a normal state (evals not set up yet),
	// not a breach: exit 0 with guidance on creating one.
	vault := buildEvalVaultCLI(t)

	out, err := runEval(t, vault)
	if err != nil {
		t.Fatalf("hebb eval must exit 0 when the golden file is absent: %v\n%s", err, out)
	}
	if !bytes.Contains([]byte(out), []byte("no golden file")) {
		t.Errorf("expected a friendly no-golden-file message:\n%s", out)
	}
	if !bytes.Contains([]byte(out), []byte("[[case]]")) {
		t.Errorf("expected the message to show how to create a golden file:\n%s", out)
	}
}

func TestEvalCommandGoldenFlag(t *testing.T) {
	// --golden overrides the default <vault>/.hebb/golden.toml location.
	vault := buildEvalVaultCLI(t)
	alt := filepath.Join(t.TempDir(), "alt-golden.toml")
	if err := os.WriteFile(alt, []byte(`
[[case]]
query = "quarterly board pack"
expect = ["2-Areas/Board/Board pack process.md"]
`), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runEval(t, vault, "--golden", alt)
	if err != nil {
		t.Fatalf("hebb eval --golden: %v\n%s", err, out)
	}
	if !bytes.Contains([]byte(out), []byte("PASS")) {
		t.Errorf("expected a PASS line with --golden:\n%s", out)
	}
}

func TestEvalCommandJSON(t *testing.T) {
	vault := buildEvalVaultCLI(t)
	writeGolden(t, vault, `
[[case]]
query = "quarterly board pack"
expect = ["2-Areas/Board/Board pack process.md"]
`)

	out, err := runEval(t, vault, "--json")
	if err != nil {
		t.Fatalf("hebb eval --json: %v\n%s", err, out)
	}

	var report struct {
		Cases []struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
			Hits  []struct {
				Path string `json:"path"`
				Rank int    `json:"rank"`
			} `json:"hits"`
			Misses []string `json:"misses"`
			Passed bool     `json:"passed"`
		} `json:"cases"`
		TotalExpected int     `json:"total_expected"`
		TotalFound    int     `json:"total_found"`
		Recall        float64 `json:"recall"`
		MRR           float64 `json:"mrr"`
		Passed        bool    `json:"passed"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("hebb eval --json output is not valid JSON: %v\n%s", err, out)
	}
	if !report.Passed {
		t.Errorf("expected passed=true, got: %s", out)
	}
	if report.TotalExpected != 1 || report.TotalFound != 1 {
		t.Errorf("expected total_expected=1 total_found=1, got: %s", out)
	}
	if report.Recall != 1.0 {
		t.Errorf("expected recall=1.0, got: %s", out)
	}
	if len(report.Cases) != 1 {
		t.Fatalf("expected 1 case, got %d:\n%s", len(report.Cases), out)
	}
	c := report.Cases[0]
	if c.Query != "quarterly board pack" || !c.Passed || c.Limit != 10 {
		t.Errorf("case shape wrong: %+v", c)
	}
	if len(c.Hits) != 1 || c.Hits[0].Path != "2-Areas/Board/Board pack process.md" || c.Hits[0].Rank < 1 {
		t.Errorf("hit shape wrong: %+v", c.Hits)
	}
	if c.Misses == nil {
		t.Errorf("misses must be [] not null in JSON output:\n%s", out)
	}
}

func TestEvalCommandJSONMissStillEmitsReport(t *testing.T) {
	// On a miss the JSON report must still be emitted (for before/after
	// diffing) even though the exit code is non-zero.
	vault := buildEvalVaultCLI(t)
	writeGolden(t, vault, `
[[case]]
query = "quarterly board pack"
expect = ["2-Areas/Board/Missing note.md"]
`)

	out, err := runEval(t, vault, "--json")
	if err == nil {
		t.Fatalf("expected non-zero exit on a miss\n%s", out)
	}
	var report struct {
		Passed bool `json:"passed"`
		Cases  []struct {
			Misses []string `json:"misses"`
		} `json:"cases"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("JSON report must still be emitted on a miss: %v\n%s", err, out)
	}
	if report.Passed {
		t.Errorf("expected passed=false on a miss:\n%s", out)
	}
	if len(report.Cases) != 1 || len(report.Cases[0].Misses) != 1 {
		t.Errorf("expected the miss to be listed in the JSON report:\n%s", out)
	}
}

func TestEvalCommandMalformedGoldenExitsNonZero(t *testing.T) {
	// A malformed golden file is an operational error (unlike a missing one):
	// fail loudly rather than silently scoring nothing.
	vault := buildEvalVaultCLI(t)
	writeGolden(t, vault, "not valid = = toml")

	out, err := runEval(t, vault)
	if err == nil {
		t.Fatalf("hebb eval must exit non-zero on a malformed golden file\n%s", out)
	}
}
