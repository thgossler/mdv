package core

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MaxFileBytes is the largest markdown document mdv will load and render. Above
// this size the rich renderers (glamour for the console/TUI and markdown-it in
// the GUI) need impractical amounts of memory and time and would appear to
// hang, so mdv refuses the file up front with a clear message instead. A
// typical 20,000-line document is only a few MiB, so this limit leaves a wide
// margin for real-world files while rejecting pathological inputs (e.g. a
// multi-hundred-MB log accidentally opened as markdown).
const MaxFileBytes = 50 << 20 // 50 MiB

// FileTooLargeError is returned when a document exceeds MaxFileBytes. It carries
// the offending path and sizes so callers can present a friendly message.
type FileTooLargeError struct {
	Path  string
	Size  int64
	Limit int64
}

func (e *FileTooLargeError) Error() string {
	name := e.Path
	if name == "" {
		name = "input"
	} else {
		name = filepath.Base(name)
	}
	return fmt.Sprintf("%s is too large (%s); mdv can display documents up to %s",
		name, FormatBytes(e.Size), FormatBytes(e.Limit))
}

// FormatBytes renders a byte count as a short human-readable string such as
// "734 B", "12.3 KB" or "1.4 GB".
func FormatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// ReadMarkdownFile reads a markdown document from disk after verifying it does
// not exceed MaxFileBytes. Oversized files yield a *FileTooLargeError so every
// run mode (console, TUI, GUI) can fail with the same friendly message instead
// of trying to render gigabytes of text. It is the single entry point all modes
// should use to load a document by path.
func ReadMarkdownFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory", filepath.Base(path))
	}
	if info.Size() > MaxFileBytes {
		return nil, &FileTooLargeError{Path: path, Size: info.Size(), Limit: MaxFileBytes}
	}
	return os.ReadFile(path)
}

// skipDirs are directories never descended into during folder listing.
var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	".svn":         true,
	".hg":          true,
	".idea":        true,
	".vscode":      true,
}

// ResolveInput turns a raw CLI argument into an absolute, classified Input.
// An empty arg yields InputNone. "." resolves to the current directory.
func ResolveInput(arg string) (Input, error) {
	if strings.TrimSpace(arg) == "" {
		return Input{Kind: InputNone}, nil
	}

	abs, err := filepath.Abs(expandHome(arg))
	if err != nil {
		return Input{}, err
	}

	info, err := os.Stat(abs)
	if err != nil {
		return Input{}, err
	}

	if info.IsDir() {
		return Input{Kind: InputFolder, Path: abs, Dir: abs}, nil
	}
	if info.Size() > MaxFileBytes {
		return Input{}, &FileTooLargeError{Path: abs, Size: info.Size(), Limit: MaxFileBytes}
	}
	return Input{Kind: InputFile, Path: abs, Dir: filepath.Dir(abs)}, nil
}

// ReadStdin reads all markdown content from r (typically os.Stdin) into memory
// and returns an InputStdin. The workspace directory is the current working
// directory so relative links and images resolve against where mdv was run.
// An empty stream yields InputNone so the caller can fall back to usage.
func ReadStdin(r io.Reader) (Input, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return Input{}, err
	}
	if int64(len(data)) > MaxFileBytes {
		return Input{}, &FileTooLargeError{Size: int64(len(data)), Limit: MaxFileBytes}
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return Input{Kind: InputNone}, nil
	}
	dir, err := os.Getwd()
	if err != nil {
		dir = ""
	}
	return Input{Kind: InputStdin, Dir: dir, Data: data}, nil
}

// WriteStdinTempFile materialises piped stdin content into a temporary markdown
// file and returns its path. It is used for the GUI, which runs as a separate
// process that loads documents by path. The caller (or the spawned process) is
// responsible for deleting the file when it is no longer needed.
func WriteStdinTempFile(data []byte) (string, error) {
	f, err := os.CreateTemp("", "mdv-stdin-*.md")
	if err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// expandHome expands a leading ~ to the user's home directory.
func expandHome(p string) string {
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// IsMarkdownPath reports whether path has a recognised markdown extension.
func IsMarkdownPath(path string, cfg Defaults) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, e := range cfg.MarkdownExtensions {
		if ext == e {
			return true
		}
	}
	return false
}

// ListMarkdownFiles walks dir and returns every markdown document, recursing
// into subdirectories but skipping hidden and noise folders. Results are sorted
// so that top-level files come first and READMEs float to the top of each dir.
func ListMarkdownFiles(dir string, cfg Defaults) ([]DocFile, error) {
	var files []DocFile

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // tolerate unreadable entries
		}
		if d.IsDir() {
			name := d.Name()
			if path != dir && (skipDirs[name] || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if IsMarkdownPath(path, cfg) {
			rel, relErr := filepath.Rel(dir, path)
			if relErr != nil {
				rel = d.Name()
			}
			files = append(files, DocFile{Path: path, Name: d.Name(), Rel: filepath.ToSlash(rel)})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sortDocFiles(files, dir)
	return files, nil
}

func sortDocFiles(files []DocFile, root string) {
	rank := func(f DocFile) (int, string, string) {
		rel, _ := filepath.Rel(root, f.Path)
		depth := strings.Count(rel, string(filepath.Separator))
		readme := 1
		if strings.EqualFold(strings.TrimSuffix(f.Name, filepath.Ext(f.Name)), "readme") {
			readme = 0
		}
		return depth, filepath.Dir(rel), strings.ToLower(f.Name) + boolStr(readme == 0)
	}
	sort.SliceStable(files, func(i, j int) bool {
		di, pi, ni := rank(files[i])
		dj, pj, nj := rank(files[j])
		if di != dj {
			return di < dj
		}
		if pi != pj {
			return pi < pj
		}
		return ni < nj
	})
}

func boolStr(b bool) string {
	if b {
		return ""
	}
	return "~"
}

// ExtractTitle reads the first ATX (#) or Setext (===) level-1 heading from a
// markdown file and returns its text, or "" if none is found. It scans only the
// first part of the file to stay fast on large documents.
func ExtractTitle(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	inFence := false
	var prev string
	lines := 0
	for sc.Scan() && lines < 400 {
		lines++
		line := sc.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			prev = ""
			continue
		}
		if inFence {
			continue
		}

		if strings.HasPrefix(trimmed, "# ") {
			return cleanHeading(trimmed[2:])
		}
		if trimmed == "#" {
			return ""
		}
		// Setext H1: a line of '=' under a non-empty text line.
		if prev != "" && len(trimmed) > 0 && strings.Trim(trimmed, "=") == "" {
			return cleanHeading(prev)
		}
		prev = trimmed
	}
	return ""
}

// cleanHeading strips trailing closing hashes and inline markdown emphasis so a
// navigation title reads cleanly.
func cleanHeading(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimRight(s, "#")
	s = strings.TrimSpace(s)
	replacer := strings.NewReplacer("**", "", "__", "", "`", "")
	return strings.TrimSpace(replacer.Replace(s))
}

// PopulateTitles fills in the Title field of each DocFile by extracting H1s.
func PopulateTitles(files []DocFile) {
	for i := range files {
		files[i].Title = ExtractTitle(files[i].Path)
	}
}
