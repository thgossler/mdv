//go:build !darwin

package main

// osPrefersDark reports whether the OS prefers a dark appearance. Outside macOS
// the native window background falls back to the light colour for the "system"
// theme; the webview paints the correct themed background immediately after
// load via the prefers-color-scheme fallback tokens in themes.css.
func osPrefersDark() bool { return false }
