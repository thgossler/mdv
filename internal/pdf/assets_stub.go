//go:build !pdf_bundled

package pdf

import "io/fs"

// chromiumAssets reports that no print bundle is embedded in this build, so the
// CLI Chrome engine is unavailable and callers fall back to goldmark-pdf. The
// release build (compiled with -tags pdf_bundled) provides the real bundle.
func chromiumAssets() (fs.FS, bool) { return nil, false }
