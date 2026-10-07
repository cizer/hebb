package mcp

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
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
				mcp.WithDescription("Full-text search over the configured Markdown vault. Returns ranked results with snippets. Use for finding specific notes, docs, or content matching keywords."),
				mcp.WithString("query", mcp.Required(), mcp.Description("Search terms (natural language or FTS5 syntax)")),
				mcp.WithNumber("limit", mcp.Description("Maximum results to return (default 10)")),
				mcp.WithString("tag", mcp.Description("Filter results to notes with this tag")),
				mcp.WithString("path_prefix", mcp.Description("Filter to notes under this path prefix (e.g. '2-Areas/', '1-Projects/FTTF/'). Agent memory notes are indexed under '.hebb/memory/'.")),
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
				logReturnedPaths(cfg, db, searchResultPaths(results))
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
				mcp.WithDescription("Follow wiki-style links from a seed note (by path or title) to find related context. Traverses the link graph 1-2 hops outward, returning connected notes with their relationship type (outgoing link, incoming link, 2nd hop). Use when you already have a specific note and want its neighbours."),
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
				logExpandContextAccess(cfg, db, note, results)
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
				mcp.WithDescription("Assemble relevant context for a topic by combining full-text search, link graph traversal, and tag-based expansion. Use this for broad questions where you need a comprehensive context bundle rather than a specific file. Use when you have a topic/question and want a bundle of relevant notes."),
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
				logReturnedPaths(cfg, db, topicResultPaths(results))
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
				mcp.WithDescription("Get statistics about the Markdown vault index: note count, link count, and top tags."),
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
				mcp.WithDescription("Force a full reindex of the Markdown vault, re-parsing every file. The index normally refreshes itself: changed files are picked up automatically on the next search, and a file watcher reindexes edits as they happen. Use this manual escape hatch only if the index seems stale despite that, or after bulk file operations outside the vault."),
			),
			Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				res, err := core.FullReindexForce(cfg, db)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				return mcp.NewToolResultText(fmt.Sprintf("Reindexed %d notes (%d removed).", res.Indexed, res.Removed)), nil
			},
		},
		{
			Tool: mcp.NewTool("audit_vault",
				mcp.WithDescription("Run the vault audit: dangling/ambiguous links, PARA drift, oversized notes, stubs, and orphans/islands. Returns the findings worklist plus graph stats. Read-only, mutates nothing."),
				mcp.WithString("path_prefix", mcp.Description("Filter findings to notes under this vault-relative path prefix (e.g. '2-Areas/')")),
				mcp.WithString("type", mcp.Description("Filter to a single finding type (e.g. 'dangling_link', 'stub')")),
				mcp.WithBoolean("report_unresolved", mcp.Description("Include unresolved-link findings, like the CLI --unresolved flag (default false)")),
			),
			Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				refresh(cfg, db)
				reportUnresolved := req.GetBool("report_unresolved", false) || cfg.Health.ReportUnresolvedLinks
				result, err := core.RunHealthFull(cfg, db, reportUnresolved)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				pathPrefix := req.GetString("path_prefix", "")
				typeFilter := req.GetString("type", "")
				findings := filterFindings(result.Findings, pathPrefix, typeFilter)
				return mcp.NewToolResultText(renderAudit(findings, result.Stats)), nil
			},
		},
	}
}

// logReturnedPaths bumps return_count/access_count/last_accessed in
// access_log for every path surfaced in a result set (METABOLISM.md Phase 3).
// Gated on the vault's [memory] access_log flag (default off, zero writes);
// best-effort — core.LogReturns swallows its own errors so a logging failure
// never fails the tool call or alters its output.
func logReturnedPaths(cfg core.Config, db *sql.DB, paths []string) {
	if !cfg.Memory.AccessLog {
		return
	}
	core.LogReturns(db, paths)
}

