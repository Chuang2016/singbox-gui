package main

import (
	"fmt"
	"net"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/Chuang2016/singbox-gui/core"
)

// Rules page. It mirrors the web dashboard "Routing rules"
// card exactly: the same core.Settings fields (Mode, BlockAds, GFWList,
// UserDomains/UserIPs + their outbounds, CustomRules), the same value
// sets, and the same save semantics -- write the store, then, if
// sing-box is running, offer a restart so the new config takes effect.
//
// The downloadable rule-set packs (geosite-cn, geoip-cn, ads, gfwlist)
// are NOT part of this model: in the core they are files on disk gated
// by the Mode/BlockAds/GFWList fields above, so pack refresh/import
// stays a separate feature (web: /api/rulesets/*), not part of this
// editor.

type valueLabel struct{ value, label string }

var (
	modeOptions = []valueLabel{
		{"bypass-cn", "Bypass mainland CN (CN direct, rest via proxy)"},
		{"global-proxy", "Global proxy"},
		{"global-direct", "Global direct"},
	}
	outboundOptions = []valueLabel{
		{"proxy", "Proxy"},
		{"direct", "Direct"},
		{"block", "Block"},
	}
	kindOptions = []valueLabel{
		{"domain", "Domain suffix"},
		{"ip", "IP range"},
	}
)

func labelsOf(opts []valueLabel) []string {
	out := make([]string, len(opts))
	for i, o := range opts {
		out[i] = o.label
	}
	return out
}

func labelFor(opts []valueLabel, value string) string {
	for _, o := range opts {
		if o.value == value {
			return o.label
		}
	}
	return opts[0].label
}

func valueFor(opts []valueLabel, label string) string {
	for _, o := range opts {
		if o.label == label {
			return o.value
		}
	}
	return opts[0].value
}

// validateRule checks one custom rule the way sing-box will consume it:
// domain -> domain_suffix, ip -> ip_cidr (a bare IP is fine too).
func validateRule(kind, value string) error {
	if value == "" {
		return fmt.Errorf("rule value is empty")
	}
	if strings.ContainsAny(value, " \t\r\n") {
		return fmt.Errorf("rule value must not contain spaces: %q", value)
	}
	if kind == "ip" {
		if _, _, err := net.ParseCIDR(value); err == nil {
			return nil
		}
		if net.ParseIP(value) != nil {
			return nil
		}
		return fmt.Errorf("%q is not an IP address or CIDR range", value)
	}
	if strings.Contains(value, "://") || strings.Contains(value, "/") {
		return fmt.Errorf("%q does not look like a domain suffix", value)
	}
	return nil
}

// validateIPList checks the "my IPs" bulk list line by line (empty lines
// and comments are skipped, exactly like core.WriteUserList treats them).
func validateIPList(text string) error {
	for _, line := range strings.Split(text, "\n") {
		l := strings.TrimSpace(line)
		if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "//") {
			continue
		}
		if _, _, err := net.ParseCIDR(l); err == nil {
			continue
		}
		if net.ParseIP(l) != nil {
			continue
		}
		return fmt.Errorf("invalid IP/CIDR in my-IPs list: %q", l)
	}
	return nil
}

// restartProxy applies new settings the way the web dashboard restart
// does: stop sing-box, regen (rule-set packs + config), start again.
// A system proxy that is on stays on: the mixed port does not change.
func (g *guiState) restartProxy() {
	_ = g.mgr.Stop()
	if err := g.startProxy(); err != nil {
		g.notice = "Restart failed: " + err.Error()
		g.logf("%s", g.notice)
	} else {
		g.notice = ""
		g.logf("sing-box restarted to apply new rules")
	}
	g.refreshStatus()
}

type rulesEditor struct {
	g *guiState

	modeSel *widget.Select
	adsCk   *widget.Check
	gfwCk   *widget.Check
	udText  *widget.Entry
	udObSel *widget.Select
	uiText  *widget.Entry
	uiObSel *widget.Select

	ruleList *widget.List
	kindSel  *widget.Select
	valEntry *widget.Entry
	outSel   *widget.Select

	staged []core.CustomRule // working copy, committed on Save
}

