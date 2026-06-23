//go:build windows

package main

import (
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var (
	modShlwapi           = syscall.NewLazyDLL("shlwapi.dll")
	procAssocQueryString = modShlwapi.NewProc("AssocQueryStringW")
)

const (
	// assocfNoTruncate fails rather than silently truncating an over-long path.
	assocfNoTruncate = 0x00000020
	// assocStrExecutable asks for the executable that opens a file type,
	// resolving the per-user "UserChoice" association Explorer applies.
	assocStrExecutable = 2
)

// isDefaultHandler reports whether mdv is the Windows default application for
// the file at path. It asks the shell which executable opens the extension and
// matches its base name against mdv's launcher (the program users associate via
// "Open with"). Any lookup failure is treated as "not the default" so the GUI
// errs toward offering the external-open button.
func isDefaultHandler(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return false
	}
	exe, ok := defaultHandlerExecutable(ext)
	if !ok {
		return false
	}
	return strings.EqualFold(filepath.Base(exe), "mdv.exe")
}

// defaultHandlerExecutable returns the path of the executable Windows would use
// to open files with the given extension (including the leading dot), or false
// when none is registered.
func defaultHandlerExecutable(ext string) (string, bool) {
	extPtr, err := syscall.UTF16PtrFromString(ext)
	if err != nil {
		return "", false
	}
	// First call with a nil output buffer to learn the required length.
	var size uint32
	procAssocQueryString.Call(
		uintptr(assocfNoTruncate),
		uintptr(assocStrExecutable),
		uintptr(unsafe.Pointer(extPtr)),
		0,
		0,
		uintptr(unsafe.Pointer(&size)),
	)
	if size == 0 {
		return "", false
	}
	buf := make([]uint16, size)
	ret, _, _ := procAssocQueryString.Call(
		uintptr(assocfNoTruncate),
		uintptr(assocStrExecutable),
		uintptr(unsafe.Pointer(extPtr)),
		0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if ret != 0 { // S_OK == 0
		return "", false
	}
	return syscall.UTF16ToString(buf), true
}
