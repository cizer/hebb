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

// runThemes executes `hebb themes` against a vault and returns combined
// stdout/stderr plus any execution error.
func runThemes(t *testing.T, vault string, extra ...string) (string, error) {
	t.Helper()
	root := newRoot("test")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(append([]string{"themes", "--vault", vault}, extra...))
	err := root.Execute()
	return buf.String(), err
}

// buildThemesVaultCLI writes a vault whose notes all changed "now" (fresh
// writes), with links and tags that populate every report section, plus an
// underscore machine note that must stay invisible. Returns the vault path.
func buildThemesVaultCLI(t *testing.T) string {
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

	write("Notes/Hub.md", "# Hub\n\nThe recurring topic.\n")
	write("Notes/Sidekick.md", "# Sidekick\n\nOften linked with Hub.\n")
	write("Notes/Writer1.md", "---\ntags: [momentum-tag]\n---\n\n# Writer1\n\n[[Hub]] and [[Sidekick]].\n")
	write("Notes/Writer2.md", "---\ntags: [momentum-tag]\n---\n\n# Writer2\n\nJust [[Hub]].\n")
	// Machine note: excluded from the report as source and target.
	write("Notes/_INGEST-LOG.md", "# Ingest log\n\n[[Hub]]\n")

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

func TestThemesCommandText(t *testing.T) {
	vault := buildThemesVaultCLI(t)

	out, err := runThemes(t, vault)
	if err != nil {
		t.Fatalf("hebb themes: %v\n%s", err, out)
	}

	// Header line: window and changed-note count. All five notes were written
	// moments ago, but the machine note is excluded, so 4 count.
	if !strings.Contains(out, "last 30 days, 4 notes changed") {
		t.Errorf("themes header missing window and note count:\n%s", out)
	}
	// Three titled sections.
	for _, section := range []string{"Link targets", "Tag momentum", "Co-linked pairs"} {
		if !strings.Contains(out, section) {
			t.Errorf("themes output missing section %q:\n%s", section, out)
		}
	}
	// Hub is the top target: 2 links from 2 notes (the machine note's link is
	// not counted).
	if !strings.Contains(out, "2 link(s) from 2 note(s)") {
		t.Errorf("themes output missing Hub's link-target row:\n%s", out)
	}
	// The tag appears with rising momentum (nothing in the previous window).
	if !strings.Contains(out, "momentum-tag") || !strings.Contains(out, "rising (+2)") {
		t.Errorf("themes output missing tag momentum row:\n%s", out)
	}
	// Writer1 links Hub and Sidekick together.
	if !strings.Contains(out, "Hub + Sidekick") || !strings.Contains(out, "1 shared note(s)") {
		t.Errorf("themes output missing co-linked pair row:\n%s", out)
	}
	// The machine note never surfaces.
	if strings.Contains(out, "_INGEST-LOG") {
		t.Errorf("underscore machine note must not appear in themes output:\n%s", out)
	}
}

func TestThemesCommandDaysFlag(t *testing.T) {
	vault := buildThemesVaultCLI(t)

	out, err := runThemes(t, vault, "--days", "14")
	if err != nil {
		t.Fatalf("hebb themes --days 14: %v\n%s", err, out)
	}
	if !strings.Contains(out, "last 14 days") {
		t.Errorf("themes --days 14 header should state the 14-day window:\n%s", out)
	}
}

// TestThemesCommandJSON pins the machine-readable shape: a single object with
// days, window_start, notes_changed and the three section arrays, decodable
// as core.ThemesReport, arrays never null.
func TestThemesCommandJSON(t *testing.T) {
	vault := buildThemesVaultCLI(t)

	out, err := runThemes(t, vault, "--json")
	if err != nil {
		t.Fatalf("hebb themes --json: %v\n%s", err, out)
	}

	var report core.ThemesReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("themes --json output is not a valid ThemesReport: %v\n%s", err, out)
	}
	if report.Days != 30 {
		t.Errorf("json days = %d, want 30", report.Days)
	}
	if report.NotesChanged != 4 {
		t.Errorf("json notes_changed = %d, want 4", report.NotesChanged)
	}
	if len(report.LinkTargets) == 0 || report.LinkTargets[0].Path != "Notes/Hub.md" {
		t.Errorf("json link_targets top row = %+v, want Notes/Hub.md first", report.LinkTargets)
	}
	if len(report.TagMomentum) != 1 || report.TagMomentum[0].Tag != "momentum-tag" || report.TagMomentum[0].Delta != 2 {
		t.Errorf("json tag_momentum = %+v, want [momentum-tag delta +2]", report.TagMomentum)
	}
	if len(report.CoOccurrence) != 1 || report.CoOccurrence[0].SharedSources != 1 {
		t.Errorf("json co_occurrence = %+v, want one pair with 1 shared source", report.CoOccurrence)
	}

	// The raw JSON must use the stable lowercase field names.
	for _, key := range []string{`"days"`, `"window_start"`, `"notes_changed"`, `"link_targets"`, `"tag_momentum"`, `"co_occurrence"`} {
		if !strings.Contains(out, key) {
			t.Errorf("themes --json missing field %s:\n%s", key, out)
		}
	}
}

// TestThemesCommandEmptyVault proves an empty vault gets the friendly
// one-liner and exit 0, not an error or three empty sections.
func TestThemesCommandEmptyVault(t *testing.T) {
	vault := t.TempDir()
	if err := core.DefaultVaultConfig("empty").Save(vault); err != nil {
		t.Fatal(err)
	}
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

	out, runErr := runThemes(t, vault)
	if runErr != nil {
		t.Fatalf("hebb themes on an empty vault must exit 0, got: %v\n%s", runErr, out)
	}
	if !strings.Contains(out, "no notes changed in the last 30 days") {
		t.Errorf("empty vault should print the friendly one-liner:\n%s", out)
	}
	if strings.Contains(out, "Link targets") {
		t.Errorf("empty vault must not print empty sections:\n%s", out)
	}
}

// TestThemesCommandEmptyVaultJSON proves --json still emits a decodable
// report (with [] not null) when nothing changed.
func TestThemesCommandEmptyVaultJSON(t *testing.T) {
	vault := t.TempDir()
	if err := core.DefaultVaultConfig("empty").Save(vault); err != nil {
		t.Fatal(err)
	}
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

	out, runErr := runThemes(t, vault, "--json")
	if runErr != nil {
		t.Fatalf("hebb themes --json on an empty vault: %v\n%s", runErr, out)
	}
	if strings.Contains(out, "null") {
		t.Errorf("empty report arrays must serialise as [], not null:\n%s", out)
	}
	var report core.ThemesReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("empty-vault --json is not a valid ThemesReport: %v\n%s", err, out)
	}
	if report.NotesChanged != 0 {
		t.Errorf("empty vault notes_changed = %d, want 0", report.NotesChanged)
	}
}
