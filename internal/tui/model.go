// Package tui implements the interactive terminal user interface used when no
// graphical environment is available (or when --tui is requested). It is built
// on Bubble Tea and depends only on pure-Go libraries, so it runs in headless
// containers over SSH.
package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/thgossler/mdv/internal/core"
	"github.com/thgossler/mdv/internal/mdfmt"
	"github.com/thgossler/mdv/internal/termimg"
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

	// renderCache holds the glamour-rendered markdown for renderCacheKey so
	// that re-rendering for search highlighting (a frequent operation) does not
	// re-invoke glamour. Re-invoking glamour with the "auto" style re-queries
	// the terminal background colour, and that response leaks into key input.
	renderCache    string
	renderCacheKey string

	history   []histEntry
	focus     focus
	showList  bool
	labelMode string // "filename" | "title"

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

	width  int
	height int
	ready  bool
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
		cfg:          cfg,
		workspaceDir: in.Dir,
		labelMode:    cfg.NavLabelMode,
		update:       upd,
		focus:        focusContent,
	}

	if in.Kind == core.InputFolder {
		m.showList = true
		m.focus = focusList
		files, _ := core.ListMarkdownFiles(in.Dir, cfg)
		if cfg.NavLabelMode == "title" {
			core.PopulateTitles(files)
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

	fileList := list.New(docItemsFrom(m.workspace, m.labelMode), list.NewDefaultDelegate(), 0, 0)
	fileList.Title = "Documents"
	fileList.SetShowHelp(false)
	fileList.SetShowStatusBar(false)
	m.list = fileList

	linkList := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	linkList.Title = "Links - Enter to follow, Esc to cancel"
	linkList.SetShowHelp(false)
	m.links = linkList

	return m
}

func docItemsFrom(files []core.DocFile, labelMode string) []list.Item {
	items := make([]list.Item, len(files))
	for i, f := range files {
		items[i] = docItem{doc: f, labelMode: labelMode}
	}
	return items
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Run starts the program and blocks until the user quits.
func Run(cfg core.Defaults, in core.Input, upd core.UpdateInfo) error {
	m := New(cfg, in, upd)
	opts := []tea.ProgramOption{tea.WithAltScreen(), tea.WithMouseCellMotion()}
	// When markdown is piped on stdin, os.Stdin carries the document, not the
	// keyboard. Reopen the controlling terminal so the TUI still receives key
	// and mouse input.
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		if tty, err := openControllingTerminal(); err == nil {
			opts = append(opts, tea.WithInput(tty))
			defer tty.Close()
		}
	}
	p := tea.NewProgram(m, opts...)
	_, err := p.Run()
	return err
}

// openControllingTerminal opens the process's controlling terminal for reading,
// used to source keyboard input when stdin is a pipe.
func openControllingTerminal() (*os.File, error) {
	name := "/dev/tty"
	if runtime.GOOS == "windows" {
		name = "CONIN$"
	}
	return os.OpenFile(name, os.O_RDWR, 0)
}

const sidebarWidth = 34

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layout()
		if !m.ready {
			m.ready = true
			if m.currentPath != "" {
				m.openPath(m.currentPath, true)
			} else if m.stdin {
				m.rerender()
				m.viewport.GotoTop()
			}
		} else {
			m.rerender()
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
		m.goBack()
		return *m, nil
	case "enter", "l":
		m.openLinkPicker()
		return *m, nil
	case "t":
		m.toggleLabelMode()
		return *m, nil
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
	case "ctrl+b", "esc":
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
			m.openPath(it.doc.Path, true)
			m.focus = focusContent
		}
		return *m, nil
	case "t":
		m.toggleLabelMode()
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
		switch it := m.list.SelectedItem().(type) {
		case navItem:
			if it.kind == navMatch {
				m.openContentMatch(it)
			} else {
				m.openPath(it.doc.Path, true)
				m.focus = focusContent
				m.resetNavList()
			}
		case docItem:
			m.openPath(it.doc.Path, true)
			m.focus = focusContent
			m.resetNavList()
		}
		return *m, nil
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
			m.followLink(it.link.Href)
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
			core.PopulateTitles(m.workspace)
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
func (m *Model) openContentMatch(it navItem) {
	m.openPath(it.doc.Path, true)
	m.focus = focusContent
	m.resetNavList()
	m.runSearchMulti(it.keywords)
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

func (m *Model) followLink(href string) {
	target := core.ResolveLink(href, m.currentDir, m.workspaceDir, m.cfg, m.workspace)
	switch target.Kind {
	case core.LinkMarkdown, core.LinkWikiInternal:
		m.openPath(target.Resolved, true)
		if target.Fragment != "" {
			m.scrollToHeading(target.Fragment)
		}
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
}

func (m *Model) openPath(path string, pushHistory bool) {
	data, err := core.ReadMarkdownFile(path)
	if err != nil {
		m.statusMsg = "Cannot read: " + err.Error()
		return
	}
	if pushHistory && m.currentPath != "" {
		m.history = append(m.history, histEntry{path: m.currentPath, yOffset: m.viewport.YOffset})
	}
	m.currentPath = path
	m.currentDir = filepath.Dir(path)
	m.rawMarkdown = string(data)
	m.renderCache = ""
	m.matches = nil
	m.matchIdx = 0
	m.searchQuery = ""
	m.searchTerms = nil
	m.rerender()
	m.viewport.GotoTop()
	m.syncListSelection()
}

func (m *Model) goBack() {
	if len(m.history) == 0 {
		m.statusMsg = "No history"
		return
	}
	last := m.history[len(m.history)-1]
	m.history = m.history[:len(m.history)-1]
	m.openPath(last.path, false)
	m.viewport.SetYOffset(last.yOffset)
}

func (m *Model) toggleLabelMode() {
	if m.labelMode == "title" {
		m.labelMode = "filename"
	} else {
		m.labelMode = "title"
		core.PopulateTitles(m.workspace)
	}
	m.list.SetItems(docItemsFrom(m.workspace, m.labelMode))
	m.syncListSelection()
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
			core.PopulateTitles(m.workspace)
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
	contentH := m.height - 2 // status bar + header
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
		m.list.SetSize(sidebarWidth, contentH+1)
		m.links.SetSize(sidebarWidth, contentH+1)
	} else {
		m.links.SetSize(m.width/2, m.height-2)
	}
}

func (m *Model) renderedRaw() string {
	w := m.contentWidth()
	key := fmt.Sprintf("%d|%s|%d", w, m.cfg.Theme, len(m.rawMarkdown))
	if m.renderCache != "" && m.renderCacheKey == key {
		return m.renderCache
	}
	out, err := renderMarkdown(m.rawMarkdown, w-2, m.cfg.Theme, m.imageRenderer(), m.currentDir)
	if err != nil {
		out = m.rawMarkdown
	}
	m.renderCache = out
	m.renderCacheKey = key
	return out
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
	r := termimg.NewRenderer(termimg.ProtocolBlocks, dir, m.cfg.ImagesRemote)
	return r
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

	return strings.Join([]string{m.header(), body, m.statusBar()}, "\n")
}

func (m Model) sidebar() string {
	style := lipgloss.NewStyle().Width(sidebarWidth).Height(m.viewport.Height + 1)
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

	pct := 0
	if m.viewport.TotalLineCount() > 0 {
		pct = int(m.viewport.ScrollPercent() * 100)
	}

	hints := "tab:nav  /:find  //:content  b:back  l:links  q:quit"
	if m.showList {
		hints = "tab:switch  enter:open  /:filter  //:content  t:titles  esc:hide  q:quit"
	}

	status := m.statusMsg
	if status == "" && m.update.Available {
		status = fmt.Sprintf("Update %s available → %s", m.update.Latest, m.update.DownloadURL)
	}

	left := fmt.Sprintf(" %d%% ", pct)
	right := " " + hints + " "
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - lipgloss.Width(status) - 2
	if gap < 1 {
		gap = 1
	}
	line := left + status + strings.Repeat(" ", gap) + right
	return lipgloss.NewStyle().Width(m.width).Reverse(true).Render(line)
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
