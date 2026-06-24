// Command mdv is the Markdown Viewer launcher. It is pure Go with no
// webview linkage, so it always starts - even in a headless container. Based on
// the environment and flags it renders to the console, runs the terminal UI, or
// spawns the bundled GUI helper (falling back automatically if the GUI cannot
// run).
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/thgossler/mdv/internal/console"
	"github.com/thgossler/mdv/internal/core"
	"github.com/thgossler/mdv/internal/launcher"
	"github.com/thgossler/mdv/internal/pdf"
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
		flagPDF      = flag.String("pdf", "", "render the input to a PDF at the given file or folder path and exit")
		flagForce    = flag.Bool("force", false, "overwrite an existing --pdf output file without prompting")
		flagRemote   = flag.Bool("remote", false, "allow downloading remote (http/https) images and assets (console, TUI, GUI and --pdf)")
		flagIgnore   = flag.String("ignore", "", "comma-separated .gitignore-style patterns to exclude from the document list (runtime only, not saved)")
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

	// The "update" subcommand checks GitHub Releases and, when a newer version
	// exists, launches the install script detached and exits immediately so the
	// script can replace this executable.
	if flag.Arg(0) == "update" {
		return runUpdate(cfg)
	}

	// Best-effort, once per install: add an "Open with mdv" entry to the OS file
	// manager for Markdown files. A marker in state.jsonc keeps later launches
	// fast by skipping the work after the first successful registration.
	if err := core.EnsureFileAssociations(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: file association setup: %v\n", err)
	}

	// A --max-width on the command line overrides the configured cap.
	if *flagMaxWidth > 0 {
		cfg.MaxWidth = *flagMaxWidth
	}

	// A --images value overrides the configured image rendering mode.
	if *flagImages != "" {
		cfg.Images = *flagImages
	}

	// A --remote flag enables remote (http/https) image and asset loading across
	// every viewer: console and TUI honor it via the in-process config, while the
	// GUI helper (a separate process) reads MDV_REMOTE on startup so its toolbar
	// remote-image toggle begins enabled. PDF export reads *flagRemote directly.
	if *flagRemote {
		cfg.ImagesRemote = true
		os.Setenv(launcher.MDVRemoteEnv, "1")
	}

	// A --ignore value filters the visible document list for this run only. In
	// console/TUI modes the patterns are applied inside ListMarkdownFiles via the
	// in-process config. For the GUI helper (a separate process) the raw flag
	// value is forwarded via MDV_IGNORE and applied on startup without being
	// saved to state.jsonc.
	if *flagIgnore != "" {
		var patterns []string
		for _, p := range strings.Split(*flagIgnore, ",") {
			if p = strings.TrimSpace(p); p != "" {
				patterns = append(patterns, p)
			}
		}
		cfg.ExcludePatterns = patterns
		os.Setenv(launcher.MDVIgnoreEnv, *flagIgnore)
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

	pref := launcher.Preference{
		ForceGUI:     *flagGUI,
		ForceTUI:     *flagTUI,
		ForceConsole: *flagConsole || *flagC,
	}
	mode := launcher.DetectMode(pref)

	// Resolve input. A positional argument names a file or folder; with no
	// argument but markdown piped on stdin, read that content into memory.
	// With no input at all, fall through with an InputNone below.
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
	}
	// Headless PDF export: when --pdf is given, render the input to a PDF and
	// exit without showing any UI. This works over SSH and in containers because
	// it never needs a webview (it uses an installed browser when available and
	// otherwise a pure-Go renderer).
	if *flagPDF != "" {
		return runPDFExport(*flagPDF, in, *flagForce, *flagRemote)
	}

	if in.Kind == core.InputNone {
		// No file, folder, or piped content was given. When a GUI will be
		// shown - e.g. launched by double-click from Finder/Explorer, or forced
		// with --gui - open the graphical file picker instead of doing nothing.
		// Only when no GUI will be shown (TUI/console) do we print usage.
		if mode == launcher.ModeGUI {
			if err := launcher.SpawnGUIPicker(); err == nil {
				return 0
			} else {
				// Surface the reason so users can diagnose GUI startup failures.
				fmt.Fprintf(os.Stderr, "warning: could not open GUI (%v)\n", err)
			}
		}
		usage()
		return 2
	}

	// Begin an asynchronous update check; results are consumed best-effort.
	updCh := startUpdateCheck(cfg)

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
			// No interactive terminal (e.g. stdin and stdout both piped): the
			// TUI cannot run, so render to the console instead.
			if errors.Is(err, tui.ErrNoTerminal) {
				return runConsole(cfg, in, updCh, *flagNoColor)
			}
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		return 0

	default: // ModeConsole
		return runConsole(cfg, in, updCh, *flagNoColor)
	}
}

