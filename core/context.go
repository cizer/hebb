package core

import (
	"database/sql"
	"math"
	"strings"
)

// ContextResult is a note reached by following the link graph.
type ContextResult struct {
	Path         string
	Title        string
	Relationship string
	Snippet      string
}

func resolvePath(db *sql.DB, pathOrTitle string) string {
	var p string
	if db.QueryRow("SELECT path FROM notes WHERE path = ?", pathOrTitle).Scan(&p) == nil {
		return p
	}
	if db.QueryRow("SELECT path FROM notes WHERE title = ?", pathOrTitle).Scan(&p) == nil {
		return p
	}
	if db.QueryRow("SELECT path FROM notes WHERE path LIKE ?", "%"+pathOrTitle+"%").Scan(&p) == nil {
		return p
	}
	return ""
}

type linkRow struct{ Path, Title, Snippet string }

func outgoing(db *sql.DB, sourcePath string) []linkRow {
	rows, err := db.Query(`SELECT l.target, n.path, n.title, substr(n.body,1,200)
		FROM links l
		LEFT JOIN notes n ON (n.path LIKE '%' || l.target || '.md' OR n.title = l.target)
		WHERE l.source_path = ?`, sourcePath)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []linkRow
	for rows.Next() {
		var target string
		var p, t, s sql.NullString
		if rows.Scan(&target, &p, &t, &s) != nil {
			continue
		}
		// Skip dangling links: a [[target]] with no matching note in the index
		// is not context, so don't fabricate a phantom entry from the raw link.
		if !p.Valid || p.String == "" {
			continue
		}
		title := t.String
		if title == "" {
			title = target
		}
		out = append(out, linkRow{Path: p.String, Title: title, Snippet: s.String})
	}
	return out
}

// ExpandContext follows wiki-links out from a seed note (1-2 hops).
func ExpandContext(db *sql.DB, notePath string, depth, limit int) []ContextResult {
	if depth <= 0 {
		depth = 1
	}
	if limit <= 0 {
		limit = 20
	}
	resolved := resolvePath(db, notePath)
	if resolved == "" {
		return []ContextResult{{Path: notePath, Title: notePath, Relationship: "not_found"}}
	}

	var results []ContextResult
	seen := map[string]bool{notePath: true, resolved: true}
	add := func(path, title, rel, snip string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		results = append(results, ContextResult{Path: path, Title: title, Relationship: rel, Snippet: snip})
	}

	for _, r := range outgoing(db, resolved) {
		add(r.Path, r.Title, "outgoing_link", r.Snippet)
	}

	var noteTitle string
	db.QueryRow("SELECT title FROM notes WHERE path = ?", resolved).Scan(&noteTitle)
	if noteTitle != "" {
		rows, err := db.Query(`SELECT n.path, n.title, substr(n.body,1,200)
			FROM links l JOIN notes n ON n.path = l.source_path
			WHERE l.target = ? OR l.target LIKE ?`, noteTitle, "%/"+noteTitle)
		if err == nil {
			for rows.Next() {
				var p, t, s string
				if rows.Scan(&p, &t, &s) == nil {
					add(p, t, "incoming_link", s)
				}
			}
			rows.Close()
		}
	}

	if depth >= 2 {
		var hops []string
		for _, r := range results {
			if r.Relationship != "not_found" {
				hops = append(hops, r.Path)
			}
		}
		for _, hop := range hops {
			if len(results) >= limit {
				break
			}
			for _, r := range outgoing(db, hop) {
				if len(results) >= limit {
					break
				}
				add(r.Path, r.Title, "2nd_hop", r.Snippet)
			}
		}
	}

	if len(results) > limit {
		results = results[:limit]
	}
	return results
}

// TopicResult is a note assembled into a topic context bundle.
type TopicResult struct {
	Path      string
	Title     string
	Relevance string
	Snippet   string
	Tags      string
}

var trivialTags = map[string]bool{"active": true, "work": true, "personal": true}

// GetContextForTopic combines full-text search, link expansion and tag siblings.
func GetContextForTopic(db *sql.DB, topic string, limit int, pathPrefix string) []TopicResult {
	if limit <= 0 {
		limit = 15
	}
	var order []string
	m := map[string]TopicResult{}
	put := func(r TopicResult) {
		if _, ok := m[r.Path]; ok {
			return
		}
		m[r.Path] = r
		order = append(order, r.Path)
	}

	sr, _ := Search(db, topic, int(math.Ceil(float64(limit)*0.6)), "", pathPrefix)
	for _, r := range sr {
		put(TopicResult{Path: r.Path, Title: r.Title, Relevance: "direct_match", Snippet: r.Snippet, Tags: r.Tags})
	}

	top := sr
	if len(top) > 3 {
		top = top[:3]
	}
	for _, seed := range top {
		for _, e := range ExpandContext(db, seed.Path, 1, 5) {
			if e.Relationship == "not_found" {
				continue
			}
			put(TopicResult{Path: e.Path, Title: e.Title, Relevance: "linked_from:" + seed.Path, Snippet: e.Snippet})
		}
	}

	tagCounts := map[string]int{}
	for _, r := range sr {
		for _, t := range strings.Fields(r.Tags) {
			if !trivialTags[t] {
				tagCounts[t]++
			}
		}
	}
	topTag, best := "", 0
	for t, c := range tagCounts {
		if len(t) > 2 && c > best {
			topTag, best = t, c
		}
	}
	if topTag != "" && len(order) < limit {
		q := "SELECT path, title, substr(body,1,150), tags FROM notes WHERE tags LIKE ?"
		args := []any{"%" + topTag + "%"}
		if pathPrefix != "" {
			q += " AND path LIKE ?"
			args = append(args, pathPrefix+"%")
		}
		q += " LIMIT 5"
		if rows, err := db.Query(q, args...); err == nil {
			for rows.Next() {
				var p, t, s, tg string
				if rows.Scan(&p, &t, &s, &tg) == nil {
					put(TopicResult{Path: p, Title: t, Relevance: "shared_tag:" + topTag, Snippet: s, Tags: tg})
				}
			}
			rows.Close()
		}
	}

	if len(order) > limit {
		order = order[:limit]
	}
	out := make([]TopicResult, 0, len(order))
	for _, p := range order {
		r := m[p]
		// Notes pulled in via the link graph arrive without tags (ExpandContext
		// does not carry them); backfill so every result reports its real tags.
		if r.Tags == "" {
			r.Tags = tagsFor(db, p)
		}
		out = append(out, r)
	}
	return out
}

// tagsFor returns the stored tag string for a note path, or "" if none/unknown.
func tagsFor(db *sql.DB, path string) string {
	var t sql.NullString
	db.QueryRow("SELECT tags FROM notes WHERE path = ?", path).Scan(&t)
	return t.String
}
