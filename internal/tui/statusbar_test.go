package tui

import (
	"strings"
	"testing"

	"github.com/thgossler/mdv/internal/core"
)

// TestStatusBarHeightResponsive checks that the status bar reports a single row
// on a wide terminal and, on narrower terminals, one row for the
// percentage/notification plus enough wrapped rows to show every shortcut.
func TestStatusBarHeightResponsive(t *testing.T) {
	if got := (Model{width: 200, focus: focusContent}).statusBarHeight(); got != 1 {
		t.Errorf("wide statusBarHeight() = %d, want 1", got)
	}
	tests := []struct {
		name   string
		width  int
		status string
	}{
		{name: "narrow wraps", width: 40},
		{name: "long notification forces wrap", width: 90, status: strings.Repeat("Document changed ", 4)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{width: tt.width, focus: focusContent, statusMsg: tt.status}
			_, _, hints, _ := m.statusParts()
			want := 1 + len(wrapHints(hints, tt.width))
			if got := m.statusBarHeight(); got != want {
				t.Errorf("statusBarHeight() = %d, want %d", got, want)
			}
			if got := m.statusBarHeight(); got < 2 {
				t.Errorf("statusBarHeight() = %d, want >= 2", got)
			}
		})
	}
}

// TestStatusBarHintsNotTruncated verifies that wrapping keeps every shortcut
// visible (including the final q:quit) instead of clipping the hints line.
func TestStatusBarHintsNotTruncated(t *testing.T) {
	for _, focus := range []focus{focusContent, focusList} {
		for _, width := range []int{36, 50, 70} {
			m := Model{width: width, focus: focus, showList: focus == focusList}
			out := stripANSI(m.statusBar())
			if !strings.Contains(out, "q:quit") {
				t.Errorf("focus %v width %d: hints truncated, missing q:quit:\n%s", focus, width, out)
			}
		}
	}
}

// TestStatusBarRendersRequestedRows verifies that statusBar emits exactly the
// number of rows statusBarHeight promises, so the layout reservation and the
// rendered chrome stay in sync.
func TestStatusBarRendersRequestedRows(t *testing.T) {
	for _, width := range []int{200, 40} {
		m := Model{width: width, focus: focusContent}
		want := m.statusBarHeight()
		got := strings.Count(m.statusBar(), "\n") + 1
		if got != want {
			t.Errorf("width %d: statusBar rendered %d rows, statusBarHeight = %d", width, got, want)
		}
	}
}

// TestViewFillsTerminalHeight ensures the full view (header + body + focus bar +
// status bar) occupies exactly the terminal height in both the one-line and the
// wrapped two-line status-bar cases, so the altscreen does not jitter.
func TestViewFillsTerminalHeight(t *testing.T) {
	in := core.Input{Kind: core.InputStdin, Dir: ".", Data: []byte("# Title\n\nsome content here\n")}
	for _, width := range []int{200, 36} {
		m := New(core.DefaultSettings(), in, core.UpdateInfo{})
		if m.watcher != nil {
			defer m.watcher.Close()
		}
		m.width = width
		m.height = 24
		m.layout() // creates the viewport sized for this width/height
		m.ready = true
		m.rerender()
		view := m.View()
		got := strings.Count(view, "\n") + 1
		if got != m.height {
			t.Errorf("width %d: View produced %d rows, want %d (status bar = %d rows)",
				width, got, m.height, m.statusBarHeight())
		}
	}
}
