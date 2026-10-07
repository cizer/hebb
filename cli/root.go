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
		// Execute() prints the error itself (as "error: ..."); without this cobra
		// would also print its own "Error: ..." line, double-reporting every failure.
		SilenceErrors: true,
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
		"doctor": "maintain", "audit": "maintain", "eval": "maintain", "themes": "maintain", "index": "maintain", "update": "maintain", "unwire": "maintain", "version": "maintain",
		"codex": "agents", "digest": "agents", "consolidate": "agents", "notify": "agents", "restart-services": "agents",
	}
	for _, cmd := range []*cobra.Command{
		newCmd(version), installCmd(version), vaultsCmd(),
		searchCmd(), serveCmd(), mcpCmd(version), syncCmd(),
		doctorCmd(), healthCmd(), evalCmd(), themesCmd(), indexCmd(), updateCmd(version), resetCmd(), versionCmd(version),
		codexCmd(), digestCmd(), consolidateCmd(), notifyCmd(), restartServicesCmd(),
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
		Long: "Walk the vault, parse markdown files that changed since the last index\n" +
			"(by mtime), and upsert them into the search index, removing entries for\n" +
			"files that no longer exist. Install and search both refresh automatically,\n" +
			"so this is mainly for forcing a rebuild after bulk edits outside hebb.",
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
		Long: "Full-text search over the vault's indexed notes, ranked by relevance.\n" +
			"Refreshes changed files before querying, so a note written moments ago is\n" +
			"found even without a prior 'hebb index'. Narrow results with --tag or\n" +
			"--path-prefix, or raise --limit for more hits.",
		Example: "  hebb search \"quarterly planning\"\n" +
			"  hebb search meeting --tag project-x --limit 5\n" +
			"  hebb search retro --path-prefix 1-Projects/\n" +
			"  hebb search onboarding --path-prefix .hebb/memory/   # agent memory",
		Args: cobra.MinimumNArgs(1),
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
			query := strings.Join(args, " ")
			results, err := core.Search(db, query, limit, tag, prefix)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			for _, r := range results {
				fmt.Fprintf(w, "%s\t%s\n", r.Title, r.Path)
			}
			w.Flush()
			if len(results) == 0 {
				fmt.Printf("No matches for %q.\n", query)
				fmt.Println("  try a broader or different term")
				fmt.Println("  if the vault's content recently changed, run `hebb index` and search again")
			} else {
				fmt.Printf("(%d results)\n", len(results))
			}
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
		Long: "Run hebb's Model Context Protocol server over stdio, exposing the vault's\n" +
			"search, notes and context tools to an MCP client such as Claude Code,\n" +
			"Claude Desktop or Codex. Normally launched by the client itself (via the\n" +
			"hebb plugin or a generated .mcp.json / config.toml entry from 'hebb\n" +
			"install'), not run by hand.",
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
		Example: "  hebb serve\n" +
			"  hebb serve --port 5555",
		RunE: func(cmd *cobra.Command, _ []string) error {
			targets, skipped, err := buildServeTargets()
			// Name every registry entry that could not be opened, before handling any
			// error. Serve already tolerates them (it serves the rest), but skipping
			// silently makes a vault appear to vanish from the picker with no
			// explanation — most often because its directory was moved or deleted
			// while the entry remained. When nothing is serveable at all, these lines
			// are the whole explanation for the error that follows.
			for _, s := range skipped {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"warning: skipping vault %q: %v\n  (remove it with 'hebb unwire --vault %s --force', or fix the path in the registry)\n",
					s.Name, s.Err, s.Path)
			}
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
// skippedVault is a registry entry serve could not open — most often a vault
// whose directory was moved or deleted while its registry entry remained. It is
// carried out of buildServeTargets so the caller can name it rather than let the
// vault disappear from the UI unexplained.
type skippedVault struct {
	Name string
	Path string
	Err  error
}

func buildServeTargets() ([]hebbweb.VaultTarget, []skippedVault, error) {
	var targets []hebbweb.VaultTarget
	var skipped []skippedVault
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
			// A registry entry whose directory is gone (or is no longer a directory)
			// must never stop the other vaults being served. Record it so the caller
			// can say why it vanished, and carry on.
			name := ref.Name
			if name == "" {
				name = ref.Path
			}
			skipped = append(skipped, skippedVault{Name: name, Path: ref.Path, Err: err})
			continue
		}
		name := ref.Name
		if name == "" {
			name = vaultDisplayName(cfg)
		}
		add(cfg, name)
	}
	if len(targets) == 0 {
		// Nothing serveable. Hand back the skipped list too: when every registered
		// vault is missing, those paths are the whole explanation.
		return nil, skipped, fmt.Errorf("no vaults to serve: run 'hebb install' in a vault first")
	}
	return targets, skipped, nil
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
