package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Settings page: GUI-local preferences (theme) and build info.
// Engine settings live where the Walk front-end keeps them: routing
// mode and the quick toggles on the Rules page, ports and the
// subscription interval in their own pages -- this page deliberately
// does not duplicate them.
func (g *guiState) buildSettingsPage() fyne.CanvasObject {
	themeSel := widget.NewSelect([]string{"Dark", "Light"}, func(s string) {
		name := "dark"
		if s == "Light" {
			name = "light"
		}
		g.app.Preferences().SetString(prefTheme, name)
		if name == "light" {
			g.app.Settings().SetTheme(sbguiTheme{base: theme.LightTheme()})
		} else {
			g.app.Settings().SetTheme(sbguiTheme{base: theme.DarkTheme()})
		}
		g.logf("Theme: %s", name)
	})
	if applyTheme(g.app) == "light" {
		themeSel.SetSelected("Light")
	} else {
		themeSel.SetSelected("Dark")
	}

	return container.NewBorder(
		widget.NewLabelWithStyle("Settings", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		nil, nil, nil,
		container.NewVBox(
			container.NewHBox(widget.NewLabel("Theme:"), themeSel),
			widget.NewLabel("sbgui native GUI "+guiVersion),
			widget.NewLabel("Data directory: "+g.workDir),
		),
	)
}
