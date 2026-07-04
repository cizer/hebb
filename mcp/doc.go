// Package mcp exposes the core engine over the Model Context Protocol, so
// Claude can query a vault. It is registered per-vault via a project-scoped
// .mcp.json. Built on the Go MCP SDK.
//
// Tool names follow a verb_noun convention (search_vault, expand_context,
// get_context_for_topic, audit_vault, reindex_vault); vault_stats is a legacy
// exception kept for client-contract stability.
package mcp
