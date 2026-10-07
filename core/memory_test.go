package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// memoryNote renders a Claude Code auto-memory file: YAML frontmatter (name,
// description, type) over a short body, the shape that lands in .hebb/memory
// through the memory link hebb install creates.
func memoryNote(name, body string) string {
	return "---\nname: " + name + "\ndescription: synthetic test memory\ntype: reference\n---\n" + body + "\n"
}

// TestMemoryIsSearchableUnderItsPrefix is the regression test for agent memory
// being invisible to search. .hebb is in exclude_dirs (the default, and the
// reporting vault's config) and the walk skipped it whole, so .hebb/memory never
// reached the index: searches missed it and a reindex after Claude wrote a
// memory note reported 0 notes. Memory must be indexed under the .hebb/memory/
// prefix, while the rest of .hebb and a nested vault's memory stay out.
func TestMemoryIsSearchableUnderItsPrefix(t *testing.T) {
	cfg, db, write := digestFixture(t)
	write("2-Areas/Note.md", "# Note\n\nordinary vault content")
	write(".hebb/memory/reference_raquel_disambiguation.md", memoryNote("Raquel disambiguation", "Raquel (team A) is not Raquel (team B)."))
	write(".hebb/stray.md", "# Stray\n\nstrayhebbtoken")
	write(".hebb/cache/c.md", "# Cache\n\nstrayhebbtoken")
	write("sub/.hebb/memory/nested.md", memoryNote("Nested", "strayhebbtoken in a nested vault's memory"))

	res, err := FullReindex(cfg, db)
	if err != nil {
		t.Fatal(err)
	}
	if res.Indexed != 2 {
		t.Fatalf("indexed = %d, want 2 (the vault note and the memory note)", res.Indexed)
	}
	for _, prefix := range []string{"", ".hebb/memory", ".hebb/memory/"} {
		hits, err := Search(db, "Raquel", 10, "", prefix)
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) != 1 || hits[0].Path != ".hebb/memory/reference_raquel_disambiguation.md" {
			t.Errorf("search Raquel (path prefix %q) = %+v, want the memory note", prefix, hits)
		}
	}
	if hits, _ := Search(db, "strayhebbtoken", 10, "", ""); len(hits) != 0 {
		t.Errorf("markdown elsewhere under .hebb (or a nested vault's memory) leaked into the index: %+v", hits)
	}

	// A memory note written after the index is picked up by the next reindex,
	// the case that used to report "indexed 0 notes".
	write(".hebb/memory/project_new.md", memoryNote("New", "Quarterly zebrafish review."))
	if res, err = FullReindex(cfg, db); err != nil {
		t.Fatal(err)
	}
	if res.Indexed != 1 {
		t.Errorf("reindex after a new memory note: indexed = %d, want 1", res.Indexed)
	}
	if hits, _ := Search(db, "zebrafish", 10, "", ".hebb/memory/"); len(hits) != 1 {
		t.Errorf("new memory note not searchable after reindex: %+v", hits)
	}
}

