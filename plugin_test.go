package hebb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Tests run with the working directory set to the package dir (the repo root),
// so the plugin/ paths are repo-relative.

func TestPluginManifestValid(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("plugin", ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatalf("read plugin.json: %v", err)
	}
	var m struct {
		Name        string `json:"name"`
		MCPServers  string `json:"mcpServers"`
		Skills      string `json:"skills"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("plugin.json is not valid JSON: %v", err)
	}
	if m.Name != "hebb" {
		t.Errorf("plugin name = %q, want hebb", m.Name)
	}
	if m.MCPServers == "" || m.Skills == "" {
		t.Errorf("plugin.json should declare mcpServers and skills, got %+v", m)
	}
}

func TestPluginMCPConfig(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("plugin", ".mcp.json"))
	if err != nil {
		t.Fatalf("read .mcp.json: %v", err)
	}
	var cfg struct {
		MCPServers map[string]struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatalf(".mcp.json is not valid JSON: %v", err)
	}
	srv, ok := cfg.MCPServers["hebb"]
	if !ok {
		t.Fatalf("plugin .mcp.json missing the hebb server")
	}
	if srv.Command != "hebb" {
		t.Errorf("command = %q, want hebb", srv.Command)
	}
	if len(srv.Args) != 1 || srv.Args[0] != "mcp" {
		t.Errorf("args = %v, want [mcp]", srv.Args)
	}
	// The vault is resolved from the opened project via this env var.
	if srv.Env["HEBB_VAULT"] != "${CLAUDE_PROJECT_DIR}" {
		t.Errorf("env HEBB_VAULT = %q, want ${CLAUDE_PROJECT_DIR}", srv.Env["HEBB_VAULT"])
	}
}

func TestPluginShipsSkill(t *testing.T) {
	if _, err := os.Stat(filepath.Join("plugin", "skills", "vault-ingest", "SKILL.md")); err != nil {
		t.Errorf("plugin should ship the vault-ingest skill: %v", err)
	}
}
