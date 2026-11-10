package pdf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

var (
	errNoBrowser = errors.New("no Chrome/Chromium/Edge browser found")
	errNoBundle  = errors.New("print bundle not embedded in this build")
)

// A4 page dimensions in inches, used for Chrome's printToPDF. Page margins are
// applied via the print harness CSS (@page { margin: 1cm }) rather than the
// printToPDF margin parameters: a CSS @page margin rule overrides the protocol
// margins in headless Chrome, so the protocol margins are set to zero here to
// avoid a conflict and the CSS rule is the single source of an exact, uniform
// 1 cm margin on every edge of every page.
const (
	a4WidthIn  = 8.27
	a4HeightIn = 11.69
	marginIn   = 0.0
)

// runChromePDF launches the detected browser headlessly, runs the given
// preparatory actions, navigates to navURL, waits until readyJS evaluates
// truthy (or the body is ready when readyJS is empty), and returns the printed
// PDF bytes.
//
// When allowRemote is false every network request to a non-loopback origin is
// blocked, so the document cannot fetch remote images or other assets (tracking
// pixels, IP/User-Agent leakage). The local print harness and the document's
// own directory are served from 127.0.0.1 and therefore still load.
func runChromePDF(parent context.Context, execPath string, pre []chromedp.Action[chromedp.Void], navURL, readyJS string, allowRemote bool) ([]byte, error) {
	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(execPath),
		// Local, user-owned content only; --no-sandbox lets the renderer run as
		// root inside containers (a common headless/CI scenario).
		chromedp.NoSandbox,
		chromedp.DisableGPU,
		chromedp.Flag("hide-scrollbars", true),
	)
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(parent, allocOpts...)
	defer cancelAlloc()
	ctx, cancelCtx := chromedp.NewContext(allocCtx)
	defer cancelCtx()
	ctx, cancelTimeout := context.WithTimeout(ctx, 90*time.Second)
	defer cancelTimeout()

	// Block all non-loopback network access unless remote assets are allowed, so
	// rendering a PDF never silently phones home.
	if !allowRemote {
		events := chromedp.Events(ctx, fetch.RequestPaused)
		go func() {
			for event, err := range events {
				if err != nil {
					return
				}
				if isLocalRequestURL(event.Request.URL) {
					_, _ = chromedp.Call(ctx, fetch.ContinueRequest, fetch.ContinueRequestParams{RequestID: event.RequestID})
				} else {
					_, _ = chromedp.Call(ctx, fetch.FailRequest, fetch.FailRequestParams{RequestID: event.RequestID, ErrorReason: network.ErrorReasonBlockedByClient})
				}
			}
		}()
	}

	actions := append([]chromedp.Action[chromedp.Void]{}, pre...)
	if !allowRemote {
		actions = append(actions, chromedp.Func(func(ctx context.Context, target *chromedp.Target) error {
			_, err := cdp.Call(ctx, target, fetch.Enable, fetch.EnableParams{})
			return err
		}))
	}
	actions = append(actions, chromedp.Navigate(navURL))
	if readyJS != "" {
		actions = append(actions, chromedp.Poll[chromedp.Void](readyJS, chromedp.WithPollingTimeout(60*time.Second)))
	} else {
		actions = append(actions, chromedp.WaitReady(chromedp.CSS("body")))
		// Give late layout work (web fonts, inline SVG) a brief moment to settle.
		actions = append(actions, chromedp.Sleep(250*time.Millisecond))
	}

	var buf []byte
	actions = append(actions, chromedp.Func(func(ctx context.Context, target *chromedp.Target) error {
		result, err := cdp.Call(ctx, target, page.PrintToPDF, page.PrintToPDFParams{
			PrintBackground:   new(true),
			PaperWidth:        a4WidthIn,
			PaperHeight:       a4HeightIn,
			MarginTop:         new(marginIn),
			MarginBottom:      new(marginIn),
			MarginLeft:        new(marginIn),
			MarginRight:       new(marginIn),
			PreferCSSPageSize: new(false),
		})
		buf = result.Data
		return err
	}))

	if err := chromedp.Do(ctx, actions...); err != nil {
		return nil, fmt.Errorf("chrome print to PDF: %w", err)
	}
	return buf, nil
}

// generateViaChromium renders markdown to PDF with full fidelity (Mermaid,
// KaTeX, syntax highlighting) by serving the embedded print harness to a
// headless browser. It is the CLI's preferred engine and requires both an
// installed browser and the embedded print bundle (release builds). The
// extended flag enables the opt-in inline Markdown syntax (math, sub/sup, …);
// allowRemote permits fetching remote (http/https) images and assets.
func generateViaChromium(markdown []byte, srcDir string, extended, allowRemote bool, w io.Writer) error {
	execPath, ok := FindBrowser()
	if !ok {
		return errNoBrowser
	}
	assets, ok := chromiumAssets()
	if !ok {
		return errNoBundle
	}

	srv, baseURL, err := startPrintServer(assets, srcDir)
	if err != nil {
		return err
	}
	defer srv.Close()

	mdJSON, err := json.Marshal(string(markdown))
	if err != nil {
		return err
	}
	script := "window.__mdvMarkdown=" + string(mdJSON) + ";window.__mdvExtended=" + boolJS(extended) + ";"
	pre := []chromedp.Action[chromedp.Void]{
		chromedp.Func(func(ctx context.Context, target *chromedp.Target) error {
			_, err := cdp.Call(ctx, target, page.AddScriptToEvaluateOnNewDocument, page.AddScriptToEvaluateOnNewDocumentParams{Source: script})
			return err
		}),
	}

	buf, err := runChromePDF(context.Background(), execPath, pre, baseURL+"/print.html", "window.__mdvPdfReady === true", allowRemote)
	if err != nil {
		return err
	}
	_, err = w.Write(buf)
	return err
}

func boolJS(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// isLocalRequestURL reports whether a request URL is safe to load while remote
// access is blocked: loopback http(s) (the print server) and non-network schemes
// (data:, blob:, file:). Any other http(s) host is treated as remote.
func isLocalRequestURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch u.Scheme {
	case "http", "https":
		host := u.Hostname()
		return host == "127.0.0.1" || host == "localhost" || host == "::1"
	default:
		// data:, blob:, file:, about:, and relative same-origin requests are not
		// remote network fetches.
		return true
	}
}

// startPrintServer serves the embedded print harness assets and, as a fallback,
// the document's source directory (so relative images resolve). It binds to an
// ephemeral loopback port and serves only those two sources.
func startPrintServer(assets fs.FS, srcDir string) (*http.Server, string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", err
	}

	assetServer := http.FileServer(http.FS(assets))
	var docServer http.Handler
	if srcDir != "" {
		docServer = http.FileServer(http.Dir(srcDir))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(r.URL.Path)), "/")
		if clean == "" {
			clean = "print.html"
		}
		if _, statErr := fs.Stat(assets, clean); statErr == nil {
			assetServer.ServeHTTP(rw, r)
			return
		}
		if docServer != nil {
			docServer.ServeHTTP(rw, r)
			return
		}
		http.NotFound(rw, r)
	})

	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	return srv, "http://" + ln.Addr().String(), nil
}
