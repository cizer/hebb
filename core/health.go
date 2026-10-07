package core

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
	// "para_drift", "cooling_project", "stale_pin", "oversized", "stub".
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

	cp, err := detectCoolingProjects(cfg, db)
	if err != nil {
		return HealthResult{}, fmt.Errorf("cooling-project detector: %w", err)
	}
	all = append(all, cp...)

	sp, err := detectStalePins(cfg)
	if err != nil {
		return HealthResult{}, fmt.Errorf("stale-pin detector: %w", err)
	}
	all = append(all, sp...)

	os_, err := detectOversized(cfg, db)
	if err != nil {
		return HealthResult{}, fmt.Errorf("oversized detector: %w", err)
	}
	all = append(all, os_...)

	sb, err := detectStub(cfg, db)
	if err != nil {
		return HealthResult{}, fmt.Errorf("stub detector: %w", err)
	}
	all = append(all, sb...)

	// Phase 2a: build the graph once (with any exclude_from_graph patterns applied)
	// and reuse it for all three graph metrics. The content detectors above this
	// point (dangling_link, ambiguous_link, para_drift, oversized, stub) are
	// unaffected: they run over ALL notes regardless of exclusion, because
	// exclusion is about graph centrality only, not about hiding note content.
	g, err := buildGraphExcluding(db, cfg.Health.GetExcludeFromGraph())
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

	// Agent memory (.hebb/memory/) is indexed for search only and is not vault
	// content, so no detector's finding about it reaches the worklist: a short
	// memory note would otherwise read as a stub. The graph already leaves it
	// out (buildGraphExcluding), so the stats below are unaffected too.
	kept := all[:0]
	for _, f := range all {
		if !IsMemoryNote(f.Path) {
			kept = append(kept, f)
		}
	}
	all = kept

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
		// Pass the finding's own source path so the same-directory preference
		// applies here exactly as it did when the link was indexed, keeping this
		// read-only classification consistent with the stored target_path.
		_, status := ix.resolve(l.target, l.source)
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

