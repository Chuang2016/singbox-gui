package main

import (
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/Chuang2016/singbox-gui/core"
)

// Rules page. It mirrors the web dashboard "Routing rules"
// card and the Walk front-end's Rules tab: the same core.Settings
// fields (Mode, BlockAds, GFWList, UserDomains/UserIPs + their
// outbounds, CustomRules) and the same save semantics. The four
// toggles (block ads / GFW list / auto-start / gateway) and the
// routing mode save the moment they change, like the dashboard's
// quick toggles; my lists + custom rules are staged and committed
// by "Save & apply", which offers a restart when running. The
// rule-set packs (geosite-cn / geoip-cn / ads / gfwlist) can be
// refreshed or imported manually (URL / file) through the same
// core functions the HTTP handlers call.

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

	modeSel  *widget.Select
	adsCk    *widget.Check
	gfwCk    *widget.Check
	autoCk   *widget.Check
	gwCk     *widget.Check
	intEntry *widget.Entry
	udText   *widget.Entry
	udObSel  *widget.Select
	uiText   *widget.Entry
	uiObSel  *widget.Select

	ruleList *widget.List
	kindSel  *widget.Select
	valEntry *widget.Entry
	outSel   *widget.Select

	rsInfo  *widget.Label
	rsTgt   *widget.Select
	rsURLEd *widget.Entry

	loading bool              // suppresses instant-save while widgets reload
	staged  []core.CustomRule // working copy, committed on Save
}

