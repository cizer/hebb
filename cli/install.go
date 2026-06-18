package cli

import (
	"database/sql"
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
	mcpJSON     bool
	// Agent wiring. When none of these (or --mcp-json) is set and stdin is a
	// terminal, install offers an interactive picker; --no-interaction skips it.
	codex            bool
	claudeDesktop    bool
	noInteraction    bool
	codexConfig      string // test override
	claudeDesktopCfg string // test override
	noSkills         bool   // skip installing agent skills into the skills dirs
}

func installCmd(version string) *cobra.Command {
	var p installParams
	c := &cobra.Command{
		Use:   "install",
		Short: "Wire this vault into the machine",
		Long: "Initialise the per-vault config (.hebb/config.toml), symlink memory\n" +
			"into the Claude project dir, and build the first index. Idempotent.\n" +
			"Skills and the MCP server are delivered by the hebb Claude Code plugin;\n" +
			"pass --mcp-json to write a per-vault .mcp.json + settings for plugin-less\n" +
			"use instead. Pass --launchd to render the vault's launchd jobs (and --load\n" +
			"to bootstrap them); the binary is standalone, with automation scripts\n" +
			"embedded, or pass --asset-root to use a repo checkout's automation/.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, db, err := openVault()
			if err != nil {
				return err
			}
			defer db.Close()
			return installVault(cmd, cfg, db, p, version)
		},
	}
	bindInstallFlags(c, &p)
	return c
}

// bindInstallFlags registers the install-wiring flags on a command. The
// machine-targeting flags are hidden: they exist for tests and headless runs.
func bindInstallFlags(c *cobra.Command, p *installParams) {
	c.Flags().StringVar(&p.serverName, "mcp-name", install.DefaultMCPServerName, "MCP server name written into .mcp.json")
	c.Flags().StringVar(&p.assetRoot, "asset-root", "", "dev override: use this repo checkout's automation/ instead of the bundled assets (default $HEBB_HOME)")
	c.Flags().BoolVar(&p.mcpJSON, "mcp-json", false, "write a per-vault .mcp.json + settings for plugin-less use (otherwise the hebb plugin provides the MCP server)")
	c.Flags().BoolVar(&p.noSkills, "no-skills", false, "do not install hebb's agent skills into the skills dir(s)")
	c.Flags().BoolVar(&p.codex, "codex", false, "register this vault as a Codex MCP server (~/.codex/config.toml)")
	c.Flags().BoolVar(&p.claudeDesktop, "claude-desktop", false, "register this vault as a Claude Desktop MCP server")
	c.Flags().BoolVar(&p.noInteraction, "no-interaction", false, "never prompt; wire only the agents named by flags")
	c.Flags().StringVar(&p.codexConfig, "codex-config", "", "Codex config.toml (default: <home>/.codex/config.toml)")
	c.Flags().StringVar(&p.claudeDesktopCfg, "claude-desktop-config", "", "Claude Desktop config path (default: macOS app support dir)")
	c.Flags().BoolVar(&p.withLaunchd, "launchd", false, "render the vault's launchd jobs into ~/Library/LaunchAgents")
	c.Flags().BoolVar(&p.load, "load", false, "bootstrap rendered launchd jobs via launchctl (implies --launchd)")
	c.Flags().StringVar(&p.home, "home", "", "home dir holding .claude (default: user home)")
	c.Flags().StringVar(&p.launchdDir, "launchd-dir", "", "target LaunchAgents dir (default: <home>/Library/LaunchAgents)")
	c.Flags().StringVar(&p.dataDir, "data-dir", "", "where bundled assets are materialised (default: $XDG_DATA_HOME/hebb or <home>/.local/share/hebb)")
	_ = c.Flags().MarkHidden("home")
	_ = c.Flags().MarkHidden("launchd-dir")
	_ = c.Flags().MarkHidden("data-dir")
	_ = c.Flags().MarkHidden("codex-config")
	_ = c.Flags().MarkHidden("claude-desktop-config")
}

