package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRenderMCPJSON(t *testing.T) {
	b, err := RenderMCPJSON("hebb", "hebb")
	if err != nil {
		t.Fatalf("RenderMCPJSON: %v", err)
	}
	if b[len(b)-1] != '\n' {
		t.Error("want trailing newline")
	}
	var parsed struct {
		MCPServers map[string]struct {
			Type    string   `json:"type"`
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	srv, ok := parsed.MCPServers["hebb"]
	if !ok {
		t.Fatalf("missing server %q in %s", "hebb", b)
	}
	if srv.Command != "hebb" {
		t.Errorf("command = %q, want hebb", srv.Command)
	}
	if srv.Type != "stdio" {
		t.Errorf("type = %q, want stdio", srv.Type)
	}
	if len(srv.Args) != 1 || srv.Args[0] != "mcp" {
		t.Errorf("args = %v, want [mcp] (portable: no machine-specific --vault)", srv.Args)
	}
}

func TestWriteMCPJSONIdempotent(t *testing.T) {
	vault := t.TempDir()
	changed, err := WriteMCPJSON(vault, "hebb", "hebb")
	if err != nil {
		t.Fatalf("WriteMCPJSON: %v", err)
	}
	if !changed {
		t.Error("first write should report changed=true")
	}
	path := filepath.Join(vault, ".mcp.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf(".mcp.json not at vault root: %v", err)
	}
	changed, err = WriteMCPJSON(vault, "hebb", "hebb")
	if err != nil {
		t.Fatalf("WriteMCPJSON (2nd): %v", err)
	}
	if changed {
		t.Error("identical re-write should report changed=false (idempotent)")
	}
}

func TestWriteMCPJSONOverwritesOnChange(t *testing.T) {
	vault := t.TempDir()
	if _, err := WriteMCPJSON(vault, "hebb", "hebb"); err != nil {
		t.Fatal(err)
	}
	changed, err := WriteMCPJSON(vault, "hebb", "/usr/local/bin/hebb")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("changed command should rewrite (changed=true)")
	}
}
