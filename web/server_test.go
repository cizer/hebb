package web

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// buildHealthFixtureVault creates a minimal temp vault with:
//   - a note containing a dangling wiki-link (yields a dangling_link finding), and
//   - a note in 2-Areas/ with no links and a very old mtime (yields an orphan finding).
//
// These two fixtures guarantee that the /api/health response always contains at
// least one finding so the test can assert on a non-empty worklist.
func buildHealthFixtureVault(t *testing.T) (core.Config, *sql.DB) {
	t.Helper()
	vault := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(vault, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// A note with a dangling wiki-link (target never created).
	write("Notes/Dangler.md", "# Dangler\n\nSee [[MissingTarget]] for details.\n")

	// A note in a connective folder with no links: qualifies as an orphan once
	// its mtime is set to well beyond the default stale threshold (90 days).
	write("2-Areas/Orphan.md", "# Orphan\n\nNo links here.\n")

	cfg := core.Config{
		VaultPath:   vault,
		DBPath:      filepath.Join(vault, ".hebb", "index.db"),
		ExcludeDirs: []string{".hebb", ".git", ".obsidian"},
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := core.OpenDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}

	// Age the orphan note to 200 days ago so it exceeds the default 90-day
	// orphan-stale threshold and the detector reports it.
	orphanPath := filepath.Join(vault, "2-Areas/Orphan.md")
	oldTime := time.Now().AddDate(0, 0, -200)
	if err := os.Chtimes(orphanPath, oldTime, oldTime); err != nil {
		db.Close()
		t.Fatal(err)
	}

	if _, err := core.FullReindex(cfg, db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	return cfg, db
}

// TestHealthAPI asserts that GET /api/health returns 200 with a JSON body that
// contains both "findings" and "stats" keys, and that the fixture vault produces
// at least one finding. The dashboard suppresses unresolved future-note links
// (reportUnresolved=false, like the CLI default), so the unresolved
// [[MissingTarget]] link is intentionally absent from the worklist; the aged
// orphan note is what surfaces here.
func TestHealthAPI(t *testing.T) {
	cfg, db := buildHealthFixtureVault(t)
	defer db.Close()

	srv := httptest.NewServer(newMux(cfg, db, "HealthVault"))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/health status = %d, want 200", resp.StatusCode)
	}

	var body map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := body["findings"]; !ok {
		t.Error("response missing 'findings' key")
	}
	if _, ok := body["stats"]; !ok {
		t.Error("response missing 'stats' key")
	}

	// Decode findings into a slice so we can assert non-empty.
	var findings []map[string]string
	if err := json.Unmarshal(body["findings"], &findings); err != nil {
		t.Fatalf("unmarshal findings: %v", err)
	}
	if len(findings) == 0 {
		t.Error("findings is empty; fixture vault should produce at least one finding")
	}

	// The aged orphan note must surface as an orphan finding. The unresolved
	// [[MissingTarget]] link is suppressed by default (reportUnresolved=false),
	// so the worklist is carried by the orphan, not a dangling_link.
	found := false
	for _, f := range findings {
		if f["type"] == "orphan" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no orphan finding in response; got: %v", findings)
	}
}

// TestHealthAPIStatsShape asserts that the stats object in GET /api/health has
// the expected top-level integer fields (node_count, edge_count, etc.).
func TestHealthAPIStatsShape(t *testing.T) {
	cfg, db := buildHealthFixtureVault(t)
	defer db.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Host = "127.0.0.1"
	rec := httptest.NewRecorder()
	newMux(cfg, db, "HealthVault").ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var envelope struct {
		Stats struct {
			NodeCount      int     `json:"node_count"`
			EdgeCount      int     `json:"edge_count"`
			ComponentCount int     `json:"component_count"`
			GiantRatio     float64 `json:"giant_ratio"`
			MaxCore        int     `json:"max_core"`
		} `json:"stats"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if envelope.Stats.NodeCount == 0 {
		t.Error("stats.node_count is 0; fixture vault has notes")
	}
	if envelope.Stats.ComponentCount == 0 {
		t.Error("stats.component_count is 0; fixture vault has notes so must have at least one component")
	}
}

// TestHealthAPIHostGuard asserts that GET /api/health is rejected for a foreign
// Host header, matching the DNS-rebinding defence on the other endpoints.
func TestHealthAPIHostGuard(t *testing.T) {
	cfg, db := buildHealthFixtureVault(t)
	defer db.Close()

	h := newMux(cfg, db, "HealthVault")

	cases := []struct {
		host string
		want int
	}{
		{"127.0.0.1:5432", http.StatusOK},
		{"localhost:5432", http.StatusOK},
		{"evil.example.com", http.StatusForbidden},
		{"attacker.test:5432", http.StatusForbidden},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		req.Host = c.host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("Host %q -> %d, want %d", c.host, rec.Code, c.want)
		}
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

func TestObsidianURIEncodesSpacesNotPlus(t *testing.T) {
	got := obsidianURI("1-Projects/Aurora Overview.md", "My Vault")
	if strings.Contains(got, "+") {
		t.Errorf("obsidian URI must not use '+' for spaces (breaks the obsidian:// handler): %s", got)
	}
	for _, want := range []string{"vault=My%20Vault", "file=1-Projects%2FAurora%20Overview"} {
		if !strings.Contains(got, want) {
			t.Errorf("obsidian URI %q missing %q", got, want)
		}
	}
}
