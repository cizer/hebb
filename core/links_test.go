package core

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// newTestIndex builds an in-memory note index from a set of path->title pairs,
// the same shape loadNoteIndex produces from the notes table.
func newTestIndex(notes map[string]string) *noteIndex {
	var paths []string
	for p := range notes {
		paths = append(paths, p)
	}
	return newNoteIndex(paths, notes)
}

func TestResolveExactPath(t *testing.T) {
	ix := newTestIndex(map[string]string{"Alpha.md": "Alpha"})
	got, status := ix.resolve("Alpha")
	if status != Resolved || got != "Alpha.md" {
		t.Fatalf("resolve(Alpha) = (%q, %v), want (Alpha.md, Resolved)", got, status)
	}
}

func TestResolveExactPathWithMDSuffix(t *testing.T) {
	ix := newTestIndex(map[string]string{"Alpha.md": "Alpha"})
	got, status := ix.resolve("Alpha.md")
	if status != Resolved || got != "Alpha.md" {
		t.Fatalf("resolve(Alpha.md) = (%q, %v), want (Alpha.md, Resolved)", got, status)
	}
}

func TestResolveBasename(t *testing.T) {
	// Target has no slash and the only matching note lives in a subdirectory:
	// the final-segment match should still resolve it.
	ix := newTestIndex(map[string]string{"sub/Note.md": "Note"})
	got, status := ix.resolve("Note")
	if status != Resolved || got != "sub/Note.md" {
		t.Fatalf("resolve(Note) = (%q, %v), want (sub/Note.md, Resolved)", got, status)
	}
}

func TestResolveSubpathAnchorsToDirectory(t *testing.T) {
	// A slash-bearing target must match the note ending with target+".md", not a
	// same-named note in a different directory.
	ix := newTestIndex(map[string]string{
		"sub/Note.md":   "Note",
		"other/Note.md": "Note",
	})
	got, status := ix.resolve("sub/Note")
	if status != Resolved || got != "sub/Note.md" {
		t.Fatalf("resolve(sub/Note) = (%q, %v), want (sub/Note.md, Resolved)", got, status)
	}
}

func TestResolveTitle(t *testing.T) {
	// The target matches no path or basename but does match a note's title.
	ix := newTestIndex(map[string]string{"2024-01-01.md": "Quarterly Plan"})
	got, status := ix.resolve("Quarterly Plan")
	if status != Resolved || got != "2024-01-01.md" {
		t.Fatalf("resolve(Quarterly Plan) = (%q, %v), want (2024-01-01.md, Resolved)", got, status)
	}
}

func TestResolveStripsFragment(t *testing.T) {
	// A "#section" fragment is dropped before matching, so the note still resolves.
	ix := newTestIndex(map[string]string{"Alpha.md": "Alpha"})
	got, status := ix.resolve("Alpha#Overview")
	if status != Resolved || got != "Alpha.md" {
		t.Fatalf("resolve(Alpha#Overview) = (%q, %v), want (Alpha.md, Resolved)", got, status)
	}
}

func TestResolveAliasAlreadyDropped(t *testing.T) {
	// The parser strips the alias (text after '|'), so the resolver only ever
	// sees the pre-pipe text; "Alpha" must resolve cleanly.
	ix := newTestIndex(map[string]string{"Alpha.md": "Alpha"})
	got, status := ix.resolve("Alpha")
	if status != Resolved || got != "Alpha.md" {
		t.Fatalf("resolve(Alpha) = (%q, %v), want (Alpha.md, Resolved)", got, status)
	}
}

func TestResolveDangling(t *testing.T) {
	ix := newTestIndex(map[string]string{"Alpha.md": "Alpha"})
	got, status := ix.resolve("Ghost")
	if status != Dangling || got != "" {
		t.Fatalf("resolve(Ghost) = (%q, %v), want (\"\", Dangling)", got, status)
	}
}

func TestResolveAmbiguousBasename(t *testing.T) {
	// Two notes share the basename "Note" and the bare target has no slash to
	// disambiguate, so the result is ambiguous (NULL target_path).
	ix := newTestIndex(map[string]string{
		"sub/Note.md":   "Note A",
		"other/Note.md": "Note B",
	})
	got, status := ix.resolve("Note")
	if status != Ambiguous || got != "" {
		t.Fatalf("resolve(Note) = (%q, %v), want (\"\", Ambiguous)", got, status)
	}
}

