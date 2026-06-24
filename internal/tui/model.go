// Package tui implements the interactive terminal user interface used when no
// graphical environment is available (or when --tui is requested). It is built
// on Bubble Tea and depends only on pure-Go libraries, so it runs in headless
// containers over SSH.
package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/thgossler/mdv/internal/core"
	"github.com/thgossler/mdv/internal/mdfmt"
	"github.com/thgossler/mdv/internal/termimg"
	"github.com/thgossler/mdv/internal/watch"
	"golang.org/x/term"
)

type focus int

const (
	focusContent focus = iota
	focusList
	focusLinks
	focusSearch
	focusListFilter
)

type histEntry struct {
	path    string
	yOffset int
}

// matchPos identifies a single search hit: a 0-based line index in the rendered
// (ANSI-stripped) content and the visible-column offset of the match start on
// that line. For phrase searches end is the visible-column offset just past the
// match so the whole matched span can be highlighted; it is 0 for keyword
// searches that highlight by term. Multiple hits on the same line are tracked
// independently.
type matchPos struct {
	line int
	col  int
	end  int
}

// maxHighlightBytes is the largest rendered document (in bytes) for which search
// matches are re-highlighted in place. Above it, search still finds and scrolls
// between matches, but does not recolor them: re-styling the whole buffer on
// every jump would make navigation laggy on very large documents.
const maxHighlightBytes = 4 << 20 // 4 MiB

// Model is the Bubble Tea model for the terminal viewer.
type Model struct {
	cfg          core.Defaults
	workspaceDir string
	workspace    []core.DocFile

	list     list.Model
	links    list.Model
	viewport viewport.Model

	currentPath string
	currentDir  string
	rawMarkdown string
	stdin       bool // content was piped in; there is no backing file path

	// metaExpanded reports whether the front matter metadata section is showing
	// its extra (non-headline) fields. It resets to collapsed on each new
	// document. metaHidden caches the number of extra fields so the status bar
	// can advertise the toggle without re-parsing YAML on every keystroke.
	metaExpanded bool
	metaHidden   int

	// resolvedStyle is the concrete glamour style ("light"/"dark") chosen once at
	// startup. Renders never use glamour's "auto" style, because auto-detection
	// writes an OSC 11 background-colour query to the terminal and waits for the
	// reply; under Bubble Tea's raw mode that reply is swallowed by the input
	// reader, so the query blocks until it times out on every fresh render.
	resolvedStyle string

	// renderCache holds the rendered markdown keyed by "width|theme|len". Showing
	// or hiding the navigator changes the content width, and search highlighting
	// re-renders frequently; caching per width means toggling the side panel back
	// and forth (a content-independent UI action) never re-invokes glamour or
	// re-encodes images. Cleared when the document changes. Avoiding re-renders
	// also avoids a glitch where re-invoking glamour with the "auto" style
	// re-queries the terminal background colour and that response leaks into key
	// input.
	renderCache map[string]string

	// imgRenderer is the persistent image renderer for the content view. Keeping
	// one renderer across re-renders preserves its decode cache, so remote images
	// are fetched once per document (and shared across documents) instead of on
	// every re-render. nil when images are disabled.
	imgRenderer *termimg.Renderer

	history      []histEntry
	forward      []histEntry
	focus        focus
	showList     bool
	labelMode    string // "filename" | "title"
	titlesLoaded bool   // workspace titles extracted (one-time file scan)
	entryPath    string // most-likely documentation entry point (emphasised in nav)

	searchInput string
	matches     []matchPos
	matchIdx    int
	searchQuery string   // single term currently highlighted (empty when none)
	searchTerms []string // multiple terms highlighted (content-search jump)

	// Document-navigator filter state. When focus is focusListFilter the user
	// types into listFilterInput: a plain string filters by filename/title,
	// while a leading "/" switches to content search (so "/" filters names and
	// "//" searches content). The list then holds navItem entries (document
	// headers plus indented per-match rows).
	listFilterInput string

	statusMsg string
	update    core.UpdateInfo

	// watcher delivers debounced filesystem events (active-document content
	// changes and workspace tree changes) so the viewer can live-reload and keep
	// its navigator in sync. nil when live reload is disabled or unavailable.
	watcher *watch.Watcher
	watchCh chan watch.Event
	// watchEnabled is the runtime auto-reload toggle for the active document. It
	// starts from cfg.LiveReload and is flipped with the 'w' key.
	watchEnabled bool
	// statusGen tags each transient status message so a scheduled expiry only
	// clears the message it was issued for, never a newer one.
	statusGen int

	// extendedSyntax mirrors the shared GUI/TUI "extended" inline syntax toggle
	// (math, sub/sup, highlight, inserted). The terminal renderer cannot display
	// these constructs, so toggling it here only updates and persists the shared
	// preference (state.jsonc) for the GUI; the TUI render output is unaffected.
	extendedSyntax bool

	width  int
	height int
	ready  bool
}

// imagesReadyMsg signals that a background prefetch has finished decoding a
// document's remote images, so the content view can be re-rendered with them in
// place. path identifies the document the prefetch was started for, so a stale
// result (the user already navigated away) is ignored.
type imagesReadyMsg struct {
	path string
}

// watchEventMsg carries a debounced filesystem event from the watcher into the
// Bubble Tea update loop.
type watchEventMsg struct {
	ev watch.Event
}

// statusExpireMsg clears a transient status message once its display time has
// elapsed, provided a newer message has not replaced it in the meantime.
type statusExpireMsg struct {
	gen int
}

// docItem adapts a DocFile to the list widget.
type docItem struct {
	doc       core.DocFile
	labelMode string
}

func (d docItem) Title() string {
	if d.labelMode == "title" && d.doc.Title != "" {
		return d.doc.Title
	}
	if d.doc.Rel != "" {
		return d.doc.Rel
	}
	return d.doc.Name
}
func (d docItem) Description() string { return d.doc.Path }
func (d docItem) FilterValue() string { return d.doc.Name + " " + d.doc.Rel + " " + d.doc.Title }

// navKind distinguishes a document header row from an indented content-match row
// in the document-navigator filter view.
type navKind int

const (
	navDoc navKind = iota
	navMatch
)

// navItem is a document-navigator entry used while filtering: either a document
// header (navDoc) or a content-search match (navMatch) shown indented beneath
// its document. Match rows carry the source line and the active keywords so
// selecting one can open the document and jump to the match.
type navItem struct {
	kind      navKind
	doc       core.DocFile
	labelMode string
	line      int
	text      string
	keywords  []string
}

func (n navItem) Title() string {
	if n.kind == navMatch {
		return "  › " + n.text
	}
	if n.labelMode == "title" && n.doc.Title != "" {
		return n.doc.Title
	}
	if n.doc.Rel != "" {
		return n.doc.Rel
	}
	return n.doc.Name
}
func (n navItem) Description() string {
	if n.kind == navMatch {
		return fmt.Sprintf("    line %d", n.line)
	}
	return n.doc.Path
}
func (n navItem) FilterValue() string { return n.Title() }

// linkItem adapts a Link to the picker list.
type linkItem struct{ link Link }

func (l linkItem) Title() string       { return l.link.Text }
func (l linkItem) Description() string { return l.link.Href }
func (l linkItem) FilterValue() string { return l.link.Text + " " + l.link.Href }

