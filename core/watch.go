package core

import (
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher incrementally updates the index as markdown files change.
type Watcher struct {
	w    *fsnotify.Watcher
	done chan struct{}
}

// Watch starts watching the vault tree, reindexing single files on change and
// removing them on delete. Close stops it.
func Watch(cfg Config, db *sql.DB) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	excluded := map[string]bool{}
	for _, d := range cfg.ExcludeDirs {
		excluded[d] = true
	}
	_ = filepath.WalkDir(cfg.VaultPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if excluded[d.Name()] {
				return filepath.SkipDir
			}
			_ = fw.Add(p)
		}
		return nil
	})
	wt := &Watcher{w: fw, done: make(chan struct{})}
	go wt.loop(cfg, db, excluded)
	return wt, nil
}

func (wt *Watcher) loop(cfg Config, db *sql.DB, excluded map[string]bool) {
	debounce := map[string]*time.Timer{}
	var mu sync.Mutex
	relOf := func(p string) string {
		r, err := filepath.Rel(cfg.VaultPath, p)
		if err != nil {
			return ""
		}
		return filepath.ToSlash(r)
	}
	for {
		select {
		case <-wt.done:
			return
		case ev, ok := <-wt.w.Events:
			if !ok {
				return
			}
			// A newly created directory needs its own watch.
			if ev.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
					if !excluded[filepath.Base(ev.Name)] {
						_ = wt.w.Add(ev.Name)
					}
					continue
				}
			}
			if !strings.HasSuffix(ev.Name, ".md") {
				continue
			}
			rel := relOf(ev.Name)
			if rel == "" {
				continue
			}
			if ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
				_ = RemoveFile(db, rel)
				continue
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				mu.Lock()
				if t := debounce[rel]; t != nil {
					t.Stop()
				}
				r := rel
				debounce[r] = time.AfterFunc(400*time.Millisecond, func() {
					// A single malformed file must not crash the watcher.
					defer func() { _ = recover() }()
					_ = IndexFile(cfg, db, r)
				})
				mu.Unlock()
			}
		case _, ok := <-wt.w.Errors:
			if !ok {
				return
			}
		}
	}
}

// Close stops the watcher.
func (wt *Watcher) Close() {
	close(wt.done)
	_ = wt.w.Close()
}
