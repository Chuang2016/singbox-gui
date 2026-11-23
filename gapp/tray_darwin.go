//go:build darwin

package main

// macOS menu-bar (status bar) residency for the Gio front-end.
//
// Gio has no system-tray API, so this file is a minimal cgo/AppKit
// bridge (implementation in tray_darwin.m). Every AppKit call is
// dispatched to the main queue; the main thread runs Gio's NSApp
// run loop (app.Main), which services that queue, so the bridge
// never touches AppKit off the main thread. Menu actions come back
// into Go through the exported callbacks below, which run on the
// main thread and are kept non-blocking: they only flip atomics,
// queue Gio window actions, or hand work to the core through the
// same goroutines the UI itself uses.
//
// If the status item cannot be created, setupTray reports false
// and the app keeps its historical behaviour (closing the window
// quits). See the ClosingEvent case in run().

/*
#cgo darwin CFLAGS: -fobjc-arc
#cgo darwin LDFLAGS: -framework Cocoa

#include <stdbool.h>
#include <stdlib.h>

bool sbguiTrayStart(const char *statusText, const char *toggleText, const void *iconBytes, int iconLen);
void sbguiTraySetStatus(const char *statusText, const char *toggleText);
void sbguiTrayHideWindow(void);
void sbguiTrayShowWindow(void);
*/
import "C"

import "unsafe"

// Menu-action handlers, wired to the App by setupTray before the
// status item is created.
var (
	trayOnShow   func()
	trayOnToggle func()
	trayOnQuit   func()
)

//export sbguiTrayShowCallback
func sbguiTrayShowCallback() {
	if trayOnShow != nil {
		trayOnShow()
	}
}

//export sbguiTrayToggleCallback
func sbguiTrayToggleCallback() {
	if trayOnToggle != nil {
		trayOnToggle()
	}
}

//export sbguiTrayQuitCallback
func sbguiTrayQuitCallback() {
	if trayOnQuit != nil {
		trayOnQuit()
	}
}

// setupTray installs the status-bar item and its menu. It blocks
// until the main queue has built the item (bounded wait), so the
// caller knows residency is really available before window closes
// start being vetoed. Call it from a non-main goroutine,
// concurrently with app.Main.
func setupTray(a *App) bool {
	trayOnShow = a.showFromTray
	trayOnToggle = a.toggleFromTray
	trayOnQuit = a.quitFromTray

	cs := C.CString(a.trayStatusText())
	defer C.free(unsafe.Pointer(cs))
	ct := C.CString(a.trayToggleText())
	defer C.free(unsafe.Pointer(ct))
	var iconPtr unsafe.Pointer
	if len(trayIconPNG) > 0 {
		iconPtr = unsafe.Pointer(&trayIconPNG[0])
	}
	return bool(C.sbguiTrayStart(cs, ct, iconPtr, C.int(len(trayIconPNG))))
}

// traySetStatus refreshes the menu's status line and the
// Connect/Disconnect item title.
func traySetStatus(status, toggle string) {
	cs := C.CString(status)
	defer C.free(unsafe.Pointer(cs))
	ct := C.CString(toggle)
	defer C.free(unsafe.Pointer(ct))
	C.sbguiTraySetStatus(cs, ct)
}

// trayHideWindow orders the Gio window out and drops the app to
// the accessory activation policy (no Dock icon) while hidden.
func trayHideWindow() { C.sbguiTrayHideWindow() }

// trayShowWindow restores the regular activation policy and
// orders the Gio window back in, activated.
func trayShowWindow() { C.sbguiTrayShowWindow() }
