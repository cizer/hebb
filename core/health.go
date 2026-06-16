package core

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// timeNowForTest is the clock used by health detectors. It is a variable so
// tests can inject a fixed time; production code always uses the real clock.
var timeNowForTest = time.Now

// Finding is a single vault-health observation produced by a detector. It is
// deliberately read-only: the health engine never writes to the vault or the
// index.
type Finding struct {
	// Type identifies the finding: "dangling_link", "ambiguous_link",
	// "para_drift", "oversized".
	Type string `json:"type"`
	// Path is the vault-relative path of the affected note.
	Path string `json:"path"`
	// Detail is a human-readable explanation of the specific finding.
	Detail string `json:"detail"`
	// Severity is "warn" for all Phase 1 findings; reserved for future tiers.
	Severity string `json:"severity"`
}

// HealthResult is the full output of RunHealthFull: the findings worklist, the
// structural graph-health summary produced by Phase 2a, and the count of
// unresolved links that were suppressed.
type HealthResult struct {
	// Findings is the advisory worklist of vault-content issues. All Phase 1
	// and Phase 2a detectors contribute here.
	Findings []Finding
	// Stats is the structural graph-health summary (node/edge counts, connected
	// components, giant-component ratio, k-core maximum). Populated even when
	// Findings is empty.
	Stats GraphStats
	// SuppressedUnresolved is the count of unresolved wiki-links (links to a note
	// that does not exist) that were counted but not listed because unresolved
	// reporting was off. It lets the caller surface "N unresolved links" without
	// listing each one, so the suppression is visible rather than silent. When
	// reporting is on the count is zero (the links are in Findings instead).
	SuppressedUnresolved int
}

// RunHealthFull runs all vault-health detectors (Phase 1 and Phase 2a) over the
// given index and returns the collected findings together with the structural
// graph summary and the suppressed-unresolved count. It is deterministic and
// read-only: it never writes to the vault files or the index database. The order
// of findings within a detector is determined by the database sort order (stable
// across runs on the same index).
//
// reportUnresolved is the effective setting for listing unresolved wiki-links
// (a link to a note that does not exist). When false (the default; an Obsidian
// "unresolved link" is usually an intentional future note, not an error) such
// links are counted into HealthResult.SuppressedUnresolved rather than listed.
// Ambiguous links and attachment/folder classification are unaffected by it.
func RunHealthFull(cfg Config, db *sql.DB, reportUnresolved bool) (HealthResult, error) {
	var all []Finding

	dl, suppressed, err := detectDanglingLinks(cfg, db, reportUnresolved)
	if err != nil {
		return HealthResult{}, fmt.Errorf("dangling-link detector: %w", err)
	}
	all = append(all, dl...)

	pd, err := detectPARADrift(cfg, db)
	if err != nil {
		return HealthResult{}, fmt.Errorf("para-drift detector: %w", err)
	}
	all = append(all, pd...)

	os_, err := detectOversized(cfg, db)
	if err != nil {
		return HealthResult{}, fmt.Errorf("oversized detector: %w", err)
	}
	all = append(all, os_...)

	// Phase 2a: build the graph once and reuse it for all three graph metrics.
	g, err := buildGraph(db)
	if err != nil {
		return HealthResult{}, fmt.Errorf("graph build: %w", err)
	}

	ol, err := detectOrphansAndLeaves(cfg, db, g)
	if err != nil {
		return HealthResult{}, fmt.Errorf("orphan/leaf detector: %w", err)
	}
	all = append(all, ol...)

	islands := detectIslands(cfg, g)
	all = append(all, islands...)

	// Compute stats from the same graph.
	var stats GraphStats
	if g.nodeCount() > 0 {
		compCount, giantRatio := computeComponents(g)
		coreness, maxCore := computeCoreness(g)
		coreCount := make(map[int]int, maxCore+1)
		coreMap := make(map[string]int, g.nodeCount())
		for i, c := range coreness {
			coreMap[g.nodes[i]] = c
			coreCount[c]++
		}
		stats = GraphStats{
			NodeCount:      g.nodeCount(),
			EdgeCount:      g.edgeCount(),
			ComponentCount: compCount,
			GiantRatio:     giantRatio,
			MaxCore:        maxCore,
			CoreCount:      coreCount,
			Coreness:       coreMap,
		}
	} else {
		stats = GraphStats{
			CoreCount: map[int]int{},
			Coreness:  map[string]int{},
		}
	}

	return HealthResult{Findings: all, Stats: stats, SuppressedUnresolved: suppressed}, nil
}