// installVault performs the file-level install for an already-resolved vault
// and builds its first index, printing a step report. Shared by `install` and
// the install phase of `new`.
func installVault(cmd *cobra.Command, cfg core.Config, db *sql.DB, p installParams, version string) error {
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
	// Prefer a stable symlink (e.g. /opt/homebrew/bin/hebb) over a versioned
	// Cellar path when both resolve to the same binary, so the launchd jobs'
	// Program[0] survives a Homebrew upgrade with its TCC Full Disk Access grant
	// intact. Non-Cellar binaries are left unchanged.
	hebbBin = install.StableHebbBin(hebbBin)

	// Interactive agent picker: only when the user named no agent explicitly,
	// didn't opt out, and stdin is a terminal (so CI/headless/tests never block).
	out := cmd.OutOrStdout()
	if !p.codex && !p.claudeDesktop && !p.mcpJSON && !p.noInteraction && stdinIsInteractive() {
		sel := promptAgents(cmd.InOrStdin(), out)
		p.codex, p.claudeDesktop, p.mcpJSON = sel.Codex, sel.ClaudeDesktop, sel.MCPJSON
	}

	rep, err := install.Run(install.Options{
		VaultPath:    cfg.VaultPath,
		MCPName:      p.serverName,
		MCPCommand:   install.DefaultMCPCommand,
		Home:         home,
		HebbBin:      hebbBin,
		LaunchdDir:   launchdDir,
		Load:         p.load,
		Assets:       hebbassets.Assets,
		DataDir:      dataDir,
		AssetRoot:    assetRoot,
		MCPJSON:      p.mcpJSON,
		SkipSkills:   p.noSkills,
		RegistryPath: core.RegistryPath(home),
		HebbVersion:  version,
	})
	if err != nil {
		return err
	}

	res, err := core.FullReindex(cfg, db)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "Installed vault: %s\n", cfg.VaultPath)
	for _, s := range rep.Steps {
		fmt.Fprintf(out, "  %-16s %s\n", s.Name, s.Status)
	}
	fmt.Fprintf(out, "  %-16s %d notes indexed\n", "index", res.Indexed)

	// Per-agent wiring chosen by flag or picker (.mcp.json was handled by Run).
	if p.codex {
		codexPath := p.codexConfig
		if codexPath == "" {
			codexPath = filepath.Join(home, ".codex", "config.toml")
		}
		status, err := install.WriteCodexConfig(codexPath, p.serverName, install.DefaultMCPCommand, cfg.VaultPath)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "  %-16s %s (%s)\n", "codex", status, codexPath)
		// Match `hebb codex`: also deliver the skills into Codex's skills dir.
		if !p.noSkills {
			if skillsFS, serr := fs.Sub(hebbassets.Assets, "plugin/skills"); serr == nil {
				if names, ierr := install.InstallSkills(skillsFS, install.CodexSkillsDir(home)); ierr != nil {
					return ierr
				} else if len(names) > 0 {
					fmt.Fprintf(out, "  %-16s %s (%s)\n", "codex skills", strings.Join(names, ", "), install.CodexSkillsDir(home))
				}
			}
		}
	}
	if p.claudeDesktop {
		desktopPath := p.claudeDesktopCfg
		if desktopPath == "" {
			desktopPath = install.DefaultClaudeDesktopConfigPath(home)
		}
		// Claude Desktop launches servers with a minimal PATH, so pin the
		// absolute binary path, not the bare name.
		status, err := install.WriteClaudeDesktopConfig(desktopPath, p.serverName, hebbBin, cfg.VaultPath)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "  %-16s %s (%s)\n", "claude-desktop", status, desktopPath)
		fmt.Fprintf(out, "  %-16s restart Claude Desktop to load it\n", "")
	}
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
