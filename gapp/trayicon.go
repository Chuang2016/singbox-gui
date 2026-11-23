package main

import _ "embed"

// trayIconPNG is the menu-bar status icon: the app icon's
// signal-bars-and-dot mark as a monochrome template image (solid
// black on transparent), rendered at 44x44 and shown at 22 pt.
// macOS tints template images itself for light/dark menu bars;
// only the alpha channel matters.
//
//go:embed trayicon.png
var trayIconPNG []byte
