// Package tui implements the interactive terminal user interface used when no
// graphical environment is available (or when --tui is requested). It is built
// on Bubble Tea and depends only on pure-Go libraries, so it runs in headless
// containers over SSH.
package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/thgossler/mdv/internal/core"
	"github.com/thgossler/mdv/internal/mdfmt"
	"golang.org/x/term"
)

type focus int

const (
	focusContent focus = iota
	focusList
	focusLinks
	focusSearch
)

type histEntry struct {
	path    string
	yOffset int
}

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

	history   []histEntry
	focus     focus
	showList  bool
	labelMode string // "filename" | "title"

	searchInput string
	matches     []int
	matchIdx    int

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
	return d.doc.Name
}
func (d docItem) Description() string { return d.doc.Path }
func (d docItem) FilterValue() string { return d.doc.Name + " " + d.doc.Title }

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
	linkList.Title = "Links — Enter to follow, Esc to cancel"
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
		if m.showList {
			m.focus = focusList
		}
		return *m, nil
	case "b", "backspace", "left", "alt+left":
		m.goBack()
		return *m, nil
	case "enter", "o":
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
	case "n":
		m.jumpMatch(1)
		return *m, nil
	case "N":
		m.jumpMatch(-1)
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
		m.focus = focusContent
		m.searchInput = ""
		return *m, nil
	case "enter":
		m.runSearch(m.searchInput)
		m.focus = focusContent
		return *m, nil
	case "backspace":
		if len(m.searchInput) > 0 {
			m.searchInput = m.searchInput[:len(m.searchInput)-1]
		}
		return *m, nil
	default:
		if len(msg.Runes) > 0 {
			m.searchInput += string(msg.Runes)
		}
		return *m, nil
	}
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
	target := core.ResolveLink(href, m.currentDir, m.cfg, m.workspace)
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
	data, err := os.ReadFile(path)
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
	m.matches = nil
	m.matchIdx = 0
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
	if strings.TrimSpace(q) == "" {
		return
	}
	rendered := strings.Split(stripANSI(m.renderedRaw()), "\n")
	ql := strings.ToLower(q)
	for i, line := range rendered {
		if strings.Contains(strings.ToLower(line), ql) {
			m.matches = append(m.matches, i)
		}
	}
	if len(m.matches) == 0 {
		m.statusMsg = "No matches for: " + q
		return
	}
	m.viewport.SetYOffset(m.matches[0])
	m.statusMsg = fmt.Sprintf("Match 1/%d for %q", len(m.matches), q)
}

func (m *Model) jumpMatch(dir int) {
	if len(m.matches) == 0 {
		return
	}
	m.matchIdx = (m.matchIdx + dir + len(m.matches)) % len(m.matches)
	m.viewport.SetYOffset(m.matches[m.matchIdx])
	m.statusMsg = fmt.Sprintf("Match %d/%d", m.matchIdx+1, len(m.matches))
}

// --- rendering -------------------------------------------------------------

func (m *Model) contentWidth() int {
	w := m.width
	if m.showList {
		w -= sidebarWidth
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
	out, err := renderMarkdown(m.rawMarkdown, m.contentWidth()-2, m.cfg.Theme)
	if err != nil {
		return m.rawMarkdown
	}
	return out
}

func (m *Model) rerender() {
	m.viewport.SetContent(m.renderedRaw())
}

func renderMarkdown(md string, width int, theme string) (string, error) {
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
	return mdfmt.Render(md, width, style, hyperlinks)
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
		return lipgloss.NewStyle().Width(m.width).Reverse(true).
			Render(" /" + m.searchInput + "▏ (Enter to search, Esc to cancel) ")
	}

	pct := 0
	if m.viewport.TotalLineCount() > 0 {
		pct = int(m.viewport.ScrollPercent() * 100)
	}

	hints := "b:back  o:links  t:labels  /:find  q:quit"
	if m.showList {
		hints = "tab:switch  enter:open  " + hints
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

func normalize(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}