// buildRulesPage turns the milestone-2 rules editor window into the
// Rules page of the new shell (same widgets, same staged-edit + save
// semantics, no separate window).
func (g *guiState) buildRulesPage() fyne.CanvasObject {
	cfg := g.store.Data.Cfg
	e := &rulesEditor{g: g}
	e.staged = append([]core.CustomRule{}, cfg.CustomRules...)
	g.rulesEd = e

	e.modeSel = widget.NewSelect(labelsOf(modeOptions), nil)
	e.modeSel.SetSelected(labelFor(modeOptions, cfg.Mode))
	e.adsCk = widget.NewCheck("Block ads/trackers", nil)
	e.adsCk.SetChecked(cfg.BlockAds)
	e.gfwCk = widget.NewCheck("GFW list (extra blocked domains via proxy)", nil)
	e.gfwCk.SetChecked(cfg.GFWList)

	e.udText = widget.NewMultiLineEntry()
	e.udText.SetText(cfg.UserDomains)
	e.udText.SetPlaceHolder("example.com")
	e.udObSel = widget.NewSelect(labelsOf(outboundOptions), nil)
	e.udObSel.SetSelected(labelFor(outboundOptions, cfg.UserDomainOb))
	e.uiText = widget.NewMultiLineEntry()
	e.uiText.SetText(cfg.UserIPs)
	e.uiText.SetPlaceHolder("1.2.3.0/24")
	e.uiObSel = widget.NewSelect(labelsOf(outboundOptions), nil)
	e.uiObSel.SetSelected(labelFor(outboundOptions, cfg.UserIPOb))

	e.ruleList = widget.NewList(
		func() int { return len(e.staged) },
		func() fyne.CanvasObject {
			return container.NewHBox(widget.NewLabel(""), widget.NewButton("Delete", nil))
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			r := e.staged[i]
			box := o.(*fyne.Container)
			box.Objects[0].(*widget.Label).SetText(fmt.Sprintf("%s: %s  ->  %s",
				labelFor(kindOptions, r.Kind), r.Value, r.Outbound))
			box.Objects[1].(*widget.Button).OnTapped = func() {
				e.staged = append(e.staged[:i], e.staged[i+1:]...)
				e.ruleList.Refresh()
			}
		},
	)
	e.kindSel = widget.NewSelect(labelsOf(kindOptions), nil)
	e.kindSel.SetSelected(kindOptions[0].label)
	e.valEntry = widget.NewEntry()
	e.valEntry.SetPlaceHolder("example.com or 1.2.3.0/24")
	e.outSel = widget.NewSelect(labelsOf(outboundOptions), nil)
	e.outSel.SetSelected(outboundOptions[0].label)
	addBtn := widget.NewButton("Add rule", func() {
		kind := valueFor(kindOptions, e.kindSel.Selected)
		v := strings.TrimSpace(e.valEntry.Text)
		if err := validateRule(kind, v); err != nil {
			dialog.ShowError(err, e.g.win)
			return
		}
		e.staged = append(e.staged, core.CustomRule{
			Kind: kind, Value: v, Outbound: valueFor(outboundOptions, e.outSel.Selected),
		})
		e.valEntry.SetText("")
		e.ruleList.Refresh()
	})

	saveBtn := widget.NewButton("Save & apply", func() { e.save() })
	saveBtn.Importance = widget.HighImportance

	top := container.NewVBox(
		widget.NewLabelWithStyle("Mode", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		e.modeSel,
		container.NewHBox(e.adsCk, e.gfwCk),
		widget.NewLabelWithStyle("My lists (one per line, highest priority)", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewGridWithColumns(2,
			container.NewVBox(container.NewHBox(widget.NewLabel("Domains via"), e.udObSel), e.udText),
			container.NewVBox(container.NewHBox(widget.NewLabel("IPs via"), e.uiObSel), e.uiText),
		),
		widget.NewLabelWithStyle("Custom rules", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)
	bottom := container.NewVBox(
		container.NewBorder(nil, nil, e.kindSel, addBtn,
			container.NewBorder(nil, nil, nil, e.outSel, e.valEntry)),
		container.NewHBox(saveBtn),
	)
	return container.NewBorder(top, bottom, nil, nil, e.ruleList)
}

// reload re-reads the settings into the widgets (the page is built
// once; Settings or a restart may have changed values since).
func (e *rulesEditor) reload() {
	cfg := e.g.store.Data.Cfg
	e.modeSel.SetSelected(labelFor(modeOptions, cfg.Mode))
	e.adsCk.SetChecked(cfg.BlockAds)
	e.gfwCk.SetChecked(cfg.GFWList)
	e.udText.SetText(cfg.UserDomains)
	e.udObSel.SetSelected(labelFor(outboundOptions, cfg.UserDomainOb))
	e.uiText.SetText(cfg.UserIPs)
	e.uiObSel.SetSelected(labelFor(outboundOptions, cfg.UserIPOb))
	e.staged = append([]core.CustomRule{}, cfg.CustomRules...)
	e.ruleList.Refresh()
}

// save validates and commits the staged edits to the store, then follows
// the web dashboard: if sing-box is running, ask whether to restart it
// so the new rules take effect.
func (e *rulesEditor) save() {
	for _, r := range e.staged {
		if err := validateRule(r.Kind, r.Value); err != nil {
			dialog.ShowError(err, e.g.win)
			return
		}
	}
	if err := validateIPList(e.uiText.Text); err != nil {
		dialog.ShowError(err, e.g.win)
		return
	}
	cfg := &e.g.store.Data.Cfg
	cfg.Mode = valueFor(modeOptions, e.modeSel.Selected)
	cfg.BlockAds = e.adsCk.Checked
	cfg.GFWList = e.gfwCk.Checked
	cfg.UserDomains = e.udText.Text
	cfg.UserDomainOb = valueFor(outboundOptions, e.udObSel.Selected)
	cfg.UserIPs = e.uiText.Text
	cfg.UserIPOb = valueFor(outboundOptions, e.uiObSel.Selected)
	cfg.CustomRules = append([]core.CustomRule{}, e.staged...)
	if err := e.g.store.Save(); err != nil {
		dialog.ShowError(err, e.g.win)
		return
	}
	e.g.logf("Rules saved (mode %s, %d custom rules)", cfg.Mode, len(cfg.CustomRules))
	e.g.notice = "Rules saved"
	e.g.refreshStatus()
	if !e.g.mgr.Running() {
		return
	}
	dialog.ShowConfirm("Rules saved", "Restart sing-box to apply the new rules?", func(ok bool) {
		if ok {
			e.g.restartProxy()
		}
	}, e.g.win)
}
