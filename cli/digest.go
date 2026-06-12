package cli

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/cizer/hebb/core"
	"github.com/cizer/hebb/install"
	"github.com/spf13/cobra"
)

// digestCmd is the daily-digest launchd entrypoint: it runs the digest
// generator then refreshes the search index in-process. It replaces the
// run-vault-digest.sh wrapper as the job's Program[0] so launchd's TCC
// attribution lands on the grantable hebb binary, not on a shell interpreter
// that macOS blocks from the protected vault folders (see SPEC item 2).
//
// It resolves python the same way the rendered launchd jobs do (the pinned
// $PYTHON env, else install.PythonPath()), locates generate-vault-digest.py via
// the same asset resolution install and doctor use (--asset-root override, else
// the data dir), runs it, then reindexes via the in-process engine rather than
// shelling out to `hebb index`. Any failure exits non-zero so launchd records it.
func digestCmd() *cobra.Command {
	var vaultRoot, assetRoot, home, dataDir string
	c := &cobra.Command{
		Use:   "digest [-- extra digest args]",
		Short: "Generate the daily vault digest, then refresh the index",
		Long: "Run the daily-digest generator (generate-vault-digest.py) against the\n" +
			"vault, then refresh the search index in-process. This is the launchd\n" +
			"entrypoint for the daily-digest job: making it the hebb binary keeps\n" +
			"macOS Full Disk Access attributed to a grantable identity. Arguments\n" +
			"after -- are passed through to the digest generator.",
		RunE: func(cmd *cobra.Command, args []string) error {
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

			if home == "" {
				home, _ = os.UserHomeDir()
			}
			if assetRoot == "" {
				assetRoot = os.Getenv("HEBB_HOME")
			}
			if dataDir == "" {
				dataDir = defaultDataDir(home)
			}
			assetDir := assetRoot
			if assetDir == "" {
				assetDir = dataDir
			}
			script := filepath.Join(assetDir, "automation", "generate-vault-digest.py")
			if _, err := os.Stat(script); err != nil {
				return fmt.Errorf("digest generator not found at %s (run hebb install): %w", script, err)
			}

			// Resolve python the same way the launchd job does: the pinned $PYTHON
			// env (set to dodge the Xcode python3 shim on launchd's minimal PATH),
			// else the absolute python3 install.PythonPath() finds.
			python := os.Getenv("PYTHON")
			if python == "" {
				python = install.PythonPath()
			}

			out := cmd.OutOrStdout()
			scriptArgs := append([]string{script, "--vault-root", cfg.VaultPath}, args...)
			fmt.Fprintf(out, "digest: generating (%s)\n", cfg.VaultPath)
			gen := exec.Command(python, scriptArgs...)
			gen.Stdout = out
			gen.Stderr = cmd.OutOrStderr()
			if err := gen.Run(); err != nil {
				return fmt.Errorf("digest generator failed: %w", err)
			}

			fmt.Fprintln(out, "digest: refreshing index")
			res, err := core.FullReindex(cfg, db)
			if err != nil {
				return fmt.Errorf("index refresh failed: %w", err)
			}
			fmt.Fprintf(out, "digest: done (%d notes indexed, %d removed)\n", res.Indexed, res.Removed)

			// Headless notification: best-effort, never blocks or fails the digest.
			// Send only when [notify] is enabled and a URL resolves.
			vc, _, _ := core.LoadVaultConfig(cfg.VaultPath)
			if vc.Notify.Enabled {
				if url := vc.Notify.ResolveURL(); url != "" {
					summary := fmt.Sprintf("digest: %d notes indexed (%s)", res.Indexed, cfg.VaultPath)
					if err := SendNotification(url, summary); err != nil {
						log.Printf("digest: notify failed (delivery failure does not affect the note): %v", err)
					}
				}
			}
			return nil
		},
	}
	c.Flags().StringVar(&vaultRoot, "vault-root", "", "vault path to digest (default: --vault / $HEBB_VAULT / nearest .hebb)")
	c.Flags().StringVar(&assetRoot, "asset-root", "", "dev override: asset dir holding automation/ (default $HEBB_HOME, then the data dir)")
	c.Flags().StringVar(&home, "home", "", "home dir (default: user home)")
	c.Flags().StringVar(&dataDir, "data-dir", "", "hebb data dir holding materialised automation/ (default: $XDG_DATA_HOME/hebb or <home>/.local/share/hebb)")
	_ = c.Flags().MarkHidden("asset-root")
	_ = c.Flags().MarkHidden("home")
	_ = c.Flags().MarkHidden("data-dir")
	return c
}
