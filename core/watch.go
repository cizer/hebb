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

// WatcherHealth is a point-in-time snapshot of a watcher's liveness, exposed so
// surfaces can report whether incremental indexing is actually running rather
// than assuming it. Alive is false once the loop has exited (channel closed or
// Close called) or when the watcher never started; StartErr carries the reason
// in the never-started case. Errors counts fsnotify error-channel events, which
// were previously drained and discarded, hiding silent watcher death.
type WatcherHealth struct {
	Alive       bool
	LastEventAt time.Time
	Errors      int
	StartErr    string
}

// FailedWatcherHealth is the health snapshot for a watcher that never started.
// Surfaces that discard the Watch startup error record this so vault_stats and
// doctor can still report that incremental indexing is not running.
func FailedWatcherHealth(err error) WatcherHealth {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return WatcherHealth{Alive: false, StartErr: msg}
}

// Watcher incrementally updates the index as markdown files change.
type Watcher struct {
	w    *fsnotify.Watcher
	done chan struct{}

	// health is read by surface handlers (vault_stats) concurrently with the
	// watcher loop writing it, so every access goes through mu.
	mu     sync.Mutex
	health WatcherHealth
}

// Health returns a snapshot of the watcher's liveness, safe to call from any
// goroutine.
func (wt *Watcher) Health() WatcherHealth {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	return wt.health
}

func (wt *Watcher) markEvent() {
	wt.mu.Lock()
	wt.health.LastEventAt = time.Now()
	wt.mu.Unlock()
}

func (wt *Watcher) markError() {
	wt.mu.Lock()
	wt.health.Errors++
	wt.mu.Unlock()
}

func (wt *Watcher) markStopped() {
	wt.mu.Lock()
	wt.health.Alive = false
	wt.mu.Unlock()
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
	// Git mode: pull before work so the session starts from the latest remote
	// state. Best-effort and non-blocking to the watcher; a conflict surfaces
	// only via `hebb sync` (auto-commit/push happens on the debounce below).
	if cfg.Git.PullEnabled() && IsGitRepo(cfg.VaultPath) {
		_ = GitPull(cfg.VaultPath)
	}

	wt := &Watcher{w: fw, done: make(chan struct{}), health: WatcherHealth{Alive: true}}
	go wt.loop(cfg, db, excluded)
	return wt, nil
}

func (wt *Watcher) loop(cfg Config, db *sql.DB, excluded map[string]bool) {
	// Whatever exits this loop (Close, a closed channel), the watcher is no longer
	// keeping the index fresh, so report it dead. Surfaces read this to warn.
	defer wt.markStopped()
	debounce := map[string]*time.Timer{}
	var mu sync.Mutex
	relOf := func(p string) string {
		r, err := filepath.Rel(cfg.VaultPath, p)
		if err != nil {
			return ""
		}
		return filepath.ToSlash(r)
	}

	// Git mode: after content settles (a quiet period with no further changes),
	// commit and push the whole burst as one coherent commit. A single shared
	// timer, reset on every content event, debounces the burst. Best-effort:
	// errors (e.g. a conflict on pull) are left for `hebb sync` to surface.
	gitOn := cfg.Git.PushEnabled() && IsGitRepo(cfg.VaultPath)
	var gitTimer *time.Timer
	scheduleGitSync := func() {
		if !gitOn {
			return
		}
		mu.Lock()
		if gitTimer != nil {
			gitTimer.Stop()
		}
		gitTimer = time.AfterFunc(time.Duration(cfg.Git.Debounce())*time.Second, func() {
			defer func() { _ = recover() }()
			_, _ = GitSync(cfg.VaultPath, SyncOptions{
				Commit:  true,
				Pull:    cfg.Git.PullEnabled(),
				Push:    true,
				Message: cfg.Git.Message(),
			})
		})
		mu.Unlock()
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
			wt.markEvent()
			if ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
				_ = RemoveFile(db, rel)
				scheduleGitSync()
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
				scheduleGitSync()
			}
		case _, ok := <-wt.w.Errors:
			if !ok {
				return
			}
			// Count fsnotify errors rather than discarding them. A rising count is
			// the only signal that the watcher is struggling (e.g. inotify watch
			// limit), which surfaces report through WatcherHealth.
			wt.markError()
		}
	}
}

// Close stops the watcher.
func (wt *Watcher) Close() {
	close(wt.done)
	_ = wt.w.Close()
}
