# AGENTS.md - Working on `mdv`

Guidance for AI coding agents (and humans) contributing to **mdv**, the
Markdown Document Viewer. Read this first; it captures the goals, the
non-negotiable constraints, and the exact commands to build, run, and test.

## What mdv is

A minimal, fast, **self-contained** markdown viewer shipped as a single
executable with no installation and no runtime dependencies. It adapts to its
environment:

- **GUI** - native-webview window (default on desktops).
- **TUI** - rich terminal UI when there is no graphical environment.
- **Console** - plain rendered stdout when piped or non-interactive.

## Core goals (optimize for these)

1. **Always starts.** Even in a headless Docker container over SSH. The shipped
   binary is a pure-Go launcher that links **no** webview libraries, so a
   missing `WebKitGTK`/WebView2 never causes a failure - it degrades to TUI or
   console.
2. **Self-contained.** One binary. The GUI helper is embedded (gzip-compressed)
   and only extracted/spawned when a graphical environment is detected.
3. **Small and fast.** Keep the codebase approachable and the dependency
   surface minimal.
4. **Cross-platform.** macOS (universal), Windows (x64), Linux (amd64/arm64).

## Hard constraints (do not violate)

- **The launcher must stay webview-free.** Never import a webview/GUI dependency
  from `cmd/mdv`, `internal/launcher`, or `internal/core`. All native UI code
  lives in `gui/` only. Verify after building:
  `otool -L build/mdv | grep -i webkit` (macOS) must print nothing.
- **Do not modify `LICENSE.md`.** The project is MIT-licensed; the license text
  and copyright stay as-is.
- **All Go code must be `gofmt`-clean** and pass `go vet`.
- **Tests must pass with no errors or warnings** before any change is
  considered done (`go test ./...`).
- **Don't add heavy dependencies** without strong justification - minimalism is
  a feature.

## Architecture map

```
cmd/mdv             pure-Go launcher (NO webview) - picks GUI/TUI/console
internal/core       shared logic: config, links, slugs, backlinks, updates, input
internal/console    glamour-based stdout rendering
internal/tui        Bubble Tea terminal UI
internal/launcher   environment detection + embedded GUI extraction/spawn
gui/                Wails v3 GUI helper (Go bridge + TypeScript frontend)
gui/frontend        Vite + TypeScript markdown rendering pipeline
scripts/            build / sign / install scripts
```

The launcher embeds the GUI helper and extracts it to a per-version cache
directory on first GUI launch, then runs it detached. Because the launcher
links no native UI libraries, it starts cleanly anywhere.

- Module path: `github.com/thgossler/mdv`
- Language/runtime: **Go 1.26+**, **Node.js 18+**, **Wails v3** CLI.

## Build

Prerequisites: Go 1.26+, Node.js 18+, and the Wails v3 CLI
(`go install github.com/wailsapp/wails/v3/cmd/wails3@latest`).

```sh
scripts/build.sh          # macOS/Linux -> build/mdv (macOS = universal arm64+amd64)
pwsh scripts/build.ps1    # Windows     -> build/mdv.exe
```

Pipeline (do not reorder): build frontend → compile GUI helper
(`-tags production`) → gzip into `internal/launcher/assets/mdv-gui.gz` → compile
launcher (`-tags gui_bundled`, `CGO_ENABLED=0`) embedding the helper.

For a quick Go-only compile check without the full pipeline: `go build ./...`.
(The macOS linker may print harmless "object file built for newer macOS"
warnings - these are cosmetic.)

## Run

```sh
./build/mdv README.md        # open a single document
./build/mdv ./docs           # open a folder (sidebar lists markdown files)
./build/mdv --tui README.md  # force the terminal UI
./build/mdv --console README.md  # render to stdout and exit (headless-friendly)
./build/mdv --version
./build/mdv --init-config    # write a default settings.jsonc
```

During development you can also run a mode directly:
`go run ./cmd/mdv --console README.md`.

## Test

The suite covers unit logic (config parsing, link/wikilink resolution,
slugging, backlinks, folder listing, version comparison) and end-to-end CLI
behavior (the built binary's `--version`, `--console`, `--init-config`, and
no-arg usage paths). It is the automated quality gate for every pull request.

```sh
go test ./...                  # unit + end-to-end tests
go test -short ./...           # skip the slower e2e build test
go test -race -coverprofile=coverage.out -covermode=atomic ./...   # what CI runs
go tool cover -func=coverage.out                                   # coverage summary
```

When you add or change behavior, **add or update a test for it**. Keep the
launcher's headless-safety guarantee covered.

## Code conventions

- Run `gofmt -w .` before committing Go changes; `npx tsc --noEmit` in
  `gui/frontend` for the TypeScript side.
- Match the surrounding style. Comments explain *why*, not *what*.
- Keep functions small and the public surface minimal.
- Do not add features, refactors, or files beyond what a task requires.

## Creating a pull request

1. Branch from `main`: `git checkout -b feature/<short-name>` (or
   `fix/<short-name>`).
2. Make the change; keep it focused and small.
3. Ensure quality locally - all of these must be clean:
   - `gofmt -l $(git ls-files '*.go')` prints nothing
   - `go vet ./...`
   - `go test ./...`
   - the project builds (`go build ./...` or `scripts/build.sh`)
   - `LICENSE.md` is unchanged
4. Commit with a clear, imperative message (e.g. `Add Nord code theme`).
5. Push and open the PR with the GitHub CLI when available:
   ```sh
   gh pr create --fill --base main
   ```
   Otherwise push the branch and open the compare URL in the browser.
6. PR description should explain the **why**, list user-visible changes, and
   include screenshots/clips for any UI change. Reference related issues with
   `Fixes #<n>`.

## Writing good issues

A good issue is specific and reproducible. Include:

- **Title**: concise and descriptive (`GUI window does not restore position on Linux`).
- **Type**: bug, feature request, or question.
- **For bugs**: mdv version (`mdv --version`), OS + version, run mode
  (GUI/TUI/console), exact steps to reproduce, expected vs. actual behavior, and
  any error output.
- **For features**: the problem being solved (not just the proposed solution),
  who benefits, and how it fits the "minimal, self-contained, always-starts"
  goals.
- **Search first** to avoid duplicates; link related issues.

Open issues at https://github.com/thgossler/mdv/issues - or use the `/write-issue`
prompt, which searches for duplicates and opens the new-issue form for you.

## Handy references

- Build/run/test tasks are also available in VS Code (Command Palette →
  *Tasks: Run Task* / *Run Test Task*).
- Prompt files in `.github/prompts/` automate common flows: `/build`, `/test`,
  `/ensure-quality`, `/write-issue`.
