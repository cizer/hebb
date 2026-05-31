package core

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Note is a parsed markdown note ready for indexing.
type Note struct {
	Title       string
	Body        string // markdown lightly stripped for FTS
	Tags        []string
	Frontmatter map[string]any
	Links       []string
}

var (
	reH1        = regexp.MustCompile(`(?m)^#\s+(.+)$`)
	reWikiLink  = regexp.MustCompile(`\[\[([^\]|]+)(?:\|[^\]]+)?\]\]`)
	reInlineTag = regexp.MustCompile(`(?:^|\s)#([a-zA-Z][\w/-]*)`)
	reTagSplit  = regexp.MustCompile(`[,\s]+`)
)

// ParseNote parses raw markdown into structured data. relPath is the vault
// relative path, used as the fallback title.
func ParseNote(content, relPath string) Note {
	fm, body := splitFrontmatter(content)

	title, _ := fm["title"].(string)
	if title == "" {
		if m := reH1.FindStringSubmatch(body); m != nil {
			title = strings.TrimSpace(m[1])
		}
	}
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(relPath), ".md")
	}

	var links []string
	for _, m := range reWikiLink.FindAllStringSubmatch(content, -1) {
		links = append(links, strings.TrimSpace(m[1]))
	}

	return Note{
		Title:       title,
		Body:        stripMarkdown(body),
		Tags:        extractTags(fm, body),
		Frontmatter: fm,
		Links:       links,
	}
}

func splitFrontmatter(content string) (map[string]any, string) {
	fm := map[string]any{}
	if !strings.HasPrefix(content, "---") {
		return fm, content
	}
	rest := content[3:]
	nl := strings.IndexByte(rest, '\n')
	if nl == -1 {
		return fm, content
	}
	rest = rest[nl+1:]
	end := strings.Index(rest, "\n---")
	if end == -1 {
		return fm, content
	}
	yamlText := rest[:end]
	after := rest[end+1:]
	body := ""
	if bnl := strings.IndexByte(after, '\n'); bnl != -1 {
		body = after[bnl+1:]
	}
	parsed := map[string]any{}
	if err := yaml.Unmarshal([]byte(yamlText), &parsed); err == nil && parsed != nil {
		fm = parsed
	}
	return fm, body
}

func extractTags(fm map[string]any, body string) []string {
	var tags []string
	seen := map[string]bool{}
	add := func(t string) {
		t = strings.TrimPrefix(strings.TrimSpace(t), "#")
		if t != "" && !seen[t] {
			seen[t] = true
			tags = append(tags, t)
		}
	}
	switch v := fm["tags"].(type) {
	case []any:
		for _, t := range v {
			add(fmt.Sprintf("%v", t))
		}
	case string:
		for _, t := range reTagSplit.Split(v, -1) {
			add(t)
		}
	}
	for _, m := range reInlineTag.FindAllStringSubmatch(body, -1) {
		add(m[1])
	}
	return tags
}

var (
	reInlineCode = regexp.MustCompile("`[^`]+`")
	reImage      = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	reMdLink     = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	reWikiAlias  = regexp.MustCompile(`\[\[[^\]|]+\|([^\]]+)\]\]`)
	reWikiPlain  = regexp.MustCompile(`\[\[([^\]|]+)\]\]`)
	reHeading    = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	reEmphasis   = regexp.MustCompile(`[*_]{1,3}([^*_]+)[*_]{1,3}`)
	reHr         = regexp.MustCompile(`(?m)^[-*_]{3,}$`)
	reBlankLines = regexp.MustCompile(`\n{3,}`)
)

// stripMarkdown lightly removes markdown syntax for cleaner FTS content.
func stripMarkdown(text string) string {
	text = stripFencedCode(text)
	text = reInlineCode.ReplaceAllString(text, "")
	text = reImage.ReplaceAllString(text, "")
	text = reMdLink.ReplaceAllString(text, "$1")
	text = reWikiAlias.ReplaceAllString(text, "$1")
	text = reWikiPlain.ReplaceAllString(text, "$1")
	text = reHeading.ReplaceAllString(text, "")
	text = reEmphasis.ReplaceAllString(text, "$1")
	text = reHr.ReplaceAllString(text, "")
	text = reBlankLines.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

// stripFencedCode removes ``` fenced code blocks (RE2 has no lazy match, so a
// line scanner is used instead of a regex).
func stripFencedCode(text string) string {
	var out []string
	inFence := false
	for _, ln := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "```") {
			inFence = !inFence
			continue
		}
		if !inFence {
			out = append(out, ln)
		}
	}
	return strings.Join(out, "\n")
}
