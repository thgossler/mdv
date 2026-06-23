package pdf

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/thgossler/mdv/internal/core"
	gmpdf "github.com/stephenafamo/goldmark-pdf"
	"github.com/yuin/goldmark"
)

// generateViaGoldmark renders markdown to a PDF using the pure-Go goldmark-pdf
// engine and writes it to w. It needs no browser and works fully offline.
//
// It deliberately uses the inbuilt PDF base fonts (Helvetica/Courier) rather
// than the library's default Google web fonts, which are downloaded on demand:
// inbuilt fonts guarantee the render succeeds with no network access, which is
// essential for headless servers and containers. The tradeoff is plainer
// typography and limited Unicode coverage.
//
// Relative image references resolve against srcDir. Leading YAML front matter
// is stripped so it is not rendered as a literal table of dashes and text.
//
// The underlying gofpdf only understands a few raster image formats, so this
// engine pre-strips images it cannot embed (remote URLs, SVG, WebP) to avoid a
// hard failure on the kind of badge images common in README files. As a final
// safety net, if rendering still fails it retries once with every image removed
// so a PDF is always produced.
func generateViaGoldmark(markdown []byte, srcDir string, allowRemote bool, w io.Writer) ([]string, error) {
	body := core.StripFrontmatter(string(markdown))

	// Remove HTML tags that sit outside code blocks so the body text stays
	// clean; the goldmark-pdf engine ignores HTML blocks anyway, but doing it
	// here lets us warn the user about what was dropped.
	body, htmlRemoved := stripHTMLOutsideCode(body)

	var warnings []string
	if htmlRemoved > 0 {
		warnings = append(warnings, fmt.Sprintf("removed %d HTML %s outside code blocks", htmlRemoved, plural(htmlRemoved, "tag", "tags")))
	}

	filtered, imgRemoved := filterUnsupportedImages(body, allowRemote)
	retried := false
	if err := renderGoldmarkOnce(filtered, srcDir, w); err != nil {
		// Retry without any images rather than failing the whole export. This
		// typically happens when an image the policy allowed (e.g. a remote
		// asset under --remote) could not be fetched or decoded by the engine.
		retried = true
		stripped, allImg := stripAllImages(body)
		imgRemoved = allImg
		if err := renderGoldmarkOnce(stripped, srcDir, w); err != nil {
			return warnings, err
		}
	}
	if imgRemoved > 0 {
		switch {
		case retried:
			warnings = append(warnings, fmt.Sprintf("dropped %d %s the engine could not embed", imgRemoved, plural(imgRemoved, "image", "images")))
		case allowRemote:
			warnings = append(warnings, fmt.Sprintf("skipped %d unsupported %s (SVG or WebP)", imgRemoved, plural(imgRemoved, "image", "images")))
		default:
			warnings = append(warnings, fmt.Sprintf("skipped %d unsupported %s (remote, SVG, or WebP)", imgRemoved, plural(imgRemoved, "image", "images")))
		}
	}
	return warnings, nil
}

// marginPt is a 1 cm page margin expressed in PDF points (gofpdf uses "pt").
const marginPt = 28.3465

// renderGoldmarkOnce performs a single goldmark-pdf conversion into w.
func renderGoldmarkOnce(body, srcDir string, w io.Writer) error {
	ctx := context.Background()

	// Build the Fpdf explicitly so we can set a uniform 1 cm page margin on all
	// sides: FpdfConfig exposes no margin fields, and the renderer reads the
	// margins from the PDF instance at the start of the render.
	fpdf := gmpdf.NewFpdf(ctx, gmpdf.FpdfConfig{
		Orientation: "Portrait",
		PaperSize:   "A4",
	}, nil)
	fpdf.SetMarginLeft(marginPt)
	fpdf.SetMarginRight(marginPt)
	fpdf.SetMarginTop(marginPt)
	fpdf.Fpdf.SetAutoPageBreak(true, marginPt)

	opts := []gmpdf.Option{
		gmpdf.WithContext(ctx),
		gmpdf.WithBodyFont(gmpdf.FontHelvetica),
		gmpdf.WithHeadingFont(gmpdf.FontHelvetica),
		gmpdf.WithCodeFont(gmpdf.FontCourier),
		gmpdf.WithPDF(fpdf),
		// Keep special characters (&, <, >) literal instead of turning them into
		// HTML entities like "&amp;", and render HTML inside code blocks verbatim.
		gmpdf.WithEscapeHTML(false),
	}
	if srcDir != "" {
		opts = append(opts, gmpdf.WithImageFS(rasterOnlyFS{inner: http.FS(os.DirFS(srcDir))}))
	}

	md := goldmark.New(goldmark.WithRenderer(gmpdf.New(opts...)))
	var buf bytes.Buffer
	if err := md.Convert([]byte(body), &buf); err != nil {
		return fmt.Errorf("goldmark-pdf render: %w", err)
	}
	_, err := w.Write(buf.Bytes())
	return err
}

// mdImageRE matches a Markdown image: ![alt](url "optional title").
var mdImageRE = regexp.MustCompile(`!\[[^\]]*\]\(\s*([^)\s]+)[^)]*\)`)

// filterUnsupportedImages removes Markdown images whose source the goldmark-pdf
// engine cannot embed: vector/modern formats (SVG, WebP, AVIF) and, unless
// allowRemote is set, every remote (http/https) URL. Local raster images are
// kept. It returns the filtered body and the number of images removed.
func filterUnsupportedImages(body string, allowRemote bool) (string, int) {
	removed := 0
	out := mdImageRE.ReplaceAllStringFunc(body, func(m string) string {
		sub := mdImageRE.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		if isUnsupportedImageURL(sub[1], allowRemote) {
			removed++
			return ""
		}
		return m
	})
	return out, removed
}

