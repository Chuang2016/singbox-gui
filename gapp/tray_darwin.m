// Menu-bar residency bridge for the Gio front-end (macOS only).
// Go side: tray_darwin.go.
//
// Threading contract (preview.14 — read before touching this file):
//
//   * The main thread runs Gio's NSApp run loop. Gio delivers
//     window events to the Go client goroutine through a blocking
//     rendezvous: while an event is being delivered, the main
//     thread waits for the client to finish it and ask for the
//     next one. Any code that runs on the main thread (menu
//     actions included) must therefore never wait for the main
//     queue, and must never synchronously drive Gio back into a
//     frame delivery — a frame for a window that is not on screen
//     may never complete, which wedges both threads for good.
//
//   * Everything here is fire-and-forget: entry points either run
//     inline when already on the main thread or dispatch_async to
//     the main queue. The single exception is sbguiTrayStart's
//     bounded semaphore, documented there; it is waited on from a
//     plain Go goroutine, never from the main thread and never
//     from an AppKit callback.
//
//   * Show/hide requests are deduplicated against the bridge's
//     own visibility state (gWindowHidden / queued flags), so
//     repeated menu clicks cannot stack window operations.

#import <Cocoa/Cocoa.h>
#import <dispatch/dispatch.h>

#include <stdbool.h>
#include <stdlib.h>

extern void sbguiTrayShowCallback(void);
extern void sbguiTrayToggleCallback(void);
extern void sbguiTrayQuitCallback(void);
extern void sbguiTrayReadyCallback(void);
extern void sbguiTrayShownCallback(void);

@interface SbguiTrayTarget : NSObject
- (void)onShow:(id)sender;
- (void)onToggle:(id)sender;
- (void)onQuit:(id)sender;
@end

@implementation SbguiTrayTarget
- (void)onShow:(id)sender { sbguiTrayShowCallback(); }
- (void)onToggle:(id)sender { sbguiTrayToggleCallback(); }
- (void)onQuit:(id)sender { sbguiTrayQuitCallback(); }
@end

static NSStatusItem *gStatusItem = nil;
static NSMenuItem *gStatusMenuItem = nil;
static NSMenuItem *gToggleMenuItem = nil;
static SbguiTrayTarget *gTarget = nil;
static NSWindow *gStashedWindow = nil;
static dispatch_semaphore_t gStartSem = NULL;
static bool gStartOK = false;

// Bridge-owned window visibility state. The state bit itself is
// written only inside main-queue blocks; the queued flags are
// also set from caller threads before dispatching — a lost race
// there can at worst enqueue one redundant block whose execution
// re-checks the state and no-ops.
static bool gWindowHidden = false;
static bool gHideQueued = false;
static bool gShowQueued = false;
static bool gCloseQueued = false;

// Runs block on the main queue: inline when the caller is already
// on the main thread (menu actions), dispatch_async otherwise.
// Never blocks the caller.
static void sbguiDispatchMain(dispatch_block_t block) {
	if ([NSThread isMainThread]) {
		block();
	} else {
		dispatch_async(dispatch_get_main_queue(), block);
	}
}

// Builds the status item and menu on the main queue and reports
// back through a semaphore: the Go caller (a dedicated non-main
// goroutine) blocks in dispatch_semaphore_wait, bounded to 3 s,
// so it only enables the close-veto once the item really exists.
//
// This is the bridge's one synchronous wait, and it is safe
// precisely because of where it sits: the waiting thread is a
// plain Go goroutine that owns nothing the main thread needs, and
// the block that signals it is a leaf (it touches only AppKit).
// No AppKit callback can reach this wait. If the wait times out,
// the block still runs eventually and reports through
// sbguiTrayReadyCallback, so residency turns on late rather than
// never.
bool sbguiTrayStart(const char *statusText, const char *toggleText,
                    const void *iconBytes, int iconLen) {
	if (gStartSem != NULL) {
		return gStartOK;
	}
	gStartSem = dispatch_semaphore_create(0);
	NSString *status = [NSString stringWithUTF8String:statusText];
	NSString *toggle = [NSString stringWithUTF8String:toggleText];
	NSData *icon = nil;
	if (iconBytes != NULL && iconLen > 0) {
		icon = [NSData dataWithBytes:iconBytes length:(NSUInteger)iconLen];
	}
	dispatch_async(dispatch_get_main_queue(), ^{
		@autoreleasepool {
			NSStatusBar *bar = [NSStatusBar systemStatusBar];
			if (bar != nil) {
				gTarget = [[SbguiTrayTarget alloc] init];
				gStatusItem = [bar statusItemWithLength:NSVariableStatusItemLength];
				if (icon != nil) {
					NSImage *img = [[NSImage alloc] initWithData:icon];
					if (img != nil) {
						[img setSize:NSMakeSize(22, 22)];
						[img setTemplate:YES];
						gStatusItem.button.image = img;
					}
				}
				gStatusItem.button.toolTip = @"sbgui";
				NSMenu *menu = [[NSMenu alloc] init];
				[menu setAutoenablesItems:NO];
				gStatusMenuItem = [[NSMenuItem alloc] initWithTitle:status
				                                              action:nil
				                                       keyEquivalent:@""];
				[gStatusMenuItem setEnabled:NO];
				[menu addItem:gStatusMenuItem];
				[menu addItem:[NSMenuItem separatorItem]];
				gToggleMenuItem = [[NSMenuItem alloc] initWithTitle:toggle
				                                              action:@selector(onToggle:)
				                                       keyEquivalent:@""];
				[gToggleMenuItem setTarget:gTarget];
				[menu addItem:gToggleMenuItem];
				NSMenuItem *showItem = [[NSMenuItem alloc] initWithTitle:@"Show Window"
				                                                  action:@selector(onShow:)
				                                           keyEquivalent:@""];
				[showItem setTarget:gTarget];
				[menu addItem:showItem];
				NSMenuItem *quitItem = [[NSMenuItem alloc] initWithTitle:@"Quit"
				                                                  action:@selector(onQuit:)
				                                           keyEquivalent:@""];
				[quitItem setTarget:gTarget];
				[menu addItem:quitItem];
				gStatusItem.menu = menu;
				gStartOK = true;
				sbguiTrayReadyCallback();
			}
		}
		dispatch_semaphore_signal(gStartSem);
	});
	long rc = dispatch_semaphore_wait(gStartSem,
		dispatch_time(DISPATCH_TIME_NOW, (int64_t)(3 * NSEC_PER_SEC)));
	if (rc != 0) {
		return false;
	}
	return gStartOK;
}