// New constructs a TUI model for the given input.
func New(cfg core.Defaults, in core.Input, upd core.UpdateInfo) Model {
	m := Model{
		cfg:           cfg,
		workspaceDir:  in.Dir,
		labelMode:     cfg.NavLabelMode,
		resolvedStyle: resolveStyle(cfg.Theme),
		update:        upd,
		focus:         focusContent,
	}

	// The extended-syntax toggle is shared with the GUI: a persisted runtime
	// choice in state.jsonc overrides the settings.jsonc default.
	m.extendedSyntax = cfg.EnableExtendedSyntax
	if v, ok := core.LoadViewerExtendedSyntax(); ok {
		m.extendedSyntax = v
	}

	if in.Kind == core.InputFolder {
		m.showList = true
		m.focus = focusList
		files, _ := core.ListMarkdownFiles(in.Dir, cfg)
		if cfg.NavLabelMode == "title" {
			core.PopulateTitles(files)
			m.titlesLoaded = true
		}
		m.workspace = files
	} else if in.Kind == core.InputFile {
		// Build a workspace from the file's folder for wikilink resolution.
		files, _ := core.ListMarkdownFiles(in.Dir, cfg)
		m.workspace = files
		m.currentPath = in.Path
		m.currentDir = in.Dir
	} else if in.Kind == core.InputStdin {
		// Markdown piped in: render the in-memory buffer. Use the working
		// directory as the workspace so relative links and images resolve.
		files, _ := core.ListMarkdownFiles(in.Dir, cfg)
		m.workspace = files
		m.currentDir = in.Dir
		m.rawMarkdown = string(in.Data)
		m.stdin = true
	}
	m.refreshMeta()

	m.entryPath = entryPointPath(m.workspace)

	fileList := list.New(docItemsFrom(m.workspace, m.labelMode), newDocDelegate(m.entryPath), 0, 0)
	fileList.Title = "Documents"
	fileList.SetShowHelp(false)
	fileList.SetShowStatusBar(false)
	m.list = fileList

	linkList := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	linkList.Title = "Links - Enter to follow, Esc to cancel"
	linkList.SetShowHelp(false)
	m.links = linkList

	// Live reload: watch only the active document for content changes. The emit
	// callback runs on the watcher's goroutine and hands events to the update
	// loop via a buffered channel. The workspace tree is deliberately not watched
	// (it can be huge); structural changes are picked up on demand via the manual
	// reload key instead. The watcher is always created so auto-reload can be
	// toggled at runtime with the 'w' key, but the active document is only armed
	// when watching is enabled (off by default). Stdin content has no backing
	// file, so nothing is armed until a real document becomes active.
	m.watchEnabled = cfg.LiveReload
	ch := make(chan watch.Event, 8)
	m.watchCh = ch
	m.watcher = watch.New(cfg, func(ev watch.Event) {
		select {
		case ch <- ev:
		default:
		}
	})
	if m.watcher == nil {
		m.watchCh = nil
	} else if m.watchEnabled && m.currentPath != "" {
		m.watcher.Watch(m.currentPath)
	}

	return m
}

func docItemsFrom(files []core.DocFile, labelMode string) []list.Item {
	items := make([]list.Item, len(files))
	for i, f := range files {
		items[i] = docItem{doc: f, labelMode: labelMode}
	}
	return items
}

// entryPointNames and entryPointFolders list the recognised landing-page file
// names and typical documentation folders, each in descending priority order.
var (
	entryPointNames   = []string{"readme.md", "index.md", "home.md"}
	entryPointFolders = []string{"docs", "doc", "documentation", "wiki"}
)

// entryPointPath returns the absolute path of the single most-probable
// documentation entry point in files, or "" when none qualifies. A root-level
// README/index/home page always wins; otherwise a matching page directly inside
// a typical documentation folder (depth 1 only) is chosen. Anything deeper never
// qualifies. Mirrors the GUI's computeEntryPoint logic.
func entryPointPath(files []core.DocFile) string {
	best := ""
	bestScore := -1 << 30
	for _, d := range files {
		rel := strings.ToLower(strings.TrimLeft(d.Rel, "/"))
		if rel == "" {
			rel = strings.ToLower(d.Name)
		}
		base := rel
		if i := strings.LastIndex(rel, "/"); i >= 0 {
			base = rel[i+1:]
		}
		nameRank := indexOf(entryPointNames, base)
		if nameRank < 0 {
			continue
		}
		slash := strings.Index(rel, "/")
		var score int
		switch {
		case slash < 0:
			// Root level: highest priority, ordered by file-name rank.
			score = len(entryPointNames) - nameRank
		case strings.Index(rel[slash+1:], "/") < 0:
			// Depth 1: only inside a recognised documentation folder.
			folderRank := indexOf(entryPointFolders, rel[:slash])
			if folderRank < 0 {
				continue
			}
			score = -1 - (folderRank*len(entryPointNames) + nameRank)
		default:
			continue // deeper than depth 1 never qualifies
		}
		if score > bestScore {
			bestScore = score
			best = d.Path
		}
	}
	return best
}

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

// docDelegate renders the document navigator, emphasising the single entry-point
// document (bold + accent colour) so a reader immediately sees where to start.
type docDelegate struct {
	list.DefaultDelegate
	entryPath string
}

// newDocDelegate builds the navigator delegate for the given entry-point path.
func newDocDelegate(entryPath string) docDelegate {
	return docDelegate{DefaultDelegate: list.NewDefaultDelegate(), entryPath: entryPath}
}

// Render emphasises the entry-point document; all other rows fall through to the
// default rendering.
func (d docDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	var path string
	switch it := item.(type) {
	case docItem:
		path = it.doc.Path
	case navItem:
		if it.kind == navDoc {
			path = it.doc.Path
		}
	}
	if d.entryPath != "" && path == d.entryPath {
		accent := lipgloss.Color("12") // bright blue, matches the GUI accent
		dd := d.DefaultDelegate
		dd.Styles.NormalTitle = dd.Styles.NormalTitle.Bold(true).Foreground(accent)
		dd.Styles.SelectedTitle = dd.Styles.SelectedTitle.Bold(true).Foreground(accent)
		dd.Styles.DimmedTitle = dd.Styles.DimmedTitle.Bold(true).Foreground(accent)
		dd.Render(w, m, index, item)
		return
	}
	d.DefaultDelegate.Render(w, m, index, item)
}

// resolveStyle picks a concrete glamour style ("light"/"dark") once, so renders
// during the session never use glamour's "auto" style. Auto-detection writes an
// OSC 11 background-colour query to the terminal and waits for the reply; under
// Bubble Tea's raw mode that reply is consumed by the input reader, so the query
// blocks until it times out on every fresh render. Detecting once here, before
// the program takes over the terminal, avoids that content-independent stall.
func resolveStyle(theme string) string {
	switch theme {
	case "light", "dark":
		return theme
	}
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return "dark"
	}
	if lipgloss.HasDarkBackground() {
		return "dark"
	}
	return "light"
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return waitForWatch(m.watchCh) }

// waitForWatch returns a command that blocks until the next filesystem event is
// available on ch and delivers it as a watchEventMsg. It returns nil when live
// reload is disabled, so the program simply never receives watch events.
func waitForWatch(ch chan watch.Event) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return watchEventMsg{ev: ev}
	}
}

// ErrNoTerminal reports that the TUI cannot run because no interactive terminal
// is available on stdin and stdout (for example both are pipes). Callers can use
// it to fall back to plain console rendering.
var ErrNoTerminal = errors.New("no interactive terminal available")

// Run starts the program and blocks until the user quits.
func Run(cfg core.Defaults, in core.Input, upd core.UpdateInfo) error {
	m := New(cfg, in, upd)
	opts := []tea.ProgramOption{tea.WithAltScreen(), tea.WithMouseCellMotion()}
	// When markdown is piped on stdin, os.Stdin carries the document, not the
	// keyboard, so reopen the controlling terminal for interactive use. We
	// rebind os.Stdin/os.Stdout (rather than passing tea.WithInput/WithOutput)
	// on purpose: Bubble Tea's Windows cancelreader only supports interrupting a
	// blocking read when the input's file descriptor matches os.Stdin. A handle
	// supplied via WithInput falls back to a non-cancelable reader, which leaves
	// the program waiting for one extra keypress before it can exit. On Windows
	// we additionally repoint the Win32 standard handles (bindStdHandle), because
	// Bubble Tea resolves the console through GetStdHandle(STD_INPUT_HANDLE), not
	// os.Stdin - without this it calls GetConsoleMode on the redirected pipe and
	// fails with "The handle is invalid". The piped document has already been
	// read into memory by now, so repurposing the std handles is safe.
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		if tty, err := openControllingTerminal(ttyInput); err == nil {
			os.Stdin = tty
			bindStdHandle(ttyInput, tty)
		}
		// On Windows the launcher rebinds os.Stdout to a write-only CONOUT$
		// handle, which fails the TTY check - so Bubble Tea never learns the
		// window size and stays stuck on the initial "Loading…" frame. Reopen
		// the console output as a real (read-write) TTY so it gets an initial
		// size and renders normally.
		if !term.IsTerminal(int(os.Stdout.Fd())) {
			if tty, err := openControllingTerminal(ttyOutput); err == nil {
				os.Stdout = tty
				bindStdHandle(ttyOutput, tty)
			}
		}
		// If we still lack an interactive terminal on either end (for example
		// both stdin and stdout are pipes, as in `cmd | mdv --tui | Out-String`),
		// there is no console to drive the TUI. Report a recoverable error so the
		// caller can fall back to console rendering instead of letting Bubble Tea
		// fail deep inside its console-input setup.
		if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
			return ErrNoTerminal
		}
	}
	p := tea.NewProgram(m, opts...)
	_, err := p.Run()
	return err
}

// ttyRole selects which end of the controlling terminal to open. On Windows the
// console input and output are distinct devices (CONIN$ / CONOUT$); on Unix both
// map to /dev/tty.
type ttyRole int

const (
	ttyInput ttyRole = iota
	ttyOutput
)

