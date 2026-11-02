package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// prefTheme is the app-preferences key for the UI theme ("dark"/"light").
// It is a GUI-local preference (Fyne preferences), not a core.Settings
// field: the web dashboard has no equivalent setting.
const prefTheme = "theme"

// sbguiTheme wraps the stock dark/light theme with a restrained blue
// accent and slightly roomier padding, Qv2ray-style: quiet surfaces,
// one accent colour, no gradients.
type sbguiTheme struct{ base fyne.Theme }

func (t sbguiTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNamePrimary:
		return color.NRGBA{R: 0x4c, G: 0x8d, B: 0xff, A: 0xff}
	case theme.ColorNameSelection:
		return color.NRGBA{R: 0x35, G: 0x5a, B: 0x8f, A: 0x99}
	}
	return t.base.Color(name, variant)
}

func (t sbguiTheme) Font(style fyne.TextStyle) fyne.Resource { return t.base.Font(style) }
func (t sbguiTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return t.base.Icon(name)
}

func (t sbguiTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 6
	case theme.SizeNameInnerPadding:
		return 6
	}
	return t.base.Size(name)
}

// applyTheme installs the theme named by the preference (default dark)
// and returns the effective name.
func applyTheme(a fyne.App) string {
	if a.Preferences().StringWithFallback(prefTheme, "dark") == "light" {
		a.Settings().SetTheme(sbguiTheme{base: theme.LightTheme()})
		return "light"
	}
	a.Settings().SetTheme(sbguiTheme{base: theme.DarkTheme()})
	return "dark"
}
