package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cizer/hebb/core"
)

// runHealth executes `hebb health` against a temp vault and returns the combined
// stdout/stderr and any execution error.
func runHealth(t *testing.T, vault string, extra ...string) (string, error) {
	t.Helper()
	root := newRoot("test")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	args := append([]string{"health", "--vault", vault}, extra...)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

// buildHealthVaultCLI sets up a minimal vault with a dangling link, a drifted
// project note, and an oversized note, returning the vault path.
func buildHealthVaultCLI(t *testing.T) string {
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

	// Dangling link.
	write("Notes/Linker.md", "# Linker\n\nSee [[GhostNote]] for details.\n")

	// PARA drift: done project.
	write("1-Projects/Done.md", "---\ntitle: Done\nstatus: done\n---\n\nFinished.\n")

	// Oversized: token-heavy body with 4 H2 sections, above the 4000-token default threshold.
	bigBody := strings.Builder{}
	bigBody.WriteString("# Big Note\n\n")
	for section := 0; section < 4; section++ {
		bigBody.WriteString("## Section\n\n")
		for line := 0; line < 160; line++ {
			bigBody.WriteString("This is a line of body text in the section to pad out the token count.\n")
		}
		bigBody.WriteString("\n")
	}
	write("Notes/Big.md", bigBody.String())

	// Build the initial index so hebb health has something to read.
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

func TestHealthCommandFindings(t *testing.T) {
	vault := buildHealthVaultCLI(t)

	// Run with --unresolved so the dangling link type is listed too: unresolved
	// links are suppressed by default, and this test asserts every detector type
	// renders.
	out, err := runHealth(t, vault, "--unresolved")
	if err != nil {
		t.Fatalf("hebb health returned an error on a vault with findings: %v\n%s", err, out)
	}

	// All three detector types must appear in the output.
	for _, want := range []string{"dangling_link", "para_drift", "oversized"} {
		if !strings.Contains(out, want) {
			t.Errorf("health output missing %q:\n%s", want, out)
		}
	}
}

func TestHealthCommandExitsZeroWithFindings(t *testing.T) {
	// hebb health is an advisory worklist, not a pass/fail install check. It
	// must exit 0 even when findings exist; only operational errors (cannot open
	// vault/index) warrant a non-zero exit.
	vault := buildHealthVaultCLI(t)

	_, err := runHealth(t, vault)
	if err != nil {
		t.Fatalf("hebb health must exit 0 when findings are present (it is advisory), got: %v", err)
	}
}

func TestHealthCommandJSON(t *testing.T) {
	// hebb health --json must emit a top-level JSON array of findings. This is
	// the Phase 1 contract that existing consumers (jq '.[]', Go []Finding
	// decoders) depend on. The web dashboard uses /api/health which returns
	// {findings, stats}; the CLI flag returns the plain array only.
	vault := buildHealthVaultCLI(t)

	out, err := runHealth(t, vault, "--json")
	if err != nil {
		t.Fatalf("hebb health --json: %v\n%s", err, out)
	}

	// The output must decode as a top-level array, not an object.
	var findings []core.Finding
	if err := json.Unmarshal([]byte(out), &findings); err != nil {
		t.Fatalf("hebb health --json output is not a valid JSON array: %v\n%s", err, out)
	}
	if len(findings) == 0 {
		t.Errorf("expected findings in JSON output, got empty slice")
	}

	// Every finding must have non-empty required fields.
	for i, f := range findings {
		if f.Type == "" {
			t.Errorf("finding[%d].Type is empty", i)
		}
		if f.Path == "" {
			t.Errorf("finding[%d].Path is empty", i)
		}
		if f.Severity == "" {
			t.Errorf("finding[%d].Severity is empty", i)
		}
	}
}

func TestHealthCommandJSONEmptyVault(t *testing.T) {
	// An empty vault (no notes, no findings) must produce a valid empty JSON
	// array, not null or a parse error.
	vault := t.TempDir()
	if err := core.DefaultVaultConfig("empty").Save(vault); err != nil {
		t.Fatal(err)
	}
	// Build an empty index.
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

	out, err := runHealth(t, vault, "--json")
	if err != nil {
		t.Fatalf("hebb health --json on empty vault: %v\n%s", err, out)
	}
	var findings []core.Finding
	if err := json.Unmarshal([]byte(out), &findings); err != nil {
		t.Fatalf("empty vault --json is not a valid JSON array: %v\n%s", err, out)
	}
	if findings == nil {
		t.Error("expected [] not null for empty findings JSON")
	}
}

// TestHealthCommandRefreshFailureExitsNonZero is review finding E: hebb health
// must not swallow a RefreshChanged error and run detectors on a stale or
// partial index. A corrupt index (here, the notes table dropped after the vault
// was built) makes the pre-query RefreshChanged fail, and the command must
// surface that as a non-zero exit rather than silently proceeding.
func TestHealthCommandRefreshFailureExitsNonZero(t *testing.T) {
	vault := buildHealthVaultCLI(t)

	// Corrupt the on-disk index so the pre-query refresh fails: replace notes with
	// a table lacking the mtime column. OpenDB's "CREATE TABLE IF NOT EXISTS notes"
	// will not repair an existing table, so this survives reopening, and
	// indexedMtimes (SELECT path, mtime FROM notes) errors inside RefreshChanged.
	cfg, err := core.ResolveVault(vault, "")
	if err != nil {
		t.Fatal(err)
	}
	db, err := core.OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		"DROP TABLE notes",
		"CREATE TABLE notes (path TEXT PRIMARY KEY, title TEXT)",
	} {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	db.Close()

	_, runErr := runHealth(t, vault)
	if runErr == nil {
		t.Fatal("hebb health must exit non-zero when RefreshChanged fails, got nil error")
	}
	// The error must come from the refresh stage (captured and propagated), not
	// from the detector stage running on a stale index. The CLI wraps the two
	// stages with distinct prefixes; assert the refresh prefix so a future
	// regression that swallows the refresh error (and only surfaces a later
	// detector error) is caught.
	if !strings.Contains(runErr.Error(), "refresh before health check failed") {
		t.Fatalf("error %q must identify the refresh stage, proving RefreshChanged was not swallowed", runErr)
	}
}

// TestHealthCommandSuppressesUnresolvedByDefault proves a genuinely unresolved
// link ([[GhostNote]]) is not listed by default but its count surfaces as an
// informational line, so the suppression is visible rather than silent.
func TestHealthCommandSuppressesUnresolvedByDefault(t *testing.T) {
	vault := buildHealthVaultCLI(t)

	out, err := runHealth(t, vault)
	if err != nil {
		t.Fatalf("hebb health: %v\n%s", err, out)
	}
	if strings.Contains(out, "GhostNote") {
		t.Errorf("unresolved [[GhostNote]] must not be listed by default:\n%s", out)
	}
	if !strings.Contains(out, "unresolved link") || !strings.Contains(out, "--unresolved") {
		t.Errorf("expected an informational suppressed-count line mentioning --unresolved:\n%s", out)
	}
}

// TestHealthCommandUnresolvedFlagListsThem proves --unresolved forces the
// unresolved link to be listed as a dangling_link finding for that run.
func TestHealthCommandUnresolvedFlagListsThem(t *testing.T) {
	vault := buildHealthVaultCLI(t)

	out, err := runHealth(t, vault, "--unresolved")
	if err != nil {
		t.Fatalf("hebb health --unresolved: %v\n%s", err, out)
	}
	if !strings.Contains(out, "dangling_link") || !strings.Contains(out, "GhostNote") {
		t.Errorf("--unresolved should list the dangling [[GhostNote]] link:\n%s", out)
	}
}

// TestHealthCommandUnresolvedFlagJSON proves --unresolved keeps the JSON output
// a top-level []Finding array and includes the now-reported dangling link.
func TestHealthCommandUnresolvedFlagJSON(t *testing.T) {
	vault := buildHealthVaultCLI(t)

	out, err := runHealth(t, vault, "--json", "--unresolved")
	if err != nil {
		t.Fatalf("hebb health --json --unresolved: %v\n%s", err, out)
	}
	var findings []core.Finding
	if err := json.Unmarshal([]byte(out), &findings); err != nil {
		t.Fatalf("--json --unresolved output is not a top-level []Finding array: %v\n%s", err, out)
	}
	var sawDangling bool
	for _, f := range findings {
		if f.Type == "dangling_link" && strings.Contains(f.Detail, "GhostNote") {
			sawDangling = true
		}
	}
	if !sawDangling {
		t.Errorf("expected the dangling [[GhostNote]] link in --json --unresolved output:\n%s", out)
	}
}

func TestHealthCommandTextGroupedByType(t *testing.T) {
	vault := buildHealthVaultCLI(t)

	// --unresolved so the dangling_link type renders alongside the others.
	out, err := runHealth(t, vault, "--unresolved")
	if err != nil {
		t.Fatalf("hebb health: %v\n%s", err, out)
	}

	// Each type should appear as a section header followed by a count.
	for _, header := range []string{"dangling_link", "para_drift", "oversized"} {
		if !strings.Contains(out, header) {
			t.Errorf("text output missing type header %q:\n%s", header, out)
		}
	}
}

func TestHealthCommandStructuralSummaryLine(t *testing.T) {
	// The structural graph summary must appear in text output above the findings
	// worklist. It takes the form "graph: N notes, E edges, ...".
	vault := buildHealthVaultCLI(t)

	out, err := runHealth(t, vault)
	if err != nil {
		t.Fatalf("hebb health: %v\n%s", err, out)
	}

	if !strings.Contains(out, "graph:") {
		t.Errorf("text output missing structural summary line (expected 'graph: ...'):\n%s", out)
	}
	// The summary must include note count, edge count, component count, and k-core.
	for _, keyword := range []string{"notes", "edges", "components", "k-core"} {
		if !strings.Contains(out, keyword) {
			t.Errorf("structural summary missing %q:\n%s", keyword, out)
		}
	}
}

// buildExcludeVaultCLI builds a vault with a high-degree hub note ("Vault Daily
// Digest") and ordinary notes, returning the vault path and the name of the hub.
func buildExcludeVaultCLI(t *testing.T) string {
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

	if err := core.DefaultVaultConfig("test").Save(vault); err != nil {
		t.Fatal(err)
	}

	write("Notes/A.md", "# A\n\n[[B]] [[C]]\n")
	write("Notes/B.md", "# B\n\n[[A]]\n")
	write("Notes/C.md", "# C\n\n[[A]]\n")
	// Hub links to every other note.
	write("Daily/Digest.md", "# Vault Daily Digest\n\n[[A]] [[B]] [[C]]\n")

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

// TestHealthCommandExcludeFromGraphFlag verifies that passing
// --exclude-from-graph="Vault Daily Digest" drops the hub note from the graph
// stats (node count falls by 1, the hub does not appear in text output's node
// count comparison).
func TestHealthCommandExcludeFromGraphFlag(t *testing.T) {
	vault := buildExcludeVaultCLI(t)

	// Without the flag: 4 notes in the graph.
	outAll, err := runHealth(t, vault)
	if err != nil {
		t.Fatalf("hebb health (baseline): %v\n%s", err, outAll)
	}
	if !strings.Contains(outAll, "4 notes") {
		t.Errorf("baseline graph summary should report 4 notes:\n%s", outAll)
	}

	// With the flag: 3 notes (Digest excluded).
	outExcl, err := runHealth(t, vault, "--exclude-from-graph=Vault Daily Digest")
	if err != nil {
		t.Fatalf("hebb health --exclude-from-graph: %v\n%s", err, outExcl)
	}
	if !strings.Contains(outExcl, "3 notes") {
		t.Errorf("excluded graph summary should report 3 notes:\n%s", outExcl)
	}
}

// TestHealthCommandExcludeFromGraphFlagMultiple verifies that a
// comma-separated list of patterns in --exclude-from-graph excludes each one.
func TestHealthCommandExcludeFromGraphFlagMultiple(t *testing.T) {
	vault := buildExcludeVaultCLI(t)

	// Exclude both "Vault Daily Digest" and "A" (by title/basename).
	outExcl, err := runHealth(t, vault, "--exclude-from-graph=Vault Daily Digest,A")
	if err != nil {
		t.Fatalf("hebb health --exclude-from-graph (multi): %v\n%s", err, outExcl)
	}
	if !strings.Contains(outExcl, "2 notes") {
		t.Errorf("two excluded notes: graph summary should report 2 notes:\n%s", outExcl)
	}
}
