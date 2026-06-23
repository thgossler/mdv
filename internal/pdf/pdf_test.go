package pdf

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateViaGoldmarkProducesPDF(t *testing.T) {
	md := []byte("# Title\n\nSome **bold** text and a list:\n\n- one\n- two\n")
	var buf bytes.Buffer
	if _, err := generateViaGoldmark(md, "", false, &buf); err != nil {
		t.Fatalf("generateViaGoldmark: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected non-empty PDF output")
	}
	if got := buf.Bytes()[:5]; string(got) != "%PDF-" {
		t.Fatalf("output does not start with PDF magic: %q", got)
	}
}

func TestGenerateViaGoldmarkStripsFrontmatter(t *testing.T) {
	md := []byte("---\ntitle: Hi\n---\n\n# Body\n")
	var buf bytes.Buffer
	if _, err := generateViaGoldmark(md, "", false, &buf); err != nil {
		t.Fatalf("generateViaGoldmark: %v", err)
	}
	if string(buf.Bytes()[:5]) != "%PDF-" {
		t.Fatal("expected a valid PDF")
	}
}

func TestStripHTMLOutsideCode(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		removed int
	}{
		{"inline tags", "a <u>b</u> c", "a b c", 2},
		{"comment", "x <!-- hide --> y", "x  y", 1},
		{"keep inline code", "use `<div>` here <br>", "use `<div>` here ", 1},
		{"keep fenced code", "```\n<div>&amp;</div>\n```\n<p>gone</p>", "```\n<div>&amp;</div>\n```\ngone", 2},
		{"no html", "plain text only", "plain text only", 0},
		{"multiline comment", "a <!-- one\ntwo --> b", "a \n b", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, n := stripHTMLOutsideCode(c.in)
			if got != c.want {
				t.Errorf("body = %q, want %q", got, c.want)
			}
			if n != c.removed {
				t.Errorf("removed = %d, want %d", n, c.removed)
			}
		})
	}
}

func TestFilterUnsupportedImagesCounts(t *testing.T) {
	in := "![a](pic.png) ![b](https://x/y.svg) ![c](local.webp)"
	got, n := filterUnsupportedImages(in, false)
	if n != 2 {
		t.Fatalf("removed = %d, want 2", n)
	}
	if got != "![a](pic.png)  " {
		t.Fatalf("body = %q", got)
	}
}

func TestFilterUnsupportedImagesAllowRemote(t *testing.T) {
	in := "![a](https://x/y.png) ![b](https://x/y.svg)"
	// With allowRemote, a remote raster image is kept but a remote SVG is still
	// dropped (the engine cannot embed it).
	got, n := filterUnsupportedImages(in, true)
	if n != 1 {
		t.Fatalf("removed = %d, want 1", n)
	}
	if got != "![a](https://x/y.png) " {
		t.Fatalf("body = %q", got)
	}
}

func TestFindBrowserEnvOverride(t *testing.T) {
	// Point MDV_CHROME at a real existing file (this test binary) to confirm the
	// override path is honoured regardless of an installed browser.
	self, err := os.Executable()
	if err != nil {
		t.Skipf("cannot determine test executable: %v", err)
	}
	t.Setenv("MDV_CHROME", self)
	path, ok := FindBrowser()
	if !ok {
		t.Fatal("expected FindBrowser to honour MDV_CHROME")
	}
	if path != self {
		t.Fatalf("got %q, want %q", path, self)
	}
}

func TestFindBrowserEnvMissingFileIgnored(t *testing.T) {
	t.Setenv("MDV_CHROME", filepath.Join(t.TempDir(), "does-not-exist"))
	// Should not panic or return the bogus path; result depends on the host, so
	// only assert that the bogus override is not returned.
	if path, ok := FindBrowser(); ok && path == os.Getenv("MDV_CHROME") {
		t.Fatal("should not return a non-existent override path")
	}
}

func TestChromiumAssetsUnavailableWithoutBundle(t *testing.T) {
	// In the default (non pdf_bundled) build, no print bundle is embedded.
	if _, ok := chromiumAssets(); ok {
		t.Skip("print bundle is embedded in this build")
	}
}
