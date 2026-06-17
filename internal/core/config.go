package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ConfigDir returns the directory that holds mdv's settings and cache, honoring
// XDG_CONFIG_HOME when set and falling back to ~/.config/mdv on every platform
// (the task specifies ~/.config/mdv for all platforms).
func ConfigDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, AppName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", AppName), nil
}

// ConfigPath returns the full path to settings.jsonc.
func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.jsonc"), nil
}

// LoadConfig returns the effective settings: built-in defaults with any values
// from settings.jsonc merged on top. A missing config file is not an error.
// A malformed config file returns the defaults plus a non-nil error so callers
// can warn without failing.
func LoadConfig() (Defaults, error) {
	cfg := DefaultSettings()

	path, err := ConfigPath()
	if err != nil {
		return cfg, nil // can't locate home; just use defaults silently
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("reading %s: %w", path, err)
	}

	clean := stripJSONC(raw)
	// Merge over defaults: only keys present in the file override defaults.
	if err := json.Unmarshal(clean, &cfg); err != nil {
		return DefaultSettings(), fmt.Errorf("parsing %s: %w", path, err)
	}
	return cfg, nil
}

// WriteDefaultConfig writes a commented template settings file if none exists.
// It returns the path written, or an error. If the file already exists it is
// left untouched and (path, nil) is returned.
func WriteDefaultConfig() (string, error) {
	path, err := ConfigPath()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err == nil {
		return path, nil // already exists
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(defaultConfigTemplate), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// StripJSONC removes comments and trailing commas from a JSONC byte slice so it
// can be parsed by encoding/json. It is exported for other packages (e.g. the
// GUI) that read JSONC files such as state.jsonc.
func StripJSONC(in []byte) []byte { return stripJSONC(in) }

// stripJSONC removes // line comments, /* block */ comments and trailing commas
// from a JSONC byte slice so it can be parsed by encoding/json. It is
// string/escape aware so comment markers inside string literals are preserved.
func stripJSONC(in []byte) []byte {
	var out strings.Builder
	out.Grow(len(in))

	inString := false
	escaped := false
	inLine := false
	inBlock := false

	for i := 0; i < len(in); i++ {
		c := in[i]
		var next byte
		if i+1 < len(in) {
			next = in[i+1]
		}

		switch {
		case inLine:
			if c == '\n' {
				inLine = false
				out.WriteByte(c)
			}
		case inBlock:
			if c == '*' && next == '/' {
				inBlock = false
				i++
			}
		case inString:
			out.WriteByte(c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
		default:
			switch {
			case c == '/' && next == '/':
				inLine = true
				i++
			case c == '/' && next == '*':
				inBlock = true
				i++
			case c == '"':
				inString = true
				out.WriteByte(c)
			default:
				out.WriteByte(c)
			}
		}
	}

	return removeTrailingCommas(out.String())
}

// removeTrailingCommas strips commas that immediately precede a closing } or ]
// (ignoring whitespace). It is string-aware to avoid touching commas in values.
func removeTrailingCommas(s string) []byte {
	var out strings.Builder
	out.Grow(len(s))

	inString := false
	escaped := false

	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			out.WriteByte(c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			out.WriteByte(c)
			continue
		}
		if c == ',' {
			// Look ahead past whitespace for a closing bracket.
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
				j++
			}
			if j < len(s) && (s[j] == '}' || s[j] == ']') {
				continue // drop the comma
			}
		}
		out.WriteByte(c)
	}
	return []byte(out.String())
}

const defaultConfigTemplate = `// mdv — Markdown Document Viewer — user settings (JSONC).
// Delete any key to fall back to the built-in default. Comments and trailing
// commas are allowed. Docs: https://github.com/thgossler/mdv
{
  // "system" | "light" | "dark"
  "theme": "system",

  // Syntax-highlight theme for code blocks (GUI):
  // "glyph" | "github" | "monokai" | "nord" | "solarized-light" | "solarized-dark"
  "codeTheme": "github",

  // Empty uses the OS default font. Set a family name to override.
  "fontFamily": "",
  "fontSizePx": 16,
  "lineHeight": 1.6,
  "contentWidthPx": 860,
  "monospace": false,

  // Cap the console/terminal-UI render width (columns). 0 = use full width.
  "maxWidth": 0,

  // Terminal image rendering (console/TUI):
  //   "auto"     pick the best the terminal supports (kitty/iTerm2/sixel, else
  //              low-res Unicode half-blocks, else alt text)
  //   "graphics" force a pixel protocol (kitty/iTerm2/sixel)
  //   "blocks"   force low-res Unicode half-blocks
  //   "off"      show alt text only
  "images": "auto",
  // Allow fetching http(s) images in console/TUI (on by default; failures
  // fall back to alt text). Set to false to disable remote fetches.
  "imagesRemote": true,

  // Folder navigation label: "filename" | "title"
  "navLabelMode": "filename",

  "liveReload": true,

  "checkForUpdates": true,
  "updateCheckHours": 24
}
`
