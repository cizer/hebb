package core

import (
	"path/filepath"
	"testing"
)

// TestProtectedVaultRoot covers the protected folders, the path-boundary guard
// (a sibling like "Documents-archive" must not match "Documents"), iCloud Drive,
// a safe location, a foreign home, and the empty-home short-circuit.
func TestProtectedVaultRoot(t *testing.T) {
	home := filepath.FromSlash("/Users/x")
	icloud := filepath.Join(home, "Library", "Mobile Documents", "com~apple~CloudDocs")

	cases := []struct {
		name     string
		path     string
		wantRoot string
		wantOK   bool
	}{
		{"documents nested", "/Users/x/Documents/Vault", filepath.Join(home, "Documents"), true},
		{"desktop deep", "/Users/x/Desktop/notes/v", filepath.Join(home, "Desktop"), true},
		{"downloads", "/Users/x/Downloads/v", filepath.Join(home, "Downloads"), true},
		{"icloud drive", "/Users/x/Library/Mobile Documents/com~apple~CloudDocs/v", icloud, true},
		{"exact documents root", "/Users/x/Documents", filepath.Join(home, "Documents"), true},
		{"safe vaults dir", "/Users/x/vaults/v", "", false},
		{"sibling not a match", "/Users/x/Documents-archive/v", "", false},
		{"foreign home", "/Users/y/Documents/v", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, ok := ProtectedVaultRoot(filepath.FromSlash(tc.path), home)
			if ok != tc.wantOK || root != tc.wantRoot {
				t.Errorf("ProtectedVaultRoot(%q) = (%q, %v), want (%q, %v)", tc.path, root, ok, tc.wantRoot, tc.wantOK)
			}
		})
	}

	if _, ok := ProtectedVaultRoot("/Users/x/Documents/v", ""); ok {
		t.Error("empty home must never match")
	}
}
