package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/thgossler/mdv/internal/core"
	"github.com/thgossler/mdv/internal/pdf"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// Bridge is the Wails service exposed to the frontend. Its exported methods are
// callable from TypeScript and provide all filesystem, link-resolution and
// navigation logic, keeping the webview a pure presentation layer.
type Bridge struct {
	cfg       core.Defaults
	input     core.Input
	workspace []core.DocFile
	watcher   *Watcher
	layout    *LayoutStore
	window    *application.WebviewWindow
	app       *application.App

	// pickOnInit requests that Init present a native "open file or folder"
	// dialog before bootstrapping. It is set when mdv was started with no input
	// but a GUI is shown (e.g. double-clicked from Finder/Explorer).
	pickOnInit bool

	// emit dispatches a named application event with structured data to the
	// frontend. It is wired in main.go after the app is created.
	emit func(name string, data any)

	// searchMu guards searchGen and searchCancel.
	searchMu     sync.Mutex
	searchGen    uint64
	searchCancel context.CancelFunc

	// excludeMu guards the navigator exclusion state below.
	excludeMu       sync.Mutex
	excludePatterns []string
	excludeEnabled  bool

	// watchEnabled is the runtime auto-reload toggle for the active document. It
	// starts from cfg.LiveReload and can be flipped from the toolbar at runtime.
	watchEnabled bool
}

// NewBridge builds a Bridge for the given input and configuration.
func NewBridge(cfg core.Defaults, in core.Input) *Bridge {
	b := &Bridge{cfg: cfg, input: in, watchEnabled: cfg.LiveReload}
	b.workspace, _ = core.ListMarkdownFiles(in.Dir, cfg)
	core.PopulateTitles(b.workspace)
	return b
}

// DocFileDTO is the frontend-facing shape of a workspace document.
type DocFileDTO struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Title string `json:"title"`
	Rel   string `json:"rel"`
}

// InitInfo is returned on startup to bootstrap the UI.
type InitInfo struct {
	Kind      string        `json:"kind"` // "file" | "folder"
	Path      string        `json:"path"`
	Dir       string        `json:"dir"`
	Fragment  string        `json:"fragment"`
	AppName   string        `json:"appName"`
	Version   string        `json:"version"`
	Author    string        `json:"author"`
	Copyright string        `json:"copyright"`
	RepoURL   string        `json:"repoURL"`
	Config    core.Defaults `json:"config"`
	Workspace []DocFileDTO  `json:"workspace"`
	Update    UpdateDTO     `json:"update"`
	Layout    LayoutDTO     `json:"layout"`
	// Recent is the rolling list of recently opened files and folders (mixed,
	// most recent first), surfaced in the toolbar's recents drop-down.
	Recent []RecentItem `json:"recent"`
	// ExtendedSyntax is the effective state of the opt-in "extended" inline
	// Markdown syntax (math, sub/sup, highlight, inserted): the persisted runtime
	// choice from state.jsonc if the user ever toggled it, otherwise the
	// settings.jsonc default.
	ExtendedSyntax bool `json:"extendedSyntax"`
}

// LayoutDTO carries the persisted side-panel widths (in pixels) so the frontend
// can apply them before the first paint, avoiding panels jumping after start.
type LayoutDTO struct {
	SidebarWidth int `json:"sidebarWidth"`
	TocWidth     int `json:"tocWidth"`
	// ExcludePatterns is the persisted navigator exclusion text (one pattern per
	// line) and ExcludeEnabled whether it is currently applied.
	ExcludePatterns string `json:"excludePatterns"`
	ExcludeEnabled  bool   `json:"excludeEnabled"`
}

// UpdateDTO carries version-check results to the status bar.
type UpdateDTO struct {
	Available   bool   `json:"available"`
	Latest      string `json:"latest"`
	DownloadURL string `json:"downloadUrl"`
}

