package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/Chuang2016/singbox-gui/core"
)

// Rules page. Mirrors gui/rules.go: the four toggles (block ads /
// GFW list / auto-start / gateway) and the routing mode save the
// moment they change; my lists + custom rules are staged and
// committed by "Save & apply", which offers a restart when running.
// The rule-set packs (geosite-cn / geoip-cn / ads / gfwlist) can be
// refreshed or imported (URL / file) through the same core
// functions the HTTP handlers call; the file picker lives in
// filedialog.go.

type rulesPage struct {
	a *App

	mode   *choice
	adsCk  widget.Bool
	gfwCk  widget.Bool
	autoCk widget.Bool
	gwCk   widget.Bool
	intEd  widget.Editor

	udText widget.Editor
	udOb   *choice
	uiText widget.Editor
	uiOb   *choice

	ruleDel []widget.Clickable
	kind    *choice
	valEd   widget.Editor
	out     *choice
	addBtn  widget.Clickable
	saveBtn widget.Clickable

	rsInfo  string
	rsTgt   *choice
	rsURLEd widget.Editor
	updBtn  widget.Clickable
	dlBtn   widget.Clickable
	fileBtn widget.Clickable

	list   widget.List
	loaded bool
	staged []core.CustomRule
}

func (p *rulesPage) init(a *App) {
	p.a = a
	cfg := a.store.Data.Cfg
	p.list.List.Axis = layout.Vertical
	p.mode = newChoice(modeOptions, cfg.Mode, func(string) { p.quickSaveMode() })
	p.adsCk.Value = cfg.BlockAds
	p.gfwCk.Value = cfg.GFWList
	p.autoCk.Value = cfg.AutoStart
	p.gwCk.Value = cfg.GatewayMode
	p.intEd.SingleLine = true
	p.intEd.Submit = true
	p.intEd.SetText(strconv.Itoa(cfg.AutoUpdateHr))
	p.udText.SetText(cfg.UserDomains)
	p.uiText.SetText(cfg.UserIPs)
	p.udOb = newChoice(outboundOptions, cfg.UserDomainOb, nil)
	p.uiOb = newChoice(outboundOptions, cfg.UserIPOb, nil)
	p.kind = newChoice(kindOptions, "domain", nil)
	p.out = newChoice(outboundOptions, "proxy", nil)
	p.valEd.SingleLine = true
	p.rsTgt = newChoice(targetOptions(), core.ImportableTargets[0], nil)
	p.rsURLEd.SingleLine = true
	p.staged = append([]core.CustomRule{}, cfg.CustomRules...)
	p.loaded = true
	p.refreshRuleSetInfo()
}

// targetOptions wraps core.ImportableTargets as choice options.
func targetOptions() []valueLabel {
	out := make([]valueLabel, len(core.ImportableTargets))
	for i, t := range core.ImportableTargets {
		out[i] = valueLabel{value: t, label: t}
	}
	return out
}

// reload re-reads the settings into the widgets (called when the
// page is shown; other pages may have changed values since).
func (p *rulesPage) reload() {
	if !p.loaded {
		return
	}
	cfg := p.a.store.Data.Cfg
	p.mode.setValue(cfg.Mode)
	p.adsCk.Value = cfg.BlockAds
	p.gfwCk.Value = cfg.GFWList
	p.autoCk.Value = cfg.AutoStart
	p.gwCk.Value = cfg.GatewayMode
	p.intEd.SetText(strconv.Itoa(cfg.AutoUpdateHr))
	p.udText.SetText(cfg.UserDomains)
	p.udOb.setValue(cfg.UserDomainOb)
	p.uiText.SetText(cfg.UserIPs)
	p.uiOb.setValue(cfg.UserIPOb)
	p.staged = append([]core.CustomRule{}, cfg.CustomRules...)
	p.refreshRuleSetInfo()
}

// quickToggle persists the four toggles the moment they change.
func (p *rulesPage) quickToggle() {
	a := p.a
	cfg := &a.store.Data.Cfg
	cfg.BlockAds = p.adsCk.Value
	cfg.GFWList = p.gfwCk.Value
	cfg.AutoStart = p.autoCk.Value
	cfg.GatewayMode = p.gwCk.Value
	if err := a.store.Save(); err != nil {
		a.fail("Save settings failed: %v", err)
		return
	}
	a.logf("Settings saved (ads %v, gfw %v, auto-start %v, gateway %v)",
		cfg.BlockAds, cfg.GFWList, cfg.AutoStart, cfg.GatewayMode)
}

// quickSaveMode persists a routing-mode change immediately and
// offers a restart when running.
func (p *rulesPage) quickSaveMode() {
	a := p.a
	cfg := &a.store.Data.Cfg
	cfg.Mode = p.mode.value()
	if err := a.store.Save(); err != nil {
		a.fail("Save mode failed: %v", err)
		return
	}
	a.logf("Routing mode saved: %s", cfg.Mode)
	a.offerRestart("Mode saved. Restart sing-box to apply?")
}

