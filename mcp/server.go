package mcp

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/cizer/hebb/core"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Serve opens the vault index, builds it, and runs the MCP server over stdio.
// The tool names are a stable contract: clients (the plugin, Codex, Desktop)
// rely on them, so they are guarded by tests.
func Serve(cfg core.Config, version string) error {
	db, err := core.OpenDB(cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := core.FullReindex(cfg, db); err != nil {
		return err
	}
	// Keep the latest watcher health where the vault_stats handler can read it.
	// A startup failure is no longer discarded: it is recorded so the surface can
	// report that incremental indexing is not running, instead of silently
	// relying on the read-time refresh alone.
	var health watcherHealth
	if w, werr := core.Watch(cfg, db); werr == nil {
		defer w.Close()
		health = w.Health
	} else {
		failed := core.FailedWatcherHealth(werr)
		health = func() core.WatcherHealth { return failed }
	}

	s := server.NewMCPServer("hebb", version)
	register(s, db, cfg, health)
	return server.ServeStdio(s)
}

// watcherHealth returns the current watcher health snapshot. It is a function so
// a live watcher reports current state while a never-started watcher reports a
// fixed failure snapshot.
type watcherHealth func() core.WatcherHealth

func register(s *server.MCPServer, db *sql.DB, cfg core.Config, health watcherHealth) {
	for _, st := range toolset(db, cfg, health) {
		s.AddTool(st.Tool, st.Handler)
	}
}

// toolset builds the MCP tool definitions and their handlers. It is at package
// scope so tests can invoke the handlers directly against a real index, which
// is the layer Claude actually consumes.
func toolset(db *sql.DB, cfg core.Config, health watcherHealth) []server.ServerTool {
	return []server.ServerTool{
		{
			Tool: mcp.NewTool("search_vault",
				mcp.WithDescription("Full-text search over the configured Markdown corpus. Returns ranked results with snippets. Use for finding specific notes, docs, or content matching keywords."),
				mcp.WithString("query", mcp.Required(), mcp.Description("Search terms (natural language or FTS5 syntax)")),
				mcp.WithNumber("limit", mcp.Description("Maximum results to return (default 10)")),
				mcp.WithString("tag", mcp.Description("Filter results to notes with this tag")),
				mcp.WithString("path_prefix", mcp.Description("Filter to notes under this path prefix (e.g. '2-Areas/', '1-Projects/FTTF/')")),
			),
			Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				refresh(cfg, db)
				query := req.GetString("query", "")
				results, err := core.Search(db, query, req.GetInt("limit", 10), req.GetString("tag", ""), req.GetString("path_prefix", ""))
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				if len(results) == 0 {
					return mcp.NewToolResultText(fmt.Sprintf("No results found for: %q", query)), nil
				}
				var b strings.Builder
				fmt.Fprintf(&b, "Found %d results:\n\n", len(results))
				for i, r := range results {
					snip := strings.NewReplacer(">>>", "**", "<<<", "**").Replace(r.Snippet)
					fmt.Fprintf(&b, "%d. **%s**\n   Path: %s\n   Tags: %s\n   %s\n\n", i+1, r.Title, r.Path, orNone(r.Tags), snip)
				}
				return mcp.NewToolResultText(b.String()), nil
			},
		},
		{
			Tool: mcp.NewTool("expand_context",
				mcp.WithDescription("Follow wiki-style links from a seed Markdown file to find related context. Traverses the link graph 1-2 hops outward, returning connected notes with their relationship type (outgoing link, incoming link, 2nd hop)."),
				mcp.WithString("note_path", mcp.Required(), mcp.Description("Path or title of the seed note to expand from")),
				mcp.WithNumber("depth", mcp.Description("How many link-hops to follow, 1 or 2 (default 1)")),
				mcp.WithNumber("limit", mcp.Description("Maximum notes to return (default 20)")),
			),
			Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				refresh(cfg, db)
				note := req.GetString("note_path", "")
				results := core.ExpandContext(db, note, req.GetInt("depth", 1), req.GetInt("limit", 20))
				if len(results) == 0 {
					return mcp.NewToolResultText(fmt.Sprintf("No linked notes found for: %q", note)), nil
				}
				var b strings.Builder
				fmt.Fprintf(&b, "Context expanded from %q (%d related notes):\n\n", note, len(results))
				for i, r := range results {
					fmt.Fprintf(&b, "%d. **%s** [%s]\n   Path: %s\n   %s\n\n", i+1, r.Title, r.Relationship, r.Path, clip(r.Snippet, 150))
				}
				return mcp.NewToolResultText(b.String()), nil
			},
		},
		{
			Tool: mcp.NewTool("get_context_for_topic",
				mcp.WithDescription("Assemble relevant context for a topic by combining full-text search, link graph traversal, and tag-based expansion. Use this for broad questions where you need a comprehensive context bundle rather than a specific file."),
				mcp.WithString("topic", mcp.Required(), mcp.Description("Natural language topic or question")),
				mcp.WithNumber("limit", mcp.Description("Maximum notes to include (default 15)")),
				mcp.WithString("path_prefix", mcp.Description("Filter to notes under this path prefix")),
			),
			Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				refresh(cfg, db)
				topic := req.GetString("topic", "")
				results := core.GetContextForTopic(db, topic, req.GetInt("limit", 15), req.GetString("path_prefix", ""))
				if len(results) == 0 {
					return mcp.NewToolResultText(fmt.Sprintf("No relevant context found for: %q", topic)), nil
				}
				var b strings.Builder
				fmt.Fprintf(&b, "Context for %q (%d notes assembled):\n\n", topic, len(results))
				for i, r := range results {
					fmt.Fprintf(&b, "%d. **%s** [%s]\n   Path: %s\n   Tags: %s\n   %s\n\n", i+1, r.Title, r.Relevance, r.Path, orNone(r.Tags), clip(r.Snippet, 200))
				}
				return mcp.NewToolResultText(b.String()), nil
			},
		},
		{
			Tool: mcp.NewTool("vault_stats",
				mcp.WithDescription("Get statistics about the Markdown corpus index: note count, link count, and top tags."),
			),
			Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				refresh(cfg, db)
				notes, links, tags, err := core.Stats(db)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				var b strings.Builder
				fmt.Fprintf(&b, "Corpus index stats:\n- Notes: %d\n- Links: %d\n- Last indexed: %s\n- Watcher: %s\n\nTop tags:\n",
					notes, links, lastIndexedLine(db), watcherLine(health))
				for _, t := range tags {
					fmt.Fprintf(&b, "  %s: %d\n", t.Tag, t.Count)
				}
				return mcp.NewToolResultText(b.String()), nil
			},
		},
		{
			Tool: mcp.NewTool("reindex_vault",
				mcp.WithDescription("Force a full reindex of the Markdown corpus, re-parsing every file. The index normally refreshes itself: changed files are picked up automatically on the next search, and a file watcher reindexes edits as they happen. Use this manual escape hatch only if the index seems stale despite that, or after bulk file operations outside the vault."),
			),
			Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				res, err := core.FullReindexForce(cfg, db)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				return mcp.NewToolResultText(fmt.Sprintf("Reindexed %d notes (%d removed).", res.Indexed, res.Removed)), nil
			},
		},
	}
}

