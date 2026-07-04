package core

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// DefaultConsolidationOutput is the weekly consolidation review note, relative
// to the vault root.
const DefaultConsolidationOutput = "2-Areas/_CONSOLIDATION.md"

// maxConsolidationProposals caps the number of ranked proposals written to the
// consolidation note. The known failure mode (METABOLISM's contrarian case) is
// a proposal list nobody actions, so the list stays tiny by design rather than
// exhaustive.
const maxConsolidationProposals = 5

// maxCoolingProjectCitations and maxThemeCitations cap the wiki-link citations
// listed per proposal: enough to ground the claim, few enough to stay scannable.
const (
	maxCoolingProjectCitations = 3
	maxThemeCitations          = 3
)

// minThemeLinksForProposal is the minimum recent-link count a theme's link
// target needs before it is worth proposing as a pin/MOC candidate.
const minThemeLinksForProposal = 3

// maxThemeProposals caps how many recurring-theme proposals are drawn from the
// themes report, regardless of how many qualify.
const maxThemeProposals = 2

// ConsolidationOptions configures a consolidation run. The zero value runs
// against the default output path at the current time.
type ConsolidationOptions struct {
	Output string    // consolidation note path relative to the vault root (default DefaultConsolidationOutput)
	Now    time.Time // run time, for testing; defaults to time.Now()
}

// ConsolidationResult summarises a consolidation run.
type ConsolidationResult struct {
	OutputPath string // the path written, as supplied (relative to vault, unless overridden absolute)
	Proposals  int    // number of proposals written
}

// consolidationProposal is one ranked, citation-bearing entry in the
// consolidation note.
type consolidationProposal struct {
	text    string   // the full proposal sentence, wiki-links inline
	sources []string // vault-relative paths cited, for the regulated-content guard
}

// GenerateConsolidation runs the temporal detectors and the themes report over
// the current index and writes at most maxConsolidationProposals ranked
// proposals to the consolidation note (2-Areas/_CONSOLIDATION.md by default).
// It is proposal-only: nothing here mutates vault content. Every content
// mutation implied by a proposal is left to a human via the vault-gardener
// skill. Callers should refresh the index (FullReindex) before calling so the
// detectors and themes report reflect the current vault.
//
// The file is fully overwritten on every run (no rolling history; it is a
// job-owned surface and git already keeps history), so freshness is owned
// mechanically rather than left to whoever last edited it.
func GenerateConsolidation(cfg Config, db *sql.DB, opts ConsolidationOptions) (ConsolidationResult, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	output := opts.Output
	if output == "" {
		output = DefaultConsolidationOutput
	}

	health, err := RunHealthFull(cfg, db, false)
	if err != nil {
		return ConsolidationResult{}, fmt.Errorf("consolidate: health run: %w", err)
	}
	themes, err := Themes(db, cfg, 30, now)
	if err != nil {
		return ConsolidationResult{}, fmt.Errorf("consolidate: themes: %w", err)
	}
	pinned, err := pinnedTitles(cfg)
	if err != nil {
		return ConsolidationResult{}, fmt.Errorf("consolidate: read %s: %w", nowPinBoard, err)
	}
	// Skip-regulated rule: computed up front so citation selection never picks
	// a regulated/compliance-tagged note as evidence in the first place, rather
	// than discovering it after a proposal has already committed to it.
	regulated, err := regulatedPaths(db)
	if err != nil {
		return ConsolidationResult{}, fmt.Errorf("consolidate: regulated lookup: %w", err)
	}

	var proposals []consolidationProposal

	if p := stalePinProposal(health.Findings); p != nil {
		proposals = append(proposals, *p)
	}
	proposals = append(proposals, coolingProjectProposals(db, health.Findings, regulated)...)
	proposals = append(proposals, themeProposals(db, themes, pinned, regulated)...)

	// Belt and braces: even with citation selection excluding regulated notes,
	// drop any proposal whose target itself is regulated (e.g. a themed link
	// target that is regulated content, not just one of its citations).
	proposals = filterRegulatedProposals(proposals, regulated)

	if len(proposals) > maxConsolidationProposals {
		proposals = proposals[:maxConsolidationProposals]
	}

	doc := renderConsolidationDocument(now, proposals)

	outPath := output
	if !filepath.IsAbs(outPath) {
		outPath = filepath.Join(cfg.VaultPath, filepath.FromSlash(output))
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return ConsolidationResult{}, err
	}
	if err := os.WriteFile(outPath, []byte(doc), 0o644); err != nil {
		return ConsolidationResult{}, err
	}

	return ConsolidationResult{OutputPath: output, Proposals: len(proposals)}, nil
}

// stalePinProposal composes one proposal covering every stale_pin finding, so
// a vault with several stale pins gets a single line rather than crowding out
// the other proposal families. Returns nil when there are no stale_pin
// findings.
func stalePinProposal(findings []Finding) *consolidationProposal {
	var details []string
	for _, f := range findings {
		if f.Type != "stale_pin" {
			continue
		}
		details = append(details, f.Detail)
	}
	if len(details) == 0 {
		return nil
	}
	text := fmt.Sprintf("Refresh or retire the pin — [[_NOW]] %s", strings.Join(details, "; "))
	return &consolidationProposal{text: text, sources: []string{nowPinBoard}}
}

