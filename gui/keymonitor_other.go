//go:build !darwin

package main

// installKeyMonitor is a no-op on non-darwin platforms; Home/End handling there
// relies on the webview dispatching normal DOM keydown events.
func installKeyMonitor(emit func(name string)) {}
