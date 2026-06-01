package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cizer/hebb/core"
	"github.com/cizer/hebb/install"
	"github.com/spf13/cobra"
)

func installCmd() *cobra.Command {
	var serverName, home, assetRoot, launchdDir string
	var withLaunchd, load bool
	c := &cobra.Command{
		Use:   "install",
		Short: "Wire this vault into the machine",
		Long: "Initialise the per-vault contracts (.hebb/config.toml, .mcp.json),\n" +
			"write project settings, symlink global skills, and build the first index.\n" +
			"Idempotent: safe to re-run. Skills are only linked when an asset root is\n" +
			"known (--asset-root or $HEBB_HOME). Pass --launchd to render the vault's\n" +
			"launchd jobs (and --load to bootstrap them).",
		RunE: func(cmd *cobra.Command, _ []string) error {
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
			if load {
				withLaunchd = true
			}
			if withLaunchd && launchdDir == "" {
				launchdDir = filepath.Join(home, "Library", "LaunchAgents")
			}
			hebbBin, _ := os.Executable()

			rep, err := install.Run(install.Options{
				VaultPath:  cfg.VaultPath,
				MCPName:    serverName,
				MCPCommand: install.DefaultMCPCommand,
				Home:       home,
				AssetRoot:  assetRoot,
				HebbBin:    hebbBin,
				LaunchdDir: launchdDir,
				Load:       load,
			})
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
				fmt.Fprintf(out, "  %-16s %s\n", s.Name, s.Status)
			}
			fmt.Fprintf(out, "  %-16s %d notes indexed\n", "index", res.Indexed)
			if assetRoot == "" {
				fmt.Fprintln(out, "note: skills not linked (set --asset-root or $HEBB_HOME to the hebb repo)")
			}
			return nil
		},
	}
	c.Flags().StringVar(&serverName, "mcp-name", install.DefaultMCPServerName, "MCP server name written into .mcp.json")
	c.Flags().StringVar(&assetRoot, "asset-root", "", "hebb repo/asset dir holding skills/ (default $HEBB_HOME)")
	c.Flags().BoolVar(&withLaunchd, "launchd", false, "render the vault's launchd jobs into ~/Library/LaunchAgents")
	c.Flags().BoolVar(&load, "load", false, "bootstrap rendered launchd jobs via launchctl (implies --launchd)")
	c.Flags().StringVar(&home, "home", "", "home dir holding .claude (default: user home)")
	c.Flags().StringVar(&launchdDir, "launchd-dir", "", "target LaunchAgents dir (default: <home>/Library/LaunchAgents)")
	_ = c.Flags().MarkHidden("home")
	_ = c.Flags().MarkHidden("launchd-dir")
	return c
}
