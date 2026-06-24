package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/thgossler/mdv/internal/core"
	"github.com/thgossler/mdv/internal/termimg"
)

// TestStdinFirstRenderTriggersImagePrewarm guards the piped-stdin image path:
// the renderer defers image loading on the first render (drawing alt text), and
// a background prefetch is what actually loads the images. For file input that
// prefetch is kicked off by openPath; stdin has no path to open, so the first
// WindowSizeMsg must start the prefetch itself. Without it, every image in a
// piped document stays as alt text and never appears.
func TestStdinFirstRenderTriggersImagePrewarm(t *testing.T) {
	in := core.Input{
		Kind: core.InputStdin,
		Dir:  ".",
		Data: []byte("# Title\n\n![hero](images/image.png)\n"),
	}
	m := New(core.DefaultSettings(), in, core.UpdateInfo{})
	if m.watcher != nil {
		defer m.watcher.Close()
	}
	// Inject an enabled image renderer so the prefetch has work to do regardless
	// of whether the test runs under a color-capable terminal (imageRenderer()
	// otherwise returns nil when stdout is not a TTY).
	m.imgRenderer = termimg.NewRenderer(termimg.ProtocolBlocks, in.Dir, false)

	_, cmd := m.routeMsg(tea.WindowSizeMsg{Width: 80, Height: 24})
	if cmd == nil {
		t.Fatal("stdin first WindowSizeMsg returned no command; image prefetch was not triggered")
	}
}
