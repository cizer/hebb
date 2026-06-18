package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/cizer/hebb/core"
	"github.com/spf13/cobra"
)

func vaultsCmd() *cobra.Command {
	var home string
	c := &cobra.Command{
		Use:   "vaults",
		Short: "List the vaults hebb knows about",
		Long: "List the vaults in the machine registry: the set `hebb serve` switches\n" +
			"between, and that `hebb install`/`hebb new` add to. Marks the current\n" +
			"vault with '*', and flags any whose directory is missing.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if home == "" {
				home, _ = os.UserHomeDir()
			}
			reg, err := core.LoadRegistry(core.RegistryPath(home))
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(reg.Vaults) == 0 {
				fmt.Fprintln(out, "No vaults registered yet. Run 'hebb new <path>' or 'hebb install --vault <path>'.")
				return nil
			}
			// Resolve the current vault (if any) to mark it.
			current := ""
			if cfg, err := core.ResolveVault(flagVault, flagDB); err == nil {
				current = cfg.VaultPath
				if r, err := filepath.EvalSymlinks(current); err == nil {
					current = r
				}
			}
			w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
			for _, v := range reg.Vaults {
				mark := "  "
				if v.Path == current {
					mark = "* "
				}
				note := ""
				if _, err := os.Stat(filepath.Join(v.Path, ".hebb")); err != nil {
					note = "\t(unavailable)"
				}
				fmt.Fprintf(w, "%s%s\t%s%s\n", mark, v.Name, v.Path, note)
			}
			w.Flush()
			return nil
		},
	}
	c.Flags().StringVar(&home, "home", "", "home dir holding the registry (default: user home)")
	_ = c.Flags().MarkHidden("home")
	return c
}
