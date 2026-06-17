#import <Cocoa/Cocoa.h>
#import "_cgo_export.h"

// mdvInstallKeyMonitor adds a local key-down monitor. Returning the event
// unchanged keeps native behaviour intact (e.g. Home/End still move the caret
// inside focused text fields); we only piggy-back a notification to Go so the
// frontend can run its context-sensitive jump for lists and the content view.
void mdvInstallKeyMonitor(void) {
	[NSEvent addLocalMonitorForEventsMatchingMask:NSEventMaskKeyDown
		handler:^NSEvent *(NSEvent *event) {
			switch ([event keyCode]) {
				case 115: // kVK_Home
					mdvKeyMonitorCallback(0);
					break;
				case 119: // kVK_End
					mdvKeyMonitorCallback(1);
					break;
			}
			return event;
		}];
}