// logExpandContextAccess records the Phase 3 access-log signals for an
// expand_context call: the resolved seed note gets followup_count bumped (the
// stronger "agent chose to follow up on this note" signal), and every
// returned neighbour path gets return_count bumped, in line with
// METABOLISM.md Phase 3's use-vs-return distinction. Gated on [memory]
// access_log (default off). The seed is re-resolved with the same exact
// resolver ExpandContext uses internally (core.ResolveTargetDB with no source
// path) rather than trusting the raw note_path argument, so a lookup by title
// logs against the canonical path, not the input string. A seed that fails to
// resolve (the "not_found" sentinel result) logs no followup.
func logExpandContextAccess(cfg core.Config, db *sql.DB, notePath string, results []core.ContextResult) {
	if !cfg.Memory.AccessLog {
		return
	}
	if resolved, status := core.ResolveTargetDB(db, notePath, ""); status == core.Resolved {
		core.LogFollowup(db, resolved)
	}
	var neighbours []string
	for _, r := range results {
		if r.Relationship == "not_found" {
			continue
		}
		neighbours = append(neighbours, r.Path)
	}
	core.LogReturns(db, neighbours)
}

// searchResultPaths extracts the surfaced note paths from a search_vault
// result set, for access-log bumping.
func searchResultPaths(results []core.SearchResult) []string {
	paths := make([]string, len(results))
	for i, r := range results {
		paths[i] = r.Path
	}
	return paths
}

// topicResultPaths extracts the surfaced note paths from a
// get_context_for_topic result set, for access-log bumping.
func topicResultPaths(results []core.TopicResult) []string {
	paths := make([]string, len(results))
	for i, r := range results {
		paths[i] = r.Path
	}
	return paths
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

// findingTypeOrder is the fixed display order for finding types in audit_vault
// output, matching the CLI's `hebb audit` grouping (cli/health.go typeOrder).
// Types not listed here appear last in lexicographic order, so a future
// detector is still rendered rather than dropped.
var findingTypeOrder = []string{"dangling_link", "ambiguous_link", "para_drift", "cooling_project", "stale_pin", "oversized", "stub", "orphan", "leaf", "island"}

// filterFindings narrows a findings worklist to those under pathPrefix (when
// set) and matching typeFilter (when set). Either or both may be empty, which
// disables that filter.
func filterFindings(findings []core.Finding, pathPrefix, typeFilter string) []core.Finding {
	if pathPrefix == "" && typeFilter == "" {
		return findings
	}
	var out []core.Finding
	for _, f := range findings {
		if pathPrefix != "" && !strings.HasPrefix(f.Path, pathPrefix) {
			continue
		}
		if typeFilter != "" && f.Type != typeFilter {
			continue
		}
		out = append(out, f)
	}
	return out
}

// renderAudit renders the audit_vault findings worklist as markdown: a one-line
// graph summary, then findings grouped by type with counts, one line per
// finding. An empty worklist renders a clear "no findings" line instead of an
// empty section, matching the voice of the other tools' empty-result messages.
func renderAudit(findings []core.Finding, stats core.GraphStats) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Graph: %d notes, %d links, %d components, giant component %.0f%%\n\n",
		stats.NodeCount, stats.EdgeCount, stats.ComponentCount, stats.GiantRatio*100)

	if len(findings) == 0 {
		b.WriteString("No findings: the vault is clean.\n")
		return b.String()
	}

	byType := make(map[string][]core.Finding)
	for _, f := range findings {
		byType[f.Type] = append(byType[f.Type], f)
	}
	seen := make(map[string]bool)
	var order []string
	for _, t := range findingTypeOrder {
		if _, ok := byType[t]; ok {
			order = append(order, t)
			seen[t] = true
		}
	}
	var extra []string
	for t := range byType {
		if !seen[t] {
			extra = append(extra, t)
		}
	}
	sort.Strings(extra)
	order = append(order, extra...)

	fmt.Fprintf(&b, "%d finding(s):\n", len(findings))
	for _, t := range order {
		group := byType[t]
		fmt.Fprintf(&b, "\n**%s** (%d)\n", t, len(group))
		for _, f := range group {
			fmt.Fprintf(&b, "- %s — %s\n", f.Path, f.Detail)
		}
	}
	return b.String()
}