// detectDanglingLinks finds every links row whose target_path is NULL and
// classifies it to match Obsidian's link semantics. A NULL target_path is
// written by the Phase 0 resolver when the raw target matched no note (dangling)
// or more than one (ambiguous). Before treating either as a finding, the
// detector excludes link targets that are not note links at all, then sorts the
// remainder. Classification order, on the canonical raw target (everything
// before the first '#' or '^', trimmed):
//
//  1. ATTACHMENT: the target ends with a known attachment extension (.png, .pdf,
//     ...). hebb does not index non-note files, so it cannot judge them broken;
//     these are excluded entirely.
//  2. FOLDER: the raw target ends with '/', or names an existing directory under
//     the vault. A folder/MOC link is not a note link; excluded.
//  3. AMBIGUOUS: 2+ case-insensitive note matches. A real data-quality issue,
//     reported by default as an ambiguous_link.
//  4. DANGLING: no note matches. This is an Obsidian "unresolved link", usually
//     an intentional future note. Reported as a per-link dangling_link only when
//     reportUnresolved is true; otherwise counted (returned suppressed) but not
//     listed.
//
// Re-running the resolver here keeps the detector read-only: it reads the notes
// table to classify but writes nothing.
func detectDanglingLinks(cfg Config, db *sql.DB, reportUnresolved bool) ([]Finding, int, error) {
	rows, err := db.Query(`
		SELECT source_path, target
		FROM links
		WHERE target_path IS NULL
		ORDER BY source_path, target
	`)
	if err != nil {
		return nil, 0, err
	}
	type nullLink struct{ source, target string }
	var links []nullLink
	scanErr := func() error {
		defer rows.Close()
		for rows.Next() {
			var source, target string
			if err := rows.Scan(&source, &target); err != nil {
				return err
			}
			links = append(links, nullLink{source, target})
		}
		return rows.Err()
	}()
	if scanErr != nil {
		return nil, 0, scanErr
	}
	if len(links) == 0 {
		return nil, 0, nil
	}

	// Build the in-memory index once so classification is one notes scan, not one
	// query per NULL link.
	ix, err := loadNoteIndex(db)
	if err != nil {
		return nil, 0, err
	}

	attachmentExts := attachmentExtSet(cfg.Health.GetAttachmentExtensions())

	var findings []Finding
	suppressed := 0
	for _, l := range links {
		canon := canonicalLinkTarget(l.target)

		// (1) Attachment links are not note links: exclude entirely.
		if isAttachmentTarget(canon, attachmentExts) {
			continue
		}
		// (2) Folder links (trailing slash or an existing directory) are not note
		// links: exclude.
		if isFolderTarget(cfg, l.target, canon) {
			continue
		}

		// (3)/(4) Re-run the resolver to tell ambiguous from genuinely unresolved.
		_, status := ix.resolve(l.target)
		if status == Ambiguous {
			findings = append(findings, Finding{
				Type:     "ambiguous_link",
				Path:     l.source,
				Detail:   fmt.Sprintf("[[%s]] is ambiguous (matches multiple notes)", l.target),
				Severity: "warn",
			})
			continue
		}
		// Unresolved (Obsidian "unresolved link"). Resolved is not possible here
		// (target_path would be non-NULL on the current graph).
		if !reportUnresolved {
			suppressed++
			continue
		}
		findings = append(findings, Finding{
			Type:     "dangling_link",
			Path:     l.source,
			Detail:   fmt.Sprintf("[[%s]] resolves to no note", l.target),
			Severity: "warn",
		})
	}
	return findings, suppressed, nil
}

