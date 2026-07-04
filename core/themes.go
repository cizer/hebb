package core

import (
	"database/sql"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
)

// Result caps for the themes report. The report is a scannable "where do the
// discussions keep returning" summary, not a dump: enough rows to see the
// pattern, few enough to read in one glance.
const (
	maxThemeLinkTargets  = 15
	maxThemeTagMomentum  = 10
	maxThemeCoOccurrence = 10
)

// defaultThemesDays is the trailing window applied when the caller passes a
// non-positive days value.
const defaultThemesDays = 30

// ThemeLinkTarget is one row of the link-target frequency table: a resolved
// link target and how often recently-changed notes point at it.
type ThemeLinkTarget struct {
	// Path is the vault-relative path of the target note (resolved target_path;
	// raw unresolved targets never appear here).
	Path string `json:"path"`
	// Title is the target note's title.
	Title string `json:"title"`
	// Links is the number of resolved links to this target from sources whose
	// content changed inside the window.
	Links int `json:"links"`
	// Sources is the number of distinct recently-changed source notes linking
	// to this target.
	Sources int `json:"sources"`
}

// ThemeTagMomentum is one row of the tag-momentum table: how often a tag
// appears on notes changed in the current window versus the equal-length
// window immediately before it.
type ThemeTagMomentum struct {
	Tag string `json:"tag"`
	// Count is the number of notes changed in the window that carry the tag.
	Count int `json:"count"`
	// Previous is the same count over the equal-length preceding window.
	Previous int `json:"previous"`
	// Delta is Count - Previous: positive means rising, negative falling.
	Delta int `json:"delta"`
}

// ThemeCoOccurrence is one row of the co-occurrence table: two targets that
// recently-changed source notes keep linking together.
type ThemeCoOccurrence struct {
	// A and B are the vault-relative paths of the pair, ordered so A < B
	// lexicographically (deterministic pair identity).
	A string `json:"a"`
	B string `json:"b"`
	// ATitle and BTitle are the corresponding note titles.
	ATitle string `json:"a_title"`
	BTitle string `json:"b_title"`
	// SharedSources is the number of distinct recently-changed source notes
	// that link to both A and B.
	SharedSources int `json:"shared_sources"`
}

// ThemesReport is the recurrence report over a trailing content-change
// window: which link targets the recent work keeps returning to, which tags
// are gaining or losing momentum, and which targets are repeatedly linked
// together. It is computed entirely from the index (notes + resolved links)
// and never reads or writes vault files.
type ThemesReport struct {
	// Days is the trailing window length actually used (defaulted when the
	// caller passed <= 0).
	Days int `json:"days"`
	// WindowStart is the lower bound of the window (now - Days).
	WindowStart time.Time `json:"window_start"`
	// NotesChanged is the number of notes whose content changed inside the
	// window, after exclusions (machine notes and exclude_from_graph matches).
	NotesChanged int `json:"notes_changed"`
	// LinkTargets is the top link targets by links from recently-changed
	// sources (max maxThemeLinkTargets rows).
	LinkTargets []ThemeLinkTarget `json:"link_targets"`
	// TagMomentum is the top tags on recently-changed notes with their
	// window-over-window delta (max maxThemeTagMomentum rows).
	TagMomentum []ThemeTagMomentum `json:"tag_momentum"`
	// CoOccurrence is the top co-linked target pairs (max
	// maxThemeCoOccurrence rows).
	CoOccurrence []ThemeCoOccurrence `json:"co_occurrence"`
}

// themeNote is the in-memory projection of one notes row used by Themes.
type themeNote struct {
	title     string
	tags      string
	changedAt float64
	hasStamp  bool
}

// themeExcluded reports whether a note is kept out of the themes report
// entirely (as a source, a target, and a tag carrier). Two exclusion families
// apply, mirroring how the digest and the graph detectors strip machine
// scaffolding:
//   - any basename starting with "_" (the digest-style machine notes:
//     _DAILY-DIGEST.md, _ACTION-REVIEW.md, _INGEST-LOG.md, _NOW.md, ...),
//   - anything matching cfg.Health.ExcludeFromGraph (title, basename without
//     .md, or vault-relative path; path.Match semantics).
func themeExcluded(patterns []string, title, notePath string) bool {
	if strings.HasPrefix(path.Base(notePath), "_") {
		return true
	}
	return matchesExcludePatterns(patterns, title, notePath)
}