// Init returns the bootstrap information for the frontend.
func (b *Bridge) Init() InitInfo {
	if b.pickOnInit {
		b.pickOnInit = false
		if !b.promptForInput() {
			// The user cancelled the picker: quit. The window is closing, so a
			// minimal InitInfo is enough for the frontend that requested it.
			if b.app != nil {
				b.app.Quit()
			}
			return InitInfo{AppName: core.AppName, Version: core.Version, Config: b.cfg}
		}
	}
	kind := "file"
	if b.input.Kind == core.InputFolder {
		kind = "folder"
	}
	b.recordRecent()
	return InitInfo{
		Kind:      kind,
		Path:      b.input.Path,
		Dir:       b.input.Dir,
		Fragment:  b.input.Fragment,
		AppName:   core.AppName,
		Version:   core.Version,
		Author:    core.AppAuthor,
		Copyright: core.AppCopyright,
		RepoURL:   "https://github.com/" + core.DefaultSettings().UpdateRepo,
		Config:    b.cfg,
		Workspace: b.workspaceDTO(),
		Update:    b.checkUpdate(),
		Layout:    b.layoutDTO(),
		Recent:    b.recentList(),
		ExtendedSyntax: b.effectiveExtendedSyntax(),
	}
}

// Reinit re-resolves path as the program's input and refreshes the workspace
// listing, so a file or folder dropped onto the window replaces what mdv is
// viewing without restarting the process. It returns fresh bootstrap info (the
// network update check is skipped); live UI settings are preserved by the
// frontend. An unreadable selection leaves the current input unchanged.
func (b *Bridge) Reinit(path string) InitInfo {
	if in, err := core.ResolveInput(path); err == nil && in.Kind != core.InputNone {
		b.input = in
		b.workspace, _ = core.ListMarkdownFiles(in.Dir, b.cfg)
		core.PopulateTitles(b.workspace)
	}
	kind := "file"
	if b.input.Kind == core.InputFolder {
		kind = "folder"
	}
	b.recordRecent()
	return InitInfo{
		Kind:      kind,
		Path:      b.input.Path,
		Dir:       b.input.Dir,
		Fragment:  b.input.Fragment,
		AppName:   core.AppName,
		Version:   core.Version,
		Author:    core.AppAuthor,
		Copyright: core.AppCopyright,
		RepoURL:   "https://github.com/" + core.DefaultSettings().UpdateRepo,
		Config:    b.cfg,
		Workspace: b.workspaceDTO(),
		Layout:    b.layoutDTO(),
		Recent:    b.recentList(),
		ExtendedSyntax: b.effectiveExtendedSyntax(),
	}
}

// layoutDTO returns the persisted side-panel widths, substituting defaults for
// any unset (zero) value.
func (b *Bridge) layoutDTO() LayoutDTO {
	sidebar, toc := defaultSidebarWidth, defaultTocWidth
	patterns, enabled := "", false
	if b.layout != nil {
		st := b.layout.Get()
		if st.SidebarWidth > 0 {
			sidebar = st.SidebarWidth
		}
		if st.TocWidth > 0 {
			toc = st.TocWidth
		}
		patterns, enabled = st.ExcludePatterns, st.ExcludeEnabled
	}
	return LayoutDTO{
		SidebarWidth:    sidebar,
		TocWidth:        toc,
		ExcludePatterns: patterns,
		ExcludeEnabled:  enabled,
	}
}

// SaveLayout records the current side-panel widths (in pixels). The store
// debounces the write, so the frontend may call this freely on every drag.
func (b *Bridge) SaveLayout(sidebarWidth, tocWidth int) {
	if b.layout != nil {
		b.layout.UpdatePanels(sidebarWidth, tocWidth)
	}
}

// effectiveExtendedSyntax resolves the extended-syntax toggle: the persisted
// runtime choice from state.jsonc takes precedence when present, otherwise the
// settings.jsonc default is used.
func (b *Bridge) effectiveExtendedSyntax() bool {
	if b.layout != nil {
		if p := b.layout.Get().ExtendedSyntax; p != nil {
			return *p
		}
	}
	return b.cfg.EnableExtendedSyntax
}

// SaveExtendedSyntax persists the user's runtime choice for the extended inline
// Markdown syntax toggle to state.jsonc so it is restored on the next launch
// and shared with the terminal UI.
func (b *Bridge) SaveExtendedSyntax(enabled bool) {
	if b.layout != nil {
		b.layout.UpdateExtendedSyntax(enabled)
	}
}

// ResetLayout restores the window to its default size (centered) and the side
// panels to their default widths, persisting the result. It returns the default
// panel widths so the frontend can update its CSS variables.
func (b *Bridge) ResetLayout() LayoutDTO {
	if b.window != nil {
		if b.window.IsMaximised() {
			b.window.UnMaximise()
		}
		b.window.SetSize(defaultWindowWidth, defaultWindowHeight)
		b.window.Center()
	}
	if b.layout != nil {
		b.layout.ResetPanels()
	}
	return LayoutDTO{SidebarWidth: defaultSidebarWidth, TocWidth: defaultTocWidth}
}

