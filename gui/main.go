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

// appIcon is the application icon (a copy of images/icon.png). Wails uses it on
// macOS for the Dock icon, on Windows as the window/App-switcher/title-bar icon
// fallback, and on Linux for the window icon.
//
//go:embed appicon.png
var appIcon []byte

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
		Icon:        appIcon,
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

	st := LoadLayoutState()
	store := NewLayoutStore(st)
	bridge.layout = store
	bridge.initExcludes()
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
		Width:            defaultWindowWidth,
		Height:           defaultWindowHeight,
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
		if st.Width > 0 {
			opts.Width = st.Width
		}
		if st.Height > 0 {
			opts.Height = st.Height
		}
		opts.X = st.X
		opts.Y = st.Y
	}
	win := app.Window.NewWithOptions(opts)
	bridge.window = win

	// Wire the event emitter so the bridge can stream search results and other
	// notifications to the frontend, and detect ripgrep in the background so the
	// first content search can use it without blocking startup.
	bridge.emit = func(name string, data any) { app.Event.Emit(name, data) }
	go bridge.detectRipgrep()

	// Restore the maximized state and persist subsequent geometry changes. The
	// store debounces writes so a drag or resize burst becomes one file write.
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		if st.Valid && st.Maximized {
			win.Maximise()
		}
	})
	saveGeometry := func() {
		if win.IsMaximised() {
			store.UpdateGeometry(0, 0, 0, 0, true)
			return
		}
		w, h := win.Size()
		if w <= 0 || h <= 0 {
			return
		}
		x, y := win.Position()
		store.UpdateGeometry(x, y, w, h, false)
	}
	win.OnWindowEvent(events.Common.WindowDidMove, func(*application.WindowEvent) { saveGeometry() })
	win.OnWindowEvent(events.Common.WindowDidResize, func(*application.WindowEvent) { saveGeometry() })

	if err := app.Run(); err != nil {
		return err
	}

	// Flush any pending geometry change on exit so nothing is lost.
	store.Flush()
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
