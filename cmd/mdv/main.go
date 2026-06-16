// Command mdv is the Markdown Document Viewer launcher. It is pure Go with no
// webview linkage, so it always starts — even in a headless container. Based on
// the environment and flags it renders to the console, runs the terminal UI, or
// spawns the bundled GUI helper (falling back automatically if the GUI cannot
// run).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/thgossler/mdv/internal/console"
	"github.com/thgossler/mdv/internal/core"
	"github.com/thgossler/mdv/internal/launcher"
	"github.com/thgossler/mdv/internal/tui"
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		flagTUI     = flag.Bool("tui", false, "force the interactive terminal UI")
		flagGUI     = flag.Bool("gui", false, "force the graphical UI")
		flagConsole = flag.Bool("console", false, "render to stdout and exit (no UI)")
		flagC       = flag.Bool("c", false, "alias for --console")
		flagVersion = flag.Bool("version", false, "print version and exit")
		flagInit    = flag.Bool("init-config", false, "write a default settings.jsonc and exit")
		flagNoColor = flag.Bool("no-color", false, "disable ANSI colors in console output")
	)
	flag.Usage = usage
	flag.Parse()

	if *flagVersion {
		fmt.Println(strings.TrimPrefix(core.Version, "v"))
		return 0
	}

	cfg, cfgErr := core.LoadConfig()
	if cfgErr != nil {
		fmt.Fprintf(os.Stderr, "warning: %v\n", cfgErr)
	}

	if *flagInit {
		path, err := core.WriteDefaultConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		fmt.Printf("Settings file: %s\n", path)
		return 0
	}

	// Resolve input (positional arg). With no argument, show usage and exit.
	arg := flag.Arg(0)
	if arg == "" {
		usage()
		return 2
	}

	in, err := core.ResolveInput(arg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot open %q: %v\n", arg, err)
		return 1
	}
	if in.Kind == core.InputNone {
		usage()
		return 2
	}

	// Begin an asynchronous update check; results are consumed best-effort.
	updCh := startUpdateCheck(cfg)

	pref := launcher.Preference{
		ForceGUI:     *flagGUI,
		ForceTUI:     *flagTUI,
		ForceConsole: *flagConsole || *flagC,
	}
	mode := launcher.DetectMode(pref)

	switch mode {
	case launcher.ModeGUI:
		if err := launcher.SpawnGUI(in.Path); err != nil {
			// GUI unavailable in this build/environment: fall back.
			return runFallback(cfg, in, updCh, *flagNoColor)
		}
		return 0

	case launcher.ModeTUI:
		upd := waitUpdate(updCh, 1500*time.Millisecond)
		if err := tui.Run(cfg, in, upd); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		return 0

	default: // ModeConsole
		return runConsole(cfg, in, updCh, *flagNoColor)
	}
}

// runFallback chooses TUI when a terminal is attached, otherwise console.
func runFallback(cfg core.Defaults, in core.Input, updCh <-chan core.UpdateInfo, noColor bool) int {
	if console.StdoutIsTTY() && console.StdinIsTTY() {
		upd := waitUpdate(updCh, 1500*time.Millisecond)
		if err := tui.Run(cfg, in, upd); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		return 0
	}
	return runConsole(cfg, in, updCh, noColor)
}

func runConsole(cfg core.Defaults, in core.Input, updCh <-chan core.UpdateInfo, noColor bool) int {
	style := ""
	if noColor {
		style = "notty"
	}

	switch in.Kind {
	case core.InputFile:
		if err := console.RenderFile(os.Stdout, in.Path, console.Options{Style: style, ShowHeader: true}); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
	case core.InputFolder:
		files, _ := core.ListMarkdownFiles(in.Dir, cfg)
		if len(files) == 0 {
			fmt.Fprintf(os.Stderr, "No markdown files found in %s\n", in.Dir)
			return 1
		}
		fmt.Printf("Markdown documents in %s:\n\n", in.Dir)
		for _, f := range files {
			fmt.Printf("  %s\n", f.Path)
		}
		fmt.Printf("\nOpen one with: %s <file>\n", core.AppName)
	}

	if upd := waitUpdate(updCh, 200*time.Millisecond); upd.Available {
		fmt.Fprintf(os.Stderr, "\n→ mdv %s is available: %s\n", upd.Latest, upd.DownloadURL)
	}
	return 0
}

// startUpdateCheck runs the version check in the background.
func startUpdateCheck(cfg core.Defaults) <-chan core.UpdateInfo {
	ch := make(chan core.UpdateInfo, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		info, _ := core.CheckForUpdate(ctx, cfg)
		ch <- info
	}()
	return ch
}

// waitUpdate returns the update info if it arrives within d, else a zero value.
func waitUpdate(ch <-chan core.UpdateInfo, d time.Duration) core.UpdateInfo {
	select {
	case info := <-ch:
		return info
	case <-time.After(d):
		return core.UpdateInfo{}
	}
}

func usage() {
	w := flag.CommandLine.Output()
	fmt.Fprintf(w, "%s %s — %s\n\n", core.AppName, core.Version, core.AppTagline)
	fmt.Fprintf(w, "Usage:\n")
	fmt.Fprintf(w, "  %s [flags] <file.md | folder>\n", core.AppName)
	fmt.Fprintf(w, "  %s .            open the current directory as a folder\n\n", core.AppName)
	fmt.Fprintf(w, "Flags:\n")
	fmt.Fprintf(w, "  --tui          force the interactive terminal UI\n")
	fmt.Fprintf(w, "  --gui          force the graphical UI\n")
	fmt.Fprintf(w, "  -c, --console  render to stdout and exit (headless-friendly)\n")
	fmt.Fprintf(w, "  --no-color     disable ANSI colors in console output\n")
	fmt.Fprintf(w, "  --init-config  write a default settings.jsonc and exit\n")
	fmt.Fprintf(w, "  --version      print version and exit\n\n")
	fmt.Fprintf(w, "Without a graphical environment, mdv automatically uses the terminal UI\n")
	fmt.Fprintf(w, "or console output, so it is safe to run over SSH in headless containers.\n")
}