// canonicalLinkTarget strips a wiki-link target down to the text used for
// classification and resolution: everything before the first '#' (heading
// fragment) or '^' (block reference), trimmed. canonicalTarget (links.go) only
// strips '#'; the leading-'^' block-ref form is handled here so an attachment or
// folder classification is not fooled by a trailing block reference.
func canonicalLinkTarget(raw string) string {
	t := canonicalTarget(raw)
	if i := strings.IndexByte(t, '^'); i != -1 {
		t = t[:i]
	}
	return strings.TrimSpace(t)
}

// attachmentExtSet builds a lowercased lookup set from the configured extension
// list (each without a leading dot).
func attachmentExtSet(exts []string) map[string]bool {
	set := make(map[string]bool, len(exts))
	for _, e := range exts {
		set[strings.ToLower(strings.TrimPrefix(e, "."))] = true
	}
	return set
}

// isAttachmentTarget reports whether the canonical target ends with one of the
// configured attachment extensions (case-insensitively).
func isAttachmentTarget(canon string, exts map[string]bool) bool {
	dot := strings.LastIndexByte(canon, '.')
	if dot < 0 || dot == len(canon)-1 {
		return false
	}
	return exts[strings.ToLower(canon[dot+1:])]
}

// isFolderTarget reports whether the link points at a folder rather than a note:
// the raw target ends with '/', or the canonical target names an existing
// directory under the vault. The directory check is read-only.
func isFolderTarget(cfg Config, raw, canon string) bool {
	if strings.HasSuffix(strings.TrimSpace(raw), "/") {
		return true
	}
	if canon == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(cfg.VaultPath, filepath.FromSlash(canon)))
	return err == nil && info.IsDir()
}

// doneStatuses is the set of frontmatter status values that indicate a project
// is complete but has not yet been moved out of 1-Projects/. Comparison is
// case-insensitive.
var doneStatuses = map[string]bool{
	"done":     true,
	"closed":   true,
	"complete": true,
	"archived": true,
}

