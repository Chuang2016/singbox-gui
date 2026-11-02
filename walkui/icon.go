//go:build windows

package main

import (
	"bytes"
	_ "embed"
	"image/png"

	"github.com/lxn/walk"
)

//go:embed icon.png
var iconPNG []byte

// loadIcon converts the embedded PNG into a walk icon for the window
// and the tray. The exe itself carries the same artwork as a Win32
// resource (rsrc_windows_amd64.syso, generated from icon.ico +
// app.manifest), so Explorer/taskbar and the tray agree.
func loadIcon() *walk.Icon {
	img, err := png.Decode(bytes.NewReader(iconPNG))
	if err != nil {
		return nil
	}
	ic, err := walk.NewIconFromImage(img)
	if err != nil {
		return nil
	}
	return ic
}
