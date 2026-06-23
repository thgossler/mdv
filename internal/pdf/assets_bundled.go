//go:build pdf_bundled

package pdf

import (
	"embed"
	"io/fs"
)

// assetsFS holds the compiled frontend "print" harness used to render Markdown
// to PDF via a headless browser. The build pipeline writes the production build
// of gui/frontend into internal/pdf/assets/dist before compiling with
// `-tags pdf_bundled`. Only a .gitkeep is committed, so when the bundle has not
// been produced chromiumAssets reports unavailable and callers fall back to
// goldmark-pdf.
//
//go:embed all:assets
var assetsFS embed.FS

// chromiumAssets returns the embedded print harness rooted at print.html, and
// true when a usable bundle is present.
func chromiumAssets() (fs.FS, bool) {
	sub, err := fs.Sub(assetsFS, "assets/dist")
	if err != nil {
		return nil, false
	}
	if _, err := fs.Stat(sub, "print.html"); err != nil {
		return nil, false
	}
	return sub, true
}
