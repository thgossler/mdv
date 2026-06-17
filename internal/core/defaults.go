package core

// Defaults holds every tunable setting. Values here are the hard-coded,
// developer-configurable defaults. Anything a user is allowed to change lives
// in ~/.config/mdv/settings.jsonc and is merged over these at startup.
//
// Fields that are intentionally NOT user-configurable today still live here so
// a future release can expose them without code churn.
type Defaults struct {
	// Appearance.
	Theme          string  `json:"theme"`          // "system" | "light" | "dark"
	CodeTheme      string  `json:"codeTheme"`      // syntax highlight theme name (GUI)
	FontFamily     string  `json:"fontFamily"`     // empty => OS default UI/serif font
	FontSizePx     float64 `json:"fontSizePx"`     // base content font size
	LineHeight     float64 `json:"lineHeight"`     // unit-less line-height multiplier
	ContentWidthPx int     `json:"contentWidthPx"` // max readable column width
	Monospace      bool    `json:"monospace"`      // use a monospaced content font
	MaxWidth       int     `json:"maxWidth"`       // console/TUI wrap cap in columns (0 = no cap)

	// Behaviour.
	NavLabelMode   string  `json:"navLabelMode"`   // "filename" | "title"
	FollowExternal bool    `json:"followExternal"` // open http links in OS browser
	LiveReload     bool    `json:"liveReload"`     // watch & auto-refresh
	ZoomStep       float64 `json:"zoomStep"`       // zoom increment per +/- step
	MinZoom        float64 `json:"minZoom"`
	MaxZoom        float64 `json:"maxZoom"`

	// Terminal image rendering (console/TUI):
	//   "auto"     pick the best method the terminal supports
	//   "graphics" force a pixel protocol (kitty/iTerm2/sixel)
	//   "blocks"   force the Unicode half-block renderer
	//   "off"      show alt text only
	Images string `json:"images"`
	// ImagesRemote allows fetching http(s) images in the console/TUI. On by
	// default; failures (no network, restricted environments) fall back to alt
	// text. Set to false to disable remote fetches entirely.
	ImagesRemote bool `json:"imagesRemote"`

	// Updates.
	CheckForUpdates  bool   `json:"checkForUpdates"`
	UpdateRepo       string `json:"updateRepo"`       // "owner/repo" on GitHub
	UpdateCheckHours int    `json:"updateCheckHours"` // min hours between checks

	// Markdown extensions (all on by default).
	EnableMath        bool `json:"enableMath"`
	EnableMermaid     bool `json:"enableMermaid"`
	EnableEmoji       bool `json:"enableEmoji"`
	EnableWikilinks   bool `json:"enableWikilinks"`
	EnableFootnotes   bool `json:"enableFootnotes"`
	EnableAlerts      bool `json:"enableAlerts"`
	EnableAzureDevOps bool `json:"enableAzureDevOps"`

	// Files recognised as markdown documents in folder navigation.
	MarkdownExtensions []string `json:"markdownExtensions"`
}

// DefaultSettings returns a fresh copy of the built-in defaults.
func DefaultSettings() Defaults {
	return Defaults{
		Theme:          "system",
		CodeTheme:      "github",
		FontFamily:     "",
		FontSizePx:     16,
		LineHeight:     1.6,
		ContentWidthPx: 860,
		Monospace:      false,
		MaxWidth:       0,

		NavLabelMode:   "filename",
		FollowExternal: true,
		LiveReload:     true,
		ZoomStep:       0.1,
		MinZoom:        0.5,
		MaxZoom:        3.0,

		Images:       "auto",
		ImagesRemote: true,

		CheckForUpdates:  true,
		UpdateRepo:       "thgossler/mdv",
		UpdateCheckHours: 24,

		EnableMath:        true,
		EnableMermaid:     true,
		EnableEmoji:       true,
		EnableWikilinks:   true,
		EnableFootnotes:   true,
		EnableAlerts:      true,
		EnableAzureDevOps: true,

		MarkdownExtensions: []string{".md", ".markdown", ".mdown", ".mkd", ".mmd"},
	}
}
