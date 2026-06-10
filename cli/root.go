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

	root.AddCommand(indexCmd(), searchCmd(), mcpCmd(version), serveCmd(), installCmd(), doctorCmd(), newCmd(), codexCmd(), resetCmd(), syncCmd())
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
		Short: "Build or refresh the search index",
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
			_, db, err := openVault()
			if err != nil {
				return err
			}
			defer db.Close()
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
		Short: "Run the MCP server over stdio (for Claude)",
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
		Short: "Serve the local web search UI",
		RunE: func(*cobra.Command, []string) error {
			cfg, err := core.ResolveVault(flagVault, flagDB)
			if err != nil {
				return err
			}
			name := os.Getenv("HEBB_VAULT_NAME")
			if name == "" {
				name = filepath.Base(cfg.VaultPath)
			}
			return hebbweb.Serve(cfg, port, name)
		},
	}
	c.Flags().IntVar(&port, "port", defaultWebPort(), "port (default 4321, or $HEBB_WEB_PORT)")
	return c
}

func defaultWebPort() int {
	if v := os.Getenv("HEBB_WEB_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 4321
}
