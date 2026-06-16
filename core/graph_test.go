package core

import (
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// buildGraphVault creates a temp vault with a specific link topology for graph
// metric tests and returns the cfg and an indexed, open database. Caller must
// defer db.Close().
//
// Topology (all links are resolved):
//
//	Hub     -> A, B, C, D, E  (hub: degree 5)
//	A, B, C, D, E -> Hub     (same edges, undirected)
//	A <-> B                  (extra edge: A-B)
//
//	Island1 -> Island2        (2-note island in 3-Resources, not archived)
//	OldOrphan                 (degree 0, in 2-Areas, old mtime -- should be flagged)
//	FreshOrphan               (degree 0, in 2-Areas, new mtime -- must NOT be flagged)
//	JournalOrphan             (degree 0, in Journal -- never flagged)
//	ArchivedIsland1 <-> ArchivedIsland2  (2-note island, both in 4-Archives -- not flagged)
func buildGraphVault(t *testing.T) (Config, *sql.DB) {
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

	// Hub and its spokes -- all in Notes/ so they are not orphan-candidates.
	write("Notes/Hub.md", "# Hub\n\n[[A]] [[B]] [[C]] [[D]] [[E]]\n")
	write("Notes/A.md", "# A\n\n[[Hub]] [[B]]\n")
	write("Notes/B.md", "# B\n\n[[Hub]]\n")
	write("Notes/C.md", "# C\n\n[[Hub]]\n")
	write("Notes/D.md", "# D\n\n[[Hub]]\n")
	write("Notes/E.md", "# E\n\n[[Hub]]\n")

	// 2-note island in 3-Resources (not archived -- should be flagged).
	write("3-Resources/Island1.md", "# Island1\n\n[[Island2]]\n")
	write("3-Resources/Island2.md", "# Island2\n\nSee [[Island1]].\n")

	// Archived 2-note island (both under 4-Archives -- must NOT be flagged).
	write("4-Archives/Arch1.md", "# Arch1\n\n[[Arch2]]\n")
	write("4-Archives/Arch2.md", "# Arch2\n\nSee [[Arch1]].\n")

	// Orphan in 2-Areas with an OLD mtime (should be flagged).
	write("2-Areas/OldOrphan.md", "# OldOrphan\n\nNo links here.\n")

	// Orphan in 2-Areas with a FRESH mtime (must NOT be flagged).
	write("2-Areas/FreshOrphan.md", "# FreshOrphan\n\nNo links, but new.\n")

	// Orphan in Journal (expected-orphan folder -- must never be flagged).
	write("Journal/JournalOrphan.md", "# JournalOrphan\n\nCapture note.\n")

	cfg := Config{
		VaultPath:   vault,
		DBPath:      filepath.Join(vault, ".hebb", "index.db"),
		ExcludeDirs: defaultExcludeDirs,
		// Set orphan_stale_days to 30 so "old" means >30 days, "fresh" means today.
		Health: HealthConfig{
			OrphanStaleDays: 30,
		},
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

	// Age OldOrphan by setting its mtime to 60 days ago, then re-index so the
	// stored mtime reflects the old timestamp.
	oldTime := timeNowForTest().AddDate(0, 0, -60)
	orphanPath := filepath.Join(vault, "2-Areas/OldOrphan.md")
	if err := os.Chtimes(orphanPath, oldTime, oldTime); err != nil {
		db.Close()
		t.Fatal(err)
	}
	// Force a full re-index so the updated mtime is picked up.
	if _, err := FullReindexForce(cfg, db); err != nil {
		db.Close()
		t.Fatal(err)
	}

	return cfg, db
}

// TestBuildGraph_NodeCount verifies that every note in the vault appears as a
// graph node, including orphans and isolated notes.
func TestBuildGraph_NodeCount(t *testing.T) {
	cfg, db := buildGraphVault(t)
	defer db.Close()

	g, err := buildGraph(db)
	if err != nil {
		t.Fatalf("buildGraph: %v", err)
	}

	// The vault has: Hub, A, B, C, D, E, Island1, Island2, Arch1, Arch2,
	// OldOrphan, FreshOrphan, JournalOrphan = 13 notes.
	if g.nodeCount() != 13 {
		t.Errorf("nodeCount = %d, want 13; nodes: %v", g.nodeCount(), g.nodes)
	}
	_ = cfg
}

// TestBuildGraph_Undirected verifies that the graph is undirected: a link A->B
// and the reverse B->A contribute only one edge and both nodes gain a neighbour.
func TestBuildGraph_Undirected(t *testing.T) {
	_, db := buildGraphVault(t)
	defer db.Close()

	g, err := buildGraph(db)
	if err != nil {
		t.Fatalf("buildGraph: %v", err)
	}

	// Hub links to A, B, C, D, E; each of those links back. Each pair should
	// appear as exactly one edge.
	// A also links to B; the reverse (B links Hub only) means the A-B edge
	// comes from A's link to B (undirected).
	// Hub-A, Hub-B, Hub-C, Hub-D, Hub-E, A-B = 6 edges.
	// Island1-Island2, Arch1-Arch2 = 2 more edges. Total = 8.
	if g.edgeCount() != 8 {
		t.Errorf("edgeCount = %d, want 8", g.edgeCount())
	}
}

// TestBuildGraph_SelfLoopsDropped creates a note that links to itself and
// verifies no self-loop appears in the adjacency.
func TestBuildGraph_SelfLoopsDropped(t *testing.T) {
	vault := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(vault, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(content), 0o644)
	}
	write("Notes/Self.md", "# Self\n\n[[Self]]\n")

	cfg := Config{
		VaultPath:   vault,
		DBPath:      filepath.Join(vault, ".hebb", "index.db"),
		ExcludeDirs: defaultExcludeDirs,
	}
	os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755)
	db, err := OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()
	if _, err := FullReindex(cfg, db); err != nil {
		t.Fatal(err)
	}
	g, err := buildGraph(db)
	if err != nil {
		t.Fatalf("buildGraph: %v", err)
	}
	if g.edgeCount() != 0 {
		t.Errorf("self-loop must produce 0 edges, got %d", g.edgeCount())
	}
	selfIdx := g.nodeIdx["Notes/Self.md"]
	if g.degree(selfIdx) != 0 {
		t.Errorf("self-loop note must have degree 0, got %d", g.degree(selfIdx))
	}
}

