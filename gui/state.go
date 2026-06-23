package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/thgossler/mdv/internal/core"
)

// Default window and panel dimensions, applied when no persisted state exists
// and restored by the "Reset Layout" action.
const (
	defaultWindowWidth  = 1100
	defaultWindowHeight = 780
	defaultSidebarWidth = 260
	defaultTocWidth     = 260
)

// saveDebounce collapses bursts of window move/resize (or panel drag) updates
// into a single file write, firing this long after the last change.
const saveDebounce = 500 * time.Millisecond

// maxRecentItems caps the rolling "recently opened" list so the program menu
// stays manageable. Older entries fall off the end as new ones are added.
const maxRecentItems = 20

// RecentItem is a single entry in the rolling list of recently opened inputs.
// Files and folders share one list; Kind ("file" or "folder") only affects how
// the entry is labelled in the toolbar's recents drop-down.
type RecentItem struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

// recentItemFor builds a RecentItem from a resolved program input.
func recentItemFor(in core.Input) RecentItem {
	kind := "file"
	if in.Kind == core.InputFolder {
		kind = "folder"
	}
	return RecentItem{Path: in.Path, Kind: kind}
}

// LayoutState is the persisted window geometry and side-panel widths restored
// across runs. A zero value for any width means "use the default".
type LayoutState struct {
	X            int  `json:"x"`
	Y            int  `json:"y"`
	Width        int  `json:"width"`
	Height       int  `json:"height"`
	Maximized    bool `json:"maximized"`
	SidebarWidth int  `json:"sidebarWidth"`
	TocWidth     int  `json:"tocWidth"`
	// ExcludePatterns holds the navigator exclusion patterns (.gitignore style),
	// one per line, exactly as entered in the sidebar's Exclude field.
	ExcludePatterns string `json:"excludePatterns"`
	// ExcludeEnabled toggles whether the exclusion patterns are applied.
	ExcludeEnabled bool `json:"excludeEnabled"`
	// ExtendedSyntax is the user's runtime choice for the opt-in "extended"
	// inline Markdown syntax (math, sub/sup, highlight, inserted), shared with the
	// terminal UI. A nil pointer means "never toggled" so the settings.jsonc
	// default applies; a non-nil value overrides it. omitempty keeps it out of
	// the file until the user actually flips the toggle.
	ExtendedSyntax *bool `json:"extendedSyntax,omitempty"`
	// FileAssocVersion records the OS file-manager integration scheme version
	// last registered by the launcher (see core.EnsureFileAssociations). The GUI
	// never sets it, but carries it through here so rewriting state.jsonc does
	// not drop the marker and trigger needless re-registration.
	FileAssocVersion int  `json:"fileAssocVersion"`
	Valid            bool `json:"valid"`
	// Recent is the rolling list of recently opened files and folders, most
	// recent first, capped at maxRecentItems. It is surfaced only in the program
	// menu, never in the window's toolbar content.
	Recent []RecentItem `json:"recent,omitempty"`
}

func layoutStatePath() (string, error) {
	dir, err := core.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "state.jsonc"), nil
}

// LoadLayoutState reads the saved layout, or returns an invalid (zero) state.
func LoadLayoutState() LayoutState {
	path, err := layoutStatePath()
	if err != nil {
		return LayoutState{}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return LayoutState{}
	}
	var st LayoutState
	if json.Unmarshal(core.StripJSONC(raw), &st) != nil {
		return LayoutState{}
	}
	return st
}

// layoutStateHeader is prepended to the persisted file so it reads as JSONC,
// consistent with settings.jsonc. Go's encoding/json cannot emit comments, so
// the body itself is plain (valid) JSON.
const layoutStateHeader = "// mdv window & panel layout - managed automatically, safe to delete.\n"

func writeLayoutState(st LayoutState) {
	path, err := layoutStatePath()
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	st.Valid = true
	if raw, err := json.MarshalIndent(st, "", "  "); err == nil {
		_ = os.WriteFile(path, append([]byte(layoutStateHeader), raw...), 0o644)
	}
}

// LayoutStore holds the live layout state and persists it with a debounce so
// frequent window move/resize and panel-drag updates collapse into a single
// file write rather than thrashing the disk.
type LayoutStore struct {
	mu    sync.Mutex
	st    LayoutState
	timer *time.Timer
}

