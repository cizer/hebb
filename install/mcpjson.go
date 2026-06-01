// Package install wires a vault into the machine: it writes the per-vault
// contracts (.hebb/config.toml, the project-scoped .mcp.json), symlinks global
// skills and memory, and renders launchd jobs. Every operation is idempotent
// and parameterised by its target directories so it is fully testable and never
// hard-codes a home directory.
package install

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
)

// DefaultMCPServerName is the server key written into .mcp.json (and so the
// tool prefix Claude sees, e.g. mcp__hebb__search_vault).
const DefaultMCPServerName = "hebb"

// DefaultMCPCommand is the binary Claude launches for the MCP server. It is a
// bare name resolved on PATH (brew/npm install) rather than an absolute path,
// so the committed .mcp.json travels with a synced vault.
const DefaultMCPCommand = "hebb"

type mcpConfig struct {
	MCPServers map[string]mcpServer `json:"mcpServers"`
}

type mcpServer struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// RenderMCPJSON returns the canonical project-scoped .mcp.json for a vault.
// args is just ["mcp"]: hebb resolves the vault from the directory it is
// launched in (the vault root, where this file lives), keeping the committed
// file free of machine-specific absolute paths.
func RenderMCPJSON(serverName, command string) ([]byte, error) {
	cfg := mcpConfig{MCPServers: map[string]mcpServer{
		serverName: {Type: "stdio", Command: command, Args: []string{"mcp"}},
	}}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// WriteMCPJSON writes .mcp.json at the vault root, idempotently. It returns true
// if the file was created or its contents changed.
func WriteMCPJSON(vaultPath, serverName, command string) (bool, error) {
	want, err := RenderMCPJSON(serverName, command)
	if err != nil {
		return false, err
	}
	path := filepath.Join(vaultPath, ".mcp.json")
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, want) {
		return false, nil
	}
	if err := os.WriteFile(path, want, 0o644); err != nil {
		return false, err
	}
	return true, nil
}