// TestBuildGraph_DanglingLinksExcluded verifies that dangling (NULL target_path)
// links do not contribute any edges to the graph.
func TestBuildGraph_DanglingLinksExcluded(t *testing.T) {
	vault := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(vault, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(content), 0o644)
	}
	write("Notes/Source.md", "# Source\n\n[[NonexistentTarget]]\n")

	cfg := Config{
		VaultPath:   vault,
		DBPath:      filepath.Join(vault, ".hebb", "index.db"),
		ExcludeDirs: defaultExcludeDirs,
	}
	os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755)
	db, err := OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()
	if _, err := FullReindex(cfg, db); err != nil {
		t.Fatal(err)
	}
	g, err := buildGraph(db)
	if err != nil {
		t.Fatalf("buildGraph: %v", err)
	}
	if g.edgeCount() != 0 {
		t.Errorf("dangling link must produce 0 edges, got %d", g.edgeCount())
	}
}

// TestGraphStats_ComponentCount verifies the number of connected components in
// the standard graph vault.
func TestGraphStats_ComponentCount(t *testing.T) {
	cfg, db := buildGraphVault(t)
	defer db.Close()

	stats, err := GraphHealth(cfg, db)
	if err != nil {
		t.Fatalf("GraphHealth: %v", err)
	}
	// Components: {Hub,A,B,C,D,E}, {Island1,Island2}, {Arch1,Arch2},
	// {OldOrphan}, {FreshOrphan}, {JournalOrphan} = 6 components.
	if stats.ComponentCount != 6 {
		t.Errorf("ComponentCount = %d, want 6", stats.ComponentCount)
	}
}