// NewLayoutStore seeds a store with the given (possibly invalid) initial state.
func NewLayoutStore(initial LayoutState) *LayoutStore {
	return &LayoutStore{st: initial}
}

// Get returns a copy of the current layout state.
func (s *LayoutStore) Get() LayoutState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st
}

// UpdateGeometry records new window position/size and schedules a save. While
// maximized the normal geometry is preserved so un-maximizing restores it.
func (s *LayoutStore) UpdateGeometry(x, y, w, h int, maximized bool) {
	s.mu.Lock()
	if maximized {
		s.st.Maximized = true
	} else {
		s.st.X, s.st.Y, s.st.Width, s.st.Height, s.st.Maximized = x, y, w, h, false
	}
	s.scheduleLocked()
	s.mu.Unlock()
}

// UpdatePanels records new side-panel widths and schedules a save. A
// non-positive value leaves the corresponding width unchanged.
func (s *LayoutStore) UpdatePanels(sidebar, toc int) {
	s.mu.Lock()
	if sidebar > 0 {
		s.st.SidebarWidth = sidebar
	}
	if toc > 0 {
		s.st.TocWidth = toc
	}
	s.scheduleLocked()
	s.mu.Unlock()
}

// ResetPanels restores the side-panel widths to defaults and schedules a save.
func (s *LayoutStore) ResetPanels() {
	s.mu.Lock()
	s.st.SidebarWidth = defaultSidebarWidth
	s.st.TocWidth = defaultTocWidth
	s.scheduleLocked()
	s.mu.Unlock()
}

// UpdateExcludes records the navigator exclusion patterns and their enabled
// state and schedules a save.
func (s *LayoutStore) UpdateExcludes(patterns string, enabled bool) {
	s.mu.Lock()
	s.st.ExcludePatterns = patterns
	s.st.ExcludeEnabled = enabled
	s.scheduleLocked()
	s.mu.Unlock()
}

// UpdateExtendedSyntax records the user's extended-syntax toggle choice and
// schedules a save.
func (s *LayoutStore) UpdateExtendedSyntax(enabled bool) {
	s.mu.Lock()
	s.st.ExtendedSyntax = &enabled
	s.scheduleLocked()
	s.mu.Unlock()
}

// AddRecent records a freshly opened file or folder at the top of the rolling
// recents list. Any existing entry for the same path is removed first so the
// item moves to the front rather than duplicating, and the list is capped at
// maxRecentItems. The write is debounced like every other layout change.
func (s *LayoutStore) AddRecent(item RecentItem) {
	if item.Path == "" {
		return
	}
	s.mu.Lock()
	filtered := s.st.Recent[:0:0]
	for _, r := range s.st.Recent {
		if r.Path != item.Path {
			filtered = append(filtered, r)
		}
	}
	s.st.Recent = append([]RecentItem{item}, filtered...)
	if len(s.st.Recent) > maxRecentItems {
		s.st.Recent = s.st.Recent[:maxRecentItems]
	}
	s.scheduleLocked()
	s.mu.Unlock()
}

// Recent returns a copy of the current recently-opened list, most recent first.
func (s *LayoutStore) Recent() []RecentItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.st.Recent) == 0 {
		return nil
	}
	out := make([]RecentItem, len(s.st.Recent))
	copy(out, s.st.Recent)
	return out
}

// ClearRecent empties the recently-opened list and schedules a save.
func (s *LayoutStore) ClearRecent() {
	s.mu.Lock()
	s.st.Recent = nil
	s.scheduleLocked()
	s.mu.Unlock()
}

// scheduleLocked (re)arms the debounce timer; the caller must hold s.mu.
func (s *LayoutStore) scheduleLocked() {
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(saveDebounce, s.flush)
}

func (s *LayoutStore) flush() {
	s.mu.Lock()
	st := s.st
	s.mu.Unlock()
	writeLayoutState(st)
}

// Flush cancels any pending debounce and writes the current state immediately,
// used on shutdown so the final geometry is never lost.
func (s *LayoutStore) Flush() {
	s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	st := s.st
	s.mu.Unlock()
	writeLayoutState(st)
}
