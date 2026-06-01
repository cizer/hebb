package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// MCPToolNames are the tools the hebb MCP server exposes. They must match the
// tool names registered in package mcp.
func MCPToolNames() []string {
	return []string{
		"search_vault",
		"expand_context",
		"get_context_for_topic",
		"vault_stats",
		"reindex_vault",
	}
}

// WriteProjectSettings merges hebb's MCP wiring into <vault>/.claude/settings.json:
// it enables the project-scoped MCP server and pre-approves its tools so the
// agent is not prompted for each call. Any existing settings are preserved;
// only the relevant arrays are extended. Returns true if the file was created
// or changed.
func WriteProjectSettings(vaultPath, serverName string) (bool, error) {
	path := filepath.Join(vaultPath, ".claude", "settings.json")

	settings := map[string]any{}
	existing, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// start from empty
	case err != nil:
		return false, err
	default:
		if err := json.Unmarshal(existing, &settings); err != nil {
			return false, err
		}
	}

	addToStringArray(settings, "enabledMcpjsonServers", serverName)

	perm, _ := settings["permissions"].(map[string]any)
	if perm == nil {
		perm = map[string]any{}
	}
	for _, tool := range MCPToolNames() {
		addToStringArray(perm, "allow", "mcp__"+serverName+"__"+tool)
	}
	settings["permissions"] = perm

	want, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return false, err
	}
	want = append(want, '\n')
	if bytes.Equal(existing, want) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, want, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// addToStringArray appends val to m[key] (a JSON string array) unless already
// present, preserving existing order.
func addToStringArray(m map[string]any, key, val string) {
	arr, _ := m[key].([]any)
	for _, x := range arr {
		if s, ok := x.(string); ok && s == val {
			return
		}
	}
	m[key] = append(arr, val)
}
