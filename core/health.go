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

// RunHealth runs all Phase 1 vault-health detectors over the given index and
// returns the collected findings. It is deterministic and read-only: it never
// writes to the vault files or the index database. The order of findings within
// a detector is determined by the database sort order (stable across runs on the
// same index).
func RunHealth(cfg Config, db *sql.DB) ([]Finding, error) {
	var all []Finding

	dl, err := detectDanglingLinks(db)
	if err != nil {
		return nil, fmt.Errorf("dangling-link detector: %w", err)
	}
	all = append(all, dl...)

	pd, err := detectPARADrift(cfg, db)
	if err != nil {
		return nil, fmt.Errorf("para-drift detector: %w", err)
	}
	all = append(all, pd...)

	os_, err := detectOversized(cfg, db)
	if err != nil {
		return nil, fmt.Errorf("oversized detector: %w", err)
	}
	all = append(all, os_...)

	return all, nil
}

// detectDanglingLinks finds every links row whose target_path is NULL and
// classifies it by re-running the resolver. A NULL target_path is written by the
// Phase 0 resolver either because no note matched the raw target (Dangling) or
// because more than one note matched (Ambiguous). The two are distinct
// data-quality problems, so they are surfaced as distinct finding types with
// accurate wording: a dangling link must gain a target, an ambiguous one must be
// disambiguated. Re-running the resolver here keeps the detector read-only: it
// reads the notes table to classify but writes nothing.
func detectDanglingLinks(db *sql.DB) ([]Finding, error) {
	rows, err := db.Query(`
		SELECT source_path, target
		FROM links
		WHERE target_path IS NULL
		ORDER BY source_path, target
	`)
	if err != nil {
		return nil, err
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
		return nil, scanErr
	}
	if len(links) == 0 {
		return nil, nil
	}

	// Build the in-memory index once so classification is one notes scan, not one
	// query per NULL link.
	ix, err := loadNoteIndex(db)
	if err != nil {
		return nil, err
	}

	var findings []Finding
	for _, l := range links {
		_, status := ix.resolve(l.target)
		f := Finding{Path: l.source, Severity: "warn"}
		if status == Ambiguous {
			f.Type = "ambiguous_link"
			f.Detail = fmt.Sprintf("[[%s]] is ambiguous (matches multiple notes)", l.target)
		} else {
			// Resolved is not possible here (target_path would be non-NULL on the
			// current graph); treat anything that is not Ambiguous as dangling.
			f.Type = "dangling_link"
			f.Detail = fmt.Sprintf("[[%s]] resolves to no note", l.target)
		}
		findings = append(findings, f)
	}
	return findings, nil
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
