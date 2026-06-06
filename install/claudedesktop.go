package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Claude Desktop (the consumer app) consumes MCP servers from a user-global
// claude_desktop_config.json, not the Claude Code plugin format. Like Codex, the
// vault is pinned via env.HEBB_VAULT, and the command must be an absolute path
// because the app launches servers with a minimal PATH. The config is strict
// JSON, so we parse-merge-reencode, touching only our own server key.

// DefaultClaudeDesktopConfigPath is the macOS location of the Claude Desktop
// config under the given home dir.
func DefaultClaudeDesktopConfigPath(home string) string {
	return filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
}

// WriteClaudeDesktopConfig merges an mcpServers.<name> entry (pinned to
// vaultPath via HEBB_VAULT) into the Claude Desktop config, creating it if
// absent. Other servers and top-level keys are preserved. Idempotent; returns
// "wrote", "updated", or "unchanged".
func WriteClaudeDesktopConfig(configPath, name, command, vaultPath string) (string, error) {
	root, err := readJSONObject(configPath)
	if err != nil {
		return "", err
	}
	servers, _ := root["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	want := map[string]any{
		"command": command,
		"args":    []any{"mcp"},
		"env":     map[string]any{"HEBB_VAULT": vaultPath},
	}
	existing, present := servers[name]
	if present && jsonEqual(existing, want) {
		return "unchanged", nil
	}
	servers[name] = want
	root["mcpServers"] = servers
	if err := writeJSONObject(configPath, root); err != nil {
		return "", err
	}
	if present {
		return "updated", nil
	}
	return "wrote", nil
}

// RemoveClaudeDesktopConfig deletes the mcpServers.<name> entry, preserving
// every other server and key. Returns "removed" or "absent".
func RemoveClaudeDesktopConfig(configPath, name string) (string, error) {
	b, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return "absent", nil
	}
	if err != nil {
		return "", err
	}
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		return "", fmt.Errorf("parse %s: %w", configPath, err)
	}
	servers, _ := root["mcpServers"].(map[string]any)
	if _, ok := servers[name]; !ok {
		return "absent", nil
	}
	delete(servers, name)
	root["mcpServers"] = servers
	if err := writeJSONObject(configPath, root); err != nil {
		return "", err
	}
	return "removed", nil
}

// readJSONObject reads a JSON object file, returning an empty map if it does not
// exist. A malformed file is an error (never silently overwritten).
func readJSONObject(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

func writeJSONObject(path string, m map[string]any) error {
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}

func jsonEqual(a, b any) bool {
	ab, err1 := json.Marshal(a)
	bb, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && string(ab) == string(bb)
}