// saveInterval persists the subscription auto-refresh interval.
func (p *rulesPage) saveInterval() {
	a := p.a
	h, err := strconv.Atoi(strings.TrimSpace(p.intEd.Text()))
	if err != nil || h < 0 {
		a.fail("Auto-refresh interval: enter hours >= 0 (0 = off)")
		return
	}
	a.store.Data.Cfg.AutoUpdateHr = h
	if err := a.store.Save(); err != nil {
		a.fail("Save settings: %v", err)
		return
	}
	a.subs.lastAutoRefresh = time.Now()
	a.logf("Subscription auto-refresh interval set to %d h", h)
}

func (p *rulesPage) layout(gtx layout.Context, a *App) layout.Dimensions {
	// instant-save widgets
	if p.adsCk.Update(gtx) || p.gfwCk.Update(gtx) || p.autoCk.Update(gtx) || p.gwCk.Update(gtx) {
		p.quickToggle()
	}
	for {
		ev, ok := p.intEd.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			p.saveInterval()
		}
	}
	if p.addBtn.Clicked(gtx) {
		p.addRule(a)
	}
	if p.saveBtn.Clicked(gtx) {
		p.save(a)
	}
	if p.updBtn.Clicked(gtx) {
		p.updateRuleData(a)
	}
	if p.dlBtn.Clicked(gtx) {
		p.importRuleSetURL(a)
	}
	if p.fileBtn.Clicked(gtx) {
		p.importRuleSetFile(a)
	}
	for len(p.ruleDel) < len(p.staged) {
		p.ruleDel = append(p.ruleDel, widget.Clickable{})
	}
	for i := range p.staged {
		if i < len(p.ruleDel) && p.ruleDel[i].Clicked(gtx) {
			p.staged = append(p.staged[:i], p.staged[i+1:]...)
			break
		}
	}

	return layout.Inset{Top: unit.Dp(10), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return material.List(a.th, &p.list).Layout(gtx, 1,
				func(gtx layout.Context, _ int) layout.Dimensions {
					return p.layoutColumn(gtx, a)
				})
		})
}

func boldLabel(th *material.Theme, text string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		l := material.Label(th, unit.Sp(14), text)
		l.Font.Weight = font.Bold
		return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
	}
}

func (p *rulesPage) layoutColumn(gtx layout.Context, a *App) layout.Dimensions {
	children := []layout.FlexChild{
		layout.Rigid(boldLabel(a.th, "Mode")),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return p.mode.layout(gtx, a.th)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(material.CheckBox(a.th, &p.adsCk, "Block ads/trackers").Layout),
					layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
					layout.Rigid(material.CheckBox(a.th, &p.gfwCk, "GFW list (extra blocked domains via proxy)").Layout),
				)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(material.CheckBox(a.th, &p.autoCk, "Auto-start proxy on launch").Layout),
					layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
					layout.Rigid(material.CheckBox(a.th, &p.gwCk, "Gateway mode (Linux server only)").Layout),
				)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(material.Label(a.th, a.th.TextSize, "Subscription auto-refresh (hours, 0 = off, Enter to save):").Layout),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.X = gtx.Dp(unit.Dp(70))
						gtx.Constraints.Max.X = gtx.Dp(unit.Dp(70))
						return material.Editor(a.th, &p.intEd, "").Layout(gtx)
					}),
				)
			})
		}),

		layout.Rigid(boldLabel(a.th, "Rule-set packs")),
		layout.Rigid(material.Label(a.th, a.th.TextSize, p.rsInfo).Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Start}.Layout(gtx,
					layout.Rigid(material.Label(a.th, a.th.TextSize, "Import pack:").Layout),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return p.rsTgt.layout(gtx, a.th)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
					layout.Rigid(material.Button(a.th, &p.updBtn, "Update rule data").Layout),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Rigid(material.Button(a.th, &p.dlBtn, "Download").Layout),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Rigid(material.Button(a.th, &p.fileBtn, "Import file...").Layout),
				)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return widget.Border{Color: colDivider, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(gtx,
					material.Editor(a.th, &p.rsURLEd, "If auto-download fails, paste any working download URL for the selected pack...").Layout)
			})
		}),

		layout.Rigid(boldLabel(a.th, "My lists (one per line, highest priority)")),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			col := func(title string, ob *choice, ed *widget.Editor, hint string) layout.FlexChild {
				return layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(material.Label(a.th, a.th.TextSize, title).Layout),
								layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return ob.layout(gtx, a.th)
								}),
							)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								gtx.Constraints.Min.Y = gtx.Dp(unit.Dp(84))
								gtx.Constraints.Max.Y = gtx.Dp(unit.Dp(84))
								return widget.Border{Color: colDivider, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(gtx,
									material.Editor(a.th, ed, hint).Layout)
							})
						}),
					)
				})
			}
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				col("Domains via", p.udOb, &p.udText, "example.com"),
				layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
				col("IPs via", p.uiOb, &p.uiText, "1.2.3.0/24"),
			)
		}),

		layout.Rigid(boldLabel(a.th, "Custom rules")),
	}

	// staged custom rules (plain column; the page itself scrolls)
	for i := range p.staged {
		i := i
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			r := p.staged[i]
			return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, material.Label(a.th, a.th.TextSize, fmt.Sprintf("%s: %s  ->  %s",
							labelFor(kindOptions, r.Kind), r.Value, r.Outbound)).Layout),
						layout.Rigid(material.Button(a.th, &p.ruleDel[i], "Delete").Layout),
					)
				})
		}))
	}

	children = append(children,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Start}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return p.kind.layout(gtx, a.th)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return widget.Border{Color: colDivider, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(gtx,
							material.Editor(a.th, &p.valEd, "example.com or 1.2.3.0/24").Layout)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return p.out.layout(gtx, a.th)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(material.Button(a.th, &p.addBtn, "Add rule").Layout),
				)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(16)}.Layout(gtx,
				material.Button(a.th, &p.saveBtn, "Save & apply").Layout)
		}),
	)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

