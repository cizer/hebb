package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	hebbassets "github.com/cizer/hebb"
	"github.com/cizer/hebb/core"
	"github.com/cizer/hebb/install"
	"github.com/spf13/cobra"
)

func newCmd(version string) *cobra.Command {
	var p installParams
	c := &cobra.Command{
		Use:   "new <path>",
		Short: "Scaffold a fresh vault from the template and install it",
		Long: "Create a new vault at <path> from the bundled template (PARA skeleton,\n" +
			"baseline CLAUDE.md and AGENTS.md, a note template and a memory seed), then\n" +
			"install it (.hebb/config.toml, memory, first index, agent skills). The MCP\n" +
			"server comes from the hebb Claude Code plugin, or pass --mcp-json for\n" +
			"plugin-less wiring. Refuses to scaffold into a non-empty dir.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}

			tmpl, err := templateFS(p.assetRoot)
			if err != nil {
				return err
			}
			screp, err := install.Scaffold(tmpl, target)
			if err != nil {
				return err
			}

			cfg, err := core.ResolveVault(target, "")
			if err != nil {
				return err
			}
			db, err := core.OpenDB(cfg.DBPath)
			if err != nil {
				return err
			}
			defer db.Close()

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Created vault: %s\n", target)
			for _, s := range screp.Steps {
				fmt.Fprintf(out, "  %-16s %s\n", s.Name, s.Status)
			}
			return installVault(cmd, cfg, db, p, version)
		},
	}
	bindInstallFlags(c, &p)
	return c
}

// templateFS returns the vault template filesystem to scaffold from. With
// --asset-root (or $HEBB_HOME) set it is that checkout's vault-template/ on
// disk, for live editing in development; otherwise it is the sub-FS of the
// assets embedded in the binary, so `hebb new` works standalone.
func templateFS(assetRoot string) (fs.FS, error) {
	if assetRoot == "" {
		assetRoot = os.Getenv("HEBB_HOME")
	}
	if assetRoot != "" {
		return os.DirFS(filepath.Join(assetRoot, "vault-template")), nil
	}
	return fs.Sub(hebbassets.Assets, "vault-template")
}
