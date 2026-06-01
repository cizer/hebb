package cli

import (
	"fmt"
	"os"
	"path/filepath"

	hebbassets "github.com/cizer/hebb"
	"github.com/cizer/hebb/core"
	"github.com/cizer/hebb/install"
	"github.com/spf13/cobra"
)

func installCmd() *cobra.Command {
	var serverName, home, assetRoot, launchdDir, dataDir string
	var withLaunchd, load bool
	c := &cobra.Command{
		Use:   "install",
		Short: "Wire this vault into the machine",
		Long: "Initialise the per-vault contracts (.hebb/config.toml, .mcp.json),\n" +
			"write project settings, materialise the bundled skills and link them into\n" +
			"~/.claude/skills, symlink memory, and build the first index. Idempotent.\n" +
			"The binary is standalone (assets are embedded); pass --asset-root to link\n" +
			"skills straight from a repo checkout instead. Pass --launchd to render the\n" +
			"vault's launchd jobs (and --load to bootstrap them).",
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
			if dataDir == "" {
				dataDir = defaultDataDir(home)
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
				HebbBin:    hebbBin,
				LaunchdDir: launchdDir,
				Load:       load,
				Assets:     hebbassets.Assets,
				DataDir:    dataDir,
				AssetRoot:  assetRoot,
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
			return nil
		},
	}
	c.Flags().StringVar(&serverName, "mcp-name", install.DefaultMCPServerName, "MCP server name written into .mcp.json")
	c.Flags().StringVar(&assetRoot, "asset-root", "", "dev override: link skills from this repo checkout instead of the bundled assets (default $HEBB_HOME)")
	c.Flags().BoolVar(&withLaunchd, "launchd", false, "render the vault's launchd jobs into ~/Library/LaunchAgents")
	c.Flags().BoolVar(&load, "load", false, "bootstrap rendered launchd jobs via launchctl (implies --launchd)")
	c.Flags().StringVar(&home, "home", "", "home dir holding .claude (default: user home)")
	c.Flags().StringVar(&launchdDir, "launchd-dir", "", "target LaunchAgents dir (default: <home>/Library/LaunchAgents)")
	c.Flags().StringVar(&dataDir, "data-dir", "", "where bundled assets are materialised (default: $XDG_DATA_HOME/hebb or <home>/.local/share/hebb)")
	_ = c.Flags().MarkHidden("home")
	_ = c.Flags().MarkHidden("launchd-dir")
	_ = c.Flags().MarkHidden("data-dir")
	return c
}

// defaultDataDir is the hebb data dir: $XDG_DATA_HOME/hebb if set, else
// <home>/.local/share/hebb.
func defaultDataDir(home string) string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "hebb")
	}
	return filepath.Join(home, ".local", "share", "hebb")
}