// detectCoolingProjects finds direct subfolders of 1-Projects/ (project
// folders) whose most recent content change, taken as MAX(content_changed_at)
// over every note anywhere under the folder, is older than
// cfg.Health.GetCoolingProjectDays(). This is folder-level PARA drift: the
// per-note para_drift detector flags individual stale notes (including notes
// sitting directly in the 1-Projects/ root, which have no project folder and
// are therefore skipped here), while cooling_project asks whether the project
// as a whole has stopped moving. It is driven by content_changed_at, not
// mtime, so a bulk operation that rewrites bytes without changing content
// cannot make a cooling project look active.
func detectCoolingProjects(cfg Config, db *sql.DB) ([]Finding, error) {
	days := cfg.Health.GetCoolingProjectDays()
	now := timeNowForTest()
	cutoffMs := float64(now.AddDate(0, 0, -days).UnixNano()) / 1e6

	// path LIKE '1-Projects/%/%' requires at least one path segment between
	// 1-Projects/ and the basename, so notes directly in the 1-Projects/ root
	// (already covered by para_drift) never contribute to a folder aggregate.
	rows, err := db.Query(`
		SELECT path, content_changed_at
		FROM notes
		WHERE path LIKE '1-Projects/%/%'
		ORDER BY path
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type folderAgg struct {
		lastChangedMs float64 // max content_changed_at over notes with a stamp
		hasStamp      bool
		noteCount     int
	}
	agg := map[string]*folderAgg{}
	for rows.Next() {
		var path string
		var changedAt sql.NullFloat64
		if err := rows.Scan(&path, &changedAt); err != nil {
			return nil, err
		}
		// The project folder is the first segment after "1-Projects/".
		rest := strings.TrimPrefix(path, "1-Projects/")
		folder := rest[:strings.IndexByte(rest, '/')]
		a := agg[folder]
		if a == nil {
			a = &folderAgg{}
			agg[folder] = a
		}
		a.noteCount++
		if changedAt.Valid {
			if !a.hasStamp || changedAt.Float64 > a.lastChangedMs {
				a.lastChangedMs = changedAt.Float64
				a.hasStamp = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Deterministic output order: sort folders by name.
	folders := make([]string, 0, len(agg))
	for f := range agg {
		folders = append(folders, f)
	}
	sort.Strings(folders)

	var findings []Finding
	for _, folder := range folders {
		a := agg[folder]
		// A folder with no content_changed_at stamp at all (possible on an index
		// that predates the column's backfill) cannot be judged; skip it rather
		// than guess.
		if !a.hasStamp || a.lastChangedMs >= cutoffMs {
			continue
		}
		ageDays := int(now.Sub(time.UnixMilli(int64(a.lastChangedMs))).Hours() / 24)
		findings = append(findings, Finding{
			Type:     "cooling_project",
			Path:     "1-Projects/" + folder + "/",
			Detail:   fmt.Sprintf("no note changed in ~%d days (%d notes)", ageDays, a.noteCount),
			Severity: "warn",
		})
	}
	return findings, nil
}

// nowPinBoard is the front-and-centre pin board at the vault root. Its
// contract (documented in the vault-template's _NOW.md) is: each pin is an H2
// heading, and the first non-blank line under it is "review-by: YYYY-MM-DD".
const nowPinBoard = "_NOW.md"

// reReviewBy matches the review-by line under a pin heading and captures the
// date. The prefix is matched case-insensitively so "Review-by:" also counts.
var reReviewBy = regexp.MustCompile(`(?i)^review-by:\s*(\d{4}-\d{2}-\d{2})\s*$`)

// detectStalePins parses _NOW.md at the vault root (raw file read; the pin
// board is often excluded from graph metrics and its structure, not its
// indexed body, is what matters here) and enforces the pin-decay contract:
// every H2 pin carries a "review-by: YYYY-MM-DD" first line, and a pin past
// that date is stale. A pin with no review-by line (or an unparseable date) is
// also flagged: a pin that can never decay violates the contract. No _NOW.md
// file means no findings; the pin board is optional.
func detectStalePins(cfg Config) ([]Finding, error) {
	raw, err := os.ReadFile(filepath.Join(cfg.VaultPath, nowPinBoard))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	today := timeNowForTest()
	// Compare at day granularity in local time: a pin is past its review-by
	// only once that whole day is over.
	todayDate := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())

	var findings []Finding
	lines := strings.Split(string(raw), "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if !strings.HasPrefix(line, "## ") {
			continue
		}
		heading := strings.TrimSpace(strings.TrimPrefix(line, "## "))

		// Find the first non-blank line under the heading.
		var first string
		for j := i + 1; j < len(lines); j++ {
			if t := strings.TrimSpace(lines[j]); t != "" {
				first = t
				break
			}
		}

		m := reReviewBy.FindStringSubmatch(first)
		if m == nil {
			findings = append(findings, Finding{
				Type:     "stale_pin",
				Path:     nowPinBoard,
				Detail:   fmt.Sprintf("pin %q has no review-by date", heading),
				Severity: "warn",
			})
			continue
		}
		reviewBy, perr := time.ParseInLocation("2006-01-02", m[1], today.Location())
		if perr != nil {
			// Matched the shape but not a real date (e.g. 2026-13-99): the pin
			// still cannot decay, so it violates the contract the same way.
			findings = append(findings, Finding{
				Type:     "stale_pin",
				Path:     nowPinBoard,
				Detail:   fmt.Sprintf("pin %q has no review-by date", heading),
				Severity: "warn",
			})
			continue
		}
		if reviewBy.Before(todayDate) {
			findings = append(findings, Finding{
				Type:     "stale_pin",
				Path:     nowPinBoard,
				Detail:   fmt.Sprintf("pin %q past review-by %s", heading, m[1]),
				Severity: "warn",
			})
		}
	}
	return findings, nil
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

// detectStub finds notes that are genuine dead stubs: near-empty notes with no
// outbound resolved links that are not under the configured expected-orphan
// folders. All three conditions must hold:
//
//  1. The note's estimated token count (len(body)/4) is below
//     cfg.Health.GetStubThreshold() (default 20).
//  2. The note has zero outbound resolved links: no row in links where
//     source_path = this note AND target_path IS NOT NULL. A thin note that
//     links out is likely an intentional map/index stub, not a dead one.
//  3. The note is NOT under any of the expected-orphan folder prefixes
//     (cfg.Health.GetExpectedOrphanFolders(), default ["Journal", "Notes",
//     "4-Archives"]), where short sparse notes are entirely normal.
//
// The detector runs over all notes regardless of exclude_from_graph (it
// concerns the note's own content and outbound links, not graph centrality).
func detectStub(cfg Config, db *sql.DB) ([]Finding, error) {
	threshold := cfg.Health.GetStubThreshold()
	orphanFolders := cfg.Health.GetExpectedOrphanFolders()

	// Select notes whose body token estimate is below the stub threshold.
	rows, err := db.Query(`
		SELECT path, body
		FROM notes
		WHERE (length(body) / 4) < ?
		ORDER BY path
	`, threshold)
	if err != nil {
		return nil, err
	}
	type candidate struct {
		path   string
		tokens int
	}
	var candidates []candidate
	scanErr := func() error {
		defer rows.Close()
		for rows.Next() {
			var path, body string
			if err := rows.Scan(&path, &body); err != nil {
				return err
			}
			candidates = append(candidates, candidate{path: path, tokens: len(body) / 4})
		}
		return rows.Err()
	}()
	if scanErr != nil {
		return nil, scanErr
	}

	// The set of notes with at least one resolved outbound link, built in a single
	// query rather than a COUNT per candidate, so the stub check does not degrade
	// into an N+1 pattern that slows `hebb audit` on large vaults.
	linked := map[string]bool{}
	lrows, err := db.Query(`SELECT DISTINCT source_path FROM links WHERE target_path IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	if scanErr := func() error {
		defer lrows.Close()
		for lrows.Next() {
			var p string
			if err := lrows.Scan(&p); err != nil {
				return err
			}
			linked[p] = true
		}
		return lrows.Err()
	}(); scanErr != nil {
		return nil, scanErr
	}

	var findings []Finding
	for _, c := range candidates {
		// Condition 3: skip notes under expected-orphan folders.
		if isUnderFolders(c.path, orphanFolders) {
			continue
		}

		// Condition 2: skip notes that have at least one resolved outbound link
		// (a thin note that links out is an intentional map or index stub).
		if linked[c.path] {
			continue
		}

		findings = append(findings, Finding{
			Type:     "stub",
			Path:     c.path,
			Detail:   fmt.Sprintf("near-empty (~%d tokens), links nowhere - merge or archive?", c.tokens),
			Severity: "warn",
		})
	}
	return findings, nil
}

// isUnderFolders reports whether the vault-relative path sits under any of the
// given folder prefixes. The comparison is prefix-based: a folder "Journal"
// matches "Journal/foo.md" but not "JournalExtra/foo.md". The path separator
// is always '/' in vault-relative paths.
func isUnderFolders(path string, folders []string) bool {
	for _, folder := range folders {
		prefix := strings.TrimSuffix(folder, "/") + "/"
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
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
