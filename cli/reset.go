package cli

import (
	"fmt"
	"os"

	"github.com/cizer/hebb/core"
	"github.com/cizer/hebb/install"
	"github.com/spf13/cobra"
)

// resetCmd is the inverse of install: un-wire a vault from this machine. It is a
// dry run unless --force is passed, and it NEVER removes vault content (notes,
// .hebb/memory, .hebb/config.toml).
func resetCmd() *cobra.Command {
	var home, launchdDir, codexConfig, desktopConfig, mcpName string
	var force, keepIndex bool
	c := &cobra.Command{
		Use:     "unwire",
		Aliases: []string{"reset"},
		Short:   "Un-wire this vault from the machine (keeps all vault content)",
		Long: "Remove the machine-side wiring `hebb install` created: the Claude\n" +
			"memory symlink, this vault's launchd jobs, the Codex [mcp_servers.hebb]\n" +
			"block, the opt-in per-vault .mcp.json/settings, and the (regenerable)\n" +
			"index. It NEVER touches vault content - notes, .hebb/memory and\n" +
			".hebb/config.toml are always kept. Dry run by default; pass --force to\n" +
			"apply. Rebuild the index any time with `hebb index`.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := core.ResolveVault(flagVault, flagDB)
			if err != nil {
				return err
			}
			if home == "" {
				home, _ = os.UserHomeDir()
			}
			rep, err := install.Teardown(install.TeardownOptions{
				VaultPath:     cfg.VaultPath,
				Home:          home,
				LaunchdDir:    launchdDir,
				CodexConfig:   codexConfig,
				DesktopConfig: desktopConfig,
				MCPName:       mcpName,
				RegistryPath:  core.RegistryPath(home),
				Force:         force,
				KeepIndex:     keepIndex,
			})
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if force {
				fmt.Fprintf(out, "hebb unwire: %s\n", cfg.VaultPath)
			} else {
				fmt.Fprintf(out, "hebb unwire (dry run): %s\n", cfg.VaultPath)
			}
			for _, s := range rep.Steps {
				fmt.Fprintf(out, "  %-14s %s\n", s.Status, s.Target)
			}
			fmt.Fprintln(out, "  kept           notes, .hebb/memory, .hebb/config.toml")
			if !force {
				fmt.Fprintln(out, "\nNothing changed. Re-run with --force to apply.")
			}
			return nil
		},
	}
	c.Flags().BoolVar(&force, "force", false, "actually remove (without this, unwire only previews)")
	c.Flags().BoolVar(&keepIndex, "keep-index", false, "keep .hebb/index.db (default: clear it; rebuild with `hebb index`)")
	c.Flags().StringVar(&mcpName, "mcp-name", install.DefaultMCPServerName, "MCP server/block name to remove")
	c.Flags().StringVar(&home, "home", "", "home dir holding .claude/.codex (default: user home)")
	c.Flags().StringVar(&launchdDir, "launchd-dir", "", "LaunchAgents dir (default: <home>/Library/LaunchAgents)")
	c.Flags().StringVar(&codexConfig, "codex-config", "", "Codex config.toml (default: <home>/.codex/config.toml)")
	c.Flags().StringVar(&desktopConfig, "claude-desktop-config", "", "Claude Desktop config (default: macOS app support dir)")
	_ = c.Flags().MarkHidden("home")
	_ = c.Flags().MarkHidden("launchd-dir")
	_ = c.Flags().MarkHidden("codex-config")
	_ = c.Flags().MarkHidden("claude-desktop-config")
	return c
}