// openControllingTerminal opens the process's controlling terminal for the given
// role, used to source keyboard input and render output when stdin (and, on
// Windows, stdout) is a pipe rather than the terminal. The handle is opened
// read-write because the Windows console-output device (CONOUT$) only reports as
// a TTY - and only answers window-size queries - when the handle has read access.
func openControllingTerminal(role ttyRole) (*os.File, error) {
	name := "/dev/tty"
	if runtime.GOOS == "windows" {
		if role == ttyInput {
			name = "CONIN$"
		} else {
			name = "CONOUT$"
		}
	}
	return os.OpenFile(name, os.O_RDWR, 0)
}

const sidebarWidth = 34

// Update implements tea.Model. It dispatches the message to routeMsg and then
// re-syncs the content height: a status bar that wraps to two lines (or shrinks
// back to one) changes how many rows are left for the document, so the viewport
// must follow even when no window-resize event occurred.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.routeMsg(msg)
	if nm, ok := next.(Model); ok {
		nm.syncChromeHeight()
		return nm, cmd
	}
	return next, cmd
}

// routeMsg dispatches a single message to the relevant handler.
func (m Model) routeMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layout()
		if !m.ready {
			m.ready = true
			if m.currentPath != "" {
				cmd := m.openPath(m.currentPath, true)
				return m, cmd
			} else if m.stdin {
				m.rerender()
				m.viewport.GotoTop()
				// Piped stdin has no path to open, so kick off the background
				// image prefetch here; otherwise the deferred-load renderer
				// would leave every image as alt text (images never load).
				return m, m.prewarmImagesCmd()
			}
		} else {
			m.rerender()
		}
		return m, nil

	case imagesReadyMsg:
		if m.ready && msg.path == m.currentPath {
			// The decode cache now holds the document's images; drop the render
			// cache so the next render draws them, then re-render.
			m.renderCache = nil
			m.rerender()
		}
		return m, nil

	case watchEventMsg:
		// Keep listening for the next event regardless of how this one is handled.
		next := waitForWatch(m.watchCh)
		switch msg.ev.Kind {
		case watch.FileChanged:
			if m.ready && msg.ev.Path == m.currentPath {
				return m, tea.Batch(m.reloadCurrent("Document changed"), next)
			}
		case watch.WorkspaceChanged:
			m.refreshWorkspace()
		}
		return m, next

	case statusExpireMsg:
		if msg.gen == m.statusGen {
			m.statusMsg = ""
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		if m.focus == focusContent {
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Search input mode captures most keys.
	if m.focus == focusSearch {
		return m.handleSearchKey(msg)
	}
	// Document-navigator filter input captures most keys too.
	if m.focus == focusListFilter {
		return m.handleListFilterKey(msg)
	}

	switch msg.String() {
	case "ctrl+c", "q":
		if m.focus == focusLinks {
			break // let 'q' be typed in filter; fallthrough handled below
		}
		return *m, tea.Quit
	}

	switch m.focus {
	case focusLinks:
		return m.handleLinksKey(msg)
	case focusList:
		return m.handleListKey(msg)
	default:
		return m.handleContentKey(msg)
	}
}

func (m *Model) handleContentKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "tab":
		if !m.showList {
			m.toggleNav() // reveal and focus the navigator
		} else {
			m.focus = focusList
		}
		return *m, nil
	case "ctrl+b":
		m.toggleNav()
		return *m, nil
	case "b", "backspace", "left", "alt+left":
		return *m, m.goBack()
	case "f", "right", "alt+right":
		return *m, m.goForward()
	case "r":
		// Manual reload is the only way structural workspace changes are picked
		// up now that only the active document is watched, so re-scan the tree
		// (refreshing the navigator) before re-rendering the document.
		m.refreshWorkspace()
		return *m, m.reloadCurrent("Reloaded")
	case "w":
		return *m, m.toggleWatch()
	case "enter", "l":
		m.openLinkPicker()
		return *m, nil
	case "t":
		m.toggleLabelMode()
		return *m, nil
	case "x":
		m.toggleExtendedSyntax()
		return *m, nil
	case "m":
		m.toggleMeta()
		return *m, nil
	case "i":
		return m.toggleRemoteImages()
	case "/":
		m.focus = focusSearch
		m.searchInput = ""
		m.statusMsg = ""
		return *m, nil
	case "g", "home":
		m.viewport.GotoTop()
		return *m, nil
	case "G", "end":
		m.viewport.GotoBottom()
		return *m, nil
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return *m, cmd
}

func (m *Model) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "tab":
		m.focus = focusContent
		return *m, nil
	case "ctrl+b":
		m.toggleNav() // hide the navigator, back to the content view
		return *m, nil
	case "/":
		// Enter the navigator filter: a plain query filters by name, a leading
		// "/" (i.e. "//") switches to content search.
		m.focus = focusListFilter
		m.listFilterInput = ""
		m.statusMsg = ""
		m.rebuildNavList()
		return *m, nil
	case "enter", "right", "l":
		if it, ok := m.list.SelectedItem().(docItem); ok {
			cmd := m.openPath(it.doc.Path, true)
			m.focus = focusContent
			return *m, cmd
		}
		return *m, nil
	case "t":
		m.toggleLabelMode()
		return *m, nil
	case "x":
		m.toggleExtendedSyntax()
		return *m, nil
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return *m, cmd
}

// handleListFilterKey handles keystrokes while the document-navigator filter
// input is active.
func (m *Model) handleListFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.exitListFilter()
		return *m, nil
	case "enter":
		var cmd tea.Cmd
		switch it := m.list.SelectedItem().(type) {
		case navItem:
			if it.kind == navMatch {
				cmd = m.openContentMatch(it)
			} else {
				cmd = m.openPath(it.doc.Path, true)
				m.focus = focusContent
				m.resetNavList()
			}
		case docItem:
			cmd = m.openPath(it.doc.Path, true)
			m.focus = focusContent
			m.resetNavList()
		}
		return *m, cmd
	case "backspace":
		if len(m.listFilterInput) > 0 {
			m.listFilterInput = m.listFilterInput[:len(m.listFilterInput)-1]
			m.rebuildNavList()
		}
		return *m, nil
	case "up", "down", "pgup", "pgdown", "home", "end":
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return *m, cmd
	default:
		// Accept typed text, including spaces for multi-word queries. Bubble Tea
		// reports a single space as KeySpace (with the space in Runes) rather than
		// KeyRunes, so both types are handled; control characters are dropped to
		// keep stray terminal responses out of the input.
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			for _, r := range msg.Runes {
				if unicode.IsControl(r) {
					continue
				}
				m.listFilterInput += string(r)
			}
			m.rebuildNavList()
		}
		return *m, nil
	}
}

func (m *Model) handleLinksKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.focus = focusContent
		return *m, nil
	case "enter":
		if it, ok := m.links.SelectedItem().(linkItem); ok {
			m.focus = focusContent
			return *m, m.followLink(it.link.Href)
		}
		return *m, nil
	}
	var cmd tea.Cmd
	m.links, cmd = m.links.Update(msg)
	return *m, cmd
}

func (m *Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.exitSearch()
		return *m, nil
	case "enter":
		if strings.TrimSpace(m.searchInput) == "" {
			m.exitSearch()
			return *m, nil
		}
		// First Enter on a new query runs the search and jumps to the first
		// match; pressing Enter again cycles through the matches (wrapping
		// around) without leaving search mode.
		if m.searchInput != m.searchQuery {
			m.runSearch(m.searchInput)
		} else {
			m.jumpMatch(1)
		}
		return *m, nil
	case "down":
		// ↓ steps forward through matches (same as Enter). Arrow keys are used
		// for prev/next because Shift+Enter and modifier+Enter are not reported
		// reliably by terminals, and letter shortcuts would collide with typing
		// the query.
		if m.searchInput == m.searchQuery && m.searchQuery != "" {
			m.jumpMatch(1)
		}
		return *m, nil
	case "up":
		// ↑ steps backward through matches (wrapping around).
		if m.searchInput == m.searchQuery && m.searchQuery != "" {
			m.jumpMatch(-1)
		}
		return *m, nil
	case "backspace":
		if len(m.searchInput) > 0 {
			m.searchInput = m.searchInput[:len(m.searchInput)-1]
		}
		return *m, nil
	case "/":
		// A leading "/" in the in-document search prompt means the user typed
		// "//": switch to workspace-wide content search shown in the navigator,
		// matching the document list's "//" behavior. A "/" anywhere else in the
		// query is treated as a literal character.
		if m.searchInput == "" {
			m.enterContentSearch()
		} else {
			m.searchInput += "/"
		}
		return *m, nil
	default:
		// Only accept real typed text. Guarding on KeyRunes/KeySpace (and dropping
		// any control characters) prevents stray terminal responses - such as the
		// OSC background-colour reply - from leaking into the query, while still
		// allowing spaces (reported as KeySpace) in multi-word searches.
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			for _, r := range msg.Runes {
				if unicode.IsControl(r) {
					continue
				}
				m.searchInput += string(r)
			}
		}
		return *m, nil
	}
}