void sbguiTraySetStatus(const char *statusText, const char *toggleText) {
	NSString *status = [NSString stringWithUTF8String:statusText];
	NSString *toggle = [NSString stringWithUTF8String:toggleText];
	sbguiDispatchMain(^{
		if (gStatusMenuItem != nil) {
			[gStatusMenuItem setTitle:status];
		}
		if (gToggleMenuItem != nil) {
			[gToggleMenuItem setTitle:toggle];
		}
	});
}

// The Gio window is the app's only real window: prefer the key or
// main window (the one being closed / just shown), then any visible
// window, then the first window.
static NSWindow *sbguiFindWindow(void) {
	NSWindow *win = [NSApp keyWindow];
	if (win == nil) {
		win = [NSApp mainWindow];
	}
	if (win == nil) {
		for (NSWindow *w in [NSApp windows]) {
			if ([w isVisible]) {
				win = w;
				break;
			}
		}
	}
	if (win == nil && [[NSApp windows] count] > 0) {
		win = [[NSApp windows] firstObject];
	}
	return win;
}

// Hides the window. Always dispatched async, never inline: the
// caller is Gio's client goroutine, which runs while the main
// thread is parked inside the close-request rendezvous — the
// window must only be ordered out after windowShouldClose has
// returned and the veto is settled, and the async hop guarantees
// exactly that ordering.
void sbguiTrayHideWindow(void) {
	if (gWindowHidden || gHideQueued) {
		return;
	}
	gHideQueued = true;
	dispatch_async(dispatch_get_main_queue(), ^{
		gHideQueued = false;
		if (gWindowHidden || gShowQueued) {
			return; // already hidden, or a show superseded this hide
		}
		NSWindow *win = sbguiFindWindow();
		if (win != nil) {
			gStashedWindow = win;
			[win orderOut:nil];
		}
		[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
		gWindowHidden = true;
	});
}

// Shows the window again. Runs inline when called from the main
// thread (the menu action), so by the time the Go shown callback
// fires the window really is ordered front and Gio may safely be
// asked for a frame. Deduplicated: clicks while the window is
// visible, or while a show is already in flight, are no-ops.
void sbguiTrayShowWindow(void) {
	if (gShowQueued) {
		return;
	}
	if (!gWindowHidden && !gHideQueued) {
		return; // already visible
	}
	gShowQueued = true;
	sbguiDispatchMain(^{
		gShowQueued = false;
		[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
		NSWindow *win = gStashedWindow;
		if (win == nil) {
			win = sbguiFindWindow();
		}
		if (win != nil) {
			[win makeKeyAndOrderFront:nil];
		}
		[NSApp activateIgnoringOtherApps:YES];
		gWindowHidden = false;
		sbguiTrayShownCallback();
	});
}

// Closes the window through AppKit itself — the same
// [NSWindow performClose:] Gio's driver issues for
// system.ActionClose, reached without Gio's blocking Window.Run
// wrapper. The close flows back into Go through Gio's regular
// close-request event, where the tray-quit flag lets it through
// to the normal destroy/quit path. Works on the ordered-out
// (hidden) window as well. Fire-and-forget and deduplicated.
void sbguiTrayCloseWindow(void) {
	if (gCloseQueued) {
		return;
	}
	gCloseQueued = true;
	sbguiDispatchMain(^{
		gCloseQueued = false;
		NSWindow *win = gStashedWindow;
		if (win == nil) {
			win = sbguiFindWindow();
		}
		if (win != nil) {
			[win performClose:nil];
		}
	});
}
