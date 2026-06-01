package cli

import (
	"fmt"

	"github.com/cizer/hebb/core"
	"github.com/cizer/hebb/install"
	"github.com/spf13/cobra"
)

func installCmd() *cobra.Command {
	var serverName string
	c := &cobra.Command{
		Use:   "install",
		Short: "Wire this vault into the machine",
		Long: "Initialise the per-vault contracts (.hebb/config.toml, .mcp.json) and\n" +
			"build the first index. Idempotent: safe to re-run.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, db, err := openVault()
			if err != nil {
				return err
			}
			defer db.Close()

			rep, err := install.VaultLocal(cfg.VaultPath, serverName, install.DefaultMCPCommand)
			if err != nil {
				return err
			}

			res, err := core.FullReindex(cfg, db)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Installed vault: %s\n", cfg.VaultPath)
			for _, s := range rep.Steps {
				fmt.Fprintf(out, "  %-14s %s\n", s.Name, s.Status)
			}
			fmt.Fprintf(out, "  %-14s %d notes indexed\n", "index", res.Indexed)
			return nil
		},
	}
	c.Flags().StringVar(&serverName, "mcp-name", install.DefaultMCPServerName, "MCP server name written into .mcp.json")
	return c
}
