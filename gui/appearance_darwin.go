//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

static int mdvOSPrefersDark(void) {
	NSString *style = [[NSUserDefaults standardUserDefaults] stringForKey:@"AppleInterfaceStyle"];
	return (style != nil && [style caseInsensitiveCompare:@"Dark"] == NSOrderedSame) ? 1 : 0;
}
*/
import "C"

// osPrefersDark reports whether the macOS system appearance is currently dark.
// It is used to pick the initial native window background when the user's theme
// preference is "system", so an empty window never flashes bright white in dark
// mode (e.g. behind the startup file picker).
func osPrefersDark() bool {
	return C.mdvOSPrefersDark() != 0
}
