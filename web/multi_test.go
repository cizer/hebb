package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/cizer/hebb/core"
)

func makeVault(t *testing.T, note string) core.Config {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "n.md"), []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := core.ResolveVault(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func statsVaultName(t *testing.T, srv *httptest.Server, cookie *http.Cookie) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/stats", nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var s struct {
		VaultName string `json:"vaultName"`
	}
	json.NewDecoder(resp.Body).Decode(&s)
	return s.VaultName
}

func TestServeMultiSwitchesVaults(t *testing.T) {
	targets := []VaultTarget{
		{Slug: "alpha", Name: "Alpha", Cfg: makeVault(t, "# A\nalpha note")},
		{Slug: "beta", Name: "Beta", Cfg: makeVault(t, "# B\nbeta note")},
	}
	m := newMultiplexer(targets)
	defer m.closeAll()
	srv := httptest.NewServer(m)
	defer srv.Close()

	// /api/vaults lists both, default active is the first.
	resp, err := http.Get(srv.URL + "/api/vaults")
	if err != nil {
		t.Fatal(err)
	}
	var vs struct {
		Active string `json:"active"`
		Vaults []struct{ Slug, Name string }
	}
	json.NewDecoder(resp.Body).Decode(&vs)
	resp.Body.Close()
	if len(vs.Vaults) != 2 || vs.Active != "alpha" {
		t.Fatalf("/api/vaults = %+v, want 2 vaults, active alpha", vs)
	}

	// No cookie: default vault (Alpha).
	if got := statsVaultName(t, srv, nil); got != "Alpha" {
		t.Errorf("default stats vaultName = %q, want Alpha", got)
	}

	// ?vault=beta sets the cookie; following the cookie serves Beta.
	resp, err = http.Get(srv.URL + "/?vault=beta")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == vaultCookie {
			cookie = c
		}
	}
	if cookie == nil || cookie.Value != "beta" {
		t.Fatalf("expected hebb_vault=beta cookie, got %+v", resp.Cookies())
	}
	if got := statsVaultName(t, srv, cookie); got != "Beta" {
		t.Errorf("with beta cookie, stats vaultName = %q, want Beta", got)
	}
}

func TestServeMultiInvalidVaultFallsBackToDefault(t *testing.T) {
	targets := []VaultTarget{{Slug: "alpha", Name: "Alpha", Cfg: makeVault(t, "# A\nx")}}
	m := newMultiplexer(targets)
	defer m.closeAll()
	srv := httptest.NewServer(m)
	defer srv.Close()

	// A bogus cookie value is ignored; the default vault answers.
	if got := statsVaultName(t, srv, &http.Cookie{Name: vaultCookie, Value: "ghost"}); got != "Alpha" {
		t.Errorf("invalid cookie should fall back to default, got %q", got)
	}
}
