package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/cizer/hebb/core"
	"github.com/spf13/cobra"
)

func themesCmd() *cobra.Command {
	var days int
	var asJSON bool
	c := &cobra.Command{
		Use:   "themes",
		Short: "Report recurring themes: link targets, tag momentum, co-linked pairs",
		Long: "Report where the recent work keeps returning: which notes the changed\n" +
			"notes link to most, which tags are gaining or losing momentum against the\n" +
			"previous window, and which pairs of notes keep being linked together from\n" +
			"the same sources.\n\n" +
			"The report is computed entirely from the index over a trailing window of\n" +
			"content changes (--days, default 30; 14 is the other window the organiser\n" +
			"uses). It reads the index only and changes nothing: no vault file is\n" +
			"touched and no index row is written. Only resolved links count -- a\n" +
			"[[wiki-link]] that matches no note (or several) contributes nothing.\n" +
			"Machine notes (any basename starting with '_', such as _DAILY-DIGEST.md)\n" +
			"and notes matching exclude_from_graph under [audit] in config.toml are\n" +
			"ignored as both sources and targets, so generated scaffolding cannot\n" +
			"dominate the signal.",
		Example: "  hebb themes\n" +
			"  hebb themes --days 14\n" +
			"  hebb themes --json | jq '.link_targets'",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, db, err := openVault()
			if err != nil {
				return err
			}
			defer db.Close()

			// Refresh the index before querying so this morning's notes count --
			// mirrors hebb audit. A refresh failure is an operational error:
			// better to fail loudly than report themes over a stale index.
			if cfg.AutoRefresh {
				if _, err := core.RefreshChanged(cfg, db); err != nil {
					return fmt.Errorf("refresh before themes failed: %w", err)
				}
			}

			report, err := core.Themes(db, cfg, days, time.Now())
			if err != nil {
				return fmt.Errorf("themes failed: %w", err)
			}

			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(report)
			}
			printThemesText(out, report)
			return nil
		},
	}
	c.Flags().IntVar(&days, "days", 30, "trailing window in days (content changes, not mtime)")
	c.Flags().BoolVar(&asJSON, "json", false, "emit the full report as JSON")
	return c
}

// printThemesText renders the human-readable report: a window header line and
// three titled sections with tabwriter-aligned columns. An empty window gets a
// friendly one-liner instead of three empty sections.
func printThemesText(out io.Writer, r core.ThemesReport) {
	if r.NotesChanged == 0 {
		fmt.Fprintf(out, "hebb themes: no notes changed in the last %d days\n", r.Days)
		return
	}
	fmt.Fprintf(out, "hebb themes: last %d days, %d notes changed\n", r.Days, r.NotesChanged)

	fmt.Fprintf(out, "\nLink targets (most linked from changed notes):\n")
	if len(r.LinkTargets) == 0 {
		fmt.Fprintln(out, "  none: the changed notes have no resolved links")
	} else {
		w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
		for _, lt := range r.LinkTargets {
			fmt.Fprintf(w, "  %s\t%s\t%d link(s) from %d note(s)\n", lt.Title, lt.Path, lt.Links, lt.Sources)
		}
		w.Flush()
	}

	fmt.Fprintf(out, "\nTag momentum (this window vs the previous %d days):\n", r.Days)
	if len(r.TagMomentum) == 0 {
		fmt.Fprintln(out, "  none: the changed notes carry no tags")
	} else {
		w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
		for _, tm := range r.TagMomentum {
			fmt.Fprintf(w, "  %s\t%d note(s)\t%s\n", tm.Tag, tm.Count, momentumLabel(tm.Delta))
		}
		w.Flush()
	}

	fmt.Fprintf(out, "\nCo-linked pairs (targets linked from the same changed notes):\n")
	if len(r.CoOccurrence) == 0 {
		fmt.Fprintln(out, "  none: no changed note links to two targets")
	} else {
		w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
		for _, co := range r.CoOccurrence {
			fmt.Fprintf(w, "  %s + %s\t%d shared note(s)\n", co.ATitle, co.BTitle, co.SharedSources)
		}
		w.Flush()
	}
}

// momentumLabel renders a tag's window-over-window delta as a word plus the
// signed count, e.g. "rising (+3)", "falling (-1)", "steady".
func momentumLabel(delta int) string {
	switch {
	case delta > 0:
		return fmt.Sprintf("rising (+%d)", delta)
	case delta < 0:
		return fmt.Sprintf("falling (%d)", delta)
	default:
		return "steady"
	}
}
