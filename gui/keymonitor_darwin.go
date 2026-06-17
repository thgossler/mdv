//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
void mdvInstallKeyMonitor(void);
*/
import "C"

// keyMonitorEmit is invoked from the native NSEvent monitor (see
// keymonitor_darwin.m) on every Home/End press. It is set by installKeyMonitor
// before the monitor is registered.
var keyMonitorEmit func(name string)

//export mdvKeyMonitorCallback
func mdvKeyMonitorCallback(kind C.int) {
	emit := keyMonitorEmit
	if emit == nil {
		return
	}
	switch kind {
	case 0:
		emit("key:home")
	case 1:
		emit("key:end")
	}
}

// installKeyMonitor registers a native local NSEvent monitor that catches the
// Home and End keys (with or without Ctrl/Cmd) before WKWebView swallows them,
// forwarding each press to the frontend through emit. WKWebView consumes
// Home/End for its own (here no-op) document scrolling and never dispatches a
// DOM keydown nor triggers Wails key bindings for them, so this native
// interception is the only reliable path. The monitor returns the event
// unchanged, preserving native caret movement inside focused text fields.
func installKeyMonitor(emit func(name string)) {
	keyMonitorEmit = emit
	C.mdvInstallKeyMonitor()
}