// promptForInput presents a native dialog letting the user choose a markdown
// file or a folder to view, then loads the selection into the bridge. The
// dialog title carries the app name so the user can see which program is asking.
// It returns false when the user cancels or the selection cannot be resolved.
func (b *Bridge) promptForInput() bool {
	if b.app == nil {
		return false
	}
	path, err := b.app.Dialog.OpenFile().
		CanChooseFiles(true).
		CanChooseDirectories(true).
		SetTitle(core.AppName + " \u2014 Open Markdown File or Folder").
		AddFilter("Markdown", "*.md;*.markdown;*.mdown;*.mkd;*.mkdn;*.mdwn;*.mdtxt;*.mdtext;*.text").
		PromptForSingleSelection()
	if err != nil || strings.TrimSpace(path) == "" {
		return false
	}
	in, err := core.ResolveInput(path)
	if err != nil || in.Kind == core.InputNone {
		return false
	}
	b.input = in
	b.workspace, _ = core.ListMarkdownFiles(in.Dir, b.cfg)
	core.PopulateTitles(b.workspace)
	return true
}

// initExcludes seeds the in-memory exclusion state from the persisted layout.
// Called once during startup after the layout store is attached.
func (b *Bridge) initExcludes() {
	if b.layout == nil {
		return
	}
	st := b.layout.Get()
	b.excludeMu.Lock()
	b.excludePatterns = splitExcludeLines(st.ExcludePatterns)
	b.excludeEnabled = st.ExcludeEnabled
	b.excludeMu.Unlock()
}

// splitExcludeLines splits the multi-line exclude text into individual pattern
// lines. Empty lines and comments are kept here (the matcher skips them) so the
// stored text round-trips exactly.
func splitExcludeLines(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
}

// ApplyExcludes stores the navigator exclusion patterns and enabled flag,
// persists them (debounced) and returns the absolute paths of the workspace
// documents that are currently excluded. When disabled it returns an empty
// list, leaving every document visible while still remembering the patterns.
func (b *Bridge) ApplyExcludes(text string, enabled bool) []string {
	patterns := splitExcludeLines(text)

	b.excludeMu.Lock()
	b.excludePatterns = patterns
	b.excludeEnabled = enabled
	b.excludeMu.Unlock()

	if b.layout != nil {
		b.layout.UpdateExcludes(text, enabled)
	}

	if !enabled {
		return []string{}
	}
	excluded := core.ExcludedPaths(b.workspace, b.input.Dir, patterns)
	if excluded == nil {
		return []string{}
	}
	return excluded
}

// excludedSet returns the set of currently excluded absolute paths, or nil when
// exclusion is disabled or no patterns are active. Used to skip excluded files
// during content search.
func (b *Bridge) excludedSet() map[string]bool {
	b.excludeMu.Lock()
	enabled := b.excludeEnabled
	patterns := b.excludePatterns
	b.excludeMu.Unlock()
	if !enabled || len(patterns) == 0 {
		return nil
	}
	excluded := core.ExcludedPaths(b.workspace, b.input.Dir, patterns)
	if len(excluded) == 0 {
		return nil
	}
	set := make(map[string]bool, len(excluded))
	for _, p := range excluded {
		set[p] = true
	}
	return set
}

func (b *Bridge) workspaceDTO() []DocFileDTO {
	out := make([]DocFileDTO, len(b.workspace))
	for i, d := range b.workspace {
		rel, _ := filepath.Rel(b.input.Dir, d.Path)
		out[i] = DocFileDTO{Path: d.Path, Name: d.Name, Title: d.Title, Rel: filepath.ToSlash(rel)}
	}
	return out
}

// DocumentDTO is a loaded document.
type DocumentDTO struct {
	Path     string `json:"path"`
	Dir      string `json:"dir"`
	Name     string `json:"name"`
	Markdown string `json:"markdown"`
	Error    string `json:"error"`
}

