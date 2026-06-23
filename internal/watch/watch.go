// Package watch provides a filesystem watcher shared by the GUI and terminal
// front-ends. It tracks the currently displayed document (emitting a debounced
// FileChanged event when its contents change) and the workspace document tree
// (emitting a debounced WorkspaceChanged event when markdown files or folders
// are added, removed or renamed), delivering both through a single emit
// callback so each front-end can route them into its own event system.
package watch

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/thgossler/mdv/internal/core"
)

// Kind classifies a watch event.
type Kind int

const (
	// FileChanged means the active document's contents changed on disk.
	FileChanged Kind = iota
	// WorkspaceChanged means the workspace tree gained, lost or renamed a
	// markdown file or folder.
	WorkspaceChanged
)

// Event is delivered to the emit callback when a watched change is detected.
// Path is the affected document for FileChanged and the workspace root for
// WorkspaceChanged.
type Event struct {
	Kind Kind
	Path string
}

// Watcher watches the active document and the workspace tree. The zero value is
// not usable; construct one with New.
type Watcher struct {
	fsw  *fsnotify.Watcher
	cfg  core.Defaults
	emit func(Event)

	mu      sync.Mutex
	current string      // active document path (drives FileChanged)
	timer   *time.Timer // FileChanged debounce

	wsRoot  string          // workspace root (drives WorkspaceChanged)
	wsTimer *time.Timer     // WorkspaceChanged debounce
	wsDirs  map[string]bool // directories belonging to the workspace tree
	watched map[string]bool // every directory currently added to fsw
}

// New starts a watcher that reports changes through emit. It returns nil (so the
// caller continues without live reload) when a filesystem watcher cannot be
// created. emit may be called from a background goroutine.
func New(cfg core.Defaults, emit func(Event)) *Watcher {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil
	}
	w := &Watcher{
		fsw:     fsw,
		cfg:     cfg,
		emit:    emit,
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
// rooted at root, so structural changes anywhere in the tree are reported.
// Passing an empty root disables workspace watching.
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

// Close releases the underlying filesystem watcher.
func (w *Watcher) Close() {
	if w == nil {
		return
	}
	_ = w.fsw.Close()
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
		w.emit(Event{Kind: FileChanged, Path: path})
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
		w.emit(Event{Kind: WorkspaceChanged, Path: root})
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
