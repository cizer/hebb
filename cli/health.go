package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/cizer/hebb/core"
	"github.com/spf13/cobra"
)

func healthCmd() *cobra.Command {
	var asJSON bool
	var unresolved bool
	c := &cobra.Command{
		Use:     "audit",
		Aliases: []string{"health"},
		Short:   "Audit vault content: dangling links, PARA drift, oversized notes",
		Long: "Runs deterministic, read-only detectors over the vault index and prints a\n" +
			"worklist of findings grouped by type. Repairs nothing.\n\n" +
			"Detectors (Phase 1):\n" +
			"  dangling_link   wiki-links with no matching note\n" +
			"  ambiguous_link  wiki-links that match more than one note\n" +
			"  para_drift      1-Projects/ notes that are done or stale\n" +
			"  oversized       notes over the token threshold with multiple sections\n\n" +
			"Wiki-links are resolved case-insensitively (matching Obsidian), and\n" +
			"attachment links (.png, .pdf, ...) and folder links are not treated as\n" +
			"broken note links. Links to notes that do not exist yet (Obsidian\n" +
			"'unresolved links', often intentional future notes) are counted but not\n" +
			"listed by default; pass --unresolved to list them, or set\n" +
			"report_unresolved_links = true under [health] in config.toml.\n\n" +
			"Unlike 'hebb doctor', this command exits 0 even when findings exist: the\n" +
			"output is an advisory worklist of vault-content issues, not a pass/fail\n" +
			"install check. A non-zero exit signals an operational failure only (e.g.\n" +
			"cannot open the vault or index database).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, db, err := openVault()
			if err != nil {
				return err
			}
			defer db.Close()

			// Refresh the index before querying so a note written moments ago is
			// visible -- mirrors the pattern used by hebb search. A refresh failure
			// (I/O error, corrupt DB) is an operational failure: report it and exit
			// non-zero rather than silently running detectors on a stale or partial
			// index, which the command's contract reserves a non-zero exit for.
			if cfg.AutoRefresh {
				if _, err := core.RefreshChanged(cfg, db); err != nil {
					return fmt.Errorf("refresh before health check failed: %w", err)
				}
			}

			// Effective unresolved-reporting setting: the --unresolved flag forces
			// it on for this run, otherwise fall back to the [health] config
			// default (report_unresolved_links, off unless set).
			reportUnresolved := unresolved || cfg.Health.ReportUnresolvedLinks

			result, err := core.RunHealthFull(cfg, db, reportUnresolved)
			if err != nil {
				return fmt.Errorf("health check failed: %w", err)
			}

			out := cmd.OutOrStdout()

			if asJSON {
				// Emit a top-level JSON array of findings, matching the Phase 1
				// contract that existing array consumers (jq '.[]', Go []Finding
				// decoders) depend on. Suppressed unresolved links are simply absent
				// from the array (the shape is unchanged: a top-level []Finding).
				// Graph stats are available in text mode via the summary line, and
				// via /api/health for the web dashboard.
				findings := result.Findings
				if findings == nil {
					findings = []core.Finding{}
				}
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(findings)
			}

			// Text output: print the structural graph summary first, then the
			// findings worklist grouped by type, then the suppressed-unresolved
			// informational line when any were hidden.
			printGraphSummary(cmd, result.Stats)
			printHealthText(cmd, result)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "emit findings as a JSON array (for the Phase 2 dashboard)")
	c.Flags().BoolVar(&unresolved, "unresolved", false, "list unresolved wiki-links (links to non-existent notes), suppressed by default")
	return c
}

// typeOrder is the fixed display order for finding types. Types not listed here
// appear last in lexicographic order (forward-compatibility with future detectors).
var typeOrder = []string{"dangling_link", "ambiguous_link", "para_drift", "oversized", "orphan", "leaf", "island"}

// printGraphSummary writes the one-line structural graph summary to cmd's
// output writer. It is printed above the findings worklist in text mode.
func printGraphSummary(cmd *cobra.Command, s core.GraphStats) {
	out := cmd.OutOrStdout()
	if s.NodeCount == 0 {
		fmt.Fprintln(out, "graph: 0 notes")
		return
	}
	fmt.Fprintf(out,
		"graph: %d notes, %d edges, %d components, giant-component %.0f%%, max k-core %d\n",
		s.NodeCount,
		s.EdgeCount,
		s.ComponentCount,
		s.GiantRatio*100,
		s.MaxCore,
	)
}

func printHealthText(cmd *cobra.Command, result core.HealthResult) {
	out := cmd.OutOrStdout()
	findings := result.Findings

	if len(findings) == 0 {
		fmt.Fprintln(out, "hebb health: no findings")
		printSuppressedUnresolved(out, result.SuppressedUnresolved)
		return
	}

	// Group by type.
	byType := make(map[string][]core.Finding)
	for _, f := range findings {
		byType[f.Type] = append(byType[f.Type], f)
	}

	// Determine the print order: fixed known types first, then any extras sorted.
	seen := make(map[string]bool)
	var order []string
	for _, t := range typeOrder {
		if _, ok := byType[t]; ok {
			order = append(order, t)
			seen[t] = true
		}
	}
	var extra []string
	for t := range byType {
		if !seen[t] {
			extra = append(extra, t)
		}
	}
	sort.Strings(extra)
	order = append(order, extra...)

	total := len(findings)
	fmt.Fprintf(out, "hebb health: %d finding(s)\n", total)

	for _, t := range order {
		group := byType[t]
		fmt.Fprintf(out, "\n  %s (%d)\n", t, len(group))
		for _, f := range group {
			fmt.Fprintf(out, "    %-4s  %-50s  %s\n", f.Severity, f.Path, f.Detail)
		}
	}

	printSuppressedUnresolved(out, result.SuppressedUnresolved)
}

// printSuppressedUnresolved prints one informational line when unresolved links
// were hidden, so the suppression is visible and the user knows how to list
// them. It prints nothing when nothing was suppressed.
func printSuppressedUnresolved(out io.Writer, n int) {
	if n <= 0 {
		return
	}
	fmt.Fprintf(out, "\n%d unresolved link(s) (links to non-existent notes; run with --unresolved to list)\n", n)
}
