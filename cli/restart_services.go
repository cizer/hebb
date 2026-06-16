package cli

import (
	"fmt"

	"github.com/cizer/hebb/install"
	"github.com/spf13/cobra"
)

func restartServicesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart-services",
		Short: "Restart hebb's running launchd services onto the current binary",
		Long: "Restart every loaded hebb web service (the long-running 'hebb serve'\n" +
			"launchd job) across all vaults on this machine, so a replaced binary\n" +
			"takes effect. Run it after updating the binary (hebb update, a dev build,\n" +
			"brew, go install) or if a service is misbehaving. Scheduled jobs\n" +
			"(daily-digest, action-review, update-check) are left alone: they re-exec\n" +
			"the binary on their next run. macOS only; a no-op where launchctl is\n" +
			"absent.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			restarted, err := install.RestartServices()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(restarted) == 0 {
				fmt.Fprintln(out, "no hebb web services loaded; nothing to restart")
				return nil
			}
			for _, label := range restarted {
				fmt.Fprintf(out, "restarted %s\n", label)
			}
			return nil
		},
	}
}
