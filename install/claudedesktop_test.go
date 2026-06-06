package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteClaudeDesktopConfigCreatesAndPins(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "nested", "claude_desktop_config.json")
	status, err := WriteClaudeDesktopConfig(cfg, "hebb", "/abs/bin/hebb", "/vaults/work")
	if err != nil {
		t.Fatal(err)
	}
	if status != "wrote" {
		t.Errorf("status = %q, want wrote", status)
	}
	var parsed struct {
		MCPServers map[string]struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	b, _ := os.ReadFile(cfg)
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	s := parsed.MCPServers["hebb"]
	if s.Command != "/abs/bin/hebb" {
		t.Errorf("command = %q, want absolute path", s.Command)
	}
	if len(s.Args) != 1 || s.Args[0] != "mcp" {
		t.Errorf("args = %v, want [mcp]", s.Args)
	}
	if s.Env["HEBB_VAULT"] != "/vaults/work" {
		t.Errorf("HEBB_VAULT = %q, want the vault path", s.Env["HEBB_VAULT"])
	}
}

func TestWriteClaudeDesktopConfigPreservesOtherServers(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "claude_desktop_config.json")
	orig := `{"globalShortcut":"Cmd+Space","mcpServers":{"onevault":{"command":"node","args":["server.js"]}}}`
	if err := os.WriteFile(cfg, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteClaudeDesktopConfig(cfg, "hebb", "/abs/hebb", "/v"); err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	b, _ := os.ReadFile(cfg)
	if err := json.Unmarshal(b, &root); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if root["globalShortcut"] != "Cmd+Space" {
		t.Error("top-level key lost")
	}
	servers := root["mcpServers"].(map[string]any)
	if _, ok := servers["onevault"]; !ok {
		t.Error("the existing onevault server must survive")
	}
	if _, ok := servers["hebb"]; !ok {
		t.Error("hebb server should be added")
	}
}

func TestWriteClaudeDesktopConfigIdempotent(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "claude_desktop_config.json")
	if _, err := WriteClaudeDesktopConfig(cfg, "hebb", "/abs/hebb", "/v"); err != nil {
		t.Fatal(err)
	}
	status, err := WriteClaudeDesktopConfig(cfg, "hebb", "/abs/hebb", "/v")
	if err != nil {
		t.Fatal(err)
	}
	if status != "unchanged" {
		t.Errorf("second write = %q, want unchanged", status)
	}
}

func TestRemoveClaudeDesktopConfig(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "claude_desktop_config.json")
	orig := `{"mcpServers":{"onevault":{"command":"node"},"hebb":{"command":"/abs/hebb"}}}`
	if err := os.WriteFile(cfg, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	status, err := RemoveClaudeDesktopConfig(cfg, "hebb")
	if err != nil {
		t.Fatal(err)
	}
	if status != "removed" {
		t.Errorf("status = %q, want removed", status)
	}
	var root map[string]any
	b, _ := os.ReadFile(cfg)
	json.Unmarshal(b, &root)
	servers := root["mcpServers"].(map[string]any)
	if _, ok := servers["hebb"]; ok {
		t.Error("hebb should be removed")
	}
	if _, ok := servers["onevault"]; !ok {
		t.Error("onevault must survive")
	}

	// Absent cases: no file, and present-but-no-hebb.
	if st, _ := RemoveClaudeDesktopConfig(filepath.Join(t.TempDir(), "none.json"), "hebb"); st != "absent" {
		t.Errorf("missing file: %q, want absent", st)
	}
}
