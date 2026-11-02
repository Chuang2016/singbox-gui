package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// Settings page (filled in by the settings commit of milestone 3).
func (g *guiState) buildSettingsPage() fyne.CanvasObject {
	return container.NewBorder(
		widget.NewLabelWithStyle("Settings", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		nil, nil, nil,
		widget.NewLabel("Settings are being wired up."),
	)
}
