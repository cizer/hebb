package hebb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestMarketplaceValid checks the root .claude-plugin/marketplace.json that lets
// users install the plugin via `/plugin marketplace add cizer/hebb` (or a local
// dir) instead of passing --plugin-dir each session. It must list the hebb
// plugin and point at a source dir that actually holds a plugin manifest.
func TestMarketplaceValid(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(".claude-plugin", "marketplace.json"))
	if err != nil {
		t.Fatalf("read marketplace.json: %v", err)
	}
	var m struct {
		Name  string `json:"name"`
		Owner struct {
			Name string `json:"name"`
		} `json:"owner"`
		Plugins []struct {
			Name   string `json:"name"`
			Source string `json:"source"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("marketplace.json is not valid JSON: %v", err)
	}
	if m.Name == "" || m.Owner.Name == "" {
		t.Errorf("marketplace needs a name and owner.name, got %+v", m)
	}
	var hebb *struct {
		Name   string `json:"name"`
		Source string `json:"source"`
	}
	for i := range m.Plugins {
		if m.Plugins[i].Name == "hebb" {
			hebb = &m.Plugins[i]
		}
	}
	if hebb == nil {
		t.Fatalf("marketplace does not list the hebb plugin: %+v", m.Plugins)
	}
	// The source must resolve to a directory carrying a plugin manifest, or the
	// install would 404.
	manifest := filepath.Join(filepath.FromSlash(hebb.Source), ".claude-plugin", "plugin.json")
	if _, err := os.Stat(manifest); err != nil {
		t.Errorf("plugin source %q has no plugin.json (%v)", hebb.Source, manifest)
	}
}
