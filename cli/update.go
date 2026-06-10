package cli

import (
	"fmt"
	"os"
	"runtime"

	"github.com/cizer/hebb/install"
	"github.com/spf13/cobra"
)

func updateCmd(version string) *cobra.Command {
	var checkOnly, force bool
	c := &cobra.Command{
		Use:   "update",
		Short: "Check for and install a newer hebb release",
		Long: "Compare this binary to the latest GitHub release and, unless --check,\n" +
			"install it: download the matching asset, verify its checksum, and\n" +
			"atomically replace the binary. Only self-replaces a binary hebb owns\n" +
			"(e.g. installed via install.sh); a Homebrew or 'go install' binary is\n" +
			"left to its package manager (--force overrides). Skills and the MCP\n" +
			"server ship as the Claude Code plugin and are updated via '/plugin'.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			u := install.NewUpdater()
			tag, err := u.LatestTag()
			if err != nil {
				return fmt.Errorf("check for updates: %w", err)
			}
			if !install.NewerAvailable(version, tag) {
				fmt.Fprintf(out, "hebb %s is up to date (latest release %s)\n", version, tag)
				return nil
			}
			fmt.Fprintf(out, "a newer hebb is available: %s (current %s)\n", tag, version)
			if checkOnly {
				fmt.Fprintln(out, "run 'hebb update' to install it")
				return nil
			}

			exe, err := os.Executable()
			if err != nil {
				return err
			}
			if method := install.DetectInstallMethod(exe); method != install.SelfManaged && !force {
				fmt.Fprintf(out, "this hebb is %s-managed; update with:\n  %s\n", method, method.AdviseCommand())
				fmt.Fprintln(out, "(or re-run 'hebb update --force' to self-replace anyway)")
				return nil
			}

			bin, err := u.DownloadBinary(tag, runtime.GOOS, runtime.GOARCH)
			if err != nil {
				return fmt.Errorf("download %s: %w", tag, err)
			}
			if err := install.ReplaceBinary(exe, bin); err != nil {
				return fmt.Errorf("replace binary: %w", err)
			}
			fmt.Fprintf(out, "updated hebb %s -> %s (%s)\n", version, tag, exe)
			fmt.Fprintln(out, "skills/MCP ship as the Claude Code plugin: run '/plugin update' in Claude to match.")
			fmt.Fprintln(out, "re-run 'hebb install' in a vault to refresh its launchd jobs and automation.")
			return nil
		},
	}
	c.Flags().BoolVar(&checkOnly, "check", false, "only report whether a newer release exists; don't install")
	c.Flags().BoolVar(&force, "force", false, "self-replace even a package-manager-managed binary")
	return c
}