func TestResolveAmbiguousTitle(t *testing.T) {
	// Two notes carry the same title and the target matches neither path nor
	// basename, so the title precedence is ambiguous.
	ix := newTestIndex(map[string]string{
		"a.md": "Shared Title",
		"b.md": "Shared Title",
	})
	got, status := ix.resolve("Shared Title")
	if status != Ambiguous || got != "" {
		t.Fatalf("resolve(Shared Title) = (%q, %v), want (\"\", Ambiguous)", got, status)
	}
}

func TestResolvePathBeatsTitle(t *testing.T) {
	// An exact path match for "Alpha" must win even though a different note
	// carries the title "Alpha"; precedence is path, then basename, then title.
	ix := newTestIndex(map[string]string{
		"Alpha.md": "Something Else",
		"Other.md": "Alpha",
	})
	got, status := ix.resolve("Alpha")
	if status != Resolved || got != "Alpha.md" {
		t.Fatalf("resolve(Alpha) = (%q, %v), want (Alpha.md, Resolved)", got, status)
	}
}

// TestResolveCaseInsensitiveBasename matches Obsidian: a [[foo]] link resolves
// to a note filed as "Foo.md" even though the case differs. The resolved path
// must keep the note's real on-disk case, not the link's.
func TestResolveCaseInsensitiveBasename(t *testing.T) {
	ix := newTestIndex(map[string]string{"Foo.md": "Foo"})
	got, status := ix.resolve("foo")
	if status != Resolved || got != "Foo.md" {
		t.Fatalf("resolve(foo) = (%q, %v), want (Foo.md, Resolved)", got, status)
	}
}

// TestResolveCaseInsensitiveExactPath proves the exact-path precedence is also
// case-insensitive: an uppercase target resolves to a lowercase-pathed note.
func TestResolveCaseInsensitiveExactPath(t *testing.T) {
	ix := newTestIndex(map[string]string{"sub/note.md": "Note"})
	got, status := ix.resolve("SUB/NOTE")
	if status != Resolved || got != "sub/note.md" {
		t.Fatalf("resolve(SUB/NOTE) = (%q, %v), want (sub/note.md, Resolved)", got, status)
	}
}

// TestResolveCaseInsensitiveTitle proves a [[NOTE]] link resolves to a note
// whose title is "note" (differing only in case), via the title precedence.
func TestResolveCaseInsensitiveTitle(t *testing.T) {
	ix := newTestIndex(map[string]string{"2024-01-01.md": "note"})
	got, status := ix.resolve("NOTE")
	if status != Resolved || got != "2024-01-01.md" {
		t.Fatalf("resolve(NOTE) = (%q, %v), want (2024-01-01.md, Resolved)", got, status)
	}
}

// TestResolveCaseInsensitiveAmbiguity groups matches case-insensitively: two
// notes whose basenames differ only in case fall into one bucket, so a bare
// target matching both is ambiguous (as Obsidian would treat it).
func TestResolveCaseInsensitiveAmbiguity(t *testing.T) {
	ix := newTestIndex(map[string]string{
		"one/Note.md": "Note One",
		"two/note.md": "Note Two",
	})
	got, status := ix.resolve("note")
	if status != Ambiguous || got != "" {
		t.Fatalf("resolve(note) = (%q, %v), want (\"\", Ambiguous)", got, status)
	}
}

// TestResolveCaseInsensitiveSubpathAnchor proves the directory-anchored suffix
// logic still holds case-insensitively: a slash-bearing target anchors to its
// directory even when the case differs.
func TestResolveCaseInsensitiveSubpathAnchor(t *testing.T) {
	ix := newTestIndex(map[string]string{
		"sub/Note.md":   "Note",
		"other/Note.md": "Note",
	})
	got, status := ix.resolve("SUB/note")
	if status != Resolved || got != "sub/Note.md" {
		t.Fatalf("resolve(SUB/note) = (%q, %v), want (sub/Note.md, Resolved)", got, status)
	}
}

