package cli

import (
	"fmt"
	"os"

	"github.com/cizer/hebb/core"
	"github.com/cizer/hebb/install"
	"github.com/spf13/cobra"
)

func doctorCmd() *cobra.Command {
	var home, assetRoot, launchdDir, dataDir string
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Check vault and install health",
		Long:  "Inspect a vault and its install (config, .mcp.json, index, settings,\nmemory, launchd) and report each. Read-only; repairs nothing.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Resolve the vault path without opening the index (read-only).
			cfg, err := core.ResolveVault(flagVault, flagDB)
			if err != nil {
				return err
			}
			if home == "" {
				home, _ = os.UserHomeDir()
			}
			if assetRoot == "" {
				assetRoot = os.Getenv("HEBB_HOME")
			}
			if dataDir == "" {
				dataDir = defaultDataDir(home)
			}
			checks := install.Doctor(install.Options{
				VaultPath:  cfg.VaultPath,
				MCPName:    install.DefaultMCPServerName,
				Home:       home,
				AssetRoot:  assetRoot,
				DataDir:    dataDir,
				LaunchdDir: launchdDir,
			})
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "hebb doctor: %s\n", cfg.VaultPath)
			for _, ch := range checks {
				fmt.Fprintf(out, "  %-4s %-10s %s\n", statusMark(ch.Status), ch.Name, ch.Detail)
			}
			if install.AnyFailed(checks) {
				return fmt.Errorf("doctor found problems (see above)")
			}
			return nil
		},
	}
	c.Flags().StringVar(&assetRoot, "asset-root", "", "hebb repo/asset dir holding automation/ (default $HEBB_HOME)")
	c.Flags().StringVar(&home, "home", "", "home dir holding .claude (default: user home)")
	c.Flags().StringVar(&launchdDir, "launchd-dir", "", "LaunchAgents dir to check (default: <home>/Library/LaunchAgents)")
	c.Flags().StringVar(&dataDir, "data-dir", "", "hebb data dir to check (default: $XDG_DATA_HOME/hebb or <home>/.local/share/hebb)")
	_ = c.Flags().MarkHidden("home")
	_ = c.Flags().MarkHidden("launchd-dir")
	_ = c.Flags().MarkHidden("data-dir")
	return c
}

func statusMark(status string) string {
	switch status {
	case "ok":
		return "ok"
	case "warn":
		return "warn"
	default:
		return "FAIL"
	}
}