// exitSearch leaves search mode and removes the match highlighting.
func (m *Model) exitSearch() {
	m.focus = focusContent
	m.searchInput = ""
	m.statusMsg = ""
	if m.searchQuery != "" || len(m.searchTerms) > 0 || len(m.matches) > 0 {
		m.searchQuery = ""
		m.searchTerms = nil
		m.matches = nil
		m.matchIdx = 0
		m.rerender()
	}
}

// enterContentSearch switches from the in-document search prompt to the
// workspace-wide content search shown in the document navigator, so the full
// "//" search is reachable directly from the content view. It reveals the
// navigator (which lists every markdown file in the folder, even when a single
// file was opened) and seeds the filter with a leading "/" so further typing
// continues the content query.
func (m *Model) enterContentSearch() {
	if !m.showList {
		if len(m.workspace) == 0 {
			m.statusMsg = "No other documents in this folder"
			return
		}
		if m.labelMode == "title" {
			m.ensureTitles()
		}
		m.showList = true
		m.layout()
	}
	// Drop any in-document search state carried over from the content view.
	m.searchInput = ""
	m.searchQuery = ""
	m.searchTerms = nil
	m.matches = nil
	m.matchIdx = 0
	m.rerender()

	m.focus = focusListFilter
	m.listFilterInput = "/"
	m.statusMsg = ""
	m.rebuildNavList()
}

// --- document-navigator filter / content search ----------------------------

