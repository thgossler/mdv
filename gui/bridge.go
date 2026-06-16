package main

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/thgossler/mdv/internal/core"
)

// Bridge is the Wails service exposed to the frontend. Its exported methods are
// callable from TypeScript and provide all filesystem, link-resolution and
// navigation logic, keeping the webview a pure presentation layer.
type Bridge struct {
	cfg       core.Defaults
	input     core.Input
	workspace []core.DocFile
	watcher   *Watcher
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
	}
}

func (b *Bridge) workspaceDTO() []DocFileDTO {
	out := make([]DocFileDTO, len(b.workspace))
	for i, d := range b.workspace {
		rel, _ := filepath.Rel(b.input.Dir, d.Path)
		out[i] = DocFileDTO{Path: d.Path, Name: d.Name, Title: d.Title, Rel: rel}
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

// ReadDocument loads a markdown document from disk.
func (b *Bridge) ReadDocument(path string) DocumentDTO {
	data, err := os.ReadFile(path)
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
	t := core.ResolveLink(raw, currentDir, b.cfg, b.workspace)
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
	return core.FindBacklinks(path, b.cfg, b.workspace)
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
