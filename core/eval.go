package core

import (
	"database/sql"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// EvalCase is one golden-file case: a query that should surface a specific set
// of notes. Expect holds vault-relative paths (matching the "path" column in
// the notes table / links table, e.g. "2-Areas/Board/Board pack process.md").
// Limit is the result-set size passed to Search; 0 means "use the default"
// (10, mirroring Search's own default).
type EvalCase struct {
	Query  string   `toml:"query"`
	Expect []string `toml:"expect"`
	Limit  int      `toml:"limit"`
}

// GoldenFile is the on-disk shape of the golden retrieval-eval file: a TOML
// array of cases under the [[case]] table-array key.
type GoldenFile struct {
	Case []EvalCase `toml:"case"`
}

// LoadGolden reads and parses a golden file from path. It is a thin wrapper
// over BurntSushi/toml so callers (cli/eval.go) get a plain os.IsNotExist
// error they can turn into a friendly "no golden file yet" message rather than
// a hard failure.
func LoadGolden(path string) ([]EvalCase, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var gf GoldenFile
	if _, err := toml.Decode(string(data), &gf); err != nil {
		return nil, fmt.Errorf("parse golden file %s: %w", path, err)
	}
	return gf.Case, nil
}

// CaseResult is the outcome of running one EvalCase against the live index.
type CaseResult struct {
	Query string
	// Limit is the effective limit used for the Search call (after defaulting).
	Limit int
	// Hits are expected paths that were found, in the order they appear in
	// Expect, each with its 1-based rank in the result set.
	Hits []Hit
	// Misses are expected paths that were not returned within Limit results.
	Misses []string
}

// Hit is an expected note that was found, and where.
type Hit struct {
	Path string
	Rank int // 1-based position in the search result set
}

// Passed reports whether every expected note for this case was found.
func (c CaseResult) Passed() bool {
	return len(c.Misses) == 0
}

// EvalReport is the full output of RunEval: one CaseResult per input case plus
// aggregate recall and mean reciprocal rank across all expected notes.
type EvalReport struct {
	Cases []CaseResult
	// TotalExpected is the sum of len(case.Expect) across all cases.
	TotalExpected int
	// TotalFound is the sum of len(case.Hits) across all cases.
	TotalFound int
	// Recall is TotalFound / TotalExpected (0 when TotalExpected is 0).
	Recall float64
	// MRR is the mean reciprocal rank over found notes only (1/rank averaged
	// across every Hit in every case; notes that were missed do not
	// contribute a 0 term, since MRR is conventionally computed over the
	// queries/items that returned a relevant result at all). MRR is 0 when no
	// notes were found.
	MRR float64
}

// Passed reports whether every case in the report passed (no misses anywhere).
// This is the signal cli/eval.go uses to decide the process exit code.
func (r EvalReport) Passed() bool {
	for _, c := range r.Cases {
		if !c.Passed() {
			return false
		}
	}
	return true
}

// RunEval runs each golden case against the live index via the existing
// core.Search (FTS5 only; no embeddings, no new dependencies) and records,
// per expected note, whether it was returned and at what rank. It is
// read-only: it never mutates the vault or the index.
func RunEval(db *sql.DB, cases []EvalCase) (EvalReport, error) {
	report := EvalReport{Cases: make([]CaseResult, 0, len(cases))}

	var mrrSum float64
	var mrrCount int

	for _, c := range cases {
		limit := c.Limit
		if limit <= 0 {
			limit = 10
		}
		results, err := Search(db, c.Query, limit, "", "")
		if err != nil {
			return EvalReport{}, fmt.Errorf("eval case %q: %w", c.Query, err)
		}

		rank := make(map[string]int, len(results))
		for i, r := range results {
			// First occurrence wins if a path somehow repeats; Search does not
			// currently de-dupe, but rank should reflect the earliest hit.
			if _, ok := rank[r.Path]; !ok {
				rank[r.Path] = i + 1
			}
		}

		cr := CaseResult{Query: c.Query, Limit: limit}
		for _, want := range c.Expect {
			if rk, ok := rank[want]; ok {
				cr.Hits = append(cr.Hits, Hit{Path: want, Rank: rk})
				mrrSum += 1.0 / float64(rk)
				mrrCount++
			} else {
				cr.Misses = append(cr.Misses, want)
			}
		}

		report.Cases = append(report.Cases, cr)
		report.TotalExpected += len(c.Expect)
		report.TotalFound += len(cr.Hits)
	}

	if report.TotalExpected > 0 {
		report.Recall = float64(report.TotalFound) / float64(report.TotalExpected)
	}
	if mrrCount > 0 {
		report.MRR = mrrSum / float64(mrrCount)
	}

	return report, nil
}
