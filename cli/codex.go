package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	hebbassets "github.com/cizer/hebb"
	"github.com/cizer/hebb/core"
	"github.com/cizer/hebb/install"
	"github.com/spf13/cobra"
)

// codexCmd registers this vault with the Codex CLI by merging an
// [mcp_servers.<name>] block into ~/.codex/config.toml and installing hebb's
// agent skills into Codex's skills dir. Codex's config is user-global and
// hand-maintained, so this is the Codex counterpart to the Claude plugin: one
// named server per vault, merged non-destructively, plus the shared skills.
func codexCmd() *cobra.Command {
	var home, codexConfig, skillsDir, mcpName string
	var noSkills bool
	c := &cobra.Command{
		Use:   "codex",
		Short: "Register this vault as an MCP server for the Codex CLI",
		Long: "Merge an [mcp_servers." + install.DefaultMCPServerName + "] block into\n" +
			"~/.codex/config.toml so Codex launches `hebb mcp` pinned to this vault\n" +
			"(via HEBB_VAULT + cwd), and install hebb's agent skills into Codex's\n" +
			"skills dir (~/.agents/skills). Idempotent and non-destructive: other\n" +
			"servers, comments, keys, and skills are preserved. Use --mcp-name to\n" +
			"register a second vault under a different server name; --no-skills to\n" +
			"skip the skills. The Codex counterpart to the hebb Claude Code plugin.",
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

			if !noSkills {
				skillsFS, err := fs.Sub(hebbassets.Assets, "plugin/skills")
				if err != nil {
					return err
				}
				if skillsDir == "" {
					skillsDir = install.CodexSkillsDir(home)
				}
				names, err := install.InstallCodexSkills(skillsFS, skillsDir)
				if err != nil {
					return err
				}
				if len(names) > 0 {
					fmt.Fprintf(out, "  %-16s %s (%s)\n", "skills", strings.Join(names, ", "), skillsDir)
				}
			}
			fmt.Fprintf(out, "Restart Codex (or reload its config) to pick up the change.\n")
			return nil
		},
	}
	c.Flags().StringVar(&mcpName, "mcp-name", install.DefaultMCPServerName, "Codex server name (table key under [mcp_servers])")
	c.Flags().StringVar(&codexConfig, "codex-config", "", "path to Codex config.toml (default: <home>/.codex/config.toml)")
	c.Flags().BoolVar(&noSkills, "no-skills", false, "do not install hebb's skills into the Codex skills dir")
	c.Flags().StringVar(&skillsDir, "codex-skills-dir", "", "Codex skills dir (default: <home>/.agents/skills)")
	c.Flags().StringVar(&home, "home", "", "home dir holding .codex and .agents (default: user home)")
	_ = c.Flags().MarkHidden("home")
	_ = c.Flags().MarkHidden("codex-skills-dir")
	return c
}