// ReadDocument loads a markdown document from disk, enforcing the shared
// maximum document size so the webview never tries to render an oversized file.
func (b *Bridge) ReadDocument(path string) DocumentDTO {
	data, err := core.ReadMarkdownFile(path)
	if err != nil {
		return DocumentDTO{Path: path, Error: err.Error()}
	}
	return DocumentDTO{
		Path:     path,
		Dir:      filepath.Dir(path),
		Name:     filepath.Base(path),
		Markdown: string(data),
	}
}

// maxInlineAssetBytes caps the size of a local asset that ResolveAsset will
// inline as a data URI, to avoid embedding pathologically large files.
const maxInlineAssetBytes = 32 << 20 // 32 MiB

// ResolveAsset resolves a local image/media reference (e.g. "images/icon.png")
// against the directory of the current document and returns it as a data URI so
// the webview can display it. The embedded asset server only serves the
// compiled frontend, so relative filesystem paths must be inlined here.
//
// Absolute URLs (http(s), data:, etc.) and unreadable/oversized files return
// an empty string, leaving the original reference untouched.
func (b *Bridge) ResolveAsset(src, currentDir string) string {
	src = strings.TrimSpace(src)
	if src == "" || strings.HasPrefix(src, "#") {
		return ""
	}
	// Leave anything with a real URL scheme (http, https, data, file, ...)
	// untouched. A single-letter scheme is treated as a Windows drive letter
	// (e.g. "C:\\img.png") and resolved as a local path.
	if u, err := url.Parse(src); err == nil && len(u.Scheme) > 1 {
		return ""
	}

	path := src
	if !filepath.IsAbs(path) {
		path = filepath.Join(currentDir, filepath.FromSlash(src))
	}
	path = filepath.Clean(path)

	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() || fi.Size() > maxInlineAssetBytes {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	mimeType := mime.TypeByExtension(filepath.Ext(path))
	if mimeType == "" {
		mimeType = http.DetectContentType(data)
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// LinkTargetDTO is the resolved classification of a link.
type LinkTargetDTO struct {
	Kind     string `json:"kind"`
	Resolved string `json:"resolved"`
	Fragment string `json:"fragment"`
	Display  string `json:"display"`
	Raw      string `json:"raw"`
}

// ResolveLink classifies a raw href against the directory of the current doc.
func (b *Bridge) ResolveLink(raw, currentDir string) LinkTargetDTO {
	t := core.ResolveLink(raw, currentDir, b.input.Dir, b.cfg, b.workspace)
	return LinkTargetDTO{
		Kind:     t.Kind.String(),
		Resolved: t.Resolved,
		Fragment: t.Fragment,
		Display:  t.Display,
		Raw:      t.Raw,
	}
}

// OpenExternal opens a URL or non-markdown file with the OS default handler.
// As a defense-in-depth backstop (the frontend already confirms with the user),
// only a small allow-list of schemes is permitted so a crafted document cannot
// hand an arbitrary scheme (e.g. an app-launching custom protocol) to the OS.
func (b *Bridge) OpenExternal(target string) string {
	if !isAllowedExternalTarget(target) {
		return "blocked: unsupported link type"
	}
	if err := core.OpenInOS(target); err != nil {
		return err.Error()
	}
	return ""
}

// IsDefaultHandler reports whether mdv is the operating system's default
// application for the type of the file at path. The GUI uses it to decide
// whether to show the "open in the associated app" toolbar button: that button
// is only useful when something other than mdv would open the file.
func (b *Bridge) IsDefaultHandler(path string) bool {
	return isDefaultHandler(path)
}

// PDFNativeAvailable reports whether the high-fidelity headless-browser PDF
// engine is usable (an installed browser plus the embedded print bundle). The
// toolbar's PDF export uses it to decide between Chrome's printToPDF and the
// in-webview html2pdf.js fallback.
func (b *Bridge) PDFNativeAvailable() bool {
	return pdf.NativeAvailable()
}

// ExportPDFFromMarkdown renders the given Markdown to a PDF using the headless
// browser engine (the same print harness used by the CLI, so Mermaid, KaTeX and
// syntax highlighting render faithfully with selectable text), after asking the
// user where to save it. dir is the document's directory (for relative images
// and as the default save location); name seeds the default file name.
//
// allowRemote permits fetching remote (http/https) images and assets; it is
// blocked by default and should mirror the GUI's per-session remote-images
// toggle so a PDF never silently downloads more than the on-screen view.
//
// It returns an empty string on success, "fallback" when the native engine is
// unavailable (so the frontend can use its in-webview html2pdf.js path),
// "cancel" when the user dismisses the save dialog, or an error message
// otherwise.
func (b *Bridge) ExportPDFFromMarkdown(markdown, dir, name string, extended, allowRemote bool) string {
	if !pdf.NativeAvailable() {
		return "fallback"
	}
	dest := b.promptSavePDF(dir, name)
	if dest == "" {
		return "cancel"
	}
	var buf bytes.Buffer
	if err := pdf.RenderChromium([]byte(markdown), dir, extended, allowRemote, &buf); err != nil {
		return err.Error()
	}
	if err := os.WriteFile(dest, buf.Bytes(), 0o644); err != nil {
		return err.Error()
	}
	_ = core.OpenInOS(dest)
	return ""
}

// ExportPDF writes a PDF produced in the webview (the html2pdf.js fallback,
// passed as base64) to a user-chosen location and opens it. It is used when the
// native headless-browser engine is unavailable.
//
// It returns an empty string on success, "cancel" when the user dismisses the
// save dialog, or an error message otherwise.
func (b *Bridge) ExportPDF(dataB64, dir, name string) string {
	raw, err := base64.StdEncoding.DecodeString(dataB64)
	if err != nil {
		return err.Error()
	}
	dest := b.promptSavePDF(dir, name)
	if dest == "" {
		return "cancel"
	}
	if err := os.WriteFile(dest, raw, 0o644); err != nil {
		return err.Error()
	}
	_ = core.OpenInOS(dest)
	return ""
}

// promptSavePDF asks the user where to save a PDF, defaulting to dir and a .pdf
// filename derived from name. It returns the chosen path, or "" when cancelled.
func (b *Bridge) promptSavePDF(dir, name string) string {
	filename := strings.TrimSpace(name)
	if filename == "" {
		filename = "document"
	}
	if !strings.HasSuffix(strings.ToLower(filename), ".pdf") {
		filename += ".pdf"
	}
	if b.app == nil {
		// No app (shouldn't happen in the GUI): write alongside the source.
		if dir == "" {
			dir = os.TempDir()
		}
		return filepath.Join(dir, filename)
	}
	dest, err := b.app.Dialog.SaveFile().
		SetDirectory(dir).
		SetFilename(filename).
		AddFilter("PDF", "*.pdf").
		CanCreateDirectories(true).
		PromptForSingleSelection()
	if err != nil || strings.TrimSpace(dest) == "" {
		return ""
	}
	if !strings.HasSuffix(strings.ToLower(dest), ".pdf") {
		dest += ".pdf"
	}
	return dest
}

// isAllowedExternalTarget reports whether target is safe to hand to the OS
// handler: a local filesystem path, or a URL using an http/https/mailto/file
// scheme. Anything else (custom app schemes, javascript:, etc.) is rejected.
func isAllowedExternalTarget(target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	u, err := url.Parse(target)
	if err != nil {
		// Unparseable but non-empty: treat as a local path (e.g. Windows paths).
		return !strings.Contains(target, "://")
	}
	switch strings.ToLower(u.Scheme) {
	case "", "file":
		// No scheme => relative/absolute local path; file:// => local file.
		return true
	case "http", "https", "mailto":
		return true
	default:
		return false
	}
}

// WatchFile switches the live-reload watcher to the given document. It is a
// no-op while auto-reload is disabled (the default).
func (b *Bridge) WatchFile(path string) {
	if b.watchEnabled && b.watcher != nil {
		b.watcher.Watch(path)
	}
}

// SetWatchEnabled turns the active-document auto-reload watcher on or off at
// runtime. When enabling, path (the currently displayed document) is armed
// immediately; when disabling, the active-document watch is released. It returns
// the resulting enabled state.
func (b *Bridge) SetWatchEnabled(enabled bool, path string) bool {
	b.watchEnabled = enabled
	if b.watcher == nil {
		return false
	}
	if enabled {
		if path != "" {
			b.watcher.Watch(path)
		}
	} else {
		b.watcher.Unwatch()
	}
	return b.watchEnabled
}

// recordRecent adds the current input to the rolling recently-opened list so it
// surfaces in the toolbar's recents drop-down. Picker/none inputs are ignored.
func (b *Bridge) recordRecent() {
	if b.layout == nil || b.input.Kind == core.InputNone || strings.TrimSpace(b.input.Path) == "" {
		return
	}
	b.layout.AddRecent(recentItemFor(b.input))
}

// recentList returns the current rolling recents (most recent first), or nil
// when no layout store is attached.
func (b *Bridge) recentList() []RecentItem {
	if b.layout == nil {
		return nil
	}
	return b.layout.Recent()
}

// ClearRecent empties the rolling recently-opened list and returns the (now
// empty) list so the frontend can refresh the toolbar drop-down.
func (b *Bridge) ClearRecent() []RecentItem {
	if b.layout != nil {
		b.layout.ClearRecent()
	}
	return b.recentList()
}

// RefreshWorkspace re-scans the workspace directory and returns the current
// markdown document listing, so the navigator can update after files are added,
// removed or renamed on disk.
func (b *Bridge) RefreshWorkspace() []DocFileDTO {
	b.workspace, _ = core.ListMarkdownFiles(b.input.Dir, b.cfg)
	core.PopulateTitles(b.workspace)
	return b.workspaceDTO()
}

// Backlinks returns documents that link to the given file.
func (b *Bridge) Backlinks(path string) []core.Backlink {
	return core.FindBacklinks(path, b.input.Dir, b.cfg, b.workspace)
}

// OpenInNewWindow launches a separate mdv process for the given path. An
// optional fragment (anchor slug, without '#') makes the new window scroll to a
// specific section after loading.
func (b *Bridge) OpenInNewWindow(path string, fragment string) string {
	exe, err := os.Executable()
	if err != nil {
		return err.Error()
	}
	arg := path
	if fragment != "" {
		arg = path + "#" + fragment
	}
	if err := core.SpawnDetached(exe, arg); err != nil {
		return err.Error()
	}
	return ""
}

func (b *Bridge) checkUpdate() UpdateDTO {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	info, _ := core.CheckForUpdate(ctx, b.cfg)
	return UpdateDTO{Available: info.Available, Latest: info.Latest, DownloadURL: info.DownloadURL}
}

// ContentSearchResultEvent is the payload streamed to the frontend for each
// document that matches a content search.
type ContentSearchResultEvent struct {
	Gen    uint64               `json:"gen"`
	Result core.DocSearchResult `json:"result"`
}

// ContentSearchDoneEvent signals the end of a content search.
type ContentSearchDoneEvent struct {
	Gen   uint64 `json:"gen"`
	Count int    `json:"count"`
}

// SearchContent runs a streaming, case-insensitive fuzzy-phrase content search
// over the workspace markdown files. Results are delivered to the
// frontend as "content-search:result" events (one per matching document) and a
// final "content-search:done" event. Each call cancels any in-flight search.
// The caller passes a generation number that is echoed back in every event so
// the frontend can discard results from a superseded search.
func (b *Bridge) SearchContent(query string, gen int) {
	b.searchMu.Lock()
	if b.searchCancel != nil {
		b.searchCancel()
	}
	b.searchGen = uint64(gen)
	cur := b.searchGen
	ctx, cancel := context.WithCancel(context.Background())
	b.searchCancel = cancel
	b.searchMu.Unlock()

	// Restrict the search to documents that are not currently excluded by the
	// navigator's exclusion patterns, so hidden files never surface as matches.
	files := b.workspace
	if excluded := b.excludedSet(); excluded != nil {
		filtered := make([]core.DocFile, 0, len(files))
		for _, f := range files {
			if !excluded[f.Path] {
				filtered = append(filtered, f)
			}
		}
		files = filtered
	}
	emit := b.emit

	go func() {
		defer cancel()
		count := 0
		core.SearchDocuments(ctx, files, query, func(r core.DocSearchResult) {
			if ctx.Err() != nil {
				return
			}
			// Drop results from a superseded search generation.
			b.searchMu.Lock()
			stale := cur != b.searchGen
			b.searchMu.Unlock()
			if stale {
				return
			}
			count++
			if emit != nil {
				emit("content-search:result", ContentSearchResultEvent{Gen: cur, Result: r})
			}
		})
		if ctx.Err() == nil && emit != nil {
			b.searchMu.Lock()
			stale := cur != b.searchGen
			b.searchMu.Unlock()
			if !stale {
				emit("content-search:done", ContentSearchDoneEvent{Gen: cur, Count: count})
			}
		}
	}()
}
