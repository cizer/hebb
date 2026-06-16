package web

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/cizer/hebb/core"
)

// VaultTarget is one vault the multi-vault server can serve. Slug is the stable
// id used in the picker and the hebb_vault cookie; Name is for display.
type VaultTarget struct {
	Slug string
	Name string
	Cfg  core.Config
}

// ServeMulti serves the search/health UI for several vaults on one loopback
// port, switching between them by a hebb_vault cookie. Per-vault resources (the
// index db, file watcher and the existing per-vault mux) are opened lazily on
// first access, so you pay only for vaults you actually open and each stays
// independent. One server on one port removes the per-vault port collision.
func ServeMulti(targets []VaultTarget, port int) error {
	if len(targets) == 0 {
		return fmt.Errorf("no vaults to serve")
	}
	m := newMultiplexer(targets)
	defer m.closeAll()
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	fmt.Printf("hebb serve  ->  http://%s  (%d vault(s))\n", addr, len(m.order))
	return http.ListenAndServe(addr, guardLocalhost(m))
}

const vaultCookie = "hebb_vault"

type vaultRuntime struct {
	db      *sql.DB
	watcher *core.Watcher
	handler http.Handler
}

// multiplexer routes each request to the active vault's mux, opening that
// vault's runtime lazily. It is safe for concurrent use.
type multiplexer struct {
	order   []VaultTarget // display/default order; order[0] is the default vault
	bySlug  map[string]VaultTarget
	mu      sync.Mutex
	runtime map[string]*vaultRuntime
}

func newMultiplexer(targets []VaultTarget) *multiplexer {
	m := &multiplexer{bySlug: map[string]VaultTarget{}, runtime: map[string]*vaultRuntime{}}
	for _, t := range targets {
		if _, dup := m.bySlug[t.Slug]; dup {
			continue // first spelling of a slug wins
		}
		m.order = append(m.order, t)
		m.bySlug[t.Slug] = t
	}
	return m
}

func (m *multiplexer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/vaults" {
		m.writeVaults(w, r)
		return
	}
	rt, err := m.runtimeFor(m.activeSlug(w, r))
	if err != nil {
		http.Error(w, "vault unavailable: "+err.Error(), http.StatusInternalServerError)
		return
	}
	rt.handler.ServeHTTP(w, r)
}

// activeSlug picks the vault for this request: an explicit, valid ?vault= (which
// it also pins via the cookie), else a valid existing cookie, else the default
// (first) vault.
func (m *multiplexer) activeSlug(w http.ResponseWriter, r *http.Request) string {
	if q := r.URL.Query().Get("vault"); q != "" {
		if _, ok := m.bySlug[q]; ok {
			http.SetCookie(w, &http.Cookie{Name: vaultCookie, Value: q, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			return q
		}
	}
	if c, err := r.Cookie(vaultCookie); err == nil {
		if _, ok := m.bySlug[c.Value]; ok {
			return c.Value
		}
	}
	return m.order[0].Slug
}

// writeVaults returns the picker payload: the vault list and the active slug.
func (m *multiplexer) writeVaults(w http.ResponseWriter, r *http.Request) {
	type entry struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	}
	list := make([]entry, 0, len(m.order))
	for _, t := range m.order {
		list = append(list, entry{Slug: t.Slug, Name: t.Name})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"active": m.activeSlug(w, r), "vaults": list})
}

// runtimeFor returns the (lazily opened) runtime for a vault slug, building the
// index, starting a watcher, and wiring the per-vault mux on first use.
func (m *multiplexer) runtimeFor(slug string) (*vaultRuntime, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rt, ok := m.runtime[slug]; ok {
		return rt, nil
	}
	t, ok := m.bySlug[slug]
	if !ok {
		return nil, fmt.Errorf("unknown vault %q", slug)
	}
	db, err := core.OpenDB(t.Cfg.DBPath)
	if err != nil {
		return nil, err
	}
	if _, err := core.FullReindex(t.Cfg, db); err != nil {
		db.Close()
		return nil, err
	}
	rt := &vaultRuntime{db: db, handler: newMux(t.Cfg, db, t.Name)}
	// A failed watcher is non-fatal: the UI still serves and rebuilds on demand.
	if wch, werr := core.Watch(t.Cfg, db); werr == nil {
		rt.watcher = wch
	}
	m.runtime[slug] = rt
	return rt, nil
}

func (m *multiplexer) closeAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, rt := range m.runtime {
		if rt.watcher != nil {
			rt.watcher.Close()
		}
		rt.db.Close()
	}
}
