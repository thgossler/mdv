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
		flagTUI      = flag.Bool("tui", false, "force the interactive terminal UI")
		flagGUI      = flag.Bool("gui", false, "force the graphical UI")
		flagConsole  = flag.Bool("console", false, "render to stdout and exit (no UI)")
		flagC        = flag.Bool("c", false, "alias for --console")
		flagVersion  = flag.Bool("version", false, "print version and exit")
		flagInit     = flag.Bool("init-config", false, "write a default settings.jsonc and exit")
		flagNoColor  = flag.Bool("no-color", false, "disable ANSI colors in console output")
		flagMaxWidth = flag.Int("max-width", 0, "cap the render width in columns (0 = full width)")
		flagImages   = flag.String("images", "", "image rendering: auto|graphics|blocks|off")
	)
	flag.Usage = usage
	// Accept flags on either side of the positional input argument. Go's flag
	// package stops at the first non-flag token, so reorder flags ahead of
	// positionals before parsing. All mdv flags are booleans, so no flag takes
	// a separate value; everything after a literal "--" stays positional.
	_ = flag.CommandLine.Parse(reorderArgs(os.Args[1:]))

	if *flagVersion {
		fmt.Println(strings.TrimPrefix(core.Version, "v"))
		return 0
	}

	cfg, cfgErr := core.LoadConfig()
	if cfgErr != nil {
		fmt.Fprintf(os.Stderr, "warning: %v\n", cfgErr)
	}

	// A --max-width on the command line overrides the configured cap.
	if *flagMaxWidth > 0 {
		cfg.MaxWidth = *flagMaxWidth
	}

	// A --images value overrides the configured image rendering mode.
	if *flagImages != "" {
		cfg.Images = *flagImages
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

	// Resolve input. A positional argument names a file or folder; with no
	// argument but markdown piped on stdin, read that content into memory.
	// Otherwise show usage and exit.
	var in core.Input
	var err error
	arg := flag.Arg(0)
	switch {
	case arg != "":
		in, err = core.ResolveInput(arg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: cannot open %q: %v\n", arg, err)
			return 1
		}
	case !console.StdinIsTTY():
		in, err = core.ReadStdin(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: reading stdin: %v\n", err)
			return 1
		}
	default:
		usage()
		return 2
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
		if err := spawnGUIForInput(in); err != nil {
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

// spawnGUIForInput launches the GUI helper for the resolved input. Stdin
// content is materialised into a temporary file (the GUI is a separate process
// that loads documents by path) and the helper is told via MDV_STDIN_TEMP to
// delete that file when its window closes.
func spawnGUIForInput(in core.Input) error {
	if in.Kind == core.InputStdin {
		tmp, err := core.WriteStdinTempFile(in.Data)
		if err != nil {
			return err
		}
		// SpawnDetached inherits the environment, so the child sees this var.
		os.Setenv("MDV_STDIN_TEMP", tmp)
		if err := launcher.SpawnGUI(tmp); err != nil {
			os.Remove(tmp)
			return err
		}
		return nil
	}
	return launcher.SpawnGUI(in.Path)
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
	case core.InputStdin:
		if err := console.Render(os.Stdout, string(in.Data), "", console.Options{Style: style, MaxWidth: cfg.MaxWidth, ImageMode: cfg.Images, AllowRemoteImages: cfg.ImagesRemote}); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
	case core.InputFile:
		if err := console.RenderFile(os.Stdout, in.Path, console.Options{Style: style, MaxWidth: cfg.MaxWidth, ShowHeader: true, ImageMode: cfg.Images, AllowRemoteImages: cfg.ImagesRemote}); err != nil {
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

// reorderArgs returns args with all flag tokens (starting with "-") moved ahead
// of positional arguments so flags are accepted on either side of the input
// path. Everything after a literal "--" terminator is treated as positional.
// Most mdv flags are booleans; flags that take a value (e.g. --max-width) carry
// their following token along when it is not given in --flag=value form.
func reorderArgs(args []string) []string {
	valueFlags := map[string]bool{"-max-width": true, "--max-width": true, "-images": true, "--images": true}
	var flags, positionals []string
	terminated := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case terminated:
			positionals = append(positionals, a)
		case a == "--":
			terminated = true
		case len(a) > 1 && strings.HasPrefix(a, "-"):
			flags = append(flags, a)
			name := a
			if eq := strings.IndexByte(a, '='); eq >= 0 {
				name = a[:eq]
			} else if valueFlags[name] && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		default:
			positionals = append(positionals, a)
		}
	}
	return append(flags, positionals...)
}

func usage() {
	w := flag.CommandLine.Output()
	fmt.Fprintf(w, "%s %s — %s\n\n", core.AppName, core.Version, core.AppTagline)
	fmt.Fprintf(w, "Usage:\n")
	fmt.Fprintf(w, "  %s [flags] <file.md | folder>\n", core.AppName)
	fmt.Fprintf(w, "  %s .          open the current directory as a folder\n", core.AppName)
	fmt.Fprintf(w, "  ... | %s      read markdown piped on stdin\n\n", core.AppName)
	fmt.Fprintf(w, "Flags:\n")
	fmt.Fprintf(w, "  --tui          force the interactive terminal UI\n")
	fmt.Fprintf(w, "  --gui          force the graphical UI\n")
	fmt.Fprintf(w, "  -c, --console  render to stdout and exit (headless-friendly)\n")
	fmt.Fprintf(w, "  --no-color     disable ANSI colors in console output\n")
	fmt.Fprintf(w, "  --max-width N  cap the render width to N columns\n")
	fmt.Fprintf(w, "  --images MODE  image rendering: auto|graphics|blocks|off\n")
	fmt.Fprintf(w, "  --init-config  write a default settings.jsonc and exit\n")
	fmt.Fprintf(w, "  --version      print version and exit\n\n")
	fmt.Fprintf(w, "Without a graphical environment, mdv automatically uses the terminal UI\n")
	fmt.Fprintf(w, "or console output, so it is safe to run over SSH in headless containers.\n\n")
	fmt.Fprintf(w, "Tip: the navigator's filename filter and document content search both match\n")
	fmt.Fprintf(w, "your query as a smart fuzzy phrase, so \"client approvals\" also finds\n")
	fmt.Fprintf(w, "\"Client-side Approvals\" (even when wrapped across two lines). In the GUI,\n")
	fmt.Fprintf(w, "toggle content search with the ⌕ button next to the navigator filter; in the\n")
	fmt.Fprintf(w, "TUI, type \"//\" in the document list to search content (\"/\" filters by name).\n")
}
