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
	opts := application.WebviewWindowOptions{
		Title:            core.AppName,
		Width:            1100,
		Height:           780,
		MinWidth:         480,
		MinHeight:        360,
		BackgroundColour: application.NewRGB(255, 255, 255),
		URL:              "/",
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 38,
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
	in.Fragment = fragment
	return in
}
