package main

import (
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

// Settings page: build info only (version + data directory), like
// gui/settings.go minus the theme picker — Gio's theme here is a
// fixed dark palette, so there is nothing to switch.
func layoutSettings(gtx layout.Context, a *App) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(10), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(a.th, unit.Sp(15), "Settings")
					l.Font.Weight = font.Bold
					return layout.Inset{Bottom: unit.Dp(10)}.Layout(gtx, l.Layout)
				}),
				layout.Rigid(material.Label(a.th, a.th.TextSize, "sbgui native GUI (Gio) "+gioVersion).Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(6)}.Layout(gtx,
						material.Label(a.th, a.th.TextSize, "Data directory: "+a.workDir).Layout)
				}),
			)
		})
}