// coolingProjectProposals builds one proposal per cooling_project finding,
// citing 2-3 of the folder's most recently changed notes as evidence.
// Regulated/compliance-tagged notes are never selected as citations.
func coolingProjectProposals(db *sql.DB, findings []Finding, regulated map[string]bool) []consolidationProposal {
	var out []consolidationProposal
	for _, f := range findings {
		if f.Type != "cooling_project" {
			continue
		}
		folder := strings.TrimSuffix(strings.TrimPrefix(f.Path, "1-Projects/"), "/")
		recent, err := recentNotesInFolder(db, f.Path, maxCoolingProjectCitations, regulated)
		if err != nil || len(recent) == 0 {
			continue
		}
		links := make([]string, 0, len(recent))
		sources := make([]string, 0, len(recent))
		for _, n := range recent {
			links = append(links, wikiLink(n.path, n.title))
			sources = append(sources, n.path)
		}
		text := fmt.Sprintf("Project [[%s]] has gone quiet (%s) — archive it or restate its next step. Recent notes: %s",
			folder, f.Detail, strings.Join(links, ", "))
		out = append(out, consolidationProposal{text: text, sources: sources})
	}
	return out
}

// themeProposals draws up to maxThemeProposals recurring-theme proposals from
// the themes report's top link targets: candidates need at least
// minThemeLinksForProposal recent links, must not already be pinned in
// _NOW.md, and must not look like an index/MOC-style note (which recurring
// links to are expected, not a signal). A regulated/compliance-tagged target
// is skipped outright, and a regulated note is never selected as a citation.
func themeProposals(db *sql.DB, themes ThemesReport, pinned map[string]bool, regulated map[string]bool) []consolidationProposal {
	var out []consolidationProposal
	for _, lt := range themes.LinkTargets {
		if len(out) >= maxThemeProposals {
			break
		}
		if lt.Links < minThemeLinksForProposal {
			continue
		}
		if regulated[lt.Path] {
			continue
		}
		if pinned[strings.ToLower(lt.Title)] {
			continue
		}
		if looksLikeIndexNote(lt.Path, lt.Title) {
			continue
		}
		sources, err := recentLinkSources(db, lt.Path, maxThemeCitations, regulated)
		if err != nil || len(sources) == 0 {
			continue
		}
		links := make([]string, 0, len(sources))
		srcPaths := make([]string, 0, len(sources)+1)
		for _, s := range sources {
			links = append(links, wikiLink(s.path, s.title))
			srcPaths = append(srcPaths, s.path)
		}
		srcPaths = append(srcPaths, lt.Path)
		text := fmt.Sprintf("%s keeps coming up (%d links from %d notes in the last 30 days) — consider pinning it in [[_NOW]] or giving it a map-of-content. Recent mentions: %s",
			wikiLink(lt.Path, lt.Title), lt.Links, lt.Sources, strings.Join(links, ", "))
		out = append(out, consolidationProposal{text: text, sources: srcPaths})
	}
	return out
}

// recentNote is one note path/title pair ordered by recency, used for
// citation lookups.
type recentNote struct {
	path  string
	title string
}

// citationFetchMultiplier over-fetches candidate rows before filtering out
// regulated notes in Go, so excluding a regulated candidate does not silently
// shrink a citation list that SQL's LIMIT already capped.
const citationFetchMultiplier = 4

// recentNotesInFolder returns up to limit notes under the given
// "1-Projects/<folder>/" prefix, most recently changed first, excluding any
// path present in regulated.
func recentNotesInFolder(db *sql.DB, folderPrefix string, limit int, regulated map[string]bool) ([]recentNote, error) {
	pattern := folderPrefix + "%"
	rows, err := db.Query(`
		SELECT path, title FROM notes
		WHERE path LIKE ? AND content_changed_at IS NOT NULL
		ORDER BY content_changed_at DESC, path ASC
		LIMIT ?`, pattern, limit*citationFetchMultiplier)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []recentNote
	for rows.Next() {
		var n recentNote
		if err := rows.Scan(&n.path, &n.title); err != nil {
			return nil, err
		}
		if regulated[n.path] {
			continue
		}
		out = append(out, n)
		if len(out) == limit {
			break
		}
	}
	return out, rows.Err()
}

// recentLinkSources returns up to limit distinct source notes that link to
// target, most recently changed first, excluding any path present in
// regulated.
func recentLinkSources(db *sql.DB, target string, limit int, regulated map[string]bool) ([]recentNote, error) {
	rows, err := db.Query(`
		SELECT n.path, n.title FROM notes n
		JOIN links l ON l.source_path = n.path
		WHERE l.target_path = ? AND n.content_changed_at IS NOT NULL
		ORDER BY n.content_changed_at DESC, n.path ASC
		LIMIT ?`, target, limit*citationFetchMultiplier)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []recentNote
	for rows.Next() {
		var n recentNote
		if err := rows.Scan(&n.path, &n.title); err != nil {
			return nil, err
		}
		if regulated[n.path] {
			continue
		}
		out = append(out, n)
		if len(out) == limit {
			break
		}
	}
	return out, rows.Err()
}

