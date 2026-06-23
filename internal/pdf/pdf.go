package pdf

import (
	"bytes"
	"io"
	"log"
)

// Engine names returned by GenerateAuto.
const (
	EngineChromium = "chromium"
	EngineGoldmark = "goldmark"
)

// Result describes the outcome of a PDF generation: which engine was used and
// any non-fatal warnings about content that was filtered or dropped (for
// example HTML tags or unsupported images in the pure-Go goldmark engine).
type Result struct {
	Engine   string
	Warnings []string
}

// BrowserAvailable reports whether an installed Chrome/Chromium/Edge browser
// was found that could be used for high-fidelity PDF export.
func BrowserAvailable() bool {
	_, ok := FindBrowser()
	return ok
}

// NativeAvailable reports whether the high-fidelity headless-browser engine can
// be used: it needs both an installed browser and the embedded print bundle
// (present only in release builds compiled with `-tags pdf_bundled`). The GUI
// uses it to decide between Chrome's printToPDF and the in-webview html2pdf.js
// fallback.
func NativeAvailable() bool {
	if _, ok := FindBrowser(); !ok {
		return false
	}
	_, ok := chromiumAssets()
	return ok
}

// RenderChromium renders markdown to w using the headless-browser engine only
// (no goldmark fallback), so the caller can choose its own fallback. It returns
// an error when either a browser or the embedded print bundle is unavailable.
//
// srcDir resolves relative images and may be empty; extended enables the opt-in
// inline Markdown syntax; allowRemote permits fetching remote (http/https)
// images and assets (blocked by default).
func RenderChromium(markdown []byte, srcDir string, extended, allowRemote bool, w io.Writer) error {
	return generateViaChromium(markdown, srcDir, extended, allowRemote, w)
}

// GenerateAuto renders markdown to w, choosing the best available engine. When
// a browser and the embedded print bundle are both available it uses headless
// Chrome (full fidelity: Mermaid, KaTeX, syntax highlighting, selectable text);
// otherwise it falls back to the pure-Go goldmark-pdf engine, which needs no
// browser and works offline. The Result reports the engine used and any
// warnings about content the engine had to filter out.
//
// srcDir is the directory the document lives in, used to resolve relative image
// references; it may be empty for stdin input. allowRemote permits fetching
// remote (http/https) images and assets; when false (the default) nothing is
// loaded from the network.
func GenerateAuto(markdown []byte, srcDir string, allowRemote bool, w io.Writer) (Result, error) {
	if _, ok := FindBrowser(); ok {
		if _, ok := chromiumAssets(); ok {
			// Buffer the chromium output so a mid-render failure can fall back
			// to goldmark without having written a partial PDF to w.
			var buf bytes.Buffer
			if err := generateViaChromium(markdown, srcDir, false, allowRemote, &buf); err == nil {
				_, werr := w.Write(buf.Bytes())
				return Result{Engine: EngineChromium}, werr
			} else {
				log.Printf("mdv: browser PDF engine failed, falling back to goldmark-pdf: %v", err)
			}
		}
	}
	warnings, err := generateViaGoldmark(markdown, srcDir, allowRemote, w)
	return Result{Engine: EngineGoldmark, Warnings: warnings}, err
}
