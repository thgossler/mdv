//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
char *mdvDefaultHandlerBundleID(const char *filePath);
*/
import "C"

import (
	"strings"
	"unsafe"
)

// macOSBundleID is mdv's application bundle identifier, declared in
// scripts/make-macos-app.sh. Detection compares the OS default handler against
// this identifier rather than a filesystem path, because the GUI helper runs
// from a per-version cache directory outside the .app bundle and so cannot find
// the bundle from its own executable path.
const macOSBundleID = "com.thgossler.mdv"

// isDefaultHandler reports whether mdv is the macOS default application for the
// file at path, by comparing the bundle identifier Launch Services resolves for
// it against mdv's own. Any lookup failure is treated as "not the default" so
// the GUI errs toward offering the external-open button.
func isDefaultHandler(path string) bool {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	res := C.mdvDefaultHandlerBundleID(cPath)
	if res == nil {
		return false
	}
	defer C.free(unsafe.Pointer(res))
	return strings.EqualFold(C.GoString(res), macOSBundleID)
}
