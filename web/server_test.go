package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cizer/hebb/core"
)

func TestSearchAPI(t *testing.T) {
	vault := t.TempDir()
	if err := os.WriteFile(filepath.Join(vault, "a.md"), []byte("# Alpha\n\nquick brown fox #x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := core.ResolveVault(vault, "")
	if err != nil {
		t.Fatal(err)
	}
	db, err := core.OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := core.FullReindex(cfg, db); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(newMux(cfg, db, "TestVault"))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/search?q=fox")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var sr searchResp
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		t.Fatal(err)
	}
	if sr.Count != 1 || len(sr.Results) != 1 || sr.Results[0].Path != "a.md" {
		t.Fatalf("search = %+v, want 1 hit a.md", sr)
	}
	if !strings.HasPrefix(sr.Results[0].Obsidian, "obsidian://open?vault=TestVault&file=a") {
		t.Fatalf("obsidian uri = %q", sr.Results[0].Obsidian)
	}
}
