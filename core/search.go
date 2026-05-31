package core

import (
	"database/sql"
	"regexp"
	"sort"
	"strings"
)

// SearchResult is a single FTS hit.
type SearchResult struct {
	Path    string
	Title   string
	Snippet string
	Tags    string
	Rank    float64
}

var (
	reFTSOps  = regexp.MustCompile(`["+*(){}]`)
	reBoolOps = regexp.MustCompile(`\b(AND|OR|NOT|NEAR)\b`)
)

// Search runs an FTS5 query, falling back to LIKE if the query is invalid.
func Search(db *sql.DB, query string, limit int, tag, pathPrefix string) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 10
	}
	var sb strings.Builder
	sb.WriteString(`SELECT n.path, n.title, snippet(notes_fts, 1, '>>>', '<<<', '...', 40), rank, n.tags
		FROM notes_fts JOIN notes n ON n.rowid = notes_fts.rowid
		WHERE notes_fts MATCH ?`)
	args := []any{buildFTSQuery(query)}
	args = appendFilters(&sb, args, tag, pathPrefix)
	sb.WriteString(" ORDER BY rank LIMIT ?")
	args = append(args, limit)

	results, err := queryResults(db, sb.String(), args...)
	if err != nil {
		return likeFallback(db, query, limit, tag, pathPrefix)
	}
	return results, nil
}

func appendFilters(sb *strings.Builder, args []any, tag, pathPrefix string) []any {
	if tag != "" {
		sb.WriteString(" AND n.tags LIKE ?")
		args = append(args, "%"+tag+"%")
	}
	if pathPrefix != "" {
		sb.WriteString(" AND n.path LIKE ?")
		args = append(args, pathPrefix+"%")
	}
	return args
}

func likeFallback(db *sql.DB, query string, limit int, tag, pathPrefix string) ([]SearchResult, error) {
	var sb strings.Builder
	sb.WriteString(`SELECT n.path, n.title, substr(n.body, 1, 200), 0.0, n.tags FROM notes n WHERE (n.title LIKE ? OR n.body LIKE ?)`)
	like := "%" + query + "%"
	args := []any{like, like}
	args = appendFilters(&sb, args, tag, pathPrefix)
	sb.WriteString(" LIMIT ?")
	args = append(args, limit)
	return queryResults(db, sb.String(), args...)
}

func queryResults(db *sql.DB, query string, args ...any) ([]SearchResult, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SearchResult
	for rows.Next() {
		var r SearchResult
		var tags sql.NullString
		if err := rows.Scan(&r.Path, &r.Title, &r.Snippet, &r.Rank, &tags); err != nil {
			return nil, err
		}
		r.Tags = tags.String
		out = append(out, r)
	}
	return out, rows.Err()
}

func buildFTSQuery(query string) string {
	if reFTSOps.MatchString(query) || reBoolOps.MatchString(query) {
		return query
	}
	var terms []string
	for _, t := range strings.Fields(query) {
		if len(t) > 1 {
			terms = append(terms, `"`+t+`"`)
		}
	}
	if len(terms) == 0 {
		return `"` + query + `"`
	}
	return strings.Join(terms, " ")
}

// TagCount is a tag and its frequency.
type TagCount struct {
	Tag   string
	Count int
}

// Stats returns note count, link count and the top tags.
func Stats(db *sql.DB) (notes, links int, topTags []TagCount, err error) {
	if err = db.QueryRow("SELECT COUNT(*) FROM notes").Scan(&notes); err != nil {
		return
	}
	if err = db.QueryRow("SELECT COUNT(*) FROM links").Scan(&links); err != nil {
		return
	}
	rows, qerr := db.Query("SELECT tags FROM notes WHERE tags != ''")
	if qerr != nil {
		err = qerr
		return
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var t string
		if err = rows.Scan(&t); err != nil {
			return
		}
		for _, tag := range strings.Fields(t) {
			counts[tag]++
		}
	}
	for tag, c := range counts {
		topTags = append(topTags, TagCount{tag, c})
	}
	sort.Slice(topTags, func(i, j int) bool { return topTags[i].Count > topTags[j].Count })
	if len(topTags) > 20 {
		topTags = topTags[:20]
	}
	return
}
