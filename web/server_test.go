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

// TestHostGuard checks the loopback Host guard: a foreign Host (as sent by a
// page that rebound its domain to 127.0.0.1) is refused, while loopback Hosts
// are served. This is the DNS-rebinding defence on top of the 127.0.0.1 bind.
func TestHostGuard(t *testing.T) {
	cfg, err := core.ResolveVault(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	db, err := core.OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	h := newMux(cfg, db, "TestVault")

	cases := []struct {
		host string
		want int
	}{
		{"127.0.0.1:4321", http.StatusOK},
		{"localhost:4321", http.StatusOK},
		{"[::1]:4321", http.StatusOK},
		{"evil.example.com", http.StatusForbidden},
		{"attacker.test:4321", http.StatusForbidden},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
		req.Host = c.host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("Host %q -> %d, want %d", c.host, rec.Code, c.want)
		}
	}
}