// detectPARADrift finds notes under 1-Projects/ that are either:
//   - flagged as done/closed/complete/archived via their frontmatter status, or
//   - untouched for longer than cfg.Health.GetProjectStaleDays().
func detectPARADrift(cfg Config, db *sql.DB) ([]Finding, error) {
	staleDays := cfg.Health.GetProjectStaleDays()
	// mtime is stored as milliseconds since epoch (float64 in the schema).
	// Compute the cutoff as a float64 millisecond timestamp.
	cutoffMs := float64(timeNowForTest().AddDate(0, 0, -staleDays).UnixNano()) / 1e6

	rows, err := db.Query(`
		SELECT path, frontmatter, mtime
		FROM notes
		WHERE path LIKE '1-Projects/%'
		ORDER BY path
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var findings []Finding
	for rows.Next() {
		var path string
		var fmJSON sql.NullString
		var mtime float64
		if err := rows.Scan(&path, &fmJSON, &mtime); err != nil {
			return nil, err
		}

		// Check status first: a done/closed/complete/archived status is a clear
		// signal regardless of how recently the note was modified.
		if fmJSON.Valid && fmJSON.String != "" && fmJSON.String != "{}" {
			var fm map[string]any
			if err := json.Unmarshal([]byte(fmJSON.String), &fm); err == nil {
				if statusRaw, ok := fm["status"]; ok {
					status := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", statusRaw)))
					if doneStatuses[status] {
						findings = append(findings, Finding{
							Type:     "para_drift",
							Path:     path,
							Detail:   fmt.Sprintf("status: %s, still in 1-Projects", status),
							Severity: "warn",
						})
						continue
					}
				}
			}
		}

		// Check mtime: untouched beyond the stale-days threshold.
		if mtime < cutoffMs {
			ageDays := int(timeNowForTest().Sub(time.Unix(0, int64(mtime)*int64(time.Millisecond/time.Nanosecond)).UTC()).Hours() / 24)
			findings = append(findings, Finding{
				Type:     "para_drift",
				Path:     path,
				Detail:   fmt.Sprintf("untouched %d days (threshold %d)", ageDays, staleDays),
				Severity: "warn",
			})
		}
	}
	return findings, rows.Err()
}

// reH2H3 matches H2 and H3 headings (##  or ###) at the start of a line.
// The match is anchored to the line start with (?m) multiline mode.
var reH2H3 = regexp.MustCompile(`(?m)^#{2,3}\s+\S`)

// detectOversized finds notes whose estimated token count (len(body)/4) exceeds
// cfg.Health.GetSizeThreshold() AND whose raw file contains at least 3
// substantial H2/H3 sections. Size alone does not warrant a split; a note with
// multiple distinct sections is the real split candidate.
//
// "Substantial" means the heading is followed by at least one line of non-blank
// content before the next heading or end-of-file. This avoids counting a row of
// empty section headers as 3 sections.
func detectOversized(cfg Config, db *sql.DB) ([]Finding, error) {
	threshold := cfg.Health.GetSizeThreshold()

	// Pre-filter by token estimate over the indexed body: only read raw files
	// for notes that are already over the threshold. len(body)/4 is the same
	// estimate used by the caller; the raw file may differ slightly (headings
	// stripped in body) but the filter is conservative.
	rows, err := db.Query(`
		SELECT path, body
		FROM notes
		WHERE (length(body) / 4) > ?
		ORDER BY path
	`, threshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Collect candidates to close the rows before reading files.
	type candidate struct {
		path   string
		tokens int
	}
	var candidates []candidate
	for rows.Next() {
		var path, body string
		if err := rows.Scan(&path, &body); err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate{path: path, tokens: len(body) / 4})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var findings []Finding
	for _, c := range candidates {
		rawPath := filepath.Join(cfg.VaultPath, filepath.FromSlash(c.path))
		raw, err := os.ReadFile(rawPath)
		if err != nil {
			// File may have been removed since the last index; skip gracefully.
			continue
		}
		n := countSubstantialSections(string(raw))
		if n >= 3 {
			findings = append(findings, Finding{
				Type:     "oversized",
				Path:     c.path,
				Detail:   fmt.Sprintf("~%d tokens, %d sections - split candidate", c.tokens, n),
				Severity: "warn",
			})
		}
	}
	return findings, nil
}

// countSubstantialSections counts the number of H2/H3 headings in raw markdown
// that have at least one non-blank line of body content following them before
// the next H2/H3/H1 heading or end-of-file. This guards against treating a
// table-of-contents stub or an empty section skeleton as a split candidate.
func countSubstantialSections(raw string) int {
	lines := strings.Split(raw, "\n")
	count := 0
	inSection := false
	hasBody := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		isH1 := strings.HasPrefix(line, "# ") && !strings.HasPrefix(line, "## ")
		isH23 := reH2H3.MatchString(line)

		if isH23 {
			// Flush the previous section.
			if inSection && hasBody {
				count++
			}
			inSection = true
			hasBody = false
			continue
		}
		if isH1 {
			// H1 closes any open H2/H3 section without starting a new tracked one.
			if inSection && hasBody {
				count++
			}
			inSection = false
			hasBody = false
			continue
		}
		if inSection && trimmed != "" {
			hasBody = true
		}
	}
	// Flush the last open section.
	if inSection && hasBody {
		count++
	}
	return count
}
