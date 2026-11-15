//go:build windows

package main

import (
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

// buildWindow assembles the main window: the global toolbar (connect,
// system proxy, routing mode) on top, then three tabs that carry
// everything the web dashboard has -- Nodes (latency / speed / traffic,
// edit, sort), Rules (routing rules, my lists, rule-set packs) and
// Connections (live table). The web back-end stays available as a
// fallback via "Open web back-end".
func (a *app) buildWindow() error {
	def := MainWindow{
		AssignTo: &a.mw,
		Title:    "sbgui",
		MinSize:  Size{Width: 840, Height: 560},
		Size:     Size{Width: 1080, Height: 720},
		Font:     Font{Family: "Segoe UI", PointSize: 9},
		Layout:   VBox{Margins: Margins{Left: 8, Top: 8, Right: 8, Bottom: 8}, Spacing: 6},
		StatusBarItems: []StatusBarItem{
			{AssignTo: &a.sbStatus, Text: "Stopped", Width: 120},
			{AssignTo: &a.sbNode, Text: "Node: none", Width: 330},
			{AssignTo: &a.sbPorts, Text: "", Width: 210},
			{AssignTo: &a.sbRates, Text: "", Width: 230},
		},
		Children: []Widget{
			Composite{
				Layout: HBox{MarginsZero: true, Spacing: 6},
				Children: []Widget{
					PushButton{
						AssignTo:  &a.connBtn,
						Text:      "Connect",
						MinSize:   Size{Width: 110},
						OnClicked: func() { a.toggle() },
					},
					CheckBox{
						AssignTo: &a.proxyChk,
						Text:     "System proxy",
						Enabled:  false,
					},
					Label{Text: "Mode:"},
					ComboBox{
						AssignTo: &a.modeCmb,
						Model:    []string{"Bypass CN", "Global proxy", "Global direct"},
						MinSize:  Size{Width: 130},
					},
					HSpacer{},
					PushButton{
						Text:      "Open web back-end",
						OnClicked: func() { a.openInBrowser() },
					},
				},
			},
			TabWidget{
				AssignTo:      &a.tabs,
				StretchFactor: 1,
				Pages: []TabPage{
					{
						Title:  "Nodes",
						Layout: VBox{Margins: Margins{Left: 4, Top: 6, Right: 4, Bottom: 4}, Spacing: 6},
						Children: []Widget{
							Composite{
								Layout: HBox{MarginsZero: true, Spacing: 6},
								Children: []Widget{
									PushButton{Text: "Test latency", OnClicked: func() { a.testLatencySelected() }},
									PushButton{Text: "Test all", OnClicked: func() { a.testLatencyAll() }},
									PushButton{Text: "Speed test", OnClicked: func() { a.testSpeedSelected() }},
									PushButton{Text: "Edit node...", OnClicked: func() { a.editSelectedNode() }},
									PushButton{Text: "Delete node", OnClicked: func() { a.deleteSelectedNode() }},
									PushButton{Text: "Reset traffic", OnClicked: func() { a.resetTraffic() }},
									HSpacer{},
									Label{AssignTo: &a.rateLbl, Text: ""},
								},
							},
							HSplitter{
								StretchFactor: 1,
								Children: []Widget{
									Composite{
										AssignTo: &a.railBox,
										Layout:   VBox{MarginsZero: true, Spacing: 6},
										MinSize:  Size{Width: 220},
										MaxSize:  Size{Width: 320},
										Children: []Widget{
											Label{
												Text: "Subscriptions",
												Font: Font{Family: "Segoe UI", PointSize: 9, Bold: true},
											},
										},
									},
									TableView{
										AssignTo:      &a.table,
										Model:         a.model,
										StretchFactor: 1,
										Columns: []TableViewColumn{
											{Title: "", Width: 26},
											{Title: "Name", Width: 280},
											{Title: "Protocol", Width: 84},
											{Title: "Latency", Width: 84},
											{Title: "Speed", Width: 92},
											{Title: "Traffic", Width: 110},
										},
									},
								},
							},
							Composite{
								Layout: HBox{MarginsZero: true, Spacing: 6},
								Children: []Widget{
									LineEdit{
										AssignTo:  &a.importEd,
										CueBanner: "Paste one or more share links (vless / trojan / ss / vmess / hysteria2 / tuic / anytls / privproto)...",
									},
									PushButton{
										Text:      "Import",
										MinSize:   Size{Width: 90},
										OnClicked: func() { a.importLinks() },
									},
								},
							},
							TextEdit{
								AssignTo: &a.logEd,
								ReadOnly: true,
								VScroll:  true,
								MinSize:  Size{Height: 92},
								MaxSize:  Size{Height: 150},
							},
						},
					},
					{
						Title:  "Rules",
						Layout: VBox{Margins: Margins{Left: 4, Top: 6, Right: 4, Bottom: 4}, Spacing: 6},
						Children: []Widget{
							Composite{
								Layout: HBox{MarginsZero: true, Spacing: 10},
								Children: []Widget{
									CheckBox{AssignTo: &a.adsChk, Text: "Block ads/trackers"},
									CheckBox{AssignTo: &a.gfwChk, Text: "GFW list"},
									CheckBox{AssignTo: &a.autoChk, Text: "Auto-start proxy on launch"},
									CheckBox{AssignTo: &a.gwChk, Text: "Gateway mode (Linux server only)"},
									HSpacer{},
								},
							},
							Composite{
								Layout: HBox{MarginsZero: true, Spacing: 6},
								Children: []Widget{
									Label{Text: "Subscription auto-refresh (hours, 0 = off):"},
									NumberEdit{AssignTo: &a.autoUpdEd, MinValue: 0, MaxValue: 168, Decimals: 0, MinSize: Size{Width: 60}},
									HSpacer{},
									PushButton{Text: "Update rule data", OnClicked: func() { a.refreshRuleSets() }},
									Label{AssignTo: &a.rsInfoLbl, Text: ""},
								},
							},
							Composite{
								Layout: HBox{MarginsZero: true, Spacing: 6},
								Children: []Widget{
									Label{Text: "Import pack:"},
									ComboBox{
										AssignTo: &a.rsTgtCmb,
										Model:    []string{"geosite-cn", "geoip-cn", "geosite-category-ads-all", "gfwlist"},
										MinSize:  Size{Width: 170},
									},
									LineEdit{
										AssignTo:  &a.rsUrlEd,
										CueBanner: "If auto-download fails, paste any working download URL for the selected pack...",
									},
									PushButton{Text: "Download", OnClicked: func() { a.importRuleSetURL() }},
									PushButton{Text: "Import file...", OnClicked: func() { a.importRuleSetFile() }},
								},
							},
							Label{Text: "My lists (one per line, highest priority):", Font: Font{Family: "Segoe UI", PointSize: 9, Bold: true}},
							Composite{
								Layout: HBox{MarginsZero: true, Spacing: 10},
								Children: []Widget{
									Composite{
										Layout: VBox{MarginsZero: true, Spacing: 4},
										Children: []Widget{
											Composite{
												Layout: HBox{MarginsZero: true, Spacing: 6},
												Children: []Widget{
													Label{Text: "Domains via"},
													ComboBox{AssignTo: &a.udObCmb, Model: []string{"Proxy", "Direct", "Block"}, MinSize: Size{Width: 90}},
													HSpacer{},
												},
											},
											TextEdit{AssignTo: &a.udText, VScroll: true, MinSize: Size{Height: 64}},
										},
									},
									Composite{
										Layout: VBox{MarginsZero: true, Spacing: 4},
										Children: []Widget{
											Composite{
												Layout: HBox{MarginsZero: true, Spacing: 6},
												Children: []Widget{
													Label{Text: "IPs / CIDR via"},
													ComboBox{AssignTo: &a.uiObCmb, Model: []string{"Proxy", "Direct", "Block"}, MinSize: Size{Width: 90}},
													HSpacer{},
												},
											},
											TextEdit{AssignTo: &a.uiText, VScroll: true, MinSize: Size{Height: 64}},
										},
									},
								},
							},
							Label{Text: "Custom rules (higher priority than presets):", Font: Font{Family: "Segoe UI", PointSize: 9, Bold: true}},
							ListBox{
								AssignTo: &a.ruleList,
								Model:    a.ruleModel,
								MinSize:  Size{Height: 70},
								MaxSize:  Size{Height: 110},
							},
							Composite{
								Layout: HBox{MarginsZero: true, Spacing: 6},
								Children: []Widget{
									ComboBox{AssignTo: &a.rKindCmb, Model: []string{"Domain suffix", "IP range"}, MinSize: Size{Width: 110}},
									LineEdit{AssignTo: &a.rValEd, CueBanner: "example.com or 1.2.3.0/24"},
									ComboBox{AssignTo: &a.rOutCmb, Model: []string{"Proxy", "Direct", "Block"}, MinSize: Size{Width: 90}},
									PushButton{Text: "Add rule", OnClicked: func() { a.addCustomRule() }},
									PushButton{Text: "Delete selected", OnClicked: func() { a.deleteCustomRule() }},
								},
							},
							Composite{
								Layout: HBox{MarginsZero: true, Spacing: 6},
								Children: []Widget{
									PushButton{Text: "Save rules & apply", MinSize: Size{Width: 150}, OnClicked: func() { a.saveRules() }},
									Label{Text: "Routing mode lives in the top toolbar. Saving offers a restart when running."},
									HSpacer{},
								},
							},
						},
					},
					{
						Title:  "Connections",
						Layout: VBox{Margins: Margins{Left: 4, Top: 6, Right: 4, Bottom: 4}, Spacing: 6},
						Children: []Widget{
							Composite{
								Layout: HBox{MarginsZero: true, Spacing: 6},
								Children: []Widget{
									PushButton{Text: "Refresh", OnClicked: func() { a.fetchConnsAsync(true) }},
									PushButton{Text: "Kill selected", OnClicked: func() { a.killSelectedConn() }},
									Label{AssignTo: &a.connInfoLbl, Text: "Live connections via the Clash API."},
									HSpacer{},
								},
							},
							TableView{
								AssignTo:      &a.connTable,
								Model:         a.connModel,
								StretchFactor: 1,
								Columns: []TableViewColumn{
									{Title: "Destination", Width: 250},
									{Title: "Process", Width: 110},
									{Title: "Node", Width: 140},
									{Title: "Up", Width: 80},
									{Title: "Down", Width: 80},
									{Title: "Rule", Width: 240},
								},
							},
						},
					},
				},
			},
		},
	}
	if err := def.Create(); err != nil {
		return err
	}
	if a.icon != nil {
		_ = a.mw.SetIcon(a.icon)
	}
	_ = a.mw.SetTitle("sbgui " + version)
	a.table.CurrentIndexChanged().Attach(func() { a.onRowSelected() })
	a.table.ColumnClicked().Attach(func(col int) { a.onColumnClicked(col) })
	a.proxyChk.CheckedChanged().Attach(func() { a.onProxyToggle() })
	a.modeCmb.CurrentIndexChanged().Attach(func() { a.onModeChanged() })
	_ = a.modeCmb.SetCurrentIndex(modeIndex(a.store.Data.Cfg.Mode))
	a.tabs.CurrentIndexChanged().Attach(func() { a.onTabChanged() })
	a.ready = true
	// Closing the window parks the app in the tray; only the tray's
	// Quit (or an explicit quit) actually exits.
	a.mw.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		if !a.quitting {
			*canceled = true
			a.mw.Hide()
		}
	})
	a.initSubRail()
	a.initRulesPage()
	a.lastNodeCount = len(a.store.Data.Nodes)
	a.syncTableSelection()
	a.refreshStatus()
	return nil
}

// onTabChanged loads a page's live data when it becomes visible: the
// rules editor always reflects the store (the web back-end may have
// changed it), and the connections table starts fetching.
func (a *app) onTabChanged() {
	if a.tabs == nil {
		return
	}
	switch a.tabs.CurrentIndex() {
	case 1:
		a.reloadRules()
	case 2:
		a.fetchConnsAsync(true)
	}
}
