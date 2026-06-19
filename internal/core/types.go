// Package core contains the shared, pure-Go engine used by every run mode
// (console, TUI and GUI). It has no GUI/webview dependencies so the launcher
// binary that imports it always starts - even in a headless container.
package core

// AppName is the user-facing product name.
const AppName = "mdv"

// AppTagline is shown in help output.
const AppTagline = "Markdown Document Viewer"

// Version is the current build version (SemVer, "v"-prefixed). The canonical
// value lives in the repository-root VERSION file and is injected at build time
// via -ldflags "-X github.com/thgossler/mdv/internal/core.Version=vX.Y.Z". This
// default is kept in sync with VERSION (by scripts/bump-version.*) so `go run`
// and unstamped builds still report the real SemVer instead of a commit hash.
var Version = "v0.7.7"

// LinkKind classifies a resolved markdown link target.
type LinkKind int

const (
	// LinkUnknown is an unclassifiable target.
	LinkUnknown LinkKind = iota
	// LinkMarkdown points at another markdown document that should replace the
	// current document in the viewer.
	LinkMarkdown
	// LinkAnchor is an in-document reference such as "#heading".
	LinkAnchor
	// LinkHTTP is an http(s) URL that should open in the OS default browser.
	LinkHTTP
	// LinkExternalFile is a non-markdown local file that should open in the OS
	// default application (txt, js, docx, pdf, images, ...).
	LinkExternalFile
	// LinkWikiInternal is a resolved wikilink to a workspace markdown note.
	LinkWikiInternal
	// LinkBroken is a wikilink (or relative link) that could not be resolved.
	LinkBroken
	// LinkMailto is a mailto: address.
	LinkMailto
)

func (k LinkKind) String() string {
	switch k {
	case LinkMarkdown:
		return "markdown"
	case LinkAnchor:
		return "anchor"
	case LinkHTTP:
		return "http"
	case LinkExternalFile:
		return "file"
	case LinkWikiInternal:
		return "wikilink"
	case LinkBroken:
		return "broken"
	case LinkMailto:
		return "mailto"
	default:
		return "unknown"
	}
}

// LinkTarget is the result of resolving a raw link reference against the
// currently open document and (optionally) the workspace.
type LinkTarget struct {
	Kind LinkKind
	// Raw is the original href as written in the document.
	Raw string
	// Resolved is the absolute filesystem path (for file/markdown/wiki links)
	// or the normalized URL (for http/mailto). For anchors it is the fragment
	// including the leading '#'.
	Resolved string
	// Fragment is the "#..." part for markdown/wiki links that also target a
	// heading, without the leading '#'. Empty when absent.
	Fragment string
	// Display is a human-friendly representation for the status bar / hover.
	Display string
}

// DocFile is a markdown document discovered in a workspace folder.
type DocFile struct {
	// Path is the absolute path to the file.
	Path string
	// Name is the base file name (e.g. "README.md").
	Name string
	// Rel is the path relative to the workspace root, using forward slashes
	// (e.g. "docs/guide/README.md"). It is empty for documents not discovered
	// through a workspace walk.
	Rel string
	// Title is the first level-1 heading, or empty if none was found.
	Title string
}

// InputKind describes what the user pointed mdv at.
type InputKind int

const (
	// InputNone means no input was supplied.
	InputNone InputKind = iota
	// InputFile means a single markdown (or convertible) file.
	InputFile
	// InputFolder means a directory to browse.
	InputFolder
	// InputStdin means markdown content was piped in on standard input and is
	// held in memory rather than read from a path.
	InputStdin
)

// Input is the resolved CLI input.
type Input struct {
	Kind InputKind
	// Path is the absolute path to the file or folder.
	Path string
	// Dir is the directory used as the workspace root. For a file input this is
	// the file's parent directory.
	Dir string
	// Fragment is an optional in-page anchor (slug, without the leading '#') to
	// scroll to once the document is opened.
	Fragment string
	// Data holds the in-memory markdown content for InputStdin. It is empty for
	// file and folder inputs.
	Data []byte
}
