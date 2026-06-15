package cli

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/cizer/hebb/core"
	"github.com/spf13/cobra"
)

// digestCmd is the daily-digest launchd entrypoint: it refreshes the search
// index in-process, then writes the daily digest note from the index's
// content-level change detection. It is the hebb binary (not a shell wrapper)
// so launchd's TCC attribution lands on the grantable hebb binary, which macOS
// can be granted Full Disk Access to read the protected vault folders.
//
// The digest is selected entirely from the index (which notes' content changed
// since the last successful run), never from filesystem mtime: a bulk operation
// that rewrites bytes or bumps mtimes without changing content does not inflate
// it, and a genuine edit is reported even if a later rewrite bumps its mtime.
func digestCmd() *cobra.Command {
	var vaultRoot, output, date string
	c := &cobra.Command{
		Use:   "digest",
		Short: "Refresh the index, then write the daily vault digest",
		Long: "Refresh the search index in-process, then write the daily digest note\n" +
			"from the index's content-level change detection. This is the launchd\n" +
			"entrypoint for the daily-digest job: making it the hebb binary keeps\n" +
			"macOS Full Disk Access attributed to a grantable identity. The digest\n" +
			"reports notes whose content changed since the last successful run, so a\n" +
			"vault-wide rewrite that only churns mtimes does not register.",
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

			opts := core.DigestOptions{Output: output}
			if date != "" {
				d, err := time.Parse("2006-01-02", date)
				if err != nil {
					return fmt.Errorf("invalid --date %q (want YYYY-MM-DD): %w", date, err)
				}
				opts.Now = d
			}

			out := cmd.OutOrStdout()
			// Refresh first so content_changed_at reflects the current vault before
			// the digest queries it. A note edited since the last run gets its change
			// observed here; an unchanged note (even one whose mtime was bumped) keeps
			// its prior content_changed_at and so stays out of the window.
			fmt.Fprintf(out, "digest: refreshing index (%s)\n", cfg.VaultPath)
			res, err := core.FullReindex(cfg, db)
			if err != nil {
				return fmt.Errorf("index refresh failed: %w", err)
			}

			dr, err := core.GenerateDigest(cfg, db, opts)
			if err != nil {
				return fmt.Errorf("digest generation failed: %w", err)
			}

			// Index the digest note we just wrote so it is searchable, when it lives
			// inside the vault (an --output pointed outside the vault is left alone).
			// It is excluded from future digests by name, so re-indexing it cannot
			// make it report itself as activity.
			if rel, ok := vaultRelative(cfg.VaultPath, dr.OutputPath); ok {
				_ = core.IndexFile(cfg, db, rel)
			}

			fmt.Fprintf(out, "digest: wrote %s (%d notes for %s; %d indexed, %d removed)\n",
				dr.OutputPath, dr.Count, dr.Label, res.Indexed, res.Removed)

			// Headless notification: best-effort, never blocks or fails the digest.
			// Send only when [notify] is enabled and a URL resolves.
			vc, _, _ := core.LoadVaultConfig(cfg.VaultPath)
			if vc.Notify.Enabled {
				if url := vc.Notify.ResolveURL(); url != "" {
					summary := fmt.Sprintf("digest: %d notes changed (%s)", dr.Count, cfg.VaultPath)
					if err := SendNotification(url, summary); err != nil {
						log.Printf("digest: notify failed (delivery failure does not affect the note): %v", err)
					}
				}
			}
			return nil
		},
	}
	c.Flags().StringVar(&vaultRoot, "vault-root", "", "vault path to digest (default: --vault / $HEBB_VAULT / nearest .hebb)")
	c.Flags().StringVar(&output, "output", "", "digest note path relative to vault root (default 2-Areas/_DAILY-DIGEST.md)")
	c.Flags().StringVar(&date, "date", "", "override run date as YYYY-MM-DD (for testing)")
	return c
}

// vaultRelative converts a digest output path (as supplied, relative to the
// vault root or absolute) into a vault-relative slash path, reporting false when
// it resolves outside the vault so the caller skips indexing it.
func vaultRelative(vaultPath, output string) (string, bool) {
	abs := output
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(vaultPath, filepath.FromSlash(output))
	}
	rel, err := filepath.Rel(vaultPath, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}
