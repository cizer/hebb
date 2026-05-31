// Command hebb is the CLI for the hebb knowledge-vault engine.
//
// hebb is the function layer for a markdown "second brain": the vault
// (markdown + attachments + memory) is the data, hebb is the tool that
// indexes, searches, scaffolds and maintains it. hebb is multi-vault, like
// git is multi-repo: run it inside a vault directory, or pass --vault.
//
// See ARCHITECTURE.md for the full model. Commands are stubs at this stage.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is overridden at build time via -ldflags.
var version = "0.0.0-dev"

func main() {
	root := &cobra.Command{
		Use:          "hebb",
		Short:        "hebb - a portable engine for markdown knowledge vaults",
		Long:         "hebb indexes, searches, scaffolds and maintains markdown vaults.\nMulti-vault: run inside a vault directory, or pass --vault.",
		Version:      version,
		SilenceUsage: true,
	}

	var vaultPath string
	root.PersistentFlags().StringVar(&vaultPath, "vault", "",
		"path to the vault (defaults to the nearest .hebb/ above the cwd, or $HEBB_VAULT)")

	stub := func(use, short, phase string) *cobra.Command {
		return &cobra.Command{
			Use:   use,
			Short: short,
			RunE: func(cmd *cobra.Command, args []string) error {
				return fmt.Errorf("%q is not implemented yet (planned: %s)", use, phase)
			},
		}
	}

	root.AddCommand(
		stub("new", "Scaffold a fresh vault from the template", "Phase 3"),
		stub("install", "Wire hebb into this machine for a vault", "Phase 2"),
		stub("index", "Build or refresh the search index", "Phase 1"),
		stub("search", "Search the vault", "Phase 1"),
		stub("serve", "Serve the local search UI", "Phase 1"),
		stub("sync", "Sync the vault", "Phase 5"),
		stub("doctor", "Check vault and install health", "Phase 2"),
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
