package main

import (
	"github.com/thgossler/mdv/internal/core"
	"github.com/thgossler/mdv/internal/watch"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// Watcher adapts the shared filesystem watcher to the GUI, forwarding watch
// events to the frontend as Wails application events: "file:changed" for the
// active document and "workspace:changed" for the document tree.
type Watcher struct {
	w *watch.Watcher
}

// NewWatcher starts a filesystem watcher bound to the given application. It
// returns nil (and the GUI continues without live reload) if a watcher cannot
// be created.
func NewWatcher(app *application.App, cfg core.Defaults) *Watcher {
	w := watch.New(cfg, func(ev watch.Event) {
		switch ev.Kind {
		case watch.FileChanged:
			app.Event.Emit("file:changed", ev.Path)
		case watch.WorkspaceChanged:
			app.Event.Emit("workspace:changed", ev.Path)
		}
	})
	if w == nil {
		return nil
	}
	return &Watcher{w: w}
}

// Watch switches the active-document watch to the directory containing path.
func (w *Watcher) Watch(path string) {
	if w == nil {
		return
	}
	w.w.Watch(path)
}

// Unwatch releases the active-document watch (turns auto-reload off).
func (w *Watcher) Unwatch() {
	if w == nil {
		return
	}
	w.w.Unwatch()
}

// WatchWorkspace (re)arms the recursive watch on the workspace document tree.
func (w *Watcher) WatchWorkspace(root string) {
	if w == nil {
		return
	}
	w.w.WatchWorkspace(root)
}
