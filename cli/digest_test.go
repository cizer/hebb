package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cizer/hebb/core"
)

// writeStubDigest creates an <assetRoot>/automation/generate-vault-digest.py
// stub that, when run by a POSIX shell standing in for python, writes a note
// into the vault. It returns the asset root. The stub reads --vault-root from
// its args so the test can assert the command threads the vault through.
func stubDigestAsset(t *testing.T, note string) string {
	t.Helper()
	assetRoot := t.TempDir()
	autoDir := filepath.Join(assetRoot, "automation")
	if err := os.MkdirAll(autoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A tiny shell script masquerading as the digest "interpreter target": when
	// invoked as `<sh> generate-vault-digest.py --vault-root V [extra...]` it
	// parses --vault-root and writes a marker note there. Using a shell stub as
	// $PYTHON keeps the test hermetic (no real python dependency) while still
	// exercising the command's interpreter resolution, arg threading, and the
	// in-process index refresh that follows.
	script := "#!/bin/sh\n" +
		"vault=\"\"\n" +
		"while [ $# -gt 0 ]; do\n" +
		"  case \"$1\" in\n" +
		"    --vault-root) vault=\"$2\"; shift 2 ;;\n" +
		"    *) shift ;;\n" +
		"  esac\n" +
		"done\n" +
		"[ -n \"$vault\" ] || { echo 'stub: no --vault-root' >&2; exit 2; }\n" +
		"printf '# Digest\\n\\n" + note + "\\n' > \"$vault/" + note + ".md\"\n"
	if err := os.WriteFile(filepath.Join(autoDir, "generate-vault-digest.py"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return assetRoot
}

// runDigest executes `hebb digest --vault-root V --asset-root A` with PYTHON
// pointed at /bin/sh (the stub interpreter) and returns the combined output.
func runDigest(t *testing.T, vault, assetRoot string, extra ...string) (string, error) {
	t.Helper()
	t.Setenv("PYTHON", "/bin/sh")
	root := newRoot("test")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	args := append([]string{"digest", "--vault-root", vault, "--asset-root", assetRoot}, extra...)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

func TestDigestRunsScriptThenRefreshesIndex(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub interpreter is a POSIX shell script")
	}
	vault := t.TempDir()
	// Seed the vault so install/index has something, and so we can prove the
	// post-digest refresh picks up the note the digest just wrote.
	if err := os.WriteFile(filepath.Join(vault, "seed.md"), []byte("# Seed\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	assetRoot := stubDigestAsset(t, "DIGESTNOTE")

	out, err := runDigest(t, vault, assetRoot)
	if err != nil {
		t.Fatalf("digest failed: %v\n%s", err, out)
	}

	// The digest script wrote its note.
	if _, err := os.Stat(filepath.Join(vault, "DIGESTNOTE.md")); err != nil {
		t.Fatalf("digest script did not write its note: %v\n%s", err, out)
	}

	// The in-process refresh indexed it: a search finds the note the digest just
	// wrote, with no separate `hebb index` run.
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
		t.Errorf("expected the refresh to index at least the seed + digest note, got %d", notes)
	}
}

func TestDigestPassesExtraArgsToScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub interpreter is a POSIX shell script")
	}
	vault := t.TempDir()
	assetRoot := t.TempDir()
	autoDir := filepath.Join(assetRoot, "automation")
	if err := os.MkdirAll(autoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// This stub echoes all its args to a file so we can assert pass-through.
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$2/_args.txt\"\n"
	if err := os.WriteFile(filepath.Join(autoDir, "generate-vault-digest.py"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	out, err := runDigest(t, vault, assetRoot, "--", "--output", "custom.md")
	if err != nil {
		t.Fatalf("digest failed: %v\n%s", err, out)
	}
	b, err := os.ReadFile(filepath.Join(vault, "_args.txt"))
	if err != nil {
		t.Fatalf("stub did not record args: %v", err)
	}
	got := string(b)
	for _, want := range []string{"--vault-root", vault, "--output", "custom.md"} {
		if !strings.Contains(got, want) {
			t.Errorf("script args %q missing %q", got, want)
		}
	}
}

func TestDigestFailsWhenScriptMissing(t *testing.T) {
	vault := t.TempDir()
	assetRoot := t.TempDir() // no automation/generate-vault-digest.py
	out, err := runDigest(t, vault, assetRoot)
	if err == nil {
		t.Fatalf("digest should fail when the script is absent:\n%s", out)
	}
}

func TestDigestFailsWhenScriptErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub interpreter is a POSIX shell script")
	}
	vault := t.TempDir()
	assetRoot := t.TempDir()
	autoDir := filepath.Join(assetRoot, "automation")
	if err := os.MkdirAll(autoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(autoDir, "generate-vault-digest.py"), []byte("#!/bin/sh\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := runDigest(t, vault, assetRoot)
	if err == nil {
		t.Fatalf("digest should exit non-zero when the script fails:\n%s", out)
	}
}