// splitThemeTags splits the notes.tags column (written space-joined by the
// indexer; commas tolerated for robustness) into individual tags.
func splitThemeTags(tags string) []string {
	return strings.FieldsFunc(tags, func(r rune) bool {
		return r == ' ' || r == ',' || r == '\t'
	})
}

// Themes computes the recurrence report over the trailing window of days
// ending at now. It is read-only: a pure query layer over the index, exported
// so the CLI (hebb themes) and a future consolidate job can share it. days <= 0
// applies the default (30); 14 is the other window the organiser uses.
//
// Exclusions: notes whose basename starts with "_" and notes matching
// cfg.Health.ExcludeFromGraph are ignored both as sources and as targets, so
// machine-generated hubs (digests, action reviews, pin boards) cannot
// dominate the recurrence signal. Only resolved links (target_path set)
// count; raw unresolved targets are never aggregated.
func Themes(db *sql.DB, cfg Config, days int, now time.Time) (ThemesReport, error) {
	if days <= 0 {
		days = defaultThemesDays
	}
	patterns := cfg.Health.GetExcludeFromGraph()
	// A malformed glob must fail the run, not silently compute the report over
	// the unfiltered vault (same posture as the graph detectors).
	if err := validateExcludePatterns(patterns); err != nil {
		return ThemesReport{}, err
	}

	windowStart := now.AddDate(0, 0, -days)
	cutoffMs := float64(windowStart.UnixNano()) / 1e6
	prevCutoffMs := float64(now.AddDate(0, 0, -2*days).UnixNano()) / 1e6

	report := ThemesReport{
		Days:         days,
		WindowStart:  windowStart,
		LinkTargets:  []ThemeLinkTarget{},
		TagMomentum:  []ThemeTagMomentum{},
		CoOccurrence: []ThemeCoOccurrence{},
	}

	// Load every note once: exclusion needs the title, and the tag-momentum
	// pass needs the previous window too, so one scan feeds everything.
	notes := map[string]themeNote{}
	rows, err := db.Query(`SELECT path, title, COALESCE(tags, ''), content_changed_at FROM notes`)
	if err != nil {
		return ThemesReport{}, fmt.Errorf("themes: load notes: %w", err)
	}
	scanErr := func() error {
		defer rows.Close()
		for rows.Next() {
			var p, title, tags string
			var changedAt sql.NullFloat64
			if err := rows.Scan(&p, &title, &tags, &changedAt); err != nil {
				return err
			}
			notes[p] = themeNote{
				title:     title,
				tags:      tags,
				changedAt: changedAt.Float64,
				hasStamp:  changedAt.Valid,
			}
		}
		return rows.Err()
	}()
	if scanErr != nil {
		return ThemesReport{}, fmt.Errorf("themes: scan notes: %w", scanErr)
	}

	included := func(p string) (themeNote, bool) {
		n, ok := notes[p]
		if !ok || themeExcluded(patterns, n.title, p) {
			return themeNote{}, false
		}
		return n, true
	}
	inWindow := func(n themeNote) bool { return n.hasStamp && n.changedAt > cutoffMs }
	inPrevWindow := func(n themeNote) bool {
		return n.hasStamp && n.changedAt > prevCutoffMs && n.changedAt <= cutoffMs
	}

	// Pass 1: window membership, note count, and tag momentum.
	current := map[string]int{}
	previous := map[string]int{}
	recentSource := map[string]bool{}
	for p, n := range notes {
		if themeExcluded(patterns, n.title, p) {
			continue
		}
		switch {
		case inWindow(n):
			report.NotesChanged++
			recentSource[p] = true
			for _, tag := range splitThemeTags(n.tags) {
				current[tag]++
			}
		case inPrevWindow(n):
			for _, tag := range splitThemeTags(n.tags) {
				previous[tag]++
			}
		}
	}

	for tag, count := range current {
		report.TagMomentum = append(report.TagMomentum, ThemeTagMomentum{
			Tag:      tag,
			Count:    count,
			Previous: previous[tag],
			Delta:    count - previous[tag],
		})
	}
	sort.Slice(report.TagMomentum, func(i, j int) bool {
		a, b := report.TagMomentum[i], report.TagMomentum[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		if a.Delta != b.Delta {
			return a.Delta > b.Delta
		}
		return a.Tag < b.Tag
	})
	if len(report.TagMomentum) > maxThemeTagMomentum {
		report.TagMomentum = report.TagMomentum[:maxThemeTagMomentum]
	}

	// Pass 2: resolved links from recently-changed sources. One query feeds
	// both the target-frequency table and the per-source target sets used for
	// co-occurrence.
	type targetAgg struct {
		links   int
		sources map[string]bool
	}
	targets := map[string]*targetAgg{}
	perSource := map[string]map[string]bool{}
	lrows, err := db.Query(`
		SELECT source_path, target_path
		FROM links
		WHERE target_path IS NOT NULL
		ORDER BY source_path, target_path
	`)
	if err != nil {
		return ThemesReport{}, fmt.Errorf("themes: load links: %w", err)
	}
	scanErr = func() error {
		defer lrows.Close()
		for lrows.Next() {
			var source, target string
			if err := lrows.Scan(&source, &target); err != nil {
				return err
			}
			if !recentSource[source] || source == target {
				continue
			}
			if _, ok := included(target); !ok {
				continue
			}
			ta := targets[target]
			if ta == nil {
				ta = &targetAgg{sources: map[string]bool{}}
				targets[target] = ta
			}
			ta.links++
			ta.sources[source] = true
			set := perSource[source]
			if set == nil {
				set = map[string]bool{}
				perSource[source] = set
			}
			set[target] = true
		}
		return lrows.Err()
	}()
	if scanErr != nil {
		return ThemesReport{}, fmt.Errorf("themes: scan links: %w", scanErr)
	}

	for target, ta := range targets {
		report.LinkTargets = append(report.LinkTargets, ThemeLinkTarget{
			Path:    target,
			Title:   notes[target].title,
			Links:   ta.links,
			Sources: len(ta.sources),
		})
	}
	sort.Slice(report.LinkTargets, func(i, j int) bool {
		a, b := report.LinkTargets[i], report.LinkTargets[j]
		if a.Links != b.Links {
			return a.Links > b.Links
		}
		if a.Sources != b.Sources {
			return a.Sources > b.Sources
		}
		return a.Path < b.Path
	})
	if len(report.LinkTargets) > maxThemeLinkTargets {
		report.LinkTargets = report.LinkTargets[:maxThemeLinkTargets]
	}

	// Pass 3: co-occurrence. For each recent source, every unordered pair of
	// its distinct targets counts one shared source. Pairs are keyed with the
	// lexicographically smaller path first, so identity is deterministic.
	pairCount := map[[2]string]int{}
	for _, set := range perSource {
		ts := make([]string, 0, len(set))
		for t := range set {
			ts = append(ts, t)
		}
		sort.Strings(ts)
		for i := 0; i < len(ts); i++ {
			for j := i + 1; j < len(ts); j++ {
				pairCount[[2]string{ts[i], ts[j]}]++
			}
		}
	}
	for pair, count := range pairCount {
		report.CoOccurrence = append(report.CoOccurrence, ThemeCoOccurrence{
			A:             pair[0],
			B:             pair[1],
			ATitle:        notes[pair[0]].title,
			BTitle:        notes[pair[1]].title,
			SharedSources: count,
		})
	}
	sort.Slice(report.CoOccurrence, func(i, j int) bool {
		a, b := report.CoOccurrence[i], report.CoOccurrence[j]
		if a.SharedSources != b.SharedSources {
			return a.SharedSources > b.SharedSources
		}
		if a.A != b.A {
			return a.A < b.A
		}
		return a.B < b.B
	})
	if len(report.CoOccurrence) > maxThemeCoOccurrence {
		report.CoOccurrence = report.CoOccurrence[:maxThemeCoOccurrence]
	}

	return report, nil
}
