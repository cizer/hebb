package web

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cizer/hebb/core"
)

//go:embed index.html
var indexHTML []byte

// Serve indexes the vault, starts a watcher to keep it fresh, and serves the
// search UI on 127.0.0.1 only (the vault holds personal data, never bind wide).
func Serve(cfg core.Config, port int, vaultName string) error {
	db, err := core.OpenDB(cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := core.FullReindex(cfg, db); err != nil {
		return err
	}
	if w, werr := core.Watch(cfg, db); werr == nil {
		defer w.Close()
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	fmt.Printf("hebb search  ->  http://%s\nvault: %s  ·  db: %s\n", addr, vaultName, cfg.DBPath)
	return http.ListenAndServe(addr, newMux(cfg, db, vaultName))
}

func newMux(cfg core.Config, db *sql.DB, vaultName string) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})

	mux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if len([]rune(q)) < 2 {
			writeJSON(w, http.StatusOK, searchResp{Query: q, Results: []resultItem{}})
			return
		}
		limit := clampInt(atoiOr(r.URL.Query().Get("limit"), 20), 1, 50)
		t0 := time.Now()
		rows, err := core.Search(db, q, limit, r.URL.Query().Get("tag"), r.URL.Query().Get("path_prefix"))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		ms := float64(time.Since(t0).Microseconds()) / 1000.0
		items := make([]resultItem, 0, len(rows))
		for _, r0 := range rows {
			items = append(items, resultItem{
				Title:    r0.Title,
				Path:     r0.Path,
				Snippet:  r0.Snippet,
				Tags:     strings.Fields(r0.Tags),
				Obsidian: obsidianURI(r0.Path, vaultName),
			})
		}
		writeJSON(w, http.StatusOK, searchResp{Query: q, Count: len(items), Ms: round1(ms), Results: items})
	})

	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		notes, links, _, err := core.Stats(db)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, statsResp{NoteCount: notes, LinkCount: links, VaultName: vaultName})
	})

	mux.HandleFunc("/api/reindex", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		res, err := core.FullReindex(cfg, db)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"indexed": res.Indexed})
	})

	return mux
}

type resultItem struct {
	Title    string   `json:"title"`
	Path     string   `json:"path"`
	Snippet  string   `json:"snippet"`
	Tags     []string `json:"tags"`
	Obsidian string   `json:"obsidian"`
}

type searchResp struct {
	Query   string       `json:"query"`
	Count   int          `json:"count"`
	Ms      float64      `json:"ms"`
	Results []resultItem `json:"results"`
}

type statsResp struct {
	NoteCount int    `json:"noteCount"`
	LinkCount int    `json:"linkCount"`
	VaultName string `json:"vaultName"`
}

func obsidianURI(path, vaultName string) string {
	file := strings.TrimSuffix(path, ".md")
	return "obsidian://open?vault=" + url.QueryEscape(vaultName) + "&file=" + url.QueryEscape(file)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func atoiOr(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

func clampInt(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

func round1(f float64) float64 {
	return float64(int(f*10+0.5)) / 10
}