// stripAllImages removes every Markdown image from the body and returns the
// cleaned body together with the number of images removed.
func stripAllImages(body string) (string, int) {
	removed := len(mdImageRE.FindAllString(body, -1))
	return mdImageRE.ReplaceAllString(body, ""), removed
}

// plural returns singular when n == 1 and plural otherwise.
func plural(n int, singular, pluralForm string) string {
	if n == 1 {
		return singular
	}
	return pluralForm
}

// htmlConstructRE matches an HTML comment or a single HTML start/end tag.
var htmlConstructRE = regexp.MustCompile(`(?s)<!--.*?-->|</?[a-zA-Z][^>]*>`)

// stripHTMLOutsideCode removes HTML tags and comments that appear outside fenced
// code blocks and inline code spans, returning the cleaned markdown and the
// number of HTML constructs removed. Code content is preserved verbatim so that,
// combined with WithEscapeHTML(false), raw HTML inside code blocks renders
// literally instead of as entity-escaped noise.
func stripHTMLOutsideCode(body string) (string, int) {
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	removed := 0
	inFence := false
	var fenceChar byte
	fenceLen := 0
	inComment := false

	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		indent := len(line) - len(trimmed)
		if !inComment && indent <= 3 {
			if c, n, ok := fenceMarker(trimmed); ok {
				if !inFence {
					inFence, fenceChar, fenceLen = true, c, n
					out = append(out, line)
					continue
				}
				if c == fenceChar && n >= fenceLen && onlyRuneRun(trimmed, c) {
					inFence = false
					out = append(out, line)
					continue
				}
			}
		}
		if inFence {
			out = append(out, line)
			continue
		}
		cleaned, n, stillInComment := stripHTMLFromLine(line, inComment)
		inComment = stillInComment
		removed += n
		out = append(out, cleaned)
	}
	return strings.Join(out, "\n"), removed
}

// fenceMarker reports whether a line (already left-trimmed) opens or closes a
// fenced code block, returning the fence character and its run length.
func fenceMarker(trimmed string) (byte, int, bool) {
	if len(trimmed) < 3 {
		return 0, 0, false
	}
	c := trimmed[0]
	if c != '`' && c != '~' {
		return 0, 0, false
	}
	n := 0
	for n < len(trimmed) && trimmed[n] == c {
		n++
	}
	if n < 3 {
		return 0, 0, false
	}
	return c, n, true
}

// onlyRuneRun reports whether trimmed consists solely of the rune c (allowing
// trailing whitespace), used to validate a closing fence has no info string.
func onlyRuneRun(trimmed string, c byte) bool {
	trimmed = strings.TrimRight(trimmed, " \t")
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] != c {
			return false
		}
	}
	return true
}

// stripHTMLFromLine removes HTML constructs from a single line, preserving inline
// code spans. It carries multi-line HTML comment state across lines via the
// inComment flag. It returns the cleaned line, the number of constructs removed,
// and whether an HTML comment is still open at the end of the line.
func stripHTMLFromLine(line string, inComment bool) (string, int, bool) {
	removed := 0

	// Continue consuming an HTML comment opened on a previous line. The comment
	// was already counted when it opened, so do not count it again here.
	if inComment {
		idx := strings.Index(line, "-->")
		if idx < 0 {
			return "", 0, true
		}
		line = line[idx+len("-->"):]
	}

	var b strings.Builder
	i := 0
	for i < len(line) {
		if line[i] == '`' {
			j := i
			for j < len(line) && line[j] == '`' {
				j++
			}
			runLen := j - i
			if close := findClosingBacktick(line, j, runLen); close >= 0 {
				b.WriteString(line[i : close+runLen])
				i = close + runLen
				continue
			}
			b.WriteString(line[i:j])
			i = j
			continue
		}
		k := i
		for k < len(line) && line[k] != '`' {
			k++
		}
		text := line[i:k]
		cleaned := htmlConstructRE.ReplaceAllStringFunc(text, func(string) string {
			removed++
			return ""
		})
		// Any remaining "<!--" is a comment that opens here but only closes on a
		// later line; drop the rest of the line and keep the comment state open.
		if idx := strings.Index(cleaned, "<!--"); idx >= 0 {
			b.WriteString(cleaned[:idx])
			removed++
			return b.String(), removed, true
		}
		b.WriteString(cleaned)
		i = k
	}
	return b.String(), removed, false
}

// findClosingBacktick returns the index of a backtick run of exactly runLen
// starting at or after pos, or -1 if none closes the inline code span.
func findClosingBacktick(line string, pos, runLen int) int {
	for i := pos; i < len(line); i++ {
		if line[i] != '`' {
			continue
		}
		j := i
		for j < len(line) && line[j] == '`' {
			j++
		}
		if j-i == runLen {
			return i
		}
		i = j - 1
	}
	return -1
}

func isUnsupportedImageURL(u string, allowRemote bool) bool {
	lower := strings.ToLower(strings.TrimSpace(u))
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		if !allowRemote {
			return true
		}
	}
	// Strip any query/fragment before checking the extension.
	if i := strings.IndexAny(lower, "?#"); i >= 0 {
		lower = lower[:i]
	}
	switch path.Ext(lower) {
	case ".svg", ".webp", ".avif":
		return true
	}
	return false
}

// rasterOnlyFS wraps an image filesystem and hides files the PDF engine cannot
// embed, so they are skipped (logged and ignored) rather than aborting the
// whole render.
type rasterOnlyFS struct{ inner http.FileSystem }

func (s rasterOnlyFS) Open(name string) (http.File, error) {
	switch strings.ToLower(path.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif":
		return s.inner.Open(name)
	default:
		return nil, fs.ErrNotExist
	}
}

