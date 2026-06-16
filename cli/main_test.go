package cli

import (
	"os"
	"testing"
)

// TestMain points XDG_CONFIG_HOME at a throwaway directory for the whole
// package. Tests that run `hebb install`/`hebb new` register the vault in the
// machine-global registry (under $XDG_CONFIG_HOME); this keeps that write off
// the developer's real registry.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "hebb-cli-xdg-*")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CONFIG_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
