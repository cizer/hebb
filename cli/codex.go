package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cizer/hebb/core"
	"github.com/cizer/hebb/install"
	"github.com/spf13/cobra"
)

// codexCmd registers this vault with the Codex CLI by merging an
// [mcp_servers.<name>] block into ~/.codex/config.toml. Codex's config is
// user-global and hand-maintained, so this is the Codex counterpart to the
// Claude plugin: one named server per vault, merged non-destructively.
func codexCmd() *cobra.Command {
	var home, codexConfig, mcpName string
	c := &cobra.Command{
		Use:   "codex",
		Short: "Register this vault as an MCP server for the Codex CLI",
		Long: "Merge an [mcp_servers." + install.DefaultMCPServerName + "] block into\n" +
			"~/.codex/config.toml so Codex launches `hebb mcp` pinned to this vault\n" +
			"(via HEBB_VAULT + cwd). Idempotent and non-destructive: other servers,\n" +
			"comments, and keys are preserved. Use --mcp-name to register a second\n" +
			"vault under a different server name. Skills are Claude-only; for Codex,\n" +
			"put vault guidance in the vault's AGENTS.md.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := core.ResolveVault(flagVault, flagDB)
			if err != nil {
				return err
			}
			if home == "" {
				home, _ = os.UserHomeDir()
			}
			if codexConfig == "" {
				codexConfig = filepath.Join(home, ".codex", "config.toml")
			}
			status, err := install.WriteCodexConfig(codexConfig, mcpName, install.DefaultMCPCommand, cfg.VaultPath)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Codex MCP server %q -> %s\n", mcpName, cfg.VaultPath)
			fmt.Fprintf(out, "  %-16s %s (%s)\n", "config.toml", status, codexConfig)
			fmt.Fprintf(out, "Restart Codex (or reload its config) to pick up the change.\n")
			return nil
		},
	}
	c.Flags().StringVar(&mcpName, "mcp-name", install.DefaultMCPServerName, "Codex server name (table key under [mcp_servers])")
	c.Flags().StringVar(&codexConfig, "codex-config", "", "path to Codex config.toml (default: <home>/.codex/config.toml)")
	c.Flags().StringVar(&home, "home", "", "home dir holding .codex (default: user home)")
	_ = c.Flags().MarkHidden("home")
	return c
}