func (p *rulesPage) addRule(a *App) {
	kind := p.kind.value()
	v := strings.TrimSpace(p.valEd.Text())
	if err := validateRule(kind, v); err != nil {
		a.showError(err)
		return
	}
	p.staged = append(p.staged, core.CustomRule{
		Kind: kind, Value: v, Outbound: p.out.value(),
	})
	p.valEd.SetText("")
}

// save validates and commits the staged edits to the store, then
// offers a restart when running (gui/ semantics).
func (p *rulesPage) save(a *App) {
	for _, r := range p.staged {
		if err := validateRule(r.Kind, r.Value); err != nil {
			a.showError(err)
			return
		}
	}
	if err := validateIPList(p.uiText.Text()); err != nil {
		a.showError(err)
		return
	}
	cfg := &a.store.Data.Cfg
	cfg.Mode = p.mode.value()
	cfg.BlockAds = p.adsCk.Value
	cfg.GFWList = p.gfwCk.Value
	cfg.UserDomains = p.udText.Text()
	cfg.UserDomainOb = p.udOb.value()
	cfg.UserIPs = p.uiText.Text()
	cfg.UserIPOb = p.uiOb.value()
	cfg.CustomRules = append([]core.CustomRule{}, p.staged...)
	if err := a.store.Save(); err != nil {
		a.showError(err)
		return
	}
	a.logf("Rules saved (mode %s, %d custom rules)", cfg.Mode, len(cfg.CustomRules))
	a.notice = "Rules saved"
	if !a.mgr.Running() {
		return
	}
	a.showConfirm("Rules saved", "Restart sing-box to apply the new rules?", func() {
		a.restartProxy()
	})
}

// ---------- rule-set packs ----------

// refreshRuleSetInfo renders the cached-pack summary line.
func (p *rulesPage) refreshRuleSetInfo() {
	info := core.RuleSetInfo(p.a.workDir)
	keys := make([]string, 0, len(info))
	for k := range info {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+": "+info[k])
	}
	if len(parts) == 0 {
		p.rsInfo = "no cached packs"
		return
	}
	p.rsInfo = strings.Join(parts, " | ")
}

func (p *rulesPage) ruleSetTarget() string {
	if p.rsTgt != nil {
		return p.rsTgt.value()
	}
	return core.ImportableTargets[0]
}

// updateRuleData force re-downloads the packs (backup + restore on
// failure, inside core.RefreshRuleSets), then offers a restart.
func (p *rulesPage) updateRuleData(a *App) {
	a.logf("Updating rule data...")
	go func() {
		err := core.RefreshRuleSets(a.workDir, func() error {
			return core.Regen(a.store.Data, a.workDir)
		})
		a.post(func() {
			if err != nil {
				a.fail("Rule data update failed: %v", err)
				return
			}
			a.logf("Rule data updated")
			a.notice = ""
			p.refreshRuleSetInfo()
			a.offerRestart("Rule data updated. Restart sing-box to apply?")
		})
	}()
}

// importRuleSetURL downloads the pasted URL as the selected pack.
func (p *rulesPage) importRuleSetURL(a *App) {
	u := strings.TrimSpace(p.rsURLEd.Text())
	if u == "" {
		a.fail("Import pack: paste a download URL first")
		return
	}
	target := p.ruleSetTarget()
	a.logf("Downloading %s from %s ...", target, u)
	go func() {
		err := core.ImportRuleSetFromURL(a.workDir, target, u)
		a.post(func() {
			if err != nil {
				a.fail("Import failed: %v", err)
				return
			}
			a.logf("Imported %s", target)
			a.notice = ""
			p.refreshRuleSetInfo()
			a.offerRestart("Rule-set imported. Restart sing-box to apply?")
		})
	}()
}
