package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func loadSettings(t *testing.T, vault string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(vault, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("settings is not valid JSON: %v", err)
	}
	return m
}

func contains(arr any, val string) bool {
	xs, ok := arr.([]any)
	if !ok {
		return false
	}
	for _, x := range xs {
		if s, ok := x.(string); ok && s == val {
			return true
		}
	}
	return false
}

func TestWriteProjectSettingsFresh(t *testing.T) {
	vault := t.TempDir()
	changed, err := WriteProjectSettings(vault, "hebb")
	if err != nil {
		t.Fatalf("WriteProjectSettings: %v", err)
	}
	if !changed {
		t.Error("fresh write should report changed=true")
	}
	m := loadSettings(t, vault)
	if !contains(m["enabledMcpjsonServers"], "hebb") {
		t.Errorf("enabledMcpjsonServers missing hebb: %v", m["enabledMcpjsonServers"])
	}
	perm, _ := m["permissions"].(map[string]any)
	if perm == nil {
		t.Fatal("missing permissions object")
	}
	for _, tool := range MCPToolNames() {
		want := "mcp__hebb__" + tool
		if !contains(perm["allow"], want) {
			t.Errorf("permissions.allow missing %q", want)
		}
	}
}

func TestWriteProjectSettingsIdempotent(t *testing.T) {
	vault := t.TempDir()
	if _, err := WriteProjectSettings(vault, "hebb"); err != nil {
		t.Fatal(err)
	}
	changed, err := WriteProjectSettings(vault, "hebb")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("re-run should report changed=false (idempotent)")
	}
}

func TestWriteProjectSettingsPreservesExisting(t *testing.T) {
	vault := t.TempDir()
	dir := filepath.Join(vault, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `{"theme":"dark","permissions":{"allow":["Bash(ls)"]}}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := WriteProjectSettings(vault, "hebb")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("merging into existing should report changed=true")
	}
	m := loadSettings(t, vault)
	if m["theme"] != "dark" {
		t.Errorf("unrelated key 'theme' was lost: %v", m["theme"])
	}
	perm, _ := m["permissions"].(map[string]any)
	if !contains(perm["allow"], "Bash(ls)") {
		t.Error("existing permission Bash(ls) was lost")
	}
	if !contains(perm["allow"], "mcp__hebb__search_vault") {
		t.Error("hebb permission was not added alongside existing")
	}
}
