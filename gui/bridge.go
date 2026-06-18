package main

import (
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

	// emit dispatches a named application event with structured data to the
	// frontend. It is wired in main.go after the app is created.
	emit func(name string, data any)

	// rgPath is the resolved ripgrep executable path, or "" when ripgrep is not
	// installed. It is detected in the background after startup; reads/writes are
	// guarded by searchMu.
	rgPath string

	// searchMu guards rgPath, searchGen and searchCancel.
	searchMu     sync.Mutex
	searchGen    uint64
	searchCancel context.CancelFunc
}

// NewBridge builds a Bridge for the given input and configuration.
func NewBridge(cfg core.Defaults, in core.Input) *Bridge {
	b := &Bridge{cfg: cfg, input: in}
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
	Config    core.Defaults `json:"config"`
	Workspace []DocFileDTO  `json:"workspace"`
	Update    UpdateDTO     `json:"update"`
	Layout    LayoutDTO     `json:"layout"`
}

// LayoutDTO carries the persisted side-panel widths (in pixels) so the frontend
// can apply them before the first paint, avoiding panels jumping after start.
type LayoutDTO struct {
	SidebarWidth int `json:"sidebarWidth"`
	TocWidth     int `json:"tocWidth"`
}

// UpdateDTO carries version-check results to the status bar.
type UpdateDTO struct {
	Available   bool   `json:"available"`
	Latest      string `json:"latest"`
	DownloadURL string `json:"downloadUrl"`
}

// Init returns the bootstrap information for the frontend.
func (b *Bridge) Init() InitInfo {
	kind := "file"
	if b.input.Kind == core.InputFolder {
		kind = "folder"
	}
	return InitInfo{
		Kind:      kind,
		Path:      b.input.Path,
		Dir:       b.input.Dir,
		Fragment:  b.input.Fragment,
		AppName:   core.AppName,
		Version:   core.Version,
		Config:    b.cfg,
		Workspace: b.workspaceDTO(),
		Update:    b.checkUpdate(),
		Layout:    b.layoutDTO(),
	}
}

// layoutDTO returns the persisted side-panel widths, substituting defaults for
// any unset (zero) value.
func (b *Bridge) layoutDTO() LayoutDTO {
	sidebar, toc := defaultSidebarWidth, defaultTocWidth
	if b.layout != nil {
		st := b.layout.Get()
		if st.SidebarWidth > 0 {
			sidebar = st.SidebarWidth
		}
		if st.TocWidth > 0 {
			toc = st.TocWidth
		}
	}
	return LayoutDTO{SidebarWidth: sidebar, TocWidth: toc}
}

// SaveLayout records the current side-panel widths (in pixels). The store
// debounces the write, so the frontend may call this freely on every drag.
func (b *Bridge) SaveLayout(sidebarWidth, tocWidth int) {
	if b.layout != nil {
		b.layout.UpdatePanels(sidebarWidth, tocWidth)
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
func (b *Bridge) OpenExternal(target string) string {
	if err := core.OpenInOS(target); err != nil {
		return err.Error()
	}
	return ""
}

// WatchFile switches the live-reload watcher to the given document.
func (b *Bridge) WatchFile(path string) {
	if b.cfg.LiveReload && b.watcher != nil {
		b.watcher.Watch(path)
	}
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

// detectRipgrep resolves the ripgrep path in the background and stores it for
// later content searches. Called once shortly after startup.
func (b *Bridge) detectRipgrep() {
	path := core.DetectRipgrep()
	b.searchMu.Lock()
	b.rgPath = path
	b.searchMu.Unlock()
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

// SearchContent runs a streaming, case-insensitive AND-per-document content
// search over the workspace markdown files. Results are delivered to the
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
	rgPath := b.rgPath
	b.searchMu.Unlock()

	files := b.workspace
	emit := b.emit

	go func() {
		defer cancel()
		count := 0
		core.SearchDocuments(ctx, files, query, rgPath, func(r core.DocSearchResult) {
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