// runPDFExport renders the resolved input to a PDF file and exits. It accepts a
// single Markdown file or piped stdin (a folder or empty input is an error),
// resolves the output path from the --pdf argument, picks the best available
// engine, writes the file and prints its path. It never shows a UI, so it is
// safe over SSH and in headless containers.
func runPDFExport(pdfArg string, in core.Input, force, allowRemote bool) int {
	var markdown []byte
	var inputName, srcDir string
	switch in.Kind {
	case core.InputFile:
		data, err := core.ReadMarkdownFile(in.Path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: reading %q: %v\n", in.Path, err)
			return 1
		}
		markdown = data
		inputName = filepath.Base(in.Path)
		srcDir = filepath.Dir(in.Path)
	case core.InputStdin:
		markdown = in.Data
	default:
		fmt.Fprintln(os.Stderr, "error: --pdf requires a Markdown file or piped stdin input")
		return 1
	}

	out, err := core.ResolvePDFOutputPath(pdfArg, inputName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Guard against clobbering an existing file. With --force, overwrite
	// silently; otherwise ask for confirmation when a terminal is attached, and
	// refuse when input is non-interactive (e.g. stdin piped) since there is no
	// way to prompt.
	if !force {
		if _, statErr := os.Stat(out); statErr == nil {
			if in.Kind == core.InputFile && console.StdinIsTTY() {
				if !confirmOverwrite(out) {
					fmt.Fprintln(os.Stderr, "aborted: existing file not overwritten")
					return 1
				}
			} else {
				fmt.Fprintf(os.Stderr, "error: %q already exists; pass --force to overwrite\n", out)
				return 1
			}
		}
	}

	f, err := os.Create(out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: creating %q: %v\n", out, err)
		return 1
	}
	res, genErr := pdf.GenerateAuto(markdown, srcDir, allowRemote, f)
	closeErr := f.Close()
	if genErr != nil {
		os.Remove(out)
		fmt.Fprintf(os.Stderr, "error: generating PDF: %v\n", genErr)
		return 1
	}
	if closeErr != nil {
		fmt.Fprintf(os.Stderr, "error: writing %q: %v\n", out, closeErr)
		return 1
	}

	for _, w := range res.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	fmt.Printf("PDF written to %s (%s engine)\n", out, res.Engine)
	return 0
}

// confirmOverwrite asks the user, on the terminal, whether to overwrite an
// existing file. It returns true only for an explicit yes.
func confirmOverwrite(path string) bool {
	fmt.Fprintf(os.Stderr, "File %q already exists. Overwrite? [y/N]: ", path)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
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
		fmt.Fprintf(os.Stderr, "\n→ New version %s, run `mdv update`\n", upd.Latest)
	}
	return 0
}

// runUpdate implements the `mdv update` subcommand. It performs a fresh version
// check and, when a newer release exists, launches the platform install script
// detached and returns so mdv can exit immediately - letting the script replace
// this executable (required on Windows, where a running binary is locked).
func runUpdate(cfg core.Defaults) int {
	fmt.Println("Checking for updates…")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	info, err := core.CheckForUpdateNow(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: update check failed: %v\n", err)
		return 1
	}
	if !info.Available {
		fmt.Printf("mdv %s is already up to date.\n", strings.TrimPrefix(core.Version, "v"))
		return 0
	}

	fmt.Printf("Updating mdv %s → %s …\n", strings.TrimPrefix(core.Version, "v"), info.Latest)
	if err := core.SpawnInstaller(cfg.UpdateRepo); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Println("The installer is running in the background; mdv will now exit so it can be replaced.")
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
	valueFlags := map[string]bool{"-max-width": true, "--max-width": true, "-images": true, "--images": true, "-pdf": true, "--pdf": true, "-ignore": true, "--ignore": true}
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
	fmt.Fprintf(w, "%s %s - %s\n\n", core.AppName, core.Version, core.AppTagline)
	fmt.Fprintf(w, "Usage:\n")
	fmt.Fprintf(w, "  %s [flags] <file.md | folder>\n", core.AppName)
	fmt.Fprintf(w, "  %s .          open the current directory as a folder\n", core.AppName)
	fmt.Fprintf(w, "  ... | %s      read markdown piped on stdin\n\n", core.AppName)
	fmt.Fprintf(w, "Commands:\n")
	fmt.Fprintf(w, "  update         check for a newer release and install it, then exit\n\n")
	fmt.Fprintf(w, "Flags:\n")
	fmt.Fprintf(w, "  --tui          force the interactive terminal UI\n")
	fmt.Fprintf(w, "  --gui          force the graphical UI\n")
	fmt.Fprintf(w, "  -c, --console  render to stdout and exit (headless-friendly)\n")
	fmt.Fprintf(w, "  --no-color     disable ANSI colors in console output\n")
	fmt.Fprintf(w, "  --max-width N  cap the render width to N columns\n")
	fmt.Fprintf(w, "  --images MODE  image rendering: auto|graphics|blocks|off\n")
	fmt.Fprintf(w, "  --pdf PATH     render the input to a PDF at PATH (file or folder) and exit\n")
	fmt.Fprintf(w, "  --force        with --pdf, overwrite an existing output file without asking\n")
	fmt.Fprintf(w, "  --remote       allow downloading remote (http/https) images/assets in any viewer\n")
	fmt.Fprintf(w, "  --ignore LIST  comma-separated .gitignore-style patterns to exclude from the document list (runtime only, not saved)\n")
	fmt.Fprintf(w, "  --init-config  write a default settings.jsonc and exit\n")
	fmt.Fprintf(w, "  --version      print version and exit\n\n")
	fmt.Fprintf(w, "Without a graphical environment, mdv automatically uses the terminal UI\n")
	fmt.Fprintf(w, "or console output, so it is safe to run over SSH in headless containers.\n\n")
	fmt.Fprintf(w, "Tip: the navigator's filename filter and document content search both match\n")
	fmt.Fprintf(w, "your query as a smart fuzzy phrase, so \"client approvals\" also finds\n")
	fmt.Fprintf(w, "\"Client-side Approvals\" (even when wrapped across two lines). In the GUI,\n")
	fmt.Fprintf(w, "toggle content search with the ⌕ button next to the navigator filter; in the\n")
	fmt.Fprintf(w, "TUI, press Tab (or Ctrl+B) to open the document list, then type \"//\" to search\n")
	fmt.Fprintf(w, "content (\"/\" filters by name).\n")
}
