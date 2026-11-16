package main

import (
	"os"

	"github.com/ncruces/zenity"

	"github.com/Chuang2016/singbox-gui/core"
)

// importRuleSetFile loads a user-picked file as the selected pack,
// through core.ImportRuleSetFile (gui/rules.go semantics).
//
// The picker is ncruces/zenity: pure Go on every platform — on
// macOS it drives osascript's choose-file dialog (a system tool,
// always present), on Linux it shells out to zenity if installed.
// It adds no linked toolkit and nothing to the idle footprint,
// unlike a GTK/Cocoa binding. The dialog blocks, so it runs off
// the UI goroutine.
func (p *rulesPage) importRuleSetFile(a *App) {
	target := p.ruleSetTarget()
	go func() {
		path, err := zenity.SelectFile(
			zenity.Title("Import rule-set pack — "+target),
			zenity.FileFilter{Name: "sing-box rule set", Patterns: []string{"*.srs", "*.*"}},
		)
		if err != nil || path == "" {
			return // cancelled or no picker available: stay silent, like gui/
		}
		data, err := os.ReadFile(path)
		if err != nil {
			a.post(func() { a.fail("Import failed: %v", err) })
			return
		}
		if err := core.ImportRuleSetFile(a.workDir, target, data); err != nil {
			a.post(func() { a.fail("Import failed: %v", err) })
			return
		}
		a.post(func() {
			a.logf("Imported %s from %s", target, path)
			a.notice = ""
			p.refreshRuleSetInfo()
			a.offerRestart("Rule-set imported. Restart sing-box to apply?")
		})
	}()
}