// refresh runs the read-time staleness pass before a query so a file written
// since the server started (or during the watcher debounce window, or with the
// watcher dead) is found on the next call without a manual reindex_vault. It is
// stat-only over an unchanged vault, so cheap enough for every read. Gated on
// the vault's [index] auto_refresh; errors are non-fatal (the read still runs
// against whatever the index holds).
func refresh(cfg core.Config, db *sql.DB) {
	if cfg.AutoRefresh {
		_, _ = core.RefreshChanged(cfg, db)
	}
}

// lastIndexedLine renders the last full-reindex timestamp for vault_stats.
func lastIndexedLine(db *sql.DB) string {
	if t, ok := core.LastFullReindex(db); ok {
		return t.Local().Format("2006-01-02 15:04:05")
	}
	return "never"
}

// watcherLine renders watcher liveness for vault_stats: alive plus last-event
// time, or a clear not-running line carrying the startup error when present.
func watcherLine(health watcherHealth) string {
	if health == nil {
		return "unknown"
	}
	h := health()
	if !h.Alive {
		if h.StartErr != "" {
			return "not running (" + h.StartErr + ")"
		}
		return "not running"
	}
	last := "no events yet"
	if !h.LastEventAt.IsZero() {
		last = "last event " + h.LastEventAt.Local().Format("2006-01-02 15:04:05")
	}
	if h.Errors > 0 {
		return fmt.Sprintf("alive, %s, %d errors", last, h.Errors)
	}
	return "alive, " + last
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