// splitKeywords lowercases and splits the query into distinct space-separated
// keywords, used for highlighting matched words and name/title qualification.
func splitKeywords(query string) []string {
	seen := map[string]bool{}
	var out []string
	for _, w := range strings.Fields(strings.ToLower(query)) {
		if !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	return out
}

// rebuildNavList recomputes the document-navigator list from the current filter
// input: a plain string filters by filename/title, while a leading "/" runs a
// content search and shows matching lines indented beneath their documents.
func (m *Model) rebuildNavList() {
	input := m.listFilterInput
	if strings.HasPrefix(input, "/") {
		m.rebuildContentList(input[1:])
		return
	}
	// Filename / title filter.
	q := strings.TrimSpace(input)
	var items []list.Item
	for _, d := range m.workspace {
		if q == "" || core.FuzzyMatch(d.Name, q) || core.FuzzyMatch(d.Title, q) {
			items = append(items, navItem{kind: navDoc, doc: d, labelMode: m.labelMode})
		}
	}
	m.list.SetItems(items)
	if len(items) > 0 {
		m.list.Select(0)
	}
}

// rebuildContentList runs a content search across the workspace and rebuilds the
// list with each qualifying document followed by its indented match rows. A
// document qualifies when it has content matches OR its name/title contains all
// keywords.
func (m *Model) rebuildContentList(query string) {
	keywords := splitKeywords(query)
	if len(keywords) == 0 {
		// No keywords yet: show all documents as headers.
		var items []list.Item
		for _, d := range m.workspace {
			items = append(items, navItem{kind: navDoc, doc: d, labelMode: m.labelMode})
		}
		m.list.SetItems(items)
		if len(items) > 0 {
			m.list.Select(0)
		}
		return
	}

	results := map[string][]core.ContentMatch{}
	core.SearchDocuments(context.Background(), m.workspace, query, func(r core.DocSearchResult) {
		results[r.Path] = r.Matches
	})

	var items []list.Item
	for _, d := range m.workspace {
		matches, has := results[d.Path]
		qualifies := (has && len(matches) > 0) || nameQualifies(d, query)
		if !qualifies {
			continue
		}
		items = append(items, navItem{kind: navDoc, doc: d, labelMode: m.labelMode})
		for _, mt := range matches {
			items = append(items, navItem{
				kind:     navMatch,
				doc:      d,
				line:     mt.Line,
				text:     mt.Text,
				keywords: keywords,
			})
		}
	}
	m.list.SetItems(items)
	if len(items) > 0 {
		m.list.Select(0)
	}
}

// nameQualifies reports whether a document's name or title matches the query as
// a fuzzy phrase, mirroring the content-search matching rules.
func nameQualifies(d core.DocFile, query string) bool {
	return core.FuzzyMatch(d.Name+" "+d.Title, query)
}

// exitListFilter cancels the navigator filter and restores the full document
// list.
func (m *Model) exitListFilter() {
	m.focus = focusList
	m.listFilterInput = ""
	m.resetNavList()
}

// resetNavList restores the navigator to the full, unfiltered document list.
func (m *Model) resetNavList() {
	m.list.SetItems(docItemsFrom(m.workspace, m.labelMode))
	m.syncListSelection()
}

// openContentMatch opens the document of a content-search match row and jumps to
// the first occurrence of the keywords in the rendered document, highlighting
// all of them (current match in green).
func (m *Model) openContentMatch(it navItem) tea.Cmd {
	cmd := m.openPath(it.doc.Path, true)
	m.focus = focusContent
	m.resetNavList()
	m.runSearchMulti(it.keywords)
	return cmd
}

// --- actions ---------------------------------------------------------------

func (m *Model) openLinkPicker() {
	links := ExtractLinks(m.rawMarkdown)
	if len(links) == 0 {
		m.statusMsg = "No links in this document"
		return
	}
	items := make([]list.Item, len(links))
	for i, l := range links {
		items[i] = linkItem{link: l}
	}
	m.links.SetItems(items)
	m.links.ResetSelected()
	m.focus = focusLinks
}

func (m *Model) followLink(href string) tea.Cmd {
	target := core.ResolveLink(href, m.currentDir, m.workspaceDir, m.cfg, m.workspace)
	switch target.Kind {
	case core.LinkMarkdown, core.LinkWikiInternal:
		cmd := m.openPath(target.Resolved, true)
		if target.Fragment != "" {
			m.scrollToHeading(target.Fragment)
		}
		return cmd
	case core.LinkAnchor:
		m.scrollToHeading(strings.TrimPrefix(target.Resolved, "#"))
	case core.LinkHTTP, core.LinkMailto, core.LinkExternalFile:
		if err := core.OpenInOS(target.Resolved); err != nil {
			m.statusMsg = "Open failed: " + err.Error()
		} else {
			m.statusMsg = "Opened externally: " + target.Display
		}
	case core.LinkBroken:
		m.statusMsg = "Broken link: " + target.Raw
	default:
		m.statusMsg = "Cannot open: " + target.Raw
	}
	return nil
}

func (m *Model) openPath(path string, pushHistory bool) tea.Cmd {
	data, err := core.ReadMarkdownFile(path)
	if err != nil {
		m.statusMsg = "Cannot read: " + err.Error()
		return nil
	}
	if pushHistory && m.currentPath != "" {
		m.history = append(m.history, histEntry{path: m.currentPath, yOffset: m.viewport.YOffset})
		// A fresh navigation invalidates the forward stack.
		m.forward = nil
	}
	m.currentPath = path
	m.currentDir = filepath.Dir(path)
	m.rawMarkdown = string(data)
	m.renderCache = nil
	m.refreshMeta()
	m.matches = nil
	m.matchIdx = 0
	m.searchQuery = ""
	m.searchTerms = nil
	m.rerender()
	m.viewport.GotoTop()
	m.syncListSelection()
	if m.watchEnabled {
		m.watcher.Watch(path)
	}
	return m.prewarmImagesCmd()
}

// reloadCurrent re-reads the active document from disk and re-renders it in
// place, preserving the reader's scroll position (unlike openPath, which jumps
// to the top). When status is non-empty it briefly shows that message in the
// status bar. It is used by both the manual reload key and the live-reload
// watcher.
func (m *Model) reloadCurrent(status string) tea.Cmd {
	if m.currentPath == "" {
		return nil
	}
	data, err := core.ReadMarkdownFile(m.currentPath)
	if err != nil {
		m.statusMsg = "Reload failed: " + err.Error()
		return nil
	}
	y := m.viewport.YOffset
	m.rawMarkdown = string(data)
	m.renderCache = nil
	m.refreshMeta()
	m.matches = nil
	m.matchIdx = 0
	m.searchQuery = ""
	m.searchTerms = nil
	m.rerender()
	m.viewport.SetYOffset(y)
	return tea.Batch(m.flashStatus(status), m.prewarmImagesCmd())
}

// flashStatus shows a transient status-bar message that clears itself after a
// short delay. An empty message is a no-op. The returned command schedules the
// expiry; a newer flash supersedes an older one via the generation tag.
func (m *Model) flashStatus(msg string) tea.Cmd {
	if msg == "" {
		return nil
	}
	m.statusMsg = msg
	m.statusGen++
	gen := m.statusGen
	return tea.Tick(2500*time.Millisecond, func(time.Time) tea.Msg {
		return statusExpireMsg{gen: gen}
	})
}

// refreshWorkspace re-scans the workspace directory after a filesystem change
// and rebuilds the document navigator, preserving the current selection. While
// the navigator filter is active the visible (filtered) list is left untouched
// to avoid disrupting typing; the refreshed set is still recorded so the next
// unfiltered view reflects it.
func (m *Model) refreshWorkspace() {
	files, _ := core.ListMarkdownFiles(m.workspaceDir, m.cfg)
	if m.titlesLoaded || m.labelMode == "title" {
		core.PopulateTitles(files)
		m.titlesLoaded = true
	}
	m.workspace = files
	m.entryPath = entryPointPath(files)
	if m.focus == focusListFilter || m.listFilterInput != "" {
		return
	}
	m.list.SetDelegate(newDocDelegate(m.entryPath))
	m.list.SetItems(docItemsFrom(files, m.labelMode))
	m.syncListSelection()
}

// prewarmImagesCmd returns a command that fetches the current document's images
// in the background and, when done, asks for a re-render. The first render skips
// uncached remote images (drawing alt text) so opening a document never blocks
// on the network; this command fills the decode cache so the follow-up render
// shows the images. Returns nil when there is nothing to fetch.
func (m *Model) prewarmImagesCmd() tea.Cmd {
	r := m.imgRenderer
	if r == nil || !r.Enabled() {
		return nil
	}
	srcs := mdfmt.CollectImageSrcs(m.rawMarkdown)
	if len(srcs) == 0 {
		return nil
	}
	path := m.currentPath
	return func() tea.Msg {
		r.Prefetch(srcs)
		return imagesReadyMsg{path: path}
	}
}

func (m *Model) goBack() tea.Cmd {
	if len(m.history) == 0 {
		m.statusMsg = "No history"
		return nil
	}
	last := m.history[len(m.history)-1]
	m.history = m.history[:len(m.history)-1]
	// Record the current position so Forward can return here.
	m.forward = append(m.forward, histEntry{path: m.currentPath, yOffset: m.viewport.YOffset})
	cmd := m.openPath(last.path, false)
	m.viewport.SetYOffset(last.yOffset)
	return cmd
}

func (m *Model) goForward() tea.Cmd {
	if len(m.forward) == 0 {
		m.statusMsg = "No forward history"
		return nil
	}
	next := m.forward[len(m.forward)-1]
	m.forward = m.forward[:len(m.forward)-1]
	// Record the current position so Back can return here.
	m.history = append(m.history, histEntry{path: m.currentPath, yOffset: m.viewport.YOffset})
	cmd := m.openPath(next.path, false)
	m.viewport.SetYOffset(next.yOffset)
	return cmd
}

func (m *Model) toggleLabelMode() {
	if m.labelMode == "title" {
		m.labelMode = "filename"
	} else {
		m.labelMode = "title"
		m.ensureTitles()
	}
	m.list.SetItems(docItemsFrom(m.workspace, m.labelMode))
	m.syncListSelection()
}

// toggleExtendedSyntax flips the shared "extended" inline-syntax preference and
// persists it to state.jsonc (shared with the GUI). The terminal renderer cannot
// display math/sub/sup/highlight/inserted, so the content view is left unchanged;
// only the persisted preference and a status note are updated.
func (m *Model) toggleExtendedSyntax() {
	m.extendedSyntax = !m.extendedSyntax
	core.SaveViewerExtendedSyntax(m.extendedSyntax)
	if m.extendedSyntax {
		m.statusMsg = "Extended syntax: ON (applies in GUI)"
	} else {
		m.statusMsg = "Extended syntax: OFF (applies in GUI)"
	}
}

// toggleRemoteImages flips fetching of remote (http(s)) images for the current
// session only. It is intentionally not persisted, so every launch starts with
// remote images blocked; a document from an untrusted source therefore cannot
// silently fetch remote content until the user opts in with the 'i' key.
func (m *Model) toggleRemoteImages() (tea.Model, tea.Cmd) {
	m.cfg.ImagesRemote = !m.cfg.ImagesRemote
	if m.imgRenderer != nil {
		m.imgRenderer.SetAllowRemote(m.cfg.ImagesRemote)
	}
	if m.cfg.ImagesRemote {
		m.statusMsg = "Remote images: ON (this session)"
	} else {
		m.statusMsg = "Remote images: OFF"
	}
	m.rerender()
	return *m, m.prewarmImagesCmd()
}

// toggleWatch flips the active-document auto-reload watcher on or off at
// runtime. Turning it on arms the watcher for the current document; turning it
// off releases it so on-disk edits no longer trigger a reload. The choice is not
// persisted, mirroring the GUI toolbar toggle, and starts from cfg.LiveReload.
func (m *Model) toggleWatch() tea.Cmd {
	if m.watcher == nil {
		m.watchEnabled = false
		return m.flashStatus("Auto-reload unavailable")
	}
	m.watchEnabled = !m.watchEnabled
	if m.watchEnabled {
		if m.currentPath != "" {
			m.watcher.Watch(m.currentPath)
		}
		return m.flashStatus("Auto-reload: ON")
	}
	m.watcher.Unwatch()
	return m.flashStatus("Auto-reload: OFF")
}

// ensureTitles populates document titles lazily, scanning each markdown file at
// most once per session. Title extraction opens and scans every markdown file,
// so it must not run on content-independent UI actions (showing the navigator,
// toggling the label mode back and forth): repeating that file scan is what an
// external malware scanner can stall on.
func (m *Model) ensureTitles() {
	if m.titlesLoaded {
		return
	}
	core.PopulateTitles(m.workspace)
	m.titlesLoaded = true
}

// toggleNav shows or hides the document-navigator panel. The panel lists every
// markdown file in the workspace folder regardless of whether mdv was opened on
// a single file or on a folder, so name filtering ("/") and content search
// ("//") across all those documents are always available. Showing it focuses the
// list; hiding it returns to the content view.
func (m *Model) toggleNav() {
	if m.showList {
		m.listFilterInput = ""
		m.showList = false
		m.focus = focusContent
	} else {
		if len(m.workspace) == 0 {
			m.statusMsg = "No other documents in this folder"
			return
		}
		if m.labelMode == "title" {
			m.ensureTitles()
		}
		m.showList = true
		m.focus = focusList
		m.listFilterInput = ""
		m.resetNavList()
	}
	m.layout()
	m.rerender()
}

func (m *Model) syncListSelection() {
	for i, it := range m.list.Items() {
		if d, ok := it.(docItem); ok && d.doc.Path == m.currentPath {
			m.list.Select(i)
			return
		}
	}
}

// scrollToHeading moves the viewport to the first rendered line whose text
// matches the given slug, comparing GitHub-style slugs of heading lines.
func (m *Model) scrollToHeading(slug string) {
	target := core.BaseSlug(slug)
	headings := collectHeadings(m.rawMarkdown)

	// Find the heading text for this slug.
	var wantText string
	sl := core.NewSlugger()
	for _, h := range headings {
		if sl.Slug(h.Text) == target || core.BaseSlug(h.Text) == target {
			wantText = h.Text
			break
		}
	}
	if wantText == "" {
		m.statusMsg = "Anchor not found: #" + slug
		return
	}

	rendered := strings.Split(stripANSI(m.renderedRaw()), "\n")
	for i, line := range rendered {
		if strings.Contains(normalize(line), normalize(wantText)) {
			m.viewport.SetYOffset(i)
			m.statusMsg = "→ #" + slug
			return
		}
	}
	m.statusMsg = "Anchor not found: #" + slug
}

func (m *Model) runSearch(q string) {
	m.matches = nil
	m.matchIdx = 0
	m.searchQuery = ""
	m.searchTerms = nil
	if strings.TrimSpace(q) == "" {
		m.rerender()
		return
	}
	rendered := strings.Split(stripANSI(m.renderedRaw()), "\n")
	for i, line := range rendered {
		for _, sp := range core.MatchPhraseSpans(line, q) {
			m.matches = append(m.matches, matchPos{line: i, col: sp.Start, end: sp.End})
		}
	}
	if len(m.matches) == 0 {
		m.rerender()
		m.statusMsg = "No matches for: " + q
		return
	}
	m.searchQuery = q
	m.rerender()
	m.viewport.SetYOffset(m.matches[0].line)
	if len(m.renderedRaw()) > maxHighlightBytes {
		m.statusMsg = fmt.Sprintf("Match 1/%d for %q (large file: highlight off)", len(m.matches), q)
	} else {
		m.statusMsg = fmt.Sprintf("Match 1/%d for %q", len(m.matches), q)
	}
}

func (m *Model) jumpMatch(dir int) {
	if len(m.matches) == 0 {
		return
	}
	m.matchIdx = (m.matchIdx + dir + len(m.matches)) % len(m.matches)
	if m.searchQuery != "" || len(m.searchTerms) > 0 {
		m.rerender() // move the green current-match highlight
	}
	m.viewport.SetYOffset(m.matches[m.matchIdx].line)
	m.statusMsg = fmt.Sprintf("Match %d/%d", m.matchIdx+1, len(m.matches))
}

// runSearchMulti finds and highlights every occurrence of any of the given
// keywords in the rendered document, jumping to the first match. It is used when
// the user selects a content-search result in the navigator.
func (m *Model) runSearchMulti(keywords []string) {
	m.matches = nil
	m.matchIdx = 0
	m.searchQuery = ""
	m.searchTerms = nil
	if len(keywords) == 0 {
		m.rerender()
		return
	}
	rendered := strings.Split(stripANSI(m.renderedRaw()), "\n")
	for i, line := range rendered {
		ll := strings.ToLower(line)
		for _, kw := range keywords {
			for from := 0; ; {
				idx := strings.Index(ll[from:], kw)
				if idx < 0 {
					break
				}
				col := from + idx
				m.matches = append(m.matches, matchPos{line: i, col: col})
				from = col + len(kw)
			}
		}
	}
	if len(m.matches) == 0 {
		m.rerender()
		m.statusMsg = "No matches in document"
		return
	}
	// Order matches by line then column so navigation follows reading order.
	sort.Slice(m.matches, func(a, b int) bool {
		if m.matches[a].line != m.matches[b].line {
			return m.matches[a].line < m.matches[b].line
		}
		return m.matches[a].col < m.matches[b].col
	})
	m.searchTerms = keywords
	m.rerender()
	m.viewport.SetYOffset(m.matches[0].line)
	m.statusMsg = fmt.Sprintf("Match 1/%d  Enter/↓: next, ↑: prev", len(m.matches))
}

// --- rendering -------------------------------------------------------------

func (m *Model) contentWidth() int {
	w := m.width
	if m.showList {
		w -= sidebarWidth
	}
	if m.cfg.MaxWidth > 0 && w > m.cfg.MaxWidth {
		w = m.cfg.MaxWidth
	}
	if w < 20 {
		w = 20
	}
	return w
}

func (m *Model) layout() {
	contentH := m.height - 2 - m.statusBarHeight() // header + focus bar + status bar
	if contentH < 3 {
		contentH = 3
	}
	if !m.ready {
		m.viewport = viewport.New(m.contentWidth(), contentH)
	} else {
		m.viewport.Width = m.contentWidth()
		m.viewport.Height = contentH
	}
	if m.showList {
		m.list.SetSize(sidebarWidth, contentH)
		m.links.SetSize(sidebarWidth, contentH)
	} else {
		m.links.SetSize(m.width/2, contentH)
	}
}

// syncChromeHeight resizes the viewport (and sidebar lists) when the status
// bar's line count has changed since the last full layout, e.g. because a
// transient notification appeared on a narrow terminal and pushed the shortcut
// hints onto a second row. It is a no-op until the first window size is known
// and when the height is already correct.
func (m *Model) syncChromeHeight() {
	if !m.ready || m.height == 0 {
		return
	}
	contentH := m.height - 2 - m.statusBarHeight()
	if contentH < 3 {
		contentH = 3
	}
	if m.viewport.Height == contentH {
		return
	}
	m.viewport.Height = contentH
	if m.showList {
		m.list.SetSize(sidebarWidth, contentH)
		m.links.SetSize(sidebarWidth, contentH)
	} else {
		m.links.SetSize(m.width/2, contentH)
	}
}

func (m *Model) renderedRaw() string {
	w := m.contentWidth()
	key := fmt.Sprintf("%d|%s|%d|%t", w, m.resolvedStyle, len(m.rawMarkdown), m.metaExpanded)
	if out, ok := m.renderCache[key]; ok {
		return out
	}
	body, err := renderMarkdown(m.rawMarkdown, w-2, m.resolvedStyle, m.imageRenderer(), m.currentDir)
	if err != nil {
		body = m.rawMarkdown
	}
	out := m.frontmatterBlock(w-2) + body
	if m.renderCache == nil {
		m.renderCache = make(map[string]string)
	}
	m.renderCache[key] = out
	return out
}

// refreshMeta recomputes the cached count of extra front matter fields and
// collapses the metadata section, called whenever the current document changes.
func (m *Model) refreshMeta() {
	fm, _ := core.ExtractFrontmatter(m.rawMarkdown)
	m.metaHidden = len(fm.Fields)
	m.metaExpanded = false
}

// toggleMeta expands or collapses the extra front matter fields. It is a no-op
// when the document has no extra fields to reveal.
func (m *Model) toggleMeta() {
	if m.metaHidden == 0 {
		return
	}
	m.metaExpanded = !m.metaExpanded
	m.rerender()
}

// frontmatterBlock formats the current document's front matter into an
// unobtrusive ANSI block shown above the rendered body. The headline fields are
// always shown; the extra fields appear only when expanded, with a faint hint
// advertising the toggle. width is the body wrap column.
func (m *Model) frontmatterBlock(width int) string {
	fm, _ := core.ExtractFrontmatter(m.rawMarkdown)
	if !fm.Has {
		return ""
	}
	opt := mdfmt.FrontmatterOptions{
		Width:      width,
		Color:      os.Getenv("NO_COLOR") == "",
		ShowFields: m.metaExpanded,
	}
	if n := len(fm.Fields); n > 0 {
		if m.metaExpanded {
			opt.Hint = "m: hide details"
		} else {
			plural := "s"
			if n == 1 {
				plural = ""
			}
			opt.Hint = fmt.Sprintf("m: show %d more field%s", n, plural)
		}
	}
	return mdfmt.RenderFrontmatter(fm, opt)
}

func (m *Model) rerender() {
	content := m.renderedRaw()
	if len(content) <= maxHighlightBytes {
		curLine, curCol := -1, -1
		if len(m.matches) > 0 {
			curLine = m.matches[m.matchIdx].line
			curCol = m.matches[m.matchIdx].col
		}
		if len(m.searchTerms) > 0 {
			content = highlightTerms(content, m.searchTerms, curLine, curCol)
		} else if m.searchQuery != "" {
			content = highlightSpans(content, m.matches, m.matchIdx)
		}
	}
	m.viewport.SetContent(content)
}

func renderMarkdown(md string, width int, theme string, images mdfmt.ImageRenderer, baseDir string) (string, error) {
	if width < 20 {
		width = 20
	}
	style := "auto"
	switch theme {
	case "light":
		style = "light"
	case "dark":
		style = "dark"
	}
	// The TUI always renders to an interactive terminal, so emit OSC 8
	// hyperlinks (clickable links without visible URLs) unless colors are off.
	hyperlinks := os.Getenv("NO_COLOR") == ""
	return mdfmt.Render(md, width, style, hyperlinks, images, baseDir)
}

// imageRenderer builds the image renderer for the current document. The TUI uses
// the Unicode half-block renderer rather than a pixel protocol: inline graphics
// (kitty/iTerm2/sixel) live outside Bubble Tea's cell grid and would be
// overdrawn or smeared as the viewport scrolls, whereas half-blocks are just
// colored text the viewport can manage. Returns nil when images are disabled.
func (m *Model) imageRenderer() mdfmt.ImageRenderer {
	if termimg.ParseMode(m.cfg.Images) == termimg.ModeOff {
		return nil
	}
	if !termimg.SupportsColor(os.Stdout) {
		return nil
	}
	dir := m.currentDir
	if dir == "" {
		dir = "."
	}
	if m.imgRenderer == nil {
		r := termimg.NewRenderer(termimg.ProtocolBlocks, dir, m.cfg.ImagesRemote)
		// Skip uncached image loads on the render path; prewarmImagesCmd loads them
		// in the background so opening or re-rendering a document never blocks on a
		// file read, SVG rasterization, or network fetch (any of which an external
		// scanner can stall).
		r.SetDeferLoad(true)
		m.imgRenderer = r
	} else {
		m.imgRenderer.SetBaseDir(dir)
	}
	return m.imgRenderer
}

// View implements tea.Model.
func (m Model) View() string {
	if !m.ready {
		return "Loading…"
	}

	var body string
	switch {
	case m.focus == focusLinks:
		picker := m.links.View()
		if m.showList {
			body = lipgloss.JoinHorizontal(lipgloss.Top, m.sidebar(), picker)
		} else {
			body = picker
		}
	case m.showList:
		body = lipgloss.JoinHorizontal(lipgloss.Top, m.sidebar(), m.viewport.View())
	default:
		body = m.viewport.View()
	}

	return strings.Join([]string{m.header(), body, m.focusBar(), m.statusBar()}, "\n")
}

// focusBar renders a one-line indicator beneath the views showing which panel is
// active: a blue bar sits under the focused view, a dim bar under the inactive
// one. With only the content view visible the whole bar is blue.
func (m Model) focusBar() string {
	active := lipgloss.NewStyle().Background(lipgloss.Color("12"))    // bright blue
	inactive := lipgloss.NewStyle().Background(lipgloss.Color("238")) // dim grey
	barSeg := func(style lipgloss.Style, w int) string {
		if w < 0 {
			w = 0
		}
		return style.Width(w).Render(strings.Repeat(" ", w))
	}
	if !m.showList {
		return barSeg(active, m.width)
	}
	navActive := m.focus == focusList || m.focus == focusListFilter
	leftStyle, rightStyle := inactive, inactive
	if navActive {
		leftStyle = active
	} else {
		rightStyle = active
	}
	return barSeg(leftStyle, sidebarWidth) + barSeg(rightStyle, m.width-sidebarWidth)
}

func (m Model) sidebar() string {
	style := lipgloss.NewStyle().Width(sidebarWidth).Height(m.viewport.Height)
	return style.Render(m.list.View())
}

func (m Model) header() string {
	name := "(no document)"
	if m.currentPath != "" {
		name = filepath.Base(m.currentPath)
	} else if m.stdin {
		name = "(stdin)"
	}
	left := lipgloss.NewStyle().Bold(true).Render(" " + core.AppName + " ")
	mid := lipgloss.NewStyle().Faint(true).Render(name)
	return lipgloss.NewStyle().Width(m.width).Render(left + " " + mid)
}

func (m Model) statusBar() string {
	if m.focus == focusSearch {
		hint := "Enter: search, Esc: cancel"
		if m.searchInput == "" {
			hint = "find in page - type / for content search, Esc: cancel"
		}
		if m.searchInput == m.searchQuery && m.searchQuery != "" {
			if len(m.matches) > 0 {
				hint = fmt.Sprintf("%d/%d  Enter/↓: next, ↑: prev, Esc: done", m.matchIdx+1, len(m.matches))
			} else {
				hint = "no matches, Esc: cancel"
			}
		}
		return lipgloss.NewStyle().Width(m.width).Reverse(true).
			Render(" /" + m.searchInput + "▏ (" + hint + ") ")
	}

	if m.focus == focusListFilter {
		hint := "name filter - type // to search content, Enter: open, Esc: cancel"
		if strings.HasPrefix(m.listFilterInput, "/") {
			hint = "content search - Enter: open match, Esc: cancel"
		}
		return lipgloss.NewStyle().Width(m.width).Reverse(true).
			Render(" /" + m.listFilterInput + "▏ (" + hint + ") ")
	}

	left, status, hints, flash := m.statusParts()
	bar := lipgloss.NewStyle().Reverse(true)

	// The bar is reverse-video; render the transient status in the primary
	// accent (blue) so it stands out the way the GUI's flash notification does.
	// Using Reverse+Background keeps the segment's background identical to the
	// rest of the bar (the terminal's default foreground), so only the glyphs
	// turn blue instead of sitting in an inverted dark block.
	styleStatus := func(s string) string {
		if s == "" {
			return ""
		}
		if flash {
			return lipgloss.NewStyle().Reverse(true).Background(lipgloss.Color("12")).Bold(true).Render(s)
		}
		return bar.Render(s)
	}

	right := " " + hints + " "
	// One line when the percentage, notification and shortcut hints all fit;
	// otherwise stack the percentage+notification above the hints so a narrow
	// terminal never truncates the shortcuts or a "Document changed" flash.
	if lipgloss.Width(left)+lipgloss.Width(status)+1+lipgloss.Width(right) <= m.width {
		gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - lipgloss.Width(status)
		if gap < 1 {
			gap = 1
		}
		line := bar.Render(left) + styleStatus(status) + bar.Render(strings.Repeat(" ", gap)+right)
		return lipgloss.NewStyle().MaxWidth(m.width).Render(line)
	}

	topPad := m.width - lipgloss.Width(left) - lipgloss.Width(status)
	if topPad < 0 {
		topPad = 0
	}
	rows := []string{lipgloss.NewStyle().MaxWidth(m.width).
		Render(bar.Render(left) + styleStatus(status) + bar.Render(strings.Repeat(" ", topPad)))}

	// Wrap the shortcut hints across as many rows as needed so a narrow terminal
	// never truncates them; each wrapped row fills the full width.
	for _, h := range wrapHints(hints, m.width) {
		hintsLine := " " + h + " "
		botPad := m.width - lipgloss.Width(hintsLine)
		if botPad < 0 {
			botPad = 0
		}
		rows = append(rows, lipgloss.NewStyle().MaxWidth(m.width).
			Render(bar.Render(hintsLine+strings.Repeat(" ", botPad))))
	}
	return strings.Join(rows, "\n")
}

// wrapHints splits the double-space-separated shortcut groups into the fewest
// lines that each fit within width (accounting for the one-space padding the
// status bar adds on either side). A group wider than width is left on its own
// line rather than being broken mid-token.
func wrapHints(hints string, width int) []string {
	if width <= 0 {
		return []string{hints}
	}
	var lines []string
	cur := ""
	for _, g := range strings.Split(hints, "  ") {
		if g == "" {
			continue
		}
		candidate := g
		if cur != "" {
			candidate = cur + "  " + g
		}
		if cur == "" || lipgloss.Width(" "+candidate+" ") <= width {
			cur = candidate
			continue
		}
		lines = append(lines, cur)
		cur = g
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

// statusParts computes the three segments of the main (content/navigator) status
// bar: the scroll percentage (left), the transient status / update notification
// (status), and the keyboard-shortcut hints. flash reports whether status is a
// transient user-facing message that should be highlighted in the accent colour.
func (m Model) statusParts() (left, status, hints string, flash bool) {
	pct := 0
	if m.viewport.TotalLineCount() > 0 {
		pct = int(m.viewport.ScrollPercent() * 100)
	}
	left = fmt.Sprintf(" %d%% ", pct)

	navActive := m.focus == focusList || m.focus == focusListFilter
	metaHint := ""
	if m.metaHidden > 0 {
		metaHint = "m:meta  "
	}
	watchHint := "w:watch(off)"
	if m.watchEnabled {
		watchHint = "w:watch(on)"
	}
	switch {
	case navActive:
		hints = "^b:nav  tab:switch  enter:open  /:filter  //:content(all files)  t:titles  x:ext  q:quit"
	case m.showList:
		hints = "^b:nav  tab:switch  /:content  //:content(all files)  b:back  f:fwd  l:links  " + metaHint + "i:img  r:reload  " + watchHint + "  x:ext  q:quit"
	default:
		hints = "^b:nav  /:content  //:content(all files)  b:back  f:fwd  l:links  " + metaHint + "i:img  r:reload  " + watchHint + "  x:ext  q:quit"
	}

	status = m.statusMsg
	flash = m.statusMsg != ""
	if status == "" && m.update.Available {
		status = fmt.Sprintf("New version %s, run `mdv update`", m.update.Latest)
	}
	return left, status, hints, flash
}

// statusBarHeight reports how many terminal rows the status bar occupies: two
// when the content view's percentage, notification and shortcut hints cannot
// share one line, otherwise one. The search and navigator-filter bars are
// always a single line.
func (m Model) statusBarHeight() int {
	if m.focus == focusSearch || m.focus == focusListFilter {
		return 1
	}
	left, status, hints, _ := m.statusParts()
	right := " " + hints + " "
	if lipgloss.Width(left)+lipgloss.Width(status)+1+lipgloss.Width(right) <= m.width {
		return 1
	}
	// One row for the percentage/notification plus however many rows the wrapped
	// shortcut hints require.
	return 1 + len(wrapHints(hints, m.width))
}

// collectHeadings extracts ATX headings from markdown (ignoring fenced code).
func collectHeadings(md string) []Heading {
	var hs []Heading
	inFence := false
	for _, line := range strings.Split(md, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if mt := reATX.FindStringSubmatch(line); mt != nil {
			hs = append(hs, Heading{Level: len(mt[1]), Text: strings.TrimSpace(mt[2])})
		}
	}
	return hs
}

var reANSI = regexpMustCompileANSI()

func stripANSI(s string) string { return reANSI.ReplaceAllString(s, "") }

// highlightTerm wraps every case-insensitive occurrence of term in the ANSI
// styled text so matches stand out while searching. The single occurrence at
// (currentLine, currentCol) - both 0-based, with currentCol the visible-column
// offset of the match start - gets a green background; every other occurrence
// gets a yellow background. Pass currentLine = -1 for no current match.
// Matching is performed on the visible characters only; embedded escape
// sequences are skipped. Each line is processed independently.
func highlightTerm(styled, term string, currentLine, currentCol int) string {
	if term == "" {
		return styled
	}
	lowerTerm := strings.ToLower(term)
	lines := strings.Split(styled, "\n")
	for i, line := range lines {
		cc := -1
		if i == currentLine {
			cc = currentCol
		}
		lines[i] = highlightLine(line, lowerTerm, cc)
	}
	return strings.Join(lines, "\n")
}

// highlightLine highlights every occurrence of lowerTerm (already lowercased)
// within a single ANSI-styled line. The occurrence whose visible start column
// equals currentCol is rendered with a green background; all others yellow.
// Pass currentCol = -1 when no occurrence on this line is the current match.
func highlightLine(line, lowerTerm string, currentCol int) string {
	// Decompose into visible bytes plus a map back to their byte offset in the
	// original line, skipping ANSI escape sequences.
	var vis []byte
	var off []int
	for i := 0; i < len(line); {
		if loc := reANSI.FindStringIndex(line[i:]); loc != nil && loc[0] == 0 {
			i += loc[1]
			continue
		}
		vis = append(vis, line[i])
		off = append(off, i)
		i++
	}
	visLower := strings.ToLower(string(vis))
	// Case folding that changes byte length would break the offset map; skip
	// highlighting such (rare, non-ASCII) lines rather than corrupt them.
	if len(visLower) != len(vis) {
		return line
	}

	starts := map[int]bool{}
	current := map[int]bool{} // start byte offsets that are the current match
	ends := map[int]bool{}
	found := false
	for from := 0; ; {
		idx := strings.Index(visLower[from:], lowerTerm)
		if idx < 0 {
			break
		}
		s := from + idx
		e := s + len(lowerTerm)
		startByte := off[s]
		endByte := len(line)
		if e < len(off) {
			endByte = off[e]
		}
		starts[startByte] = true
		if s == currentCol {
			current[startByte] = true
		}
		ends[endByte] = true
		found = true
		from = e
	}
	if !found {
		return line
	}

	const yellow = "\x1b[30;43m" // black foreground, yellow background
	const green = "\x1b[30;42m"  // black foreground, green background
	const off2 = "\x1b[39;49m"   // restore default foreground and background
	var b strings.Builder
	for i := 0; i <= len(line); i++ {
		if ends[i] {
			b.WriteString(off2)
		}
		if starts[i] {
			if current[i] {
				b.WriteString(green)
			} else {
				b.WriteString(yellow)
			}
		}
		if i < len(line) {
			b.WriteByte(line[i])
		}
	}
	return b.String()
}

func normalize(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// hlSpan is a visible-column byte range to highlight on a single line, flagged
// as the current match (green) or not (yellow).
type hlSpan struct {
	s, e int
	cur  bool
}

// highlightSpans highlights each matched phrase span recorded in matches across
// the ANSI-styled content. The span at index currentIdx is rendered with a green
// background; all others yellow. Spans use visible-column byte offsets, matching
// the ANSI-stripped text the phrase search ran against.
func highlightSpans(styled string, matches []matchPos, currentIdx int) string {
	if len(matches) == 0 {
		return styled
	}
	byLine := map[int][]hlSpan{}
	for idx, mt := range matches {
		byLine[mt.line] = append(byLine[mt.line], hlSpan{s: mt.col, e: mt.end, cur: idx == currentIdx})
	}
	lines := strings.Split(styled, "\n")
	for i := range lines {
		spans, ok := byLine[i]
		if !ok {
			continue
		}
		lines[i] = highlightSpanLine(lines[i], spans)
	}
	return strings.Join(lines, "\n")
}

// highlightSpanLine paints the given visible-column spans onto a single
// ANSI-styled line, mapping visible offsets back to byte offsets so existing
// escape sequences are preserved. Current spans are green, the rest yellow.
func highlightSpanLine(line string, spans []hlSpan) string {
	var vis []byte
	var off []int
	for i := 0; i < len(line); {
		if loc := reANSI.FindStringIndex(line[i:]); loc != nil && loc[0] == 0 {
			i += loc[1]
			continue
		}
		vis = append(vis, line[i])
		off = append(off, i)
		i++
	}
	n := len(vis)

	starts := map[int]bool{}
	current := map[int]bool{}
	ends := map[int]bool{}
	found := false
	for _, sp := range spans {
		if sp.s < 0 || sp.s >= n || sp.e <= sp.s {
			continue
		}
		startByte := off[sp.s]
		endByte := len(line)
		if sp.e < len(off) {
			endByte = off[sp.e]
		}
		starts[startByte] = true
		if sp.cur {
			current[startByte] = true
		}
		ends[endByte] = true
		found = true
	}
	if !found {
		return line
	}

	const yellow = "\x1b[30;43m" // black foreground, yellow background
	const green = "\x1b[30;42m"  // black foreground, green background
	const off2 = "\x1b[39;49m"   // restore default foreground and background
	var b strings.Builder
	for i := 0; i <= len(line); i++ {
		if ends[i] {
			b.WriteString(off2)
		}
		if starts[i] {
			if current[i] {
				b.WriteString(green)
			} else {
				b.WriteString(yellow)
			}
		}
		if i < len(line) {
			b.WriteByte(line[i])
		}
	}
	return b.String()
}

// highlightTerms highlights every occurrence of any of the given terms across
// the ANSI-styled text, with the single occurrence at (currentLine, currentCol)
// rendered in green and all others in yellow. It is the multi-keyword companion
// to highlightTerm, used by the content-search jump.
func highlightTerms(styled string, terms []string, currentLine, currentCol int) string {
	if len(terms) == 0 {
		return styled
	}
	lower := make([]string, 0, len(terms))
	for _, t := range terms {
		if t != "" {
			lower = append(lower, strings.ToLower(t))
		}
	}
	if len(lower) == 0 {
		return styled
	}
	lines := strings.Split(styled, "\n")
	for i, line := range lines {
		cc := -1
		if i == currentLine {
			cc = currentCol
		}
		lines[i] = highlightLineMulti(line, lower, cc)
	}
	return strings.Join(lines, "\n")
}

// highlightLineMulti highlights every occurrence of any term (already
// lowercased) within a single ANSI-styled line, merging overlapping matches.
// The occurrence whose visible start column equals currentCol is green; the rest
// yellow. Pass currentCol = -1 when no occurrence on this line is current.
func highlightLineMulti(line string, lowerTerms []string, currentCol int) string {
	var vis []byte
	var off []int
	for i := 0; i < len(line); {
		if loc := reANSI.FindStringIndex(line[i:]); loc != nil && loc[0] == 0 {
			i += loc[1]
			continue
		}
		vis = append(vis, line[i])
		off = append(off, i)
		i++
	}
	visLower := strings.ToLower(string(vis))
	if len(visLower) != len(vis) {
		return line
	}

	type iv struct {
		s, e int
		cur  bool
	}
	var ivs []iv
	for _, t := range lowerTerms {
		if t == "" {
			continue
		}
		for from := 0; ; {
			idx := strings.Index(visLower[from:], t)
			if idx < 0 {
				break
			}
			s := from + idx
			e := s + len(t)
			ivs = append(ivs, iv{s: s, e: e, cur: s == currentCol})
			from = e
		}
	}
	if len(ivs) == 0 {
		return line
	}
	sort.Slice(ivs, func(a, b int) bool { return ivs[a].s < ivs[b].s })
	merged := []iv{ivs[0]}
	for _, v := range ivs[1:] {
		last := &merged[len(merged)-1]
		if v.s <= last.e {
			if v.e > last.e {
				last.e = v.e
			}
			if v.cur {
				last.cur = true
			}
		} else {
			merged = append(merged, v)
		}
	}

	starts := map[int]bool{}
	current := map[int]bool{}
	ends := map[int]bool{}
	for _, v := range merged {
		startByte := off[v.s]
		endByte := len(line)
		if v.e < len(off) {
			endByte = off[v.e]
		}
		starts[startByte] = true
		if v.cur {
			current[startByte] = true
		}
		ends[endByte] = true
	}

	const yellow = "\x1b[30;43m"
	const green = "\x1b[30;42m"
	const off2 = "\x1b[39;49m"
	var b strings.Builder
	for i := 0; i <= len(line); i++ {
		if ends[i] {
			b.WriteString(off2)
		}
		if starts[i] {
			if current[i] {
				b.WriteString(green)
			} else {
				b.WriteString(yellow)
			}
		}
		if i < len(line) {
			b.WriteByte(line[i])
		}
	}
	return b.String()
}
