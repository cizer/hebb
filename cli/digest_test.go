package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cizer/hebb/core"
)

// runDigest executes `hebb digest --vault-root V [extra...]` and returns the
// combined output. The digest is pure Go now: no interpreter, no stub script.
func runDigest(t *testing.T, vault string, extra ...string) (string, error) {
	t.Helper()
	root := newRoot("test")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	args := append([]string{"digest", "--vault-root", vault}, extra...)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

func writeNote(t *testing.T, vault, rel, body string) {
	t.Helper()
	full := filepath.Join(vault, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDigestRefreshesIndexThenWritesNote(t *testing.T) {
	vault := t.TempDir()
	writeNote(t, vault, "1-Projects/alpha.md", "# Alpha\n\nthe alpha note\n")

	out, err := runDigest(t, vault)
	if err != nil {
		t.Fatalf("digest failed: %v\n%s", err, out)
	}

	// The digest note was written at the default output path.
	digestPath := filepath.Join(vault, filepath.FromSlash(core.DefaultDigestOutput))
	b, err := os.ReadFile(digestPath)
	if err != nil {
		t.Fatalf("digest note not written: %v\n%s", err, out)
	}
	doc := string(b)
	if !strings.Contains(doc, "# Vault Daily Digest") {
		t.Errorf("digest missing header:\n%s", doc)
	}
	// The freshly created note (its content_changed_at seeded to its mtime, which
	// is inside the first-run wall-clock window) is reported.
	if !strings.Contains(doc, "Alpha") {
		t.Errorf("digest should report the new note:\n%s", doc)
	}

	// The in-process refresh indexed both the seed and the digest note it wrote.
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
		t.Errorf("expected at least the seed + digest note indexed, got %d", notes)
	}
}

func TestDigestHonoursOutputFlag(t *testing.T) {
	vault := t.TempDir()
	writeNote(t, vault, "2-Areas/note.md", "# Note\n\nbody\n")

	out, err := runDigest(t, vault, "--output", "custom/_DIGEST.md")
	if err != nil {
		t.Fatalf("digest failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(vault, "custom", "_DIGEST.md")); err != nil {
		t.Fatalf("digest not written to --output path: %v\n%s", err, out)
	}
	// The default path must not also be written.
	if _, err := os.Stat(filepath.Join(vault, filepath.FromSlash(core.DefaultDigestOutput))); err == nil {
		t.Errorf("digest should not write the default path when --output is set")
	}
}

func TestDigestRejectsBadDate(t *testing.T) {
	vault := t.TempDir()
	out, err := runDigest(t, vault, "--date", "15-06-2026")
	if err == nil {
		t.Fatalf("digest should reject a non-ISO date:\n%s", out)
	}
}
