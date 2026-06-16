# mdv — Markdown Document Viewer

A minimal, fast, self-contained markdown viewer for reading documentation with
seamless navigation. One small executable, no installation, no dependencies.

`mdv` adapts to wherever it runs:

- **GUI** — a native-webview window with full rendering (default on desktops).
- **TUI** — a rich terminal UI when no graphical environment is available.
- **Console** — plain rendered output to stdout when piped or non-interactive.

It is built so it **always starts**, including inside headless Docker containers
over SSH: the distributed binary is a pure-Go launcher with **zero webview
linkage**, so missing `WebKitGTK`/GUI libraries never cause a failure. The GUI
is a separate helper embedded in the binary and only spawned when a graphical
environment is actually present.

## Install

No package managers needed — the install scripts download a single executable
from GitHub Releases.

**macOS / Linux:**

```sh
curl -fsSL https://raw.githubusercontent.com/thgossler/mdv/main/scripts/install.sh | sh
```

**Windows (PowerShell):**

```powershell
irm https://raw.githubusercontent.com/thgossler/mdv/main/scripts/install.ps1 | iex
```

Or download a binary directly from the [Releases](https://github.com/thgossler/mdv/releases)
page:

| Platform           | Asset                        |
| ------------------ | ---------------------------- |
| macOS (universal)  | `mdv-darwin-universal.tar.gz` |
| Windows (x64)      | `mdv-windows-amd64.exe`       |
| Linux (amd64)      | `mdv-linux-amd64.tar.gz`      |
| Linux (arm64)      | `mdv-linux-arm64.tar.gz`      |

## Usage

```sh
mdv README.md          # open a single document
mdv ./docs             # open a folder (sidebar lists all markdown files)
mdv --tui README.md    # force the terminal UI
mdv --console README.md  # render to stdout and exit
mdv --version
mdv --init-config      # write a default settings.jsonc
```

| Flag            | Description                                      |
| --------------- | ------------------------------------------------ |
| `--tui`         | Force the interactive terminal UI                |
| `--gui`         | Force the graphical UI                           |
| `--console`, `-c` | Render to stdout and exit                       |
| `--no-color`    | Disable ANSI colors in console output            |
| `--version`     | Print version and exit                           |
| `--init-config` | Write a default settings file and exit           |

## Features

- GitHub Flavored Markdown (tables, task lists, strikethrough, autolinks)
- GitHub alerts (`> [!NOTE]`, `[!TIP]`, `[!IMPORTANT]`, `[!WARNING]`, `[!CAUTION]`)
- Math via KaTeX (`$inline$` and `$$block$$`)
- Mermaid diagrams (theme-aware)
- Syntax highlighting with 6 themes (Glyph, GitHub, Monokai, Nord, Solarized Light/Dark)
- Wikilinks `[[doc]]`, `[[doc|alias]]`, `[[doc#heading]]` with a backlinks panel
- Table-of-contents sidebar with scroll-spy, heading anchors
- CSV/TSV fenced blocks rendered as tables
- YAML frontmatter metadata block, emoji shortcodes
- Azure DevOps constructs (`[[_TOC_]]`, `:::video:::`, `#123` work items)
- Sanitized inline HTML (DOMPurify)
- In-document search (Cmd/Ctrl+F), live reload, drag-and-drop
- Zoom (Cmd/Ctrl + wheel / +/-), light/dark/system themes, configurable fonts
- History navigation, link target preview in the status bar
- "Open in new window", automatic update checks

## Configuration

`mdv` works with zero configuration. To customize, create
`~/.config/mdv/settings.jsonc` (or run `mdv --init-config`). The file is JSONC
(JSON with comments and trailing commas) and is merged over the built-in
defaults. On Windows/macOS the location follows `XDG_CONFIG_HOME` if set.

```jsonc
{
  // "system" | "light" | "dark"
  "theme": "system",
  "codeTheme": "github",
  "fontFamily": "",
  "fontSizePx": 16,
  "lineHeight": 1.6,
  "contentWidthPx": 860,
  "navLabelMode": "filename", // or "title"
  "liveReload": true,
  "checkForUpdates": true,
}
```

## Building from source

Requires Go 1.26+, Node.js 18+, and the [Wails v3](https://v3alpha.wails.io/)
CLI (`go install github.com/wailsapp/wails/v3/cmd/wails3@latest`).

```sh
scripts/build.sh          # macOS/Linux -> build/mdv
pwsh scripts/build.ps1    # Windows     -> build/mdv.exe
```

The script builds the frontend, compiles the GUI helper, compresses and embeds
it into the launcher, and produces a single self-contained executable. On macOS
the result is a universal (arm64 + amd64) binary.

### Architecture

```
cmd/mdv             pure-Go launcher (no webview linkage) — picks GUI/TUI/console
internal/core       shared logic: config, links, slugs, backlinks, updates
internal/console    glamour-based stdout rendering
internal/tui        Bubble Tea terminal UI
internal/launcher   environment detection + embedded GUI extraction/spawn
gui/                Wails v3 GUI helper (Go bridge + TypeScript frontend)
```

The launcher embeds the GUI helper (gzip-compressed) and extracts it to a
per-version cache directory on first GUI launch, then runs it detached. Because
the launcher itself links no native UI libraries, it starts cleanly in any
environment and degrades gracefully to TUI or console.

## License

MIT