// TestMemoryStaysOutOfTheNoteGraph proves memory is indexed for search only and
// changes nothing else. Claude Code's memory index, MEMORY.md, collides
// case-insensitively with a vault note named Memory: as a link target it would
// turn every [[Memory]] in the vault ambiguous. A short memory note would read
// as a stub, and isolated memory nodes would inflate the graph's component
// count. Memory must also stay out of the themes report and the digest.
func TestMemoryStaysOutOfTheNoteGraph(t *testing.T) {
	cfg, db, write := digestFixture(t)
	write("2-Areas/Memory.md", "# Memory\n\nA vault note about memory techniques. Links back to [[Plan]].")
	write("1-Projects/Plan.md", "# Plan\n\nThe plan links [[Memory]].")
	write(".hebb/memory/MEMORY.md", "- [Plan feedback](feedback_plan.md) - see [[Plan]]\n")
	write(".hebb/memory/feedback_plan.md", memoryNote("Plan feedback", "Short. See [[Plan]]. #planning"))
	if _, err := FullReindex(cfg, db); err != nil {
		t.Fatal(err)
	}

	if tp := targetPathOf(t, db, "1-Projects/Plan.md", "Memory"); !tp.Valid || tp.String != "2-Areas/Memory.md" {
		t.Errorf("[[Memory]] target_path = %v, want 2-Areas/Memory.md (memory must not make it ambiguous)", tp)
	}
	assertNoMemoryLinks := func(when string) {
		t.Helper()
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM links WHERE source_path LIKE '.hebb/memory/%'").Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%s: memory notes recorded %d links, want 0", when, n)
		}
	}
	assertNoMemoryLinks("full reindex")
	// The per-call resolver behind expand_context agrees: memory is neither a
	// candidate for a vault name nor a seed of its own.
	if p, st := ResolveTargetDB(db, "memory", ""); st != Resolved || p != "2-Areas/Memory.md" {
		t.Errorf("ResolveTargetDB(memory) = (%q, %v), want (2-Areas/Memory.md, Resolved)", p, st)
	}
	if p, st := ResolveTargetDB(db, ".hebb/memory/feedback_plan.md", ""); st == Resolved {
		t.Errorf("ResolveTargetDB resolved a memory note (%q); memory is not a context seed", p)
	}

	h, err := RunHealthFull(cfg, db, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range h.Findings {
		if IsMemoryNote(f.Path) || f.Type == "ambiguous_link" {
			t.Errorf("audit finding caused by memory: %+v", f)
		}
	}
	if h.Stats.NodeCount != 2 || h.Stats.ComponentCount != 1 {
		t.Errorf("graph stats = %d nodes in %d components, want 2 in 1 (vault notes only)", h.Stats.NodeCount, h.Stats.ComponentCount)
	}

	themes, err := Themes(db, cfg, 30, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, tm := range themes.TagMomentum {
		if tm.Tag == "planning" {
			t.Errorf("themes counted a tag carried only by a memory note: %+v", tm)
		}
	}
	changed, err := changedNotesSince(db, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range changed {
		if IsMemoryNote(n.path) {
			t.Errorf("digest reported memory note %s as vault activity", n.path)
		}
	}

	// The incremental path (watcher, read-time refresh) agrees: a memory note
	// that arrives later named like a vault note leaves vault links alone.
	write(".hebb/memory/Plan.md", memoryNote("Plan", "A memory named like a vault note, citing [[Memory]]."))
	if err := IndexFile(cfg, db, ".hebb/memory/Plan.md"); err != nil {
		t.Fatal(err)
	}
	assertNoMemoryLinks("incremental index")
	write("2-Areas/Ref.md", "# Ref\n\nLinks [[Plan]] from the vault.")
	if err := IndexFile(cfg, db, "2-Areas/Ref.md"); err != nil {
		t.Fatal(err)
	}
	if tp := targetPathOf(t, db, "2-Areas/Ref.md", "Plan"); !tp.Valid || tp.String != "1-Projects/Plan.md" {
		t.Errorf("[[Plan]] after a same-named memory note = %v, want 1-Projects/Plan.md", tp)
	}
	if tp := targetPathOf(t, db, "2-Areas/Memory.md", "Plan"); !tp.Valid || tp.String != "1-Projects/Plan.md" {
		t.Errorf("existing [[Plan]] after a same-named memory note = %v, want 1-Projects/Plan.md", tp)
	}
}

// TestMemoryIndexingFollowsExcludeDirs covers the walk rules for the memory
// root: listing "memory" in exclude_dirs opts out (and drops memory already
// indexed), and a vault that does not exclude .hebb at all still lists each
// memory note exactly once rather than walking it twice.
func TestMemoryIndexingFollowsExcludeDirs(t *testing.T) {
	cfg, db, write := digestFixture(t)
	write(".hebb/memory/m.md", memoryNote("M", "okapi memory"))
	if _, err := FullReindex(cfg, db); err != nil {
		t.Fatal(err)
	}
	if hits, _ := Search(db, "okapi", 10, "", ""); len(hits) != 1 {
		t.Fatalf("memory not indexed under the default excludes: %+v", hits)
	}

	cfg.ExcludeDirs = append(DefaultExcludeDirs(), "memory")
	if MemoryIndexed(cfg.ExcludeDirs) {
		t.Error(`MemoryIndexed = true with "memory" in exclude_dirs`)
	}
	res, err := FullReindex(cfg, db)
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 1 {
		t.Errorf("opting out removed %d notes, want 1 (the memory note)", res.Removed)
	}
	if hits, _ := Search(db, "okapi", 10, "", ""); len(hits) != 0 {
		t.Errorf("memory still searchable after opting out: %+v", hits)
	}

	cfg.ExcludeDirs = []string{".git"}
	files, err := enumerateMarkdown(cfg)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, f := range files {
		if f == ".hebb/memory/m.md" {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("with .hebb not excluded the memory note was listed %d times, want 1", seen)
	}
}

// TestMemorySymlinkNotFollowed keeps the walk's never-follow-symlinks rule for
// the memory root: a .hebb/memory that is itself a symlink is not walked, so it
// cannot pull files from outside the vault into the index.
func TestMemorySymlinkNotFollowed(t *testing.T) {
	cfg, _, write := digestFixture(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte("# Secret\n\nhost file"), 0o644); err != nil {
		t.Fatal(err)
	}
	write("note.md", "# Note\n\nvault content")
	if err := os.Symlink(outside, filepath.Join(cfg.VaultPath, filepath.FromSlash(MemoryDirRel))); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	files, err := enumerateMarkdown(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "note.md" {
		t.Errorf("enumerate = %v, want only note.md (symlinked memory dir must not be followed)", files)
	}
}

// TestWatcherIndexesMemory proves the watcher covers agent memory, which sits
// under the otherwise-excluded .hebb, so a memory note is reindexed as soon as
// Claude writes it rather than only on the next read-time refresh.
func TestWatcherIndexesMemory(t *testing.T) {
	cfg, db, write := digestFixture(t)
	if err := os.MkdirAll(filepath.Join(cfg.VaultPath, filepath.FromSlash(MemoryDirRel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := FullReindex(cfg, db); err != nil {
		t.Fatal(err)
	}
	w, err := Watch(cfg, db)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	time.Sleep(150 * time.Millisecond) // let the watcher settle

	write(".hebb/memory/quokka.md", memoryNote("Quokka", "quokka sighting"))
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if hits, _ := Search(db, "quokka", 10, "", ".hebb/memory/"); len(hits) == 1 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("watcher did not index the new memory note within 3s")
}