// buildRulesPage assembles the Rules page: instant-save toggles and
// mode on top, the rule-set pack tools, then the staged my-lists /
// custom-rules editor committed by "Save & apply".
func (g *guiState) buildRulesPage() fyne.CanvasObject {
	cfg := g.store.Data.Cfg
	e := &rulesEditor{g: g, loading: true}
	e.staged = append([]core.CustomRule{}, cfg.CustomRules...)
	g.rulesEd = e

	e.modeSel = widget.NewSelect(labelsOf(modeOptions), func(string) { e.quickSaveMode() })
	e.modeSel.SetSelected(labelFor(modeOptions, cfg.Mode))
	e.adsCk = widget.NewCheck("Block ads/trackers", func(bool) { e.quickToggle() })
	e.adsCk.SetChecked(cfg.BlockAds)
	e.gfwCk = widget.NewCheck("GFW list (extra blocked domains via proxy)", func(bool) { e.quickToggle() })
	e.gfwCk.SetChecked(cfg.GFWList)
	e.autoCk = widget.NewCheck("Auto-start proxy on launch", func(bool) { e.quickToggle() })
	e.autoCk.SetChecked(cfg.AutoStart)
	e.gwCk = widget.NewCheck("Gateway mode (Linux server only)", func(bool) { e.quickToggle() })
	e.gwCk.SetChecked(cfg.GatewayMode)
	e.intEntry = widget.NewEntry()
	e.intEntry.SetText(strconv.Itoa(cfg.AutoUpdateHr))
	e.intEntry.OnSubmitted = func(string) { e.saveInterval() }

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

	// --- rule-set packs: cached-pack info, force update, manual
	// import (URL / file) through the shared core functions ---
	e.rsInfo = widget.NewLabel("")
	e.rsInfo.Wrapping = fyne.TextWrapWord
	e.rsTgt = widget.NewSelect(core.ImportableTargets, nil)
	e.rsTgt.SetSelected(core.ImportableTargets[0])
	e.rsURLEd = widget.NewEntry()
	e.rsURLEd.SetPlaceHolder("If auto-download fails, paste any working download URL for the selected pack...")
	updateDataBtn := widget.NewButton("Update rule data", func() { e.updateRuleData() })
	downloadBtn := widget.NewButton("Download", func() { e.importRuleSetURL() })
	fileBtn := widget.NewButton("Import file...", func() { e.importRuleSetFile() })
	e.refreshRuleSetInfo()

	top := container.NewVBox(
		widget.NewLabelWithStyle("Mode", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		e.modeSel,
		container.NewHBox(e.adsCk, e.gfwCk),
		container.NewHBox(e.autoCk, e.gwCk),
		container.NewBorder(nil, nil,
			widget.NewLabel("Subscription auto-refresh (hours, 0 = off, Enter to save):"), nil, e.intEntry),
		widget.NewLabelWithStyle("Rule-set packs", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		e.rsInfo,
		container.NewBorder(nil, nil, nil,
			container.NewHBox(updateDataBtn, downloadBtn, fileBtn),
			container.NewBorder(nil, nil, widget.NewLabel("Import pack:"), nil, e.rsTgt)),
		e.rsURLEd,
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
	e.loading = false
	return container.NewBorder(top, bottom, nil, nil, e.ruleList)
}

// reload re-reads the settings into the widgets (the page is built
// once; Settings or a restart may have changed values since).
// Instant-save handlers are suppressed while it runs.
func (e *rulesEditor) reload() {
	e.loading = true
	defer func() { e.loading = false }()
	cfg := e.g.store.Data.Cfg
	e.modeSel.SetSelected(labelFor(modeOptions, cfg.Mode))
	e.adsCk.SetChecked(cfg.BlockAds)
	e.gfwCk.SetChecked(cfg.GFWList)
	e.autoCk.SetChecked(cfg.AutoStart)
	e.gwCk.SetChecked(cfg.GatewayMode)
	e.intEntry.SetText(strconv.Itoa(cfg.AutoUpdateHr))
	e.udText.SetText(cfg.UserDomains)
	e.udObSel.SetSelected(labelFor(outboundOptions, cfg.UserDomainOb))
	e.uiText.SetText(cfg.UserIPs)
	e.uiObSel.SetSelected(labelFor(outboundOptions, cfg.UserIPOb))
	e.staged = append([]core.CustomRule{}, cfg.CustomRules...)
	e.ruleList.Refresh()
	e.refreshRuleSetInfo()
}

// quickToggle persists the four toggles the moment they change,
// like the dashboard's quickToggle and the Walk front-end.
func (e *rulesEditor) quickToggle() {
	if e.loading {
		return
	}
	cfg := &e.g.store.Data.Cfg
	cfg.BlockAds = e.adsCk.Checked
	cfg.GFWList = e.gfwCk.Checked
	cfg.AutoStart = e.autoCk.Checked
	cfg.GatewayMode = e.gwCk.Checked
	if err := e.g.store.Save(); err != nil {
		e.g.fail("Save settings failed: %v", err)
		return
	}
	e.g.logf("Settings saved (ads %v, gfw %v, auto-start %v, gateway %v)",
		cfg.BlockAds, cfg.GFWList, cfg.AutoStart, cfg.GatewayMode)
}

// quickSaveMode persists a routing-mode change immediately (the
// Walk front-end keeps the mode in its toolbar with the same
// semantics) and offers a restart when running.
func (e *rulesEditor) quickSaveMode() {
	if e.loading || e.modeSel.Selected == "" {
		return
	}
	cfg := &e.g.store.Data.Cfg
	cfg.Mode = valueFor(modeOptions, e.modeSel.Selected)
	if err := e.g.store.Save(); err != nil {
		e.g.fail("Save mode failed: %v", err)
		return
	}
	e.g.logf("Routing mode saved: %s", cfg.Mode)
	e.g.offerRestart("Mode saved. Restart sing-box to apply?")
}

// saveInterval persists the subscription auto-refresh interval from
// the Rules page entry (the Subscriptions page writes the same
// field).
func (e *rulesEditor) saveInterval() {
	h, err := strconv.Atoi(strings.TrimSpace(e.intEntry.Text))
	if err != nil || h < 0 {
		e.g.fail("Auto-refresh interval: enter hours >= 0 (0 = off)")
		return
	}
	e.g.store.Data.Cfg.AutoUpdateHr = h
	if err := e.g.store.Save(); err != nil {
		e.g.fail("Save settings: %v", err)
		return
	}
	e.g.lastAutoRefresh = time.Now()
	e.g.logf("Subscription auto-refresh interval set to %d h", h)
}

// offerRestart asks (when running) and restarts sing-box so a saved
// change takes effect; the mixed port does not move, so the system
// proxy is unaffected.
func (g *guiState) offerRestart(prompt string) {
	if !g.mgr.Running() {
		return
	}
	dialog.ShowConfirm("sbgui", prompt, func(ok bool) {
		if ok {
			g.restartProxy()
		}
	}, g.win)
}

// ---------- rule-set packs ----------

// refreshRuleSetInfo renders the cached-pack summary line.
func (e *rulesEditor) refreshRuleSetInfo() {
	if e.rsInfo == nil {
		return
	}
	info := core.RuleSetInfo(e.g.workDir)
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
		e.rsInfo.SetText("no cached packs")
		return
	}
	e.rsInfo.SetText(strings.Join(parts, " | "))
}

func (e *rulesEditor) ruleSetTarget() string {
	if e.rsTgt != nil && e.rsTgt.Selected != "" {
		return e.rsTgt.Selected
	}
	return core.ImportableTargets[0]
}

// updateRuleData force re-downloads the packs (backup + restore on
// failure, inside core.RefreshRuleSets), then offers a restart.
func (e *rulesEditor) updateRuleData() {
	g := e.g
	g.logf("Updating rule data...")
	go func() {
		err := core.RefreshRuleSets(g.workDir, func() error {
			return core.Regen(g.store.Data, g.workDir)
		})
		fyne.Do(func() {
			if err != nil {
				g.fail("Rule data update failed: %v", err)
				return
			}
			g.logf("Rule data updated")
			g.notice = ""
			e.refreshRuleSetInfo()
			g.refreshStatus()
			g.offerRestart("Rule data updated. Restart sing-box to apply?")
		})
	}()
}

// importRuleSetURL downloads the pasted URL as the selected pack.
func (e *rulesEditor) importRuleSetURL() {
	u := strings.TrimSpace(e.rsURLEd.Text)
	if u == "" {
		e.g.fail("Import pack: paste a download URL first")
		return
	}
	target := e.ruleSetTarget()
	g := e.g
	g.logf("Downloading %s from %s ...", target, u)
	go func() {
		err := core.ImportRuleSetFromURL(g.workDir, target, u)
		fyne.Do(func() {
			if err != nil {
				g.fail("Import failed: %v", err)
				return
			}
			g.logf("Imported %s", target)
			g.notice = ""
			e.refreshRuleSetInfo()
			g.refreshStatus()
			g.offerRestart("Rule-set imported. Restart sing-box to apply?")
		})
	}()
}

// importRuleSetFile loads a user-picked file as the selected pack.
func (e *rulesEditor) importRuleSetFile() {
	target := e.ruleSetTarget()
	g := e.g
	d := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
		if err != nil || r == nil {
			return
		}
		defer r.Close()
		data, err := io.ReadAll(r)
		if err != nil {
			g.fail("Import failed: %v", err)
			return
		}
		if err := core.ImportRuleSetFile(g.workDir, target, data); err != nil {
			g.fail("Import failed: %v", err)
			return
		}
		g.logf("Imported %s from %s", target, r.URI().Name())
		g.notice = ""
		e.refreshRuleSetInfo()
		g.refreshStatus()
		g.offerRestart("Rule-set imported. Restart sing-box to apply?")
	}, g.win)
	d.Resize(fyne.NewSize(720, 480))
	d.Show()
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
