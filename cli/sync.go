package cli

import (
	"fmt"

	"github.com/cizer/hebb/core"
	"github.com/spf13/cobra"
)

func syncCmd() *cobra.Command {
	var noPull, noPush bool
	var message string
	c := &cobra.Command{
		Use:   "sync",
		Short: "Pull, commit and push vault content via git",
		Long: "Sync the vault's markdown with its git remote: commit local changes,\n" +
			"pull (rebasing onto the upstream), then push. A no-op when there is\n" +
			"nothing to do. Never force-pushes; a conflicting pull is aborted and\n" +
			"reported so you resolve it by hand. Uses your existing git remote and\n" +
			"credentials. Needs the vault to be a git repository with an upstream.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := core.ResolveVault(flagVault, flagDB)
			if err != nil {
				return err
			}
			res, err := core.GitSync(cfg.VaultPath, core.SyncOptions{
				Pull:    !noPull,
				Commit:  true,
				Push:    !noPush,
				Message: message,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "sync %s: committed=%v pulled=%v pushed=%v\n",
				cfg.VaultPath, res.Committed, res.Pulled, res.Pushed)
			return nil
		},
	}
	c.Flags().BoolVar(&noPull, "no-pull", false, "skip the pull step")
	c.Flags().BoolVar(&noPush, "no-push", false, "skip the push step")
	c.Flags().StringVar(&message, "message", "", "commit message (default: \"hebb: sync vault\")")
	return c
}