// reNowPinHeading matches an H2 pin heading in _NOW.md.
var reNowPinHeading = regexp.MustCompile(`(?m)^##\s+(.+?)\s*$`)

// pinnedTitles reads _NOW.md (if present) and returns the lower-cased set of
// its pin headings, so theme proposals can skip a note that is already pinned.
// No _NOW.md means an empty set, matching detectStalePins' "optional" posture.
func pinnedTitles(cfg Config) (map[string]bool, error) {
	raw, err := os.ReadFile(filepath.Join(cfg.VaultPath, nowPinBoard))
	if os.IsNotExist(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, m := range reNowPinHeading.FindAllStringSubmatch(string(raw), -1) {
		out[strings.ToLower(strings.TrimSpace(m[1]))] = true
	}
	return out, nil
}

// indexNameHints are basename/title fragments that mark a note as an
// index/map-of-content note rather than an ordinary topic note: recurring
// links to these are expected structural traffic, not a "this keeps coming
// up" signal worth proposing.
var indexNameHints = []string{"index", "moc", "map-of-content", "map of content", "readme", "_series-info"}

// looksLikeIndexNote reports whether a candidate note's path or title marks it
// as an index/MOC-style note. There is no formal marker for this in the vault
// contract, so it is a conservative name-based heuristic: any basename-without-
// extension or title containing one of indexNameHints (case-insensitive), or a
// basename starting with "_" (hebb's convention for machine/structural notes,
// same as the themes and digest exclusions).
func looksLikeIndexNote(notePath, title string) bool {
	base := strings.TrimSuffix(filepath.Base(notePath), ".md")
	if strings.HasPrefix(base, "_") {
		return true
	}
	lowerBase := strings.ToLower(base)
	lowerTitle := strings.ToLower(title)
	for _, hint := range indexNameHints {
		if strings.Contains(lowerBase, hint) || strings.Contains(lowerTitle, hint) {
			return true
		}
	}
	return false
}

// regulatedPaths returns the set of vault-relative note paths whose tags
// contain "regulated" or "compliance" (case-insensitive), per the hard rule
// that consolidation never touches or cites regulated/compliance-tagged
// content (ROADMAP "Principles"; METABOLISM's safety carve-out).
func regulatedPaths(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query(`SELECT path, COALESCE(tags, '') FROM notes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var path, tags string
		if err := rows.Scan(&path, &tags); err != nil {
			return nil, err
		}
		for _, tag := range splitThemeTags(tags) {
			lt := strings.ToLower(strings.TrimPrefix(tag, "#"))
			if lt == "regulated" || lt == "compliance" {
				out[path] = true
				break
			}
		}
	}
	return out, rows.Err()
}

// filterRegulatedProposals drops any proposal that cites a regulated or
// compliance-tagged note as a source, preserving order among the survivors.
func filterRegulatedProposals(proposals []consolidationProposal, regulated map[string]bool) []consolidationProposal {
	if len(regulated) == 0 {
		return proposals
	}
	out := make([]consolidationProposal, 0, len(proposals))
	for _, p := range proposals {
		blocked := false
		for _, s := range p.sources {
			if regulated[s] {
				blocked = true
				break
			}
		}
		if !blocked {
			out = append(out, p)
		}
	}
	return out
}

func wikiLink(rel, title string) string {
	return fmt.Sprintf("[[%s|%s]]", strings.TrimSuffix(rel, ".md"), title)
}

const consolidationIntro = "Generated weekly by hebb; proposals only. Act on these via the " +
	"vault-gardener skill. Safe to ignore — it regenerates next week."

// renderConsolidationDocument builds the full consolidation note: an H1, the
// "what this is" line, the run date, and a numbered "## Proposals" section (or
// a single reassuring line when there is nothing to propose). The file is
// meant to be overwritten wholesale each run, so this never reads or merges
// prior content the way the digest does.
func renderConsolidationDocument(now time.Time, proposals []consolidationProposal) string {
	var b strings.Builder
	b.WriteString("# Consolidation review\n\n")
	b.WriteString(consolidationIntro + "\n\n")
	fmt.Fprintf(&b, "Generated: %s\n\n", now.Format("2006-01-02"))
	b.WriteString("## Proposals\n\n")
	if len(proposals) == 0 {
		b.WriteString("Nothing needs attention this week.\n")
		return b.String()
	}
	for i, p := range proposals {
		fmt.Fprintf(&b, "%d. %s\n", i+1, p.text)
		if i < len(proposals)-1 {
			b.WriteString("\n")
		}
	}
	if !strings.HasSuffix(b.String(), "\n") {
		b.WriteString("\n")
	}
	return b.String()
}
