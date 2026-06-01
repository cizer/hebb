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
// The tool surface matches the Node onevault-mcp so a project-scoped .mcp.json
// can swap one for the other transparently.
func Serve(cfg core.Config, version string) error {
	db, err := core.OpenDB(cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := core.FullReindex(cfg, db); err != nil {
		return err
	}
	if w, werr := core.Watch(cfg, db); werr == nil {
		defer w.Close()
	}

	s := server.NewMCPServer("hebb", version)
	register(s, db, cfg)
	return server.ServeStdio(s)
}

func register(s *server.MCPServer, db *sql.DB, cfg core.Config) {
	for _, st := range toolset(db, cfg) {
		s.AddTool(st.Tool, st.Handler)
	}
}

// toolset builds the MCP tool definitions and their handlers. It is at package
// scope so tests can invoke the handlers directly against a real index, which
// is the layer Claude actually consumes.
func toolset(db *sql.DB, cfg core.Config) []server.ServerTool {
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
				notes, links, tags, err := core.Stats(db)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				var b strings.Builder
				fmt.Fprintf(&b, "Corpus index stats:\n- Notes: %d\n- Links: %d\n\nTop tags:\n", notes, links)
				for _, t := range tags {
					fmt.Fprintf(&b, "  %s: %d\n", t.Tag, t.Count)
				}
				return mcp.NewToolResultText(b.String()), nil
			},
		},
		{
			Tool: mcp.NewTool("reindex_vault",
				mcp.WithDescription("Force a full reindex of the Markdown corpus. Use if the index seems stale or after bulk file operations."),
			),
			Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				res, err := core.FullReindex(cfg, db)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				return mcp.NewToolResultText(fmt.Sprintf("Reindexed %d notes (%d removed).", res.Indexed, res.Removed)), nil
			},
		},
	}
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
