package main

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// Watcher watches the directory of the currently displayed document and emits a
// "file:changed" event (debounced) so the frontend can live-reload.
type Watcher struct {
	app     *application.App
	fsw     *fsnotify.Watcher
	mu      sync.Mutex
	current string
	timer   *time.Timer
}

// NewWatcher starts a filesystem watcher bound to the given application. It
// returns nil (and the GUI continues without live reload) if a watcher cannot
// be created.
func NewWatcher(app *application.App) *Watcher {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil
	}
	w := &Watcher{app: app, fsw: fsw}
	go w.loop()
	return w
}

// Watch switches the watcher to the directory containing path and tracks that
// file for changes.
func (w *Watcher) Watch(path string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	dir := filepath.Dir(path)
	prevDir := ""
	if w.current != "" {
		prevDir = filepath.Dir(w.current)
	}
	w.current = path
	if dir != prevDir {
		if prevDir != "" {
			_ = w.fsw.Remove(prevDir)
		}
		_ = w.fsw.Add(dir)
	}
}

func (w *Watcher) loop() {
	for {
		select {
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			w.mu.Lock()
			cur := w.current
			w.mu.Unlock()
			if cur == "" {
				continue
			}
			if sameFile(ev.Name, cur) && ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0 {
				w.debouncedEmit(cur)
			}
		case _, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
		}
	}
}

func (w *Watcher) debouncedEmit(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timer != nil {
		w.timer.Stop()
	}
	w.timer = time.AfterFunc(150*time.Millisecond, func() {
		w.app.Event.Emit("file:changed", path)
	})
}

func sameFile(a, b string) bool {
	ca, _ := filepath.Abs(a)
	cb, _ := filepath.Abs(b)
	return filepath.Clean(ca) == filepath.Clean(cb)
}
