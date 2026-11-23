// Menu-bar residency bridge for the Gio front-end (macOS only).
// Go side: tray_darwin.go. Every entry point that touches AppKit
// dispatches to the main queue; the main thread runs Gio's NSApp
// run loop, which services that queue.

#import <Cocoa/Cocoa.h>
#import <dispatch/dispatch.h>

#include <stdbool.h>
#include <stdlib.h>

extern void sbguiTrayShowCallback(void);
extern void sbguiTrayToggleCallback(void);
extern void sbguiTrayQuitCallback(void);

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

// Builds the status item and menu on the main queue and reports
// back through a semaphore: the Go caller (a non-main goroutine)
// blocks in dispatch_semaphore_wait, bounded to 3 s, so it only
// enables the close-veto once the item really exists.
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
	dispatch_async(dispatch_get_main_queue(), ^{
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

void sbguiTrayHideWindow(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		NSWindow *win = sbguiFindWindow();
		if (win != nil) {
			gStashedWindow = win;
			[win orderOut:nil];
		}
		[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
	});
}

void sbguiTrayShowWindow(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
		NSWindow *win = gStashedWindow;
		if (win == nil) {
			win = sbguiFindWindow();
		}
		if (win != nil) {
			[win makeKeyAndOrderFront:nil];
		}
		[NSApp activateIgnoringOtherApps:YES];
	});
}
