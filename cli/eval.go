package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cizer/hebb/core"
	"github.com/spf13/cobra"
)

// evalJSONReport is the JSON shape emitted by `hebb eval --json`. It mirrors
// core.EvalReport with stable lowercase field names so before/after runs can
// be diffed mechanically (e.g. around a gardening or consolidation action).
type evalJSONReport struct {
	Cases         []evalJSONCase `json:"cases"`
	TotalExpected int            `json:"total_expected"`
	TotalFound    int            `json:"total_found"`
	Recall        float64        `json:"recall"`
	MRR           float64        `json:"mrr"`
	Passed        bool           `json:"passed"`
}

type evalJSONCase struct {
	Query  string        `json:"query"`
	Limit  int           `json:"limit"`
	Hits   []evalJSONHit `json:"hits"`
	Misses []string      `json:"misses"`
	Passed bool          `json:"passed"`
}

type evalJSONHit struct {
	Path string `json:"path"`
	Rank int    `json:"rank"`
}

func evalCmd() *cobra.Command {
	var golden string
	var asJSON bool
	c := &cobra.Command{
		Use:   "eval",
		Short: "Score retrieval quality against a golden query file",
		Long: "Run each query in the vault's golden file through the live FTS index and\n" +
			"check that the expected notes come back, reporting per-case ranks plus\n" +
			"aggregate recall and mean reciprocal rank (MRR).\n\n" +
			"Run it before and after a gardening or consolidation action (a merge, a\n" +
			"split, an archive sweep) to catch retrieval regressions: same golden file,\n" +
			"two runs, diff the results (--json makes that mechanical).\n\n" +
			"The golden file lives at <vault>/.hebb/golden.toml (override with\n" +
			"--golden). Each case is a query you actually run and the vault-relative\n" +
			"note paths that must come back:\n\n" +
			"  # Each case: a query you actually run, and the notes that must come back.\n" +
			"  [[case]]\n" +
			"  query = \"quarterly board pack\"\n" +
			"  expect = [\"2-Areas/Board/Board pack process.md\"]\n" +
			"  limit = 10  # optional, default 10\n\n" +
			"A missing golden file is a normal state, not an error: the command says so\n" +
			"and exits 0. Create one with a handful of queries you rely on.\n\n" +
			"Like 'hebb doctor', this command is read-only and exits non-zero on\n" +
			"breach: if any expected note is missing from its query's results, the exit\n" +
			"code is non-zero so scripts and CI can gate on it.",
		Example: "  hebb eval\n" +
			"  hebb eval --json > before.json\n" +
			"  hebb eval --golden /path/to/golden.toml",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, db, err := openVault()
			if err != nil {
				return err
			}
			defer db.Close()

			goldenPath := golden
			if goldenPath == "" {
				goldenPath = filepath.Join(cfg.VaultPath, ".hebb", "golden.toml")
			}

			out := cmd.OutOrStdout()

			cases, err := core.LoadGolden(goldenPath)
			if err != nil {
				if os.IsNotExist(err) {
					// A missing golden file is a normal state (a vault that has not
					// set up retrieval evals yet), not a breach: exit 0 with guidance.
					fmt.Fprintf(out, "hebb eval: no golden file at %s\n", goldenPath)
					fmt.Fprintln(out, "  create one to score retrieval quality before/after vault changes, e.g.:")
					fmt.Fprintln(out, "    [[case]]")
					fmt.Fprintln(out, "    query = \"quarterly board pack\"")
					fmt.Fprintln(out, "    expect = [\"2-Areas/Board/Board pack process.md\"]")
					return nil
				}
				return err
			}

			// Refresh the index before querying so the eval scores the vault as it
			// is now, not as it was at the last index -- mirrors hebb search. A
			// refresh failure is an operational error: better to fail loudly than
			// to eval a stale index.
			if cfg.AutoRefresh {
				if _, err := core.RefreshChanged(cfg, db); err != nil {
					return fmt.Errorf("refresh before eval failed: %w", err)
				}
			}

			report, err := core.RunEval(db, cases)
			if err != nil {
				return fmt.Errorf("eval failed: %w", err)
			}

			if asJSON {
				if err := printEvalJSON(out, report); err != nil {
					return err
				}
			} else {
				printEvalText(out, report)
			}

			if !report.Passed() {
				// Mirrors doctor's posture: read-only, exits non-zero on breach. The
				// details are already printed above.
				return fmt.Errorf("eval found missing notes (see above)")
			}
			return nil
		},
	}
	c.Flags().StringVar(&golden, "golden", "", "golden file path (default: <vault>/.hebb/golden.toml)")
	c.Flags().BoolVar(&asJSON, "json", false, "emit the report as JSON (for before/after diffing)")
	return c
}

// printEvalText writes the human-readable report: one line per case, then a
// summary line.
func printEvalText(out io.Writer, report core.EvalReport) {
	for _, c := range report.Cases {
		if c.Passed() {
			ranks := make([]string, 0, len(c.Hits))
			for _, h := range c.Hits {
				ranks = append(ranks, fmt.Sprintf("%s@%d", h.Path, h.Rank))
			}
			fmt.Fprintf(out, "PASS  %q  %s\n", c.Query, strings.Join(ranks, ", "))
			continue
		}
		fmt.Fprintf(out, "MISS  %q  missing: %s\n", c.Query, strings.Join(c.Misses, ", "))
		for _, h := range c.Hits {
			fmt.Fprintf(out, "      found: %s@%d\n", h.Path, h.Rank)
		}
	}
	fmt.Fprintf(out, "eval: %d case(s), %d expected, %d found, recall %.0f%%, MRR %.2f\n",
		len(report.Cases), report.TotalExpected, report.TotalFound, report.Recall*100, report.MRR)
}

// printEvalJSON writes the report as indented JSON with a stable shape.
func printEvalJSON(out io.Writer, report core.EvalReport) error {
	jr := evalJSONReport{
		Cases:         make([]evalJSONCase, 0, len(report.Cases)),
		TotalExpected: report.TotalExpected,
		TotalFound:    report.TotalFound,
		Recall:        report.Recall,
		MRR:           report.MRR,
		Passed:        report.Passed(),
	}
	for _, c := range report.Cases {
		jc := evalJSONCase{
			Query:  c.Query,
			Limit:  c.Limit,
			Hits:   make([]evalJSONHit, 0, len(c.Hits)),
			Misses: c.Misses,
			Passed: c.Passed(),
		}
		if jc.Misses == nil {
			jc.Misses = []string{}
		}
		for _, h := range c.Hits {
			jc.Hits = append(jc.Hits, evalJSONHit{Path: h.Path, Rank: h.Rank})
		}
		jr.Cases = append(jr.Cases, jc)
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(jr)
}
