package cli

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/cizer/hebb/core"
	"github.com/cizer/hebb/install"
	hebbmcp "github.com/cizer/hebb/mcp"
	hebbweb "github.com/cizer/hebb/web"
	"github.com/spf13/cobra"
)

var (
	flagVault string
	flagDB    string
)

// Execute builds the root command and runs it, exiting non-zero on error.
func Execute(version string) {
	if err := newRoot(buildVersion(version)).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func newRoot(version string) *cobra.Command {
	root := &cobra.Command{
		Use:          "hebb",
		Short:        "hebb - a portable engine for markdown knowledge vaults",
		Long:         "hebb indexes, searches, scaffolds and maintains markdown vaults.\nMulti-vault: run inside a vault directory, or pass --vault.",
		Version:      version,
		SilenceUsage: true,
	}
	root.PersistentFlags().StringVar(&flagVault, "vault", "", "vault path (default: nearest .hebb/ above cwd, or $HEBB_VAULT)")
	root.PersistentFlags().StringVar(&flagDB, "db", "", "index db path (default: <vault>/.hebb/index.db)")

	root.AddGroup(
		&cobra.Group{ID: "setup", Title: "Set up a vault:"},
		&cobra.Group{ID: "use", Title: "Use a vault:"},
		&cobra.Group{ID: "maintain", Title: "Check & maintain:"},
		&cobra.Group{ID: "agents", Title: "Agents & automation:"},
	)
	group := map[string]string{
		"new": "setup", "install": "setup", "vaults": "setup",
		"search": "use", "serve": "use", "mcp": "use", "sync": "use",
		"doctor": "maintain", "audit": "maintain", "index": "maintain", "update": "maintain", "unwire": "maintain",
		"codex": "agents", "digest": "agents", "notify": "agents", "restart-services": "agents",
	}
	for _, cmd := range []*cobra.Command{
		newCmd(version), installCmd(version), vaultsCmd(),
		searchCmd(), serveCmd(), mcpCmd(version), syncCmd(),
		doctorCmd(), healthCmd(), indexCmd(), updateCmd(version), resetCmd(),
		codexCmd(), digestCmd(), notifyCmd(), restartServicesCmd(),
	} {
		if g, ok := group[cmd.Name()]; ok {
			cmd.GroupID = g
		}
		root.AddCommand(cmd)
	}
	return root
}

func openVault() (core.Config, *sql.DB, error) {
	cfg, err := core.ResolveVault(flagVault, flagDB)
	if err != nil {
		return core.Config{}, nil, err
	}
	db, err := core.OpenDB(cfg.DBPath)
	if err != nil {
		return core.Config{}, nil, err
	}
	return cfg, db, nil
}

func indexCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "index",
		Short: "Rebuild the search index (normally automatic)",
		RunE: func(*cobra.Command, []string) error {
			cfg, db, err := openVault()
			if err != nil {
				return err
			}
			defer db.Close()
			res, err := core.FullReindex(cfg, db)
			if err != nil {
				return err
			}
			_, links, _, err := core.Stats(db)
			if err != nil {
				return err
			}
			fmt.Printf("indexed %d notes (%d removed), %d links -> %s\n", res.Indexed, res.Removed, links, cfg.DBPath)
			return nil
		},
	}
}

func searchCmd() *cobra.Command {
	var limit int
	var tag, prefix string
	c := &cobra.Command{
		Use:   "search [query]",
		Short: "Search the vault",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, db, err := openVault()
			if err != nil {
				return err
			}
			defer db.Close()
			// Refresh before querying so a note written moments ago (with no prior
			// hebb index) is found: the CLI is the most exposed read path and has no
			// watcher of its own. Stat-only over an unchanged vault, so cheap.
			if cfg.AutoRefresh {
				_, _ = core.RefreshChanged(cfg, db)
			}
			results, err := core.Search(db, strings.Join(args, " "), limit, tag, prefix)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			for _, r := range results {
				fmt.Fprintf(w, "%s\t%s\n", r.Title, r.Path)
			}
			w.Flush()
			fmt.Printf("(%d results)\n", len(results))
			return nil
		},
	}
	c.Flags().IntVar(&limit, "limit", 10, "max results")
	c.Flags().StringVar(&tag, "tag", "", "filter by tag")
	c.Flags().StringVar(&prefix, "path-prefix", "", "filter by path prefix")
	return c
}

func mcpCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run the MCP server over stdio (Claude, Codex, any MCP client)",
		RunE: func(*cobra.Command, []string) error {
			cfg, err := core.ResolveVault(flagVault, flagDB)
			if err != nil {
				return err
			}
			return hebbmcp.Serve(cfg, version)
		},
	}
}

func serveCmd() *cobra.Command {
	var port int
	c := &cobra.Command{
		Use:   "serve",
		Short: "Serve the local web UI for all vaults on one port (switchable)",
		Long: "Serve the search and health UI on one loopback port for every vault hebb\n" +
			"knows about (the current vault plus the machine registry), switchable in\n" +
			"the UI. One server on one port, so multiple vaults never collide.",
		RunE: func(*cobra.Command, []string) error {
			targets, err := buildServeTargets()
			if err != nil {
				return err
			}
			return hebbweb.ServeMulti(targets, port)
		},
	}
	c.Flags().IntVar(&port, "port", defaultWebPort(), "port (default 4321, or $HEBB_WEB_PORT)")
	return c
}

// buildServeTargets assembles the vaults to serve: the current vault (if one
// resolves, listed first so it is the default selection) plus every registered
// vault, de-duplicated by path. Stale registry entries (vault gone) are skipped.
func buildServeTargets() ([]hebbweb.VaultTarget, error) {
	var targets []hebbweb.VaultTarget
	seenPath := map[string]bool{}
	usedSlug := map[string]bool{}
	add := func(cfg core.Config, name string) {
		if seenPath[cfg.VaultPath] {
			return
		}
		seenPath[cfg.VaultPath] = true
		base := install.Slugify(name)
		if base == "" {
			base = "vault"
		}
		slug := base
		for i := 2; usedSlug[slug]; i++ {
			slug = fmt.Sprintf("%s-%d", base, i)
		}
		usedSlug[slug] = true
		targets = append(targets, hebbweb.VaultTarget{Slug: slug, Name: name, Cfg: cfg})
	}

	if cfg, err := core.ResolveVault(flagVault, flagDB); err == nil {
		add(cfg, vaultDisplayName(cfg))
	}
	home, _ := os.UserHomeDir()
	reg, _ := core.LoadRegistry(core.RegistryPath(home))
	for _, ref := range reg.Vaults {
		cfg, err := core.ResolveVault(ref.Path, "")
		if err != nil {
			continue
		}
		name := ref.Name
		if name == "" {
			name = vaultDisplayName(cfg)
		}
		add(cfg, name)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no vaults to serve: run 'hebb install' in a vault first")
	}
	return targets, nil
}

// vaultDisplayName is the vault's configured name, falling back to its directory
// name. $HEBB_VAULT_NAME overrides (back-compat with the old single-vault serve).
func vaultDisplayName(cfg core.Config) string {
	if n := os.Getenv("HEBB_VAULT_NAME"); n != "" {
		return n
	}
	if vc, existed, err := core.LoadVaultConfig(cfg.VaultPath); err == nil && existed && vc.Name != "" {
		return vc.Name
	}
	return filepath.Base(cfg.VaultPath)
}

func defaultWebPort() int {
	if v := os.Getenv("HEBB_WEB_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 4321
}
