package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cizer/hebb/core"
)

// runConsolidate executes `hebb consolidate --vault-root V [extra...]` and
// returns the combined output.
func runConsolidate(t *testing.T, vault string, extra ...string) (string, error) {
	t.Helper()
	root := newRoot("test")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	args := append([]string{"consolidate", "--vault-root", vault}, extra...)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

func TestConsolidateRefreshesIndexThenWritesNote(t *testing.T) {
	vault := t.TempDir()
	writeNote(t, vault, "_NOW.md", "---\ntitle: Now\n---\n\n# Now\n\n"+
		"## Chase the venue booking\n\nreview-by: 2020-01-01\n\nBook it.\n")

	out, err := runConsolidate(t, vault)
	if err != nil {
		t.Fatalf("consolidate failed: %v\n%s", err, out)
	}

	consolidationPath := filepath.Join(vault, filepath.FromSlash(core.DefaultConsolidationOutput))
	b, err := os.ReadFile(consolidationPath)
	if err != nil {
		t.Fatalf("consolidation note not written: %v\n%s", err, out)
	}
	doc := string(b)
	if !strings.Contains(doc, "# Consolidation review") {
		t.Errorf("consolidation note missing header:\n%s", doc)
	}
	if !strings.Contains(doc, "Chase the venue booking") {
		t.Errorf("consolidation note should surface the stale pin:\n%s", doc)
	}
	if !strings.Contains(out, "consolidate: wrote") {
		t.Errorf("expected a summary line in output, got:\n%s", out)
	}

	// The in-process refresh indexed both the seed and the consolidation note
	// it wrote.
	flagVault, flagDB = vault, ""
	defer func() { flagVault, flagDB = "", "" }()
	_, db, err := openVault()
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	defer db.Close()
	notes, _, _, err := core.Stats(db)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if notes < 2 {
		t.Errorf("expected at least the seed + consolidation note indexed, got %d", notes)
	}
}

func TestConsolidateHonoursOutputFlag(t *testing.T) {
	vault := t.TempDir()
	writeNote(t, vault, "2-Areas/note.md", "# Note\n\nbody\n")

	out, err := runConsolidate(t, vault, "--output", "custom/_CONSOLIDATION.md")
	if err != nil {
		t.Fatalf("consolidate failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(vault, "custom", "_CONSOLIDATION.md")); err != nil {
		t.Fatalf("consolidation not written to --output path: %v\n%s", err, out)
	}
	// The default path must not also be written.
	if _, err := os.Stat(filepath.Join(vault, filepath.FromSlash(core.DefaultConsolidationOutput))); err == nil {
		t.Errorf("consolidate should not write the default path when --output is set")
	}
}

func TestConsolidateRejectsBadDate(t *testing.T) {
	vault := t.TempDir()
	out, err := runConsolidate(t, vault, "--date", "15-06-2026")
	if err == nil {
		t.Fatalf("consolidate should reject a non-ISO date:\n%s", out)
	}
}

func TestConsolidateEmptyVaultStillWritesNote(t *testing.T) {
	vault := t.TempDir()
	writeNote(t, vault, "2-Areas/quiet.md", "# Quiet\n\nNothing notable.\n")

	out, err := runConsolidate(t, vault)
	if err != nil {
		t.Fatalf("consolidate failed: %v\n%s", err, out)
	}
	b, err := os.ReadFile(filepath.Join(vault, filepath.FromSlash(core.DefaultConsolidationOutput)))
	if err != nil {
		t.Fatalf("consolidation note not written: %v\n%s", err, out)
	}
	if !strings.Contains(string(b), "Nothing needs attention this week.") {
		t.Errorf("expected the reassuring empty-proposal line, got:\n%s", b)
	}
}
