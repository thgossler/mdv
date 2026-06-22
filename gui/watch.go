package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/thgossler/mdv/internal/core"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// Watcher watches both the currently displayed document (emitting a debounced
// "file:changed" event so the frontend can live-reload it) and the workspace
// document tree (emitting a debounced "workspace:changed" event so the
// navigator stays in sync when markdown files or folders are added, removed or
// renamed on disk).
type Watcher struct {
	app *application.App
	fsw *fsnotify.Watcher
	cfg core.Defaults

	mu      sync.Mutex
	current string      // active document path (drives file:changed)
	timer   *time.Timer // file:changed debounce

	wsRoot  string          // workspace root (drives workspace:changed)
	wsTimer *time.Timer     // workspace:changed debounce
	wsDirs  map[string]bool // directories belonging to the workspace tree
	watched map[string]bool // every directory currently added to fsw
}

// NewWatcher starts a filesystem watcher bound to the given application. It
// returns nil (and the GUI continues without live reload) if a watcher cannot
// be created.
func NewWatcher(app *application.App, cfg core.Defaults) *Watcher {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil
	}
	w := &Watcher{
		app:     app,
		fsw:     fsw,
		cfg:     cfg,
		wsDirs:  make(map[string]bool),
		watched: make(map[string]bool),
	}
	go w.loop()
	return w
}

// addDir adds dir to the underlying fsnotify watcher unless it is already
// watched. The caller must hold w.mu.
func (w *Watcher) addDir(dir string) {
	if dir == "" || w.watched[dir] {
		return
	}
	if err := w.fsw.Add(dir); err == nil {
		w.watched[dir] = true
	}
}

// removeDir stops watching dir if it was previously added. The caller must hold
// w.mu.
func (w *Watcher) removeDir(dir string) {
	if dir == "" || !w.watched[dir] {
		return
	}
	_ = w.fsw.Remove(dir)
	delete(w.watched, dir)
}

// Watch switches the active-document watch to the directory containing path and
// tracks that file for changes. The directory is kept watched even when it is
// not part of the workspace tree (e.g. a document opened via a link outside the
// root), and the previously active directory is released only when nothing else
// needs it.
func (w *Watcher) Watch(path string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	prevDir := ""
	if w.current != "" {
		prevDir = filepath.Dir(w.current)
	}
	w.current = path
	dir := filepath.Dir(path)
	if prevDir != "" && prevDir != dir && !w.wsDirs[prevDir] {
		w.removeDir(prevDir)
	}
	w.addDir(dir)
}

// WatchWorkspace (re)arms the recursive watch on the workspace document tree
// rooted at root, so structural changes anywhere in the tree refresh the
// navigator. Passing an empty root disables workspace watching.
func (w *Watcher) WatchWorkspace(root string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	// Release the previous tree, keeping the active document's directory.
	curDir := ""
	if w.current != "" {
		curDir = filepath.Dir(w.current)
	}
	for dir := range w.wsDirs {
		if dir != curDir {
			w.removeDir(dir)
		}
		delete(w.wsDirs, dir)
	}

	w.wsRoot = root
	if root == "" {
		return
	}
	for _, dir := range core.WorkspaceDirs(root) {
		w.wsDirs[dir] = true
		w.addDir(dir)
	}
}

func (w *Watcher) loop() {
	for {
		select {
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			w.handleEvent(ev)
		case _, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
		}
	}
}

func (w *Watcher) handleEvent(ev fsnotify.Event) {
	w.mu.Lock()
	cur := w.current
	wsActive := w.wsRoot != ""
	w.mu.Unlock()

	// Active document change → live reload.
	if cur != "" && sameFile(ev.Name, cur) && ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0 {
		w.debouncedFileEmit(cur)
	}

	if !wsActive {
		return
	}

	// A new directory appeared inside the tree: start watching its subtree so
	// documents created deeper down are noticed too.
	if ev.Op&fsnotify.Create != 0 && isDir(ev.Name) {
		if !core.IsSkippedDir(filepath.Base(ev.Name)) {
			w.addWorkspaceTree(ev.Name)
			w.debouncedWorkspaceEmit()
		}
		return
	}

	// A watched directory was removed or renamed away: stop watching it and its
	// descendants.
	if ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
		w.mu.Lock()
		known := w.wsDirs[ev.Name]
		w.mu.Unlock()
		if known {
			w.dropWorkspaceTree(ev.Name)
			w.debouncedWorkspaceEmit()
			return
		}
	}

	// A markdown document was added, removed or renamed → navigator changed.
	if ev.Op&(fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0 && core.IsMarkdownPath(ev.Name, w.cfg) {
		w.debouncedWorkspaceEmit()
	}
}

// addWorkspaceTree adds root and all of its watchable subdirectories to the
// workspace watch set.
func (w *Watcher) addWorkspaceTree(root string) {
	dirs := core.WorkspaceDirs(root)
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, d := range dirs {
		w.wsDirs[d] = true
		w.addDir(d)
	}
}

// dropWorkspaceTree removes root and any of its descendants from the workspace
// watch set, keeping the active document's directory watched.
func (w *Watcher) dropWorkspaceTree(root string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	curDir := ""
	if w.current != "" {
		curDir = filepath.Dir(w.current)
	}
	prefix := root + string(filepath.Separator)
	for d := range w.wsDirs {
		if d == root || strings.HasPrefix(d, prefix) {
			if d != curDir {
				w.removeDir(d)
			}
			delete(w.wsDirs, d)
		}
	}
}

func (w *Watcher) debouncedFileEmit(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timer != nil {
		w.timer.Stop()
	}
	w.timer = time.AfterFunc(150*time.Millisecond, func() {
		w.app.Event.Emit("file:changed", path)
	})
}

func (w *Watcher) debouncedWorkspaceEmit() {
	w.mu.Lock()
	defer w.mu.Unlock()
	root := w.wsRoot
	if w.wsTimer != nil {
		w.wsTimer.Stop()
	}
	w.wsTimer = time.AfterFunc(200*time.Millisecond, func() {
		w.app.Event.Emit("workspace:changed", root)
	})
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

func sameFile(a, b string) bool {
	ca, _ := filepath.Abs(a)
	cb, _ := filepath.Abs(b)
	return filepath.Clean(ca) == filepath.Clean(cb)
}
