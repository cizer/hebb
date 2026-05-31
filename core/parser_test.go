package core

import (
	"strings"
	"testing"
)

func TestParseNoteTitleFromH1(t *testing.T) {
	n := ParseNote("# Hello World\n\nbody text", "notes/x.md")
	if n.Title != "Hello World" {
		t.Fatalf("title = %q, want Hello World", n.Title)
	}
}

func TestParseNoteTitleFromFilename(t *testing.T) {
	n := ParseNote("no heading here", "a/Foo Bar.md")
	if n.Title != "Foo Bar" {
		t.Fatalf("title = %q, want Foo Bar", n.Title)
	}
}

func TestParseNoteFrontmatterTagsAndLinks(t *testing.T) {
	c := "---\ntitle: My Note\ntags: [project, fttf]\n---\n\nbody with #inline and a [[Link Target]].\n"
	n := ParseNote(c, "x.md")
	if n.Title != "My Note" {
		t.Fatalf("title = %q", n.Title)
	}
	for _, want := range []string{"project", "fttf", "inline"} {
		if !contains(n.Tags, want) {
			t.Fatalf("tags %v missing %q", n.Tags, want)
		}
	}
	if !contains(n.Links, "Link Target") {
		t.Fatalf("links = %v, want Link Target", n.Links)
	}
}

func TestStripMarkdownRemovesCodeKeepsText(t *testing.T) {
	n := ParseNote("# T\n\n```\nsecretcode\n```\n\nplain [alias](http://x) text", "x.md")
	if strings.Contains(n.Body, "secretcode") {
		t.Fatalf("fenced code not stripped: %q", n.Body)
	}
	if !strings.Contains(n.Body, "alias") || !strings.Contains(n.Body, "plain") {
		t.Fatalf("link text lost: %q", n.Body)
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
