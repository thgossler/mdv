#import <Cocoa/Cocoa.h>
#include <stdlib.h>
#include <string.h>

// mdvDefaultHandlerBundleID returns a newly-allocated C string with the bundle
// identifier of the application macOS would use to open the file at filePath
// (e.g. "com.microsoft.VSCode"), or NULL when it cannot be determined. The
// caller owns the returned string and must free() it.
char *mdvDefaultHandlerBundleID(const char *filePath) {
	@autoreleasepool {
		if (filePath == NULL) {
			return NULL;
		}
		NSString *p = [NSString stringWithUTF8String:filePath];
		if (p == nil) {
			return NULL;
		}
		NSURL *fileURL = [NSURL fileURLWithPath:p];
		NSURL *appURL = [[NSWorkspace sharedWorkspace] URLForApplicationToOpenURL:fileURL];
		if (appURL == nil) {
			return NULL;
		}
		NSBundle *bundle = [NSBundle bundleWithURL:appURL];
		NSString *bid = [bundle bundleIdentifier];
		if (bid == nil) {
			return NULL;
		}
		const char *utf8 = [bid UTF8String];
		if (utf8 == NULL) {
			return NULL;
		}
		return strdup(utf8);
	}
}
