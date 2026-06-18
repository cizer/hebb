package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cizer/hebb/core"
)

func TestVaultsCommandLists(t *testing.T) {
	home := t.TempDir()
	v := t.TempDir()
	if err := os.MkdirAll(filepath.Join(v, ".hebb"), 0o755); err != nil {
		t.Fatal(err)
	}
	// RegistryPath honours $XDG_CONFIG_HOME (pinned to a throwaway by TestMain),
	// so registering and listing use the same file regardless of --home.
	if err := core.RegisterVault(core.RegistryPath(home), "Demo", v); err != nil {
		t.Fatal(err)
	}

	root := newRoot("test")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"vaults", "--home", home})
	if err := root.Execute(); err != nil {
		t.Fatalf("vaults: %v\n%s", err, buf.String())
	}
	out := buf.String()
	if !strings.Contains(out, "Demo") || !strings.Contains(out, v) {
		t.Errorf("expected the registered vault listed, got:\n%s", out)
	}
}

func TestRenamedCommandsAndAliases(t *testing.T) {
	root := newRoot("test")
	for _, name := range []string{"audit", "unwire", "vaults"} {
		c, _, err := root.Find([]string{name})
		if err != nil || c.Name() != name {
			t.Errorf("command %q not found (got %v, %v)", name, c, err)
		}
	}
	// The old names keep working as aliases.
	for alias, want := range map[string]string{"health": "audit", "reset": "unwire"} {
		c, _, err := root.Find([]string{alias})
		if err != nil || c.Name() != want {
			t.Errorf("alias %q should resolve to %q, got %v (%v)", alias, want, c.Name(), err)
		}
	}
}
