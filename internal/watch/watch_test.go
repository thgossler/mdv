package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/thgossler/mdv/internal/core"
)

// waitFor returns the next event of the wanted kind, or fails after a generous
// timeout so a missed filesystem notification does not hang the suite.
func waitFor(t *testing.T, ch <-chan Event, want Kind) Event {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Kind == want {
				return ev
			}
		case <-deadline:
			t.Fatalf("timed out waiting for event kind %d", want)
			return Event{}
		}
	}
}

func TestWatcherFileChanged(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(file, []byte("# One"), 0o644); err != nil {
		t.Fatal(err)
	}

	events := make(chan Event, 16)
	w := New(core.DefaultSettings(), func(ev Event) { events <- ev })
	if w == nil {
		t.Skip("filesystem watcher unavailable on this platform")
	}
	defer w.Close()
	w.Watch(file)

	if err := os.WriteFile(file, []byte("# One\n\n# Two"), 0o644); err != nil {
		t.Fatal(err)
	}

	ev := waitFor(t, events, FileChanged)
	if ev.Path != file {
		t.Errorf("FileChanged path = %q, want %q", ev.Path, file)
	}
}

func TestWatcherWorkspaceChanged(t *testing.T) {
	dir := t.TempDir()

	events := make(chan Event, 16)
	w := New(core.DefaultSettings(), func(ev Event) { events <- ev })
	if w == nil {
		t.Skip("filesystem watcher unavailable on this platform")
	}
	defer w.Close()
	w.WatchWorkspace(dir)

	// Adding a markdown document anywhere in the tree is a structural change.
	if err := os.WriteFile(filepath.Join(dir, "new.md"), []byte("# New"), 0o644); err != nil {
		t.Fatal(err)
	}

	ev := waitFor(t, events, WorkspaceChanged)
	if ev.Path != dir {
		t.Errorf("WorkspaceChanged path = %q, want %q", ev.Path, dir)
	}
}

func TestWatcherIgnoresNonMarkdown(t *testing.T) {
	dir := t.TempDir()

	events := make(chan Event, 16)
	w := New(core.DefaultSettings(), func(ev Event) { events <- ev })
	if w == nil {
		t.Skip("filesystem watcher unavailable on this platform")
	}
	defer w.Close()
	w.WatchWorkspace(dir)

	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case ev := <-events:
		if ev.Kind == WorkspaceChanged {
			t.Errorf("non-markdown file triggered WorkspaceChanged")
		}
	case <-time.After(600 * time.Millisecond):
		// No event: the expected outcome.
	}
}
