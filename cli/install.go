package cli

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	hebbassets "github.com/cizer/hebb"
	"github.com/cizer/hebb/core"
	"github.com/cizer/hebb/install"
	"github.com/spf13/cobra"
)

// installParams carries the wiring flags shared by `hebb install` and the
// install phase of `hebb new`.
type installParams struct {
	serverName  string
	home        string
	assetRoot   string
	launchdDir  string
	dataDir     string
	withLaunchd bool
	load        bool
}

func installCmd() *cobra.Command {
	var p installParams
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
			return installVault(cmd, cfg, db, p)
		},
	}
	bindInstallFlags(c, &p)
	return c
}

// bindInstallFlags registers the install-wiring flags on a command. The
// machine-targeting flags are hidden: they exist for tests and headless runs.
func bindInstallFlags(c *cobra.Command, p *installParams) {
	c.Flags().StringVar(&p.serverName, "mcp-name", install.DefaultMCPServerName, "MCP server name written into .mcp.json")
	c.Flags().StringVar(&p.assetRoot, "asset-root", "", "dev override: link skills from this repo checkout instead of the bundled assets (default $HEBB_HOME)")
	c.Flags().BoolVar(&p.withLaunchd, "launchd", false, "render the vault's launchd jobs into ~/Library/LaunchAgents")
	c.Flags().BoolVar(&p.load, "load", false, "bootstrap rendered launchd jobs via launchctl (implies --launchd)")
	c.Flags().StringVar(&p.home, "home", "", "home dir holding .claude (default: user home)")
	c.Flags().StringVar(&p.launchdDir, "launchd-dir", "", "target LaunchAgents dir (default: <home>/Library/LaunchAgents)")
	c.Flags().StringVar(&p.dataDir, "data-dir", "", "where bundled assets are materialised (default: $XDG_DATA_HOME/hebb or <home>/.local/share/hebb)")
	_ = c.Flags().MarkHidden("home")
	_ = c.Flags().MarkHidden("launchd-dir")
	_ = c.Flags().MarkHidden("data-dir")
}

// installVault performs the file-level install for an already-resolved vault
// and builds its first index, printing a step report. Shared by `install` and
// the install phase of `new`.
func installVault(cmd *cobra.Command, cfg core.Config, db *sql.DB, p installParams) error {
	home := p.home
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	assetRoot := p.assetRoot
	if assetRoot == "" {
		assetRoot = os.Getenv("HEBB_HOME")
	}
	dataDir := p.dataDir
	if dataDir == "" {
		dataDir = defaultDataDir(home)
	}
	withLaunchd := p.withLaunchd || p.load
	launchdDir := p.launchdDir
	if withLaunchd && launchdDir == "" {
		launchdDir = filepath.Join(home, "Library", "LaunchAgents")
	}
	hebbBin, _ := os.Executable()

	rep, err := install.Run(install.Options{
		VaultPath:  cfg.VaultPath,
		MCPName:    p.serverName,
		MCPCommand: install.DefaultMCPCommand,
		Home:       home,
		HebbBin:    hebbBin,
		LaunchdDir: launchdDir,
		Load:       p.load,
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
}

// defaultDataDir is the hebb data dir: $XDG_DATA_HOME/hebb if set, else
// <home>/.local/share/hebb.
func defaultDataDir(home string) string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "hebb")
	}
	return filepath.Join(home, ".local", "share", "hebb")
}