// TestResolveCaseOnlyPathCollisionIsAmbiguous guards review finding A: on a
// case-sensitive filesystem a vault can hold two notes whose paths differ only
// in case ("Foo.md" and "foo.md"). The lowercased exact-path index must keep
// both, so a root link [[foo]] or [[FOO.md]] is reported Ambiguous rather than
// silently resolving to whichever note was loaded last. The index is built
// directly from a paths slice because a case-insensitive FS cannot hold both
// files at once.
func TestResolveCaseOnlyPathCollisionIsAmbiguous(t *testing.T) {
	ix := newNoteIndex(
		[]string{"Foo.md", "foo.md"},
		map[string]string{"Foo.md": "Foo", "foo.md": "Foo Lower"},
	)
	for _, target := range []string{"foo", "FOO.md"} {
		got, status := ix.resolve(target)
		if status != Ambiguous || got != "" {
			t.Errorf("resolve(%q) = (%q, %v), want (\"\", Ambiguous)", target, got, status)
		}
	}
}

// TestFullReindexResolvesCaseMismatchedLink is the end-to-end guard for the
// case-insensitivity fix: a vault link [[foo]] to a note filed as Foo.md must
// resolve through a full reindex (target_path = Foo.md), where before the fix it
// stayed NULL.
func TestFullReindexResolvesCaseMismatchedLink(t *testing.T) {
	vault := t.TempDir()
	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(vault, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Foo.md", "# Foo\n\nThe target note.")
	write("Beta.md", "# Beta\n\nLinks to [[foo]] in lowercase.")

	db := reindexedDB(t, vault)
	defer db.Close()

	got := targetPathOf(t, db, "Beta.md", "foo")
	if !got.Valid || got.String != "Foo.md" {
		t.Fatalf("target_path for case-mismatched [[foo]] = %#v, want Foo.md", got)
	}
}

// TestResolveTargetDBCaseInsensitive proves the per-call DB resolver
// (ResolveTargetDB, used by the context walk) is also case-insensitive, so the
// in-memory and DB paths agree.
func TestResolveTargetDBCaseInsensitive(t *testing.T) {
	vault := t.TempDir()
	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(vault, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Foo.md", "# Foo\n\nThe target.")

	db := reindexedDB(t, vault)
	defer db.Close()

	got, status := ResolveTargetDB(db, "foo")
	if status != Resolved || got != "Foo.md" {
		t.Fatalf("ResolveTargetDB(foo) = (%q, %v), want (Foo.md, Resolved)", got, status)
	}
}

// TestResolveTargetDBCaseInsensitiveFullPath guards review finding B for the
// per-call DB resolver: a slash-bearing target whose case differs from the note
// path, and which already carries a ".md" suffix ("SUB/NOTE.md" for a note at
// "sub/Note.md"), must resolve. Before the fix the lowercased target gained a
// second ".md" in the basename anchored-suffix check ("sub/note.md.md"), so it
// missed; the exact-path stage must also match case-insensitively.
func TestResolveTargetDBCaseInsensitiveFullPath(t *testing.T) {
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
	write("sub/Note.md", "# Note\n\nThe target note.")

	db := reindexedDB(t, vault)
	defer db.Close()

	// Full-path, case-mismatched, with the explicit ".md" suffix: must resolve to
	// the real on-disk path and must not suffer a "sub/note.md.md" miss.
	if got, status := ResolveTargetDB(db, "SUB/NOTE.md"); status != Resolved || got != "sub/Note.md" {
		t.Fatalf("ResolveTargetDB(SUB/NOTE.md) = (%q, %v), want (sub/Note.md, Resolved)", got, status)
	}
	// A bare basename in a different case still resolves via the basename stage.
	if got, status := ResolveTargetDB(db, "NOTE"); status != Resolved || got != "sub/Note.md" {
		t.Fatalf("ResolveTargetDB(NOTE) = (%q, %v), want (sub/Note.md, Resolved)", got, status)
	}
}

// TestResolveTargetDBSubpathAnchors proves the DB resolver still anchors a
// slash-bearing target to its directory after the trailing-.md normalisation:
// [[dir/Note]] resolves to the note in that directory, not a same-named note
// elsewhere.
func TestResolveTargetDBSubpathAnchors(t *testing.T) {
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
	write("dir/Note.md", "# Note\n\nThe anchored target.")
	write("other/Note.md", "# Note\n\nA same-named note elsewhere.")

	db := reindexedDB(t, vault)
	defer db.Close()

	if got, status := ResolveTargetDB(db, "dir/Note"); status != Resolved || got != "dir/Note.md" {
		t.Fatalf("ResolveTargetDB(dir/Note) = (%q, %v), want (dir/Note.md, Resolved)", got, status)
	}
}

// TestFullReindexResolvesForwardReference guards the ordering correctness
// requirement: a file that links to a note parsed later in the walk must still
// get its target_path resolved, because resolution happens in a second pass
// after the whole notes set is written.
func TestFullReindexResolvesForwardReference(t *testing.T) {
	vault := t.TempDir()
	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(vault, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// "AAA" sorts before "ZZZ", so AAA's links are processed before ZZZ exists
	// in the transaction. Only a post-walk resolution pass can link them.
	write("AAA.md", "# AAA\n\nLinks forward to [[ZZZ]].")
	write("ZZZ.md", "# ZZZ\n\nThe later note.")

	db := reindexedDB(t, vault)
	defer db.Close()

	got := targetPathOf(t, db, "AAA.md", "ZZZ")
	if !got.Valid || got.String != "ZZZ.md" {
		t.Fatalf("target_path for AAA->ZZZ = %#v, want ZZZ.md", got)
	}
}

// TestFullReindexDanglingIsNull verifies a link to a non-existent note stores a
// NULL target_path.
func TestFullReindexDanglingIsNull(t *testing.T) {
	vault := t.TempDir()
	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(vault, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Beta.md", "# Beta\n\nLinks to [[Ghost]] which does not exist.")

	db := reindexedDB(t, vault)
	defer db.Close()

	got := targetPathOf(t, db, "Beta.md", "Ghost")
	if got.Valid {
		t.Fatalf("dangling target_path = %q, want NULL", got.String)
	}
}

// TestFullReindexAmbiguousIsNull verifies a target matching two notes by
// basename stores NULL (ambiguity is not resolved to one note).
func TestFullReindexAmbiguousIsNull(t *testing.T) {
	vault := t.TempDir()
	write := func(rel, content string) {
		dir := filepath.Dir(filepath.Join(vault, rel))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(vault, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Source.md", "# Source\n\nLinks ambiguously to [[Note]].")
	write("sub/Note.md", "# Note A\n\nFirst.")
	write("other/Note.md", "# Note B\n\nSecond.")

	db := reindexedDB(t, vault)
	defer db.Close()

	got := targetPathOf(t, db, "Source.md", "Note")
	if got.Valid {
		t.Fatalf("ambiguous target_path = %q, want NULL", got.String)
	}
}

// TestFullReindexFragmentResolves verifies a "#section" target resolves to the
// note end-to-end through the indexer (the raw target keeps the fragment, but
// target_path resolves it).
func TestFullReindexFragmentResolves(t *testing.T) {
	vault := t.TempDir()
	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(vault, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Alpha.md", "# Alpha\n\nThe alpha note.")
	write("Beta.md", "# Beta\n\nSee [[Alpha#Overview]].")

	db := reindexedDB(t, vault)
	defer db.Close()

	// The raw target keeps the fragment.
	got := targetPathOf(t, db, "Beta.md", "Alpha#Overview")
	if !got.Valid || got.String != "Alpha.md" {
		t.Fatalf("target_path for Alpha#Overview = %#v, want Alpha.md", got)
	}
}

// TestIndexFileResolvesAtWriteTime verifies the incremental path (IndexFile)
// resolves target_path against the live notes table.
func TestIndexFileResolvesAtWriteTime(t *testing.T) {
	vault := t.TempDir()
	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(vault, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Alpha.md", "# Alpha\n\nThe alpha note.")
	write("Beta.md", "# Beta\n\nLinks to [[Alpha]].")

	cfg := Config{VaultPath: vault, DBPath: filepath.Join(vault, ".hebb", "index.db"), ExcludeDirs: defaultExcludeDirs}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Index Alpha first so it is present, then Beta resolves against it.
	if err := IndexFile(cfg, db, "Alpha.md"); err != nil {
		t.Fatal(err)
	}
	if err := IndexFile(cfg, db, "Beta.md"); err != nil {
		t.Fatal(err)
	}

	got := targetPathOf(t, db, "Beta.md", "Alpha")
	if !got.Valid || got.String != "Alpha.md" {
		t.Fatalf("IndexFile target_path for Beta->Alpha = %#v, want Alpha.md", got)
	}
}

// TestIndexFileReResolvesInboundLinks guards the incremental-path correctness
// requirement: when a note is indexed after a file that already links to it,
// indexing the target must re-resolve the inbound link that was dangling.
// note-a.md links to [[Note B]] but is indexed while note-b.md is still absent,
// so the link starts dangling (NULL). Indexing note-b.md (H1 "# Note B") must
// flip note-a's link to point at note-b.md without any full reindex.
func TestIndexFileReResolvesInboundLinks(t *testing.T) {
	vault := t.TempDir()
	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(vault, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("note-a.md", "# Note A\n\nSee [[Note B]] and [[Nonexistent]].")
	write("note-b.md", "# Note B\n\nThe target note.")

	cfg := Config{VaultPath: vault, DBPath: filepath.Join(vault, ".hebb", "index.db"), ExcludeDirs: defaultExcludeDirs}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Index note-a.md first, while note-b.md is not yet in the index: the link
	// to [[Note B]] cannot resolve and must be stored dangling (NULL).
	if err := IndexFile(cfg, db, "note-a.md"); err != nil {
		t.Fatal(err)
	}
	if got := targetPathOf(t, db, "note-a.md", "Note B"); got.Valid {
		t.Fatalf("before note-b indexed, target_path = %q, want NULL (dangling)", got.String)
	}

	// Now index note-b.md. IndexFile must re-resolve inbound links, so note-a's
	// [[Note B]] now points at note-b.md.
	if err := IndexFile(cfg, db, "note-b.md"); err != nil {
		t.Fatal(err)
	}
	if got := targetPathOf(t, db, "note-a.md", "Note B"); !got.Valid || got.String != "note-b.md" {
		t.Fatalf("after note-b indexed, target_path for note-a->[[Note B]] = %#v, want note-b.md", got)
	}
	// The genuinely dangling link must stay NULL: re-resolution must not fabricate
	// a match for a target with no note.
	if got := targetPathOf(t, db, "note-a.md", "Nonexistent"); got.Valid {
		t.Fatalf("genuinely dangling target_path = %q, want NULL", got.String)
	}
}

// TestRefreshChangedColdBuildResolvesForwardLink guards the cold-start scenario:
// a fresh vault with no index.db, refreshed once via RefreshChanged. Files are
// indexed one at a time, so [[Note B]] in a file indexed before note-b.md would
// resolve to dangling and, without inbound re-resolution, stay NULL forever.
// After a single RefreshChanged the link must be resolved.
func TestRefreshChangedColdBuildResolvesForwardLink(t *testing.T) {
	for _, names := range [][2]string{
		{"note-a.md", "note-b.md"}, // source enumerated before target
		{"note-z.md", "note-a.md"}, // target enumerated before source
	} {
		source, target := names[0], names[1]
		t.Run(source+"_links_"+target, func(t *testing.T) {
			vault := t.TempDir()
			write := func(rel, content string) {
				if err := os.WriteFile(filepath.Join(vault, rel), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			// The source links to the target by title "Note B"; the target carries
			// that H1 title. Ordering of the two files in the walk varies by name.
			write(source, "# Source\n\nSee [[Note B]] and [[Nonexistent]].")
			write(target, "# Note B\n\nThe target note.")

			cfg := Config{VaultPath: vault, DBPath: filepath.Join(vault, ".hebb", "index.db"), ExcludeDirs: defaultExcludeDirs, AutoRefresh: true}
			if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
				t.Fatal(err)
			}
			db, err := OpenDB(cfg.DBPath)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()

			// Cold build: empty index, one RefreshChanged pass.
			if _, err := RefreshChanged(cfg, db); err != nil {
				t.Fatal(err)
			}

			if got := targetPathOf(t, db, source, "Note B"); !got.Valid || got.String != target {
				t.Fatalf("cold build target_path for %s->[[Note B]] = %#v, want %s", source, got, target)
			}
			// Genuinely dangling stays NULL after the cold build.
			if got := targetPathOf(t, db, source, "Nonexistent"); got.Valid {
				t.Fatalf("genuinely dangling target_path = %q, want NULL", got.String)
			}
		})
	}
}

// TestMigrationAddsTargetPathToLegacyDB verifies an index.db created with the
// pre-Phase-0 links schema (no target_path column) is upgraded in place by
// OpenDB, without a forced rebuild, and that an existing link row survives.
func TestMigrationAddsTargetPathToLegacyDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "index.db")

	// Build a legacy database by hand: the old links table had no target_path.
	legacy, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`CREATE TABLE links (
		source_path TEXT NOT NULL,
		target TEXT NOT NULL,
		PRIMARY KEY (source_path, target)
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`INSERT INTO links (source_path, target) VALUES ('Beta.md', 'Alpha')`); err != nil {
		t.Fatal(err)
	}
	legacy.Close()

	// OpenDB must add the column (and index) without erroring on the existing table.
	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB on legacy db: %v", err)
	}
	defer db.Close()

	// The column now exists and the legacy row is intact with a NULL target_path.
	got := targetPathOf(t, db, "Beta.md", "Alpha")
	if got.Valid {
		t.Fatalf("migrated legacy row target_path = %q, want NULL", got.String)
	}
}

// TestNoteKeysIncludesDirectorySuffixes guards review finding B at the unit
// level: a note at "x/dir/Note.md" must yield every directory-suffix form the
// resolver could match, so inbound re-resolution considers a "[[dir/Note]]"
// link, not only "x/dir/Note" and "Note".
func TestNoteKeysIncludesDirectorySuffixes(t *testing.T) {
	got := map[string]bool{}
	for _, k := range noteKeys("x/dir/Note.md", "Note Title") {
		got[k] = true
	}
	for _, want := range []string{"x/dir/Note.md", "x/dir/Note", "dir/Note", "Note", "Note Title"} {
		if !got[want] {
			t.Errorf("noteKeys missing %q; got %v", want, got)
		}
	}
}

// TestIndexFileReResolvesDirectoryAnchoredInbound is review finding B end to end
// on the incremental path: a file links [[dir/Note]] before the target exists,
// so the link starts dangling. Indexing x/dir/Note.md must re-resolve it (the
// resolver's basename stage accepts a path ending "/dir/Note.md"), matching what
// FullReindex would do.
func TestIndexFileReResolvesDirectoryAnchoredInbound(t *testing.T) {
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
	write("Source.md", "# Source\n\nLinks to [[dir/Note]].")
	write("x/dir/Note.md", "# Some Heading\n\nThe target.")

	cfg := Config{VaultPath: vault, DBPath: filepath.Join(vault, ".hebb", "index.db"), ExcludeDirs: defaultExcludeDirs}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Index the source first, while the target is absent: dangling (NULL).
	if err := IndexFile(cfg, db, "Source.md"); err != nil {
		t.Fatal(err)
	}
	if got := targetPathOf(t, db, "Source.md", "dir/Note"); got.Valid {
		t.Fatalf("before target indexed, target_path = %q, want NULL", got.String)
	}
	// Index the directory-anchored target: the inbound link must now resolve.
	if err := IndexFile(cfg, db, "x/dir/Note.md"); err != nil {
		t.Fatal(err)
	}
	if got := targetPathOf(t, db, "Source.md", "dir/Note"); !got.Valid || got.String != "x/dir/Note.md" {
		t.Fatalf("incremental target_path for [[dir/Note]] = %#v, want x/dir/Note.md", got)
	}

	// FullReindex must agree (the invariant the finding is about).
	if _, err := FullReindexForce(cfg, db); err != nil {
		t.Fatal(err)
	}
	if got := targetPathOf(t, db, "Source.md", "dir/Note"); !got.Valid || got.String != "x/dir/Note.md" {
		t.Fatalf("FullReindex target_path for [[dir/Note]] = %#v, want x/dir/Note.md", got)
	}
}

// TestIndexFileFlipsToAmbiguousOnSecondNote is review finding C(i): A links
// [[Note]] and Note.md is indexed and resolved; adding a second note that also
// matches "Note" must flip the link to NULL (ambiguous) on the incremental path,
// not leave the stale pointer at the first note.
func TestIndexFileFlipsToAmbiguousOnSecondNote(t *testing.T) {
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
	write("A.md", "# A\n\nLinks to [[Note]].")
	write("one/Note.md", "# Note One\n\nFirst target.")

	cfg := Config{VaultPath: vault, DBPath: filepath.Join(vault, ".hebb", "index.db"), ExcludeDirs: defaultExcludeDirs}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := IndexFile(cfg, db, "A.md"); err != nil {
		t.Fatal(err)
	}
	if err := IndexFile(cfg, db, "one/Note.md"); err != nil {
		t.Fatal(err)
	}
	// Resolved to the single matching note.
	if got := targetPathOf(t, db, "A.md", "Note"); !got.Valid || got.String != "one/Note.md" {
		t.Fatalf("after first Note indexed, target_path = %#v, want one/Note.md", got)
	}

	// A second note also matching the basename "Note" appears: the link is now
	// ambiguous and must flip to NULL on the incremental path.
	write("two/Note.md", "# Note Two\n\nSecond target.")
	if err := IndexFile(cfg, db, "two/Note.md"); err != nil {
		t.Fatal(err)
	}
	if got := targetPathOf(t, db, "A.md", "Note"); got.Valid {
		t.Fatalf("after second Note indexed, target_path = %q, want NULL (ambiguous)", got.String)
	}

	// FullReindex must agree.
	if _, err := FullReindexForce(cfg, db); err != nil {
		t.Fatal(err)
	}
	if got := targetPathOf(t, db, "A.md", "Note"); got.Valid {
		t.Fatalf("FullReindex target_path = %q, want NULL (ambiguous)", got.String)
	}
}

// TestRemoveFileDanglesPreviouslyResolvedLink is review finding C(ii): a link
// resolved to a target note must fall back to dangling (NULL), not keep a stale
// non-NULL pointer, when that target note is removed on the incremental path.
func TestRemoveFileDanglesPreviouslyResolvedLink(t *testing.T) {
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
	write("A.md", "# A\n\nLinks to [[Target]].")
	write("Target.md", "# Target\n\nThe target note.")

	cfg := Config{VaultPath: vault, DBPath: filepath.Join(vault, ".hebb", "index.db"), ExcludeDirs: defaultExcludeDirs}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := IndexFile(cfg, db, "A.md"); err != nil {
		t.Fatal(err)
	}
	if err := IndexFile(cfg, db, "Target.md"); err != nil {
		t.Fatal(err)
	}
	if got := targetPathOf(t, db, "A.md", "Target"); !got.Valid || got.String != "Target.md" {
		t.Fatalf("before removal, target_path = %#v, want Target.md", got)
	}

	// Remove the target note: the inbound link must fall back to dangling (NULL).
	if err := RemoveFile(db, "Target.md"); err != nil {
		t.Fatal(err)
	}
	if got := targetPathOf(t, db, "A.md", "Target"); got.Valid {
		t.Fatalf("after removal, target_path = %q, want NULL (dangling)", got.String)
	}
}

// TestRemoveFileFallsBackToOtherNote checks the convergence case where removing
// one of two same-named notes flips an ambiguous link to the single survivor,
// matching FullReindex.
func TestRemoveFileFallsBackToOtherNote(t *testing.T) {
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
	write("A.md", "# A\n\nLinks to [[Note]].")
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

	for _, rel := range []string{"A.md", "one/Note.md", "two/Note.md"} {
		if err := IndexFile(cfg, db, rel); err != nil {
			t.Fatal(err)
		}
	}
	// Ambiguous: NULL.
	if got := targetPathOf(t, db, "A.md", "Note"); got.Valid {
		t.Fatalf("with two Notes, target_path = %q, want NULL", got.String)
	}
	// Remove one: the link must resolve to the survivor.
	if err := RemoveFile(db, "two/Note.md"); err != nil {
		t.Fatal(err)
	}
	if got := targetPathOf(t, db, "A.md", "Note"); !got.Valid || got.String != "one/Note.md" {
		t.Fatalf("after removing one Note, target_path = %#v, want one/Note.md", got)
	}
}

// TestMigrationBackfillsLegacyLinks is review finding A: a legacy index.db that
// has a populated notes table and a links table missing target_path must, on the
// upgrade open, both add the column AND backfill every existing link to its
// resolved path, so an unchanged vault is correct without a manual full reindex.
func TestMigrationBackfillsLegacyLinks(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "index.db")

	// Build a legacy-shaped database by hand: notes table populated, links table
	// without target_path. This is the pre-Phase-0 schema with real content.
	legacy, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`CREATE TABLE notes (
		path TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		body TEXT NOT NULL,
		tags TEXT,
		frontmatter TEXT,
		mtime REAL NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`CREATE TABLE links (
		source_path TEXT NOT NULL,
		target TEXT NOT NULL,
		PRIMARY KEY (source_path, target)
	)`); err != nil {
		t.Fatal(err)
	}
	for _, n := range []struct{ path, title string }{
		{"Alpha.md", "Alpha"},
		{"sub/Gamma.md", "Gamma"},
		{"one/Note.md", "Note One"},
		{"two/Note.md", "Note Two"},
		{"Beta.md", "Beta"},
	} {
		if _, err := legacy.Exec(`INSERT INTO notes (path, title, body, tags, frontmatter, mtime) VALUES (?, ?, '', '', '', 0)`, n.path, n.title); err != nil {
			t.Fatal(err)
		}
	}
	// Beta links: a resolvable exact-path link, a resolvable directory-anchored
	// link, an ambiguous basename link, and a genuinely dangling link.
	for _, target := range []string{"Alpha", "sub/Gamma", "Note", "Ghost"} {
		if _, err := legacy.Exec(`INSERT INTO links (source_path, target) VALUES ('Beta.md', ?)`, target); err != nil {
			t.Fatal(err)
		}
	}
	legacy.Close()

	// OpenDB must add the column AND backfill resolutions in one upgrade pass.
	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB on legacy db: %v", err)
	}
	defer db.Close()

	if got := targetPathOf(t, db, "Beta.md", "Alpha"); !got.Valid || got.String != "Alpha.md" {
		t.Errorf("backfill Alpha -> %#v, want Alpha.md", got)
	}
	if got := targetPathOf(t, db, "Beta.md", "sub/Gamma"); !got.Valid || got.String != "sub/Gamma.md" {
		t.Errorf("backfill sub/Gamma -> %#v, want sub/Gamma.md", got)
	}
	if got := targetPathOf(t, db, "Beta.md", "Note"); got.Valid {
		t.Errorf("backfill ambiguous Note -> %q, want NULL", got.String)
	}
	if got := targetPathOf(t, db, "Beta.md", "Ghost"); got.Valid {
		t.Errorf("backfill dangling Ghost -> %q, want NULL", got.String)
	}
}

// TestMigrationBackfillDoesNotRunOnFreshDB guards the gate on review finding A:
// a fresh database already has the column from schemaSQL, so the backfill pass
// must not run there. A fresh OpenDB on an empty path must leave an empty links
// table (no error, nothing to backfill).
func TestMigrationBackfillDoesNotRunOnFreshDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")
	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB on fresh db: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM links").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("fresh db links count = %d, want 0", n)
	}
	// Reopening must also succeed (the backfill must not run on a later open).
	db.Close()
	db2, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	db2.Close()
}

// reindexedDB opens a fresh index.db under the vault and runs a full reindex,
// returning the open handle for the caller to query and close.
func reindexedDB(t *testing.T, vault string) *sql.DB {
	t.Helper()
	cfg := Config{VaultPath: vault, DBPath: filepath.Join(vault, ".hebb", "index.db"), ExcludeDirs: defaultExcludeDirs}
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
	return db
}

// targetPathOf reads the stored target_path for one (source_path, target) link.
func targetPathOf(t *testing.T, db *sql.DB, source, target string) sql.NullString {
	t.Helper()
	var tp sql.NullString
	err := db.QueryRow("SELECT target_path FROM links WHERE source_path = ? AND target = ?", source, target).Scan(&tp)
	if err != nil {
		t.Fatalf("reading target_path for %s -> %s: %v", source, target, err)
	}
	return tp
}
