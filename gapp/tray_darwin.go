//go:build darwin

package main

// macOS menu-bar (status bar) residency for the Gio front-end.
//
// Gio has no system-tray API, so this file is a minimal cgo/AppKit
// bridge (implementation in tray_darwin.m). The bridge's threading
// contract, tightened in preview.14 after the preview.13
// reopen-freeze:
//
//   - Gio delivers window events to the client goroutine through
//     a blocking rendezvous on the main thread: while an event is
//     being delivered, the main thread waits for the client to
//     finish it. The exported callbacks below run ON the main
//     thread (they are AppKit menu actions) and must stay O(1):
//     flip atomics, record a timestamp, or make a fire-and-forget
//     bridge call. They must never call a Gio Window method that
//     can start a rendezvous (Invalidate / Perform / Run) — with
//     one exception: the shown callback invalidates only after
//     the bridge has actually ordered the window front, when a
//     frame can complete normally.
//   - While the window is hidden, nothing invalidates the Gio
//     window at all (see the windowHidden gates in main.go): a
//     frame requested for an ordered-out window may never
//     complete, and a stuck frame wedges both the client
//     goroutine and the main thread permanently.
//   - Quitting goes through the bridge's native performClose, so
//     the close arrives as a regular Gio close request — the
//     same path as the red close button — instead of the
//     blocking Window.Perform wrapper.
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
void sbguiTrayCloseWindow(void);
*/
import "C"

import "unsafe"

// Menu-action handlers, wired to the App by setupTray before the
// status item is created.
var (
	trayOnShow   func()
	trayOnToggle func()
	trayOnQuit   func()
	trayOnReady  func()
	trayOnShown  func()
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

//export sbguiTrayReadyCallback
func sbguiTrayReadyCallback() {
	if trayOnReady != nil {
		trayOnReady()
	}
}

//export sbguiTrayShownCallback
func sbguiTrayShownCallback() {
	if trayOnShown != nil {
		trayOnShown()
	}
}

// setupTray installs the status-bar item and its menu. It blocks
// until the main queue has built the item (bounded wait), so the
// caller knows residency is really available before window closes
// start being vetoed. Call it from a non-main goroutine,
// concurrently with app.Main. If the bounded wait times out, the
// item may still appear later; the bridge then reports through
// the ready callback and residency turns on from there.
func setupTray(a *App) bool {
	trayOnShow = a.showFromTray
	trayOnToggle = a.toggleFromTray
	trayOnQuit = a.quitFromTray
	trayOnReady = a.trayReady
	trayOnShown = a.shownFromTray

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
// orders the Gio window back in, activated. Fire-and-forget; the
// bridge deduplicates repeat calls and reports through the shown
// callback once the window is front.
func trayShowWindow() { C.sbguiTrayShowWindow() }

// trayCloseWindow closes the Gio window through AppKit's
// performClose, so the quit arrives as a regular Gio close
// request. Fire-and-forget.
func trayCloseWindow() { C.sbguiTrayCloseWindow() }
