package cli

import (
	"fmt"
	"log"
	"time"

	"github.com/cizer/hebb/core"
	"github.com/spf13/cobra"
)

// consolidateCmd is the weekly-consolidate launchd entrypoint: it refreshes
// the search index in-process, then writes the consolidation review note from
// the temporal detectors (stale_pin, cooling_project) and the themes report.
// It is the hebb binary (not a shell wrapper), same rationale as digestCmd:
// launchd's TCC attribution lands on the grantable hebb binary, which macOS
// can be granted Full Disk Access to read the protected vault folders.
//
// The consolidation review is proposal-only: it never edits vault content.
// Every proposal is meant to be actioned by a human via the vault-gardener
// skill.
func consolidateCmd() *cobra.Command {
	var vaultRoot, output, date string
	c := &cobra.Command{
		Use:   "consolidate",
		Short: "Refresh the index, then write the weekly consolidation review",
		Long: "Refresh the search index in-process, then write the weekly\n" +
			"consolidation review note: at most a handful of ranked proposals drawn\n" +
			"from the temporal detectors (stale pins, cooling projects) and the\n" +
			"themes recurrence report, each citing its source notes as\n" +
			"[[wiki-links]]. This is the launchd entrypoint for the consolidate job;\n" +
			"making it the hebb binary keeps macOS Full Disk Access attributed to a\n" +
			"grantable identity. The review is proposal-only: it never edits vault\n" +
			"content. Action proposals by hand, or via the vault-gardener skill.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Prefer --vault-root (the launchd job's flag); fall back to the global
			// --vault / $HEBB_VAULT resolution so the command is usable interactively.
			if vaultRoot != "" {
				flagVault = vaultRoot
			}
			cfg, db, err := openVault()
			if err != nil {
				return err
			}
			defer db.Close()

			opts := core.ConsolidationOptions{Output: output}
			if date != "" {
				// Parse in the local zone so a --date override uses the same local
				// day boundaries as a normal run, which defaults to time.Now() in
				// local time. time.Parse would return UTC.
				d, err := time.ParseInLocation("2006-01-02", date, time.Local)
				if err != nil {
					return fmt.Errorf("invalid --date %q (want YYYY-MM-DD): %w", date, err)
				}
				opts.Now = d
			}

			out := cmd.OutOrStdout()
			// Refresh first so the detectors and themes report reflect the current
			// vault before consolidation queries the index.
			fmt.Fprintf(out, "consolidate: refreshing index (%s)\n", cfg.VaultPath)
			res, err := core.FullReindex(cfg, db)
			if err != nil {
				return fmt.Errorf("index refresh failed: %w", err)
			}

			cr, err := core.GenerateConsolidation(cfg, db, opts)
			if err != nil {
				return fmt.Errorf("consolidation generation failed: %w", err)
			}

			// Index the consolidation note we just wrote so it is searchable, when
			// it lives inside the vault (an --output pointed outside the vault is
			// left alone). It is excluded from the digest by name, so re-indexing
			// it cannot make it report itself as activity.
			if rel, ok := vaultRelative(cfg.VaultPath, cr.OutputPath); ok {
				_ = core.IndexFile(cfg, db, rel)
			}

			fmt.Fprintf(out, "consolidate: wrote %s (%d proposals; %d indexed, %d removed)\n",
				cr.OutputPath, cr.Proposals, res.Indexed, res.Removed)

			// Headless notification: best-effort, never blocks or fails the run.
			// Send only when [notify] is enabled and a URL resolves.
			vc, _, _ := core.LoadVaultConfig(cfg.VaultPath)
			if vc.Notify.Enabled {
				if url := vc.Notify.ResolveURL(); url != "" {
					summary := fmt.Sprintf("consolidation: %d proposals (%s)", cr.Proposals, cfg.VaultPath)
					if err := SendNotification(url, summary); err != nil {
						log.Printf("consolidate: notify failed (delivery failure does not affect the note): %v", err)
					}
				}
			}
			return nil
		},
	}
	c.Flags().StringVar(&vaultRoot, "vault-root", "", "vault path to consolidate (default: --vault / $HEBB_VAULT / nearest .hebb)")
	c.Flags().StringVar(&output, "output", "", "consolidation note path relative to vault root (default 2-Areas/_CONSOLIDATION.md)")
	c.Flags().StringVar(&date, "date", "", "override run date as YYYY-MM-DD (for testing)")
	// --vault-root exists for the launchd job's explicit wiring; --vault /
	// $HEBB_VAULT / cwd walk-up cover interactive use, so keep this out of
	// --help without removing it.
	_ = c.Flags().MarkHidden("vault-root")
	return c
}