// TestGraphStats_GiantRatio verifies the giant-component ratio. The hub cluster
// (6 notes: Hub,A,B,C,D,E) out of 13 total notes = 6/13 ≈ 0.46.
func TestGraphStats_GiantRatio(t *testing.T) {
	cfg, db := buildGraphVault(t)
	defer db.Close()

	stats, err := GraphHealth(cfg, db)
	if err != nil {
		t.Fatalf("GraphHealth: %v", err)
	}
	want := float64(6) / float64(13)
	if diff := stats.GiantRatio - want; diff > 0.001 || diff < -0.001 {
		t.Errorf("GiantRatio = %.4f, want %.4f", stats.GiantRatio, want)
	}
}

// TestGraphStats_CorenessHubRankedAboveLeaves verifies that the hub note has a
// higher coreness than a leaf note.
func TestGraphStats_CorenessHubRankedAboveLeaves(t *testing.T) {
	cfg, db := buildGraphVault(t)
	defer db.Close()

	stats, err := GraphHealth(cfg, db)
	if err != nil {
		t.Fatalf("GraphHealth: %v", err)
	}

	hubCoreness := stats.Coreness["Notes/Hub.md"]
	// C, D, E only link to Hub; they are leaves of the hub cluster (degree 1).
	// A and B link to Hub and to each other (degree >= 2). Hub has degree 5.
	// Coreness: hub cluster has k-core 2 (A, B, Hub all have degree >= 2 in the
	// 2-core). C, D, E have degree 1 and are in the 1-core only.
	cCoreness := stats.Coreness["Notes/C.md"]
	if hubCoreness <= cCoreness {
		t.Errorf("Hub coreness %d must exceed leaf (C) coreness %d", hubCoreness, cCoreness)
	}
}

// TestGraphStats_MaxCore verifies that the maximum coreness is at least 1 for
// a non-trivial graph.
func TestGraphStats_MaxCore(t *testing.T) {
	cfg, db := buildGraphVault(t)
	defer db.Close()

	stats, err := GraphHealth(cfg, db)
	if err != nil {
		t.Fatalf("GraphHealth: %v", err)
	}
	if stats.MaxCore < 1 {
		t.Errorf("MaxCore = %d, expected >= 1 for a graph with linked notes", stats.MaxCore)
	}
}

// TestDetectOrphans_OldOrphanInConnectiveFlaggedRH tests that an old degree-0
// note in a connective folder appears in RunHealth findings.
func TestDetectOrphans_OldOrphanInConnectiveFlaggedRH(t *testing.T) {
	cfg, db := buildGraphVault(t)
	defer db.Close()

	result, err := RunHealthFull(cfg, db, false)
	if err != nil {
		t.Fatalf("RunHealthFull: %v", err)
	}
	findings := result.Findings

	var found bool
	for _, f := range findings {
		if f.Type == "orphan" && f.Path == "2-Areas/OldOrphan.md" {
			found = true
			if !strings.Contains(f.Detail, "degree 0") {
				t.Errorf("orphan detail missing 'degree 0': %q", f.Detail)
			}
			if !strings.Contains(f.Detail, "days") {
				t.Errorf("orphan detail missing age in days: %q", f.Detail)
			}
		}
	}
	if !found {
		t.Errorf("expected orphan finding for 2-Areas/OldOrphan.md; findings: %+v", findings)
	}
}

// TestDetectOrphans_FreshOrphanNotFlaggedAsOrphan verifies that a degree-0 note
// in a connective folder is NOT flagged as either "orphan" or "island" when its
// mtime is within OrphanStaleDays. The age exemption from the orphan detector
// must not be circumvented by the island detector: size-1 components are
// excluded from island findings by design.
func TestDetectOrphans_FreshOrphanNotFlaggedAsOrphan(t *testing.T) {
	cfg, db := buildGraphVault(t)
	defer db.Close()

	result, err := RunHealthFull(cfg, db, false)
	if err != nil {
		t.Fatalf("RunHealthFull: %v", err)
	}
	findings := result.Findings

	for _, f := range findings {
		if f.Path == "2-Areas/FreshOrphan.md" {
			if f.Type == "orphan" {
				t.Errorf("FreshOrphan must not be flagged as orphan (too new); got: %+v", f)
			}
			if f.Type == "island" {
				t.Errorf("FreshOrphan must not be flagged as island (size-1 components are orphans, not islands); got: %+v", f)
			}
		}
	}
}

