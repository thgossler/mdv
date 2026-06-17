// Command mdv-gui is the graphical front-end of mdv, built on Wails v3. It is a
// separate executable from the launcher: the launcher embeds it and spawns it
// only when a webview environment is available, so the launcher itself never
// links a webview and stays safe to run in headless containers.
package main

import (
	"embed"
	"log"
	"os"
	"strings"

	"github.com/thgossler/mdv/internal/core"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if err := runGUI(); err != nil {
		log.Fatal(err)
	}
}

func runGUI() error {
	cfg, _ := core.LoadConfig()

	in := resolveInput(cfg)

	// A stdin temp file is owned by this process: remove it once the window
	// closes so piped content leaves nothing behind on disk.
	if tmp := os.Getenv("MDV_STDIN_TEMP"); tmp != "" {
		defer os.Remove(tmp)
	}

	app := application.New(application.Options{
		Name:        core.AppName,
		Description: core.AppTagline,
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	watcher := NewWatcher(app)
	bridge := NewBridge(cfg, in)
	bridge.watcher = watcher
	app.RegisterService(application.NewService(bridge))

	app.Menu.Set(buildMenu(app))

	st := LoadWindowState()
	// Home/End (with or without Ctrl/Cmd) are swallowed by WKWebView before they
	// reach the webview's JS or Wails' key-binding system: WKWebView consumes them
	// for its own (here no-op) document scrolling and dispatches neither a DOM
	// keydown nor an NSWindow keyDown: that Wails could bind to. A native local
	// NSEvent monitor installed at startup is the only reliable interception
	// point; it emits an event the frontend turns into a context-sensitive jump
	// (first/last list item, or scroll the content view to top/bottom). Arrow keys
	// are left to the webview and handled in JS.
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		installKeyMonitor(func(name string) { app.Event.Emit(name, "") })
	})
	opts := application.WebviewWindowOptions{
		Title:            core.AppName,
		Width:            1100,
		Height:           780,
		MinWidth:         480,
		MinHeight:        360,
		BackgroundColour: application.NewRGB(255, 255, 255),
		URL:              "/",
		Mac: application.MacWindow{
			// Keep this 0. A non-zero InvisibleTitleBarHeight makes Wails install a
			// native left-mouse monitor that calls performWindowDragWithEvent
			// immediately on mousedown across the whole top band (it only spares the
			// left/right 5px, never the top edge). That immediate native drag
			// preempts the native top-edge / top-corner resize, so the window jumps
			// instead of resizing. With it disabled, dragging is driven solely by the
			// CSS `--wails-draggable: drag` toolbar, which fires on mousemove and lets
			// the native frame win the top edge just like every other edge. That JS
			// path still routes through performWindowDragWithEvent, so dragging a
			// zoomed window restores it to the pre-zoom size (standard OS behaviour).
			InvisibleTitleBarHeight: 0,
			TitleBar:                application.MacTitleBarHiddenInset,
			Backdrop:                application.MacBackdropNormal,
		},
	}
	if st.Valid {
		opts.Width = st.Width
		opts.Height = st.Height
		opts.X = st.X
		opts.Y = st.Y
	}
	win := app.Window.NewWithOptions(opts)
	_ = win

	if err := app.Run(); err != nil {
		return err
	}

	// Persist window geometry on exit.
	if w, h := win.Size(); w > 0 {
		x, y := win.Position()
		SaveWindowState(WindowState{X: x, Y: y, Width: w, Height: h})
	}
	return nil
}

// resolveInput determines what to open from the first CLI argument. The
// launcher passes an absolute path; when run directly with no argument it falls
// back to the current directory so the GUI still opens.
func resolveInput(cfg core.Defaults) core.Input {
	arg := ""
	if len(os.Args) > 1 {
		arg = os.Args[1]
	}
	if arg == "" {
		if wd, err := os.Getwd(); err == nil {
			arg = wd
		}
	}
	// Split off an optional "#fragment" so a new window can open at a section.
	fragment := ""
	if i := strings.LastIndex(arg, "#"); i > 0 {
		if _, err := os.Stat(arg[:i]); err == nil {
			fragment = arg[i+1:]
			arg = arg[:i]
		}
	}
	in, err := core.ResolveInput(arg)
	if err != nil || in.Kind == core.InputNone {
		if wd, err := os.Getwd(); err == nil {
			in = core.Input{Kind: core.InputFolder, Path: wd, Dir: wd}
		}
	}
	// When the launcher piped stdin into a temporary file, resolve relative
	// links and images against the working directory mdv was launched from
	// rather than the (throwaway) temp directory.
	if os.Getenv("MDV_STDIN_TEMP") == in.Path && in.Path != "" {
		if wd, err := os.Getwd(); err == nil {
			in.Dir = wd
		}
	}
	in.Fragment = fragment
	return in
}
