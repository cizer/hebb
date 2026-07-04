package cli

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// versionCmd prints exactly what `hebb --version` prints (cobra's default
// version template: "hebb version <version>\n"), as a discoverable
// subcommand for anyone who doesn't think to reach for the --version flag.
func versionCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the hebb version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "hebb version %s\n", version)
			return nil
		},
	}
}

// devVersion is the build-time default in cmd/hebb (overridden by
// -ldflags -X main.version=... for releases). When it is still in place we are
// running an unstamped dev build, so we enrich it from the VCS metadata Go
// embeds, making each commit distinguishable in `hebb --version`.
const devVersion = "0.0.0-dev"

// buildVersion returns the version to display. A release version (stamped via
// ldflags) is used verbatim; a dev build is augmented with the short git
// revision (and a -dirty marker for uncommitted changes) read from the binary's
// embedded build info.
func buildVersion(base string) string {
	if base != devVersion {
		return base
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return base
	}
	var rev string
	var modified bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	return formatDevVersion(base, rev, modified)
}

// formatDevVersion composes the dev version string. Pure, for testability.
func formatDevVersion(base, rev string, modified bool) string {
	if base != devVersion || rev == "" {
		return base
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if modified {
		rev += "-dirty"
	}
	return base + " (" + rev + ")"
}