// TestDetectOrphans_JournalOrphanNeverFlagged verifies that a degree-0 note
// under Journal is never flagged regardless of age.
func TestDetectOrphans_JournalOrphanNeverFlagged(t *testing.T) {
	cfg, db := buildGraphVault(t)
	defer db.Close()

	// Age the journal note to make it very old.
	oldTime := timeNowForTest().AddDate(0, 0, -365)
	journalPath := filepath.Join(cfg.VaultPath, "Journal/JournalOrphan.md")
	if err := os.Chtimes(journalPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if _, err := FullReindexForce(cfg, db); err != nil {
		t.Fatal(err)
	}

	result, err := RunHealthFull(cfg, db, false)
	if err != nil {
		t.Fatalf("RunHealthFull: %v", err)
	}
	findings := result.Findings

	for _, f := range findings {
		if f.Path == "Journal/JournalOrphan.md" {
			t.Errorf("Journal orphan must never be flagged; got: %+v", f)
		}
	}
}

// TestDetectIslands_ResourcesIslandFlagged verifies that a 2-note island in
// 3-Resources appears as an "island" finding.
func TestDetectIslands_ResourcesIslandFlagged(t *testing.T) {
	cfg, db := buildGraphVault(t)
	defer db.Close()

	result, err := RunHealthFull(cfg, db, false)
	if err != nil {
		t.Fatalf("RunHealthFull: %v", err)
	}
	findings := result.Findings

	var islandFindings []Finding
	for _, f := range findings {
		if f.Type == "island" {
			islandFindings = append(islandFindings, f)
		}
	}

	// We expect at least one island finding that covers Island1/Island2.
	var found bool
	for _, f := range islandFindings {
		if strings.Contains(f.Detail, "Island") {
			found = true
			if !strings.Contains(f.Detail, "2") {
				t.Errorf("island detail should mention size 2: %q", f.Detail)
			}
		}
	}
	if !found {
		t.Errorf("expected island finding for 3-Resources island; island findings: %+v", islandFindings)
	}
}

// TestDetectIslands_ArchivedIslandNotFlagged verifies that a 2-note island
// entirely under 4-Archives is NOT reported as an island finding. Only
// archive folders (GetArchiveFolders, default ["4-Archives"]) suppress islands;
// Journal and Notes do not.
func TestDetectIslands_ArchivedIslandNotFlagged(t *testing.T) {
	cfg, db := buildGraphVault(t)
	defer db.Close()

	result, err := RunHealthFull(cfg, db, false)
	if err != nil {
		t.Fatalf("RunHealthFull: %v", err)
	}
	findings := result.Findings

	for _, f := range findings {
		if f.Type == "island" && strings.Contains(f.Detail, "Arch") {
			t.Errorf("archived island must not be flagged; got: %+v", f)
		}
	}
}

// TestDetectIslands_JournalIslandFlagged verifies that a small island whose
// members are all under Journal IS reported. Journal is an expected-orphan
// folder (orphans/leaves there are never flagged) but it is NOT an archive
// folder, so its islands must still appear in the worklist.
func TestDetectIslands_JournalIslandFlagged(t *testing.T) {
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

	// Mainland: a hub with two spokes so there is a clear largest component.
	write("Notes/Hub.md", "# Hub\n\n[[SpokeA]] [[SpokeB]]\n")
	write("Notes/SpokeA.md", "# SpokeA\n\n[[Hub]]\n")
	write("Notes/SpokeB.md", "# SpokeB\n\n[[Hub]]\n")

	// 2-note island in Journal (expected-orphan, but NOT an archive folder).
	// Under the corrected spec this must be reported.
	write("Journal/JournalA.md", "# JournalA\n\n[[JournalB]]\n")
	write("Journal/JournalB.md", "# JournalB\n\nSee [[JournalA]].\n")

	cfg := Config{
		VaultPath:   vault,
		DBPath:      filepath.Join(vault, ".hebb", "index.db"),
		ExcludeDirs: defaultExcludeDirs,
		Health:      HealthConfig{IslandMaxSize: 3},
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

	g, err := buildGraph(db)
	if err != nil {
		t.Fatalf("buildGraph: %v", err)
	}

	findings := detectIslands(cfg, g)

	var journalIslandFound bool
	for _, f := range findings {
		if f.Type == "island" && strings.Contains(f.Detail, "Journal") {
			journalIslandFound = true
		}
	}
	if !journalIslandFound {
		t.Errorf("2-note Journal island must be reported (Journal is not an archive folder); findings: %+v", findings)
	}
}

// TestDetectIslands_NotesIslandFlagged verifies that a small island whose
// members are all under Notes IS reported. Notes is an expected-orphan folder
// but not an archive folder.
func TestDetectIslands_NotesIslandFlagged(t *testing.T) {
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

	// Mainland: three linked notes.
	write("2-Areas/Main1.md", "# Main1\n\n[[Main2]] [[Main3]]\n")
	write("2-Areas/Main2.md", "# Main2\n\n[[Main1]]\n")
	write("2-Areas/Main3.md", "# Main3\n\n[[Main1]]\n")

	// 2-note island in Notes (expected-orphan, but NOT an archive folder).
	write("Notes/NoteA.md", "# NoteA\n\n[[NoteB]]\n")
	write("Notes/NoteB.md", "# NoteB\n\nSee [[NoteA]].\n")

	cfg := Config{
		VaultPath:   vault,
		DBPath:      filepath.Join(vault, ".hebb", "index.db"),
		ExcludeDirs: defaultExcludeDirs,
		Health:      HealthConfig{IslandMaxSize: 3},
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

	g, err := buildGraph(db)
	if err != nil {
		t.Fatalf("buildGraph: %v", err)
	}

	findings := detectIslands(cfg, g)

	var notesIslandFound bool
	for _, f := range findings {
		if f.Type == "island" && strings.Contains(f.Detail, "Note") {
			notesIslandFound = true
		}
	}
	if !notesIslandFound {
		t.Errorf("2-note Notes island must be reported (Notes is not an archive folder); findings: %+v", findings)
	}
}

// TestUnderPrefix verifies the folder-prefix matching helper.
func TestUnderPrefix(t *testing.T) {
	cases := []struct {
		path     string
		prefixes []string
		want     bool
	}{
		{"2-Areas/Foo.md", []string{"2-Areas"}, true},
		{"2-Areas/sub/Bar.md", []string{"2-Areas"}, true},
		{"not-2-Areas/Foo.md", []string{"2-Areas"}, false},
		{"3-Resources/Baz.md", []string{"2-Areas", "3-Resources"}, true},
		{"Journal/Note.md", []string{"Journal"}, true},
		{"4-Archives/Old.md", []string{"4-Archives"}, true},
		{"1-Projects/Active.md", []string{"2-Areas", "3-Resources"}, false},
	}
	for _, tc := range cases {
		got := underPrefix(tc.path, tc.prefixes)
		if got != tc.want {
			t.Errorf("underPrefix(%q, %v) = %v, want %v", tc.path, tc.prefixes, got, tc.want)
		}
	}
}

// TestHealthConfigGraphDefaults verifies that all Phase 2a HealthConfig fields
// return their documented zero-value defaults.
func TestHealthConfigGraphDefaults(t *testing.T) {
	hc := HealthConfig{}

	if got := hc.GetOrphanStaleDays(); got != 90 {
		t.Errorf("GetOrphanStaleDays() = %d, want 90", got)
	}
	if got := hc.GetIslandMaxSize(); got != 3 {
		t.Errorf("GetIslandMaxSize() = %d, want 3", got)
	}

	wantConnective := []string{"2-Areas", "3-Resources"}
	gotConnective := hc.GetConnectiveFolders()
	if !equalStrSlice(gotConnective, wantConnective) {
		t.Errorf("GetConnectiveFolders() = %v, want %v", gotConnective, wantConnective)
	}

	wantExcluded := []string{"Journal", "Notes", "4-Archives"}
	gotExcluded := hc.GetExpectedOrphanFolders()
	if !equalStrSlice(gotExcluded, wantExcluded) {
		t.Errorf("GetExpectedOrphanFolders() = %v, want %v", gotExcluded, wantExcluded)
	}

	// ArchiveFolders default is narrower than ExpectedOrphanFolders: only
	// 4-Archives suppresses islands; Journal and Notes do not.
	wantArchive := []string{"4-Archives"}
	gotArchive := hc.GetArchiveFolders()
	if !equalStrSlice(gotArchive, wantArchive) {
		t.Errorf("GetArchiveFolders() = %v, want %v", gotArchive, wantArchive)
	}
}

// TestHealthConfigGraphCustom verifies that custom values override the defaults.
func TestHealthConfigGraphCustom(t *testing.T) {
	hc := HealthConfig{
		OrphanStaleDays:       45,
		IslandMaxSize:         5,
		ConnectiveFolders:     []string{"MyArea"},
		ExpectedOrphanFolders: []string{"MyJournal"},
	}
	if got := hc.GetOrphanStaleDays(); got != 45 {
		t.Errorf("GetOrphanStaleDays() = %d, want 45", got)
	}
	if got := hc.GetIslandMaxSize(); got != 5 {
		t.Errorf("GetIslandMaxSize() = %d, want 5", got)
	}
	if got := hc.GetConnectiveFolders(); !equalStrSlice(got, []string{"MyArea"}) {
		t.Errorf("GetConnectiveFolders() = %v, want [MyArea]", got)
	}
	if got := hc.GetExpectedOrphanFolders(); !equalStrSlice(got, []string{"MyJournal"}) {
		t.Errorf("GetExpectedOrphanFolders() = %v, want [MyJournal]", got)
	}
}

// TestGraphHealth_EmptyVault verifies that GraphHealth returns a zero-value
// summary (no panic, no error) when the vault has no notes.
func TestGraphHealth_EmptyVault(t *testing.T) {
	vault := t.TempDir()
	cfg := Config{
		VaultPath:   vault,
		DBPath:      filepath.Join(vault, ".hebb", "index.db"),
		ExcludeDirs: defaultExcludeDirs,
	}
	os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755)
	db, err := OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := FullReindex(cfg, db); err != nil {
		t.Fatal(err)
	}

	stats, err := GraphHealth(cfg, db)
	if err != nil {
		t.Fatalf("GraphHealth on empty vault: %v", err)
	}
	if stats.NodeCount != 0 {
		t.Errorf("empty vault NodeCount = %d, want 0", stats.NodeCount)
	}
	if stats.GiantRatio != 0 {
		t.Errorf("empty vault GiantRatio = %f, want 0", stats.GiantRatio)
	}
}

// TestMsToAgeDays verifies that the millisecond-to-age-days helper rounds
// correctly against the injected test clock.
func TestMsToAgeDays(t *testing.T) {
	// Freeze the clock.
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	orig := timeNowForTest
	timeNowForTest = func() time.Time { return now }
	defer func() { timeNowForTest = orig }()

	// 10 days ago in milliseconds.
	tenDaysAgo := now.AddDate(0, 0, -10)
	ms := float64(tenDaysAgo.UnixNano()) / 1e6

	got := msToAgeDays(ms)
	if got != 10 {
		t.Errorf("msToAgeDays = %d, want 10", got)
	}
}

// TestDetectIslands_OldOrphanNotAlsoIsland verifies that a degree-0 note (size-1
// component) reported as an "orphan" is NOT also reported as an "island". The
// two detectors must not double-report the same note.
func TestDetectIslands_OldOrphanNotAlsoIsland(t *testing.T) {
	cfg, db := buildGraphVault(t)
	defer db.Close()

	result, err := RunHealthFull(cfg, db, false)
	if err != nil {
		t.Fatalf("RunHealthFull: %v", err)
	}
	findings := result.Findings

	var sawOrphan, sawIsland bool
	for _, f := range findings {
		if f.Path == "2-Areas/OldOrphan.md" {
			switch f.Type {
			case "orphan":
				sawOrphan = true
			case "island":
				sawIsland = true
				t.Errorf("OldOrphan must not be reported as island (size-1 component is an orphan, not an island); got: %+v", f)
			}
		}
	}
	if !sawOrphan {
		t.Errorf("OldOrphan must be reported as orphan; findings: %+v", findings)
	}
	_ = sawIsland // already reported above if true
}

// TestDetectIslands_GiantNotFlaggedAsIsland verifies that the largest connected
// component (the hub-and-spoke cluster) is never reported as an island, even
// when its size is <= IslandMaxSize. On the standard test vault the hub cluster
// has 6 notes and the default IslandMaxSize is 3, so the giant is already
// filtered by the size check; this test uses a reduced topology where the giant
// component has exactly IslandMaxSize members to exercise the mainland-exclusion
// predicate directly.
func TestDetectIslands_GiantNotFlaggedAsIsland(t *testing.T) {
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

	// Giant component: Hub with 2 spokes (size 3, equal to default IslandMaxSize).
	// All notes in Notes/ -- not a connective folder, so orphan detector ignores them.
	write("Notes/Hub.md", "# Hub\n\n[[SpokeA]] [[SpokeB]]\n")
	write("Notes/SpokeA.md", "# SpokeA\n\n[[Hub]]\n")
	write("Notes/SpokeB.md", "# SpokeB\n\n[[Hub]]\n")

	// A genuine 2-note island in 3-Resources (should be flagged).
	write("3-Resources/Cluster1.md", "# Cluster1\n\n[[Cluster2]]\n")
	write("3-Resources/Cluster2.md", "# Cluster2\n\n[[Cluster1]]\n")

	cfg := Config{
		VaultPath:   vault,
		DBPath:      filepath.Join(vault, ".hebb", "index.db"),
		ExcludeDirs: defaultExcludeDirs,
		Health:      HealthConfig{
			// IslandMaxSize defaults to 3, matching the giant component size.
		},
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

	g, err := buildGraph(db)
	if err != nil {
		t.Fatalf("buildGraph: %v", err)
	}

	findings := detectIslands(cfg, g)

	var giantFlagged bool
	var clusterFlagged bool
	for _, f := range findings {
		if f.Type == "island" {
			if strings.Contains(f.Detail, "Hub") || strings.Contains(f.Detail, "Spoke") {
				giantFlagged = true
				t.Errorf("giant component (hub+spokes, size 3) must not be flagged as island; got: %+v", f)
			}
			if strings.Contains(f.Detail, "Cluster") {
				clusterFlagged = true
			}
		}
	}
	if giantFlagged {
		t.Logf("all island findings: %+v", findings)
	}
	if !clusterFlagged {
		t.Errorf("2-node cluster in 3-Resources must be flagged as island; findings: %+v", findings)
	}
}

// TestDetectIslands_ExactCounts verifies the full island finding count against
// the standard graph vault. With the corrected predicate the only island
// finding is the 2-note cluster in 3-Resources (Island1/Island2).
// The hub-and-spoke giant (6 notes), the archived 2-note cluster (4-Archives),
// and all three orphans (OldOrphan, FreshOrphan, JournalOrphan) must not appear.
func TestDetectIslands_ExactCounts(t *testing.T) {
	cfg, db := buildGraphVault(t)
	defer db.Close()

	g, err := buildGraph(db)
	if err != nil {
		t.Fatalf("buildGraph: %v", err)
	}

	findings := detectIslands(cfg, g)

	if len(findings) != 1 {
		t.Errorf("detectIslands returned %d findings, want exactly 1; findings: %+v", len(findings), findings)
	}
	if len(findings) >= 1 {
		f := findings[0]
		if f.Type != "island" {
			t.Errorf("finding type = %q, want \"island\"", f.Type)
		}
		if !strings.Contains(f.Detail, "Island") {
			t.Errorf("expected island finding to mention Island1/Island2; got: %+v", f)
		}
		if !strings.Contains(f.Detail, "2") {
			t.Errorf("expected island finding to report cluster size 2; got: %+v", f)
		}
	}
}

// equalStrSlice returns true if a and b contain the same strings in the same
// sorted order.
func equalStrSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	ac := append([]string(nil), a...)
	bc := append([]string(nil), b...)
	sort.Strings(ac)
	sort.Strings(bc)
	for i := range ac {
		if ac[i] != bc[i] {
			return false
		}
	}
	return true
}
