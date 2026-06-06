package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContextGraph(t *testing.T) {
	vault := t.TempDir()
	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(vault, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Alpha.md", "# Alpha\n\nThe alpha note. #topic")
	write("Beta.md", "# Beta\n\nBeta links to [[Alpha]]. #topic")

	cfg := Config{VaultPath: vault, DBPath: filepath.Join(vault, ".hebb", "index.db"), ExcludeDirs: defaultExcludeDirs}
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

	if !hasRel(ExpandContext(db, "Alpha", 1, 10), "Beta.md", "incoming_link") {
		t.Fatalf("expected incoming_link Beta.md when expanding Alpha")
	}
	if !hasRel(ExpandContext(db, "Beta", 1, 10), "Alpha.md", "outgoing_link") {
		t.Fatalf("expected outgoing_link Alpha.md when expanding Beta")
	}
	if len(GetContextForTopic(db, "alpha", 10, "")) == 0 {
		t.Fatalf("expected topic context for 'alpha'")
	}
}

func TestTopicContextPopulatesTagsForLinkedNotes(t *testing.T) {
	vault := t.TempDir()
	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(vault, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// "hubword" is unique to the hub note, so only it matches the search; the
	// decisions note is reached purely via the wiki-link, exercising the path
	// that previously dropped tags.
	write("Aurora Overview.md", "# Aurora Overview\n\nhubword project. See [[Aurora Decisions]].\n\n#aurora")
	write("Aurora Decisions.md", "# Aurora Decisions\n\nDecided on FTS5.\n\n#aurora")

	cfg := Config{VaultPath: vault, DBPath: filepath.Join(vault, ".hebb", "index.db"), ExcludeDirs: defaultExcludeDirs}
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

	var decisions *TopicResult
	results := GetContextForTopic(db, "hubword", 10, "")
	for i := range results {
		if results[i].Path == "Aurora Decisions.md" {
			decisions = &results[i]
		}
	}
	if decisions == nil {
		t.Fatal("expected Aurora Decisions pulled in via the link graph")
	}
	if !strings.Contains(decisions.Relevance, "linked_from") {
		t.Errorf("relevance = %q, want linked_from", decisions.Relevance)
	}
	if !strings.Contains(decisions.Tags, "aurora") {
		t.Errorf("linked note tags = %q, want to include 'aurora'", decisions.Tags)
	}
}

// TestContextExcludesDanglingLinks guards a bug found in UAT: a wiki-link to a
// note that does not exist (e.g. [[Roadmap]]) must not appear in context as a
// phantom entry with the raw link as its path and empty body/tags. Only links
// that resolve to a real indexed note are context.
func TestContextExcludesDanglingLinks(t *testing.T) {
	vault := t.TempDir()
	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(vault, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Beta links to a real note (Alpha) and a dangling one (Ghost).
	write("Alpha.md", "# Alpha\n\nReal note. #topic")
	write("Beta.md", "# Beta\n\nBeta links to [[Alpha]] and [[Ghost]]. #topic")

	cfg := Config{VaultPath: vault, DBPath: filepath.Join(vault, ".hebb", "index.db"), ExcludeDirs: defaultExcludeDirs}
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

	res := ExpandContext(db, "Beta", 1, 10)
	if !hasRel(res, "Alpha.md", "outgoing_link") {
		t.Fatal("expected the real outgoing link Alpha.md")
	}
	for _, r := range res {
		if r.Path == "Ghost" || strings.EqualFold(r.Title, "Ghost") {
			t.Errorf("dangling link should be excluded, got phantom entry %+v", r)
		}
	}
	// And it must not leak into topic context either.
	for _, r := range GetContextForTopic(db, "Beta", 10, "") {
		if r.Path == "Ghost" || strings.EqualFold(r.Title, "Ghost") {
			t.Errorf("dangling link leaked into topic context: %+v", r)
		}
	}
}

func hasRel(rs []ContextResult, path, rel string) bool {
	for _, r := range rs {
		if r.Path == path && r.Relationship == rel {
			return true
		}
	}
	return false
}
