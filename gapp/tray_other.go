//go:build !darwin

package main

// Non-macOS builds have no menu-bar tray: setupTray reports false
// and the window keeps its historical close-to-quit behaviour.
// (The Gio front-end ships for macOS only; these stubs keep the
// package type-checkable on other platforms.)

func setupTray(a *App) bool               { return false }
func traySetStatus(status, toggle string) {}
func trayHideWindow()                     {}
func trayShowWindow()                     {}
