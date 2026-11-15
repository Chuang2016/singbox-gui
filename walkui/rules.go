//go:build windows

package main

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/lxn/walk"

	"github.com/Chuang2016/singbox-gui/core"
)

// Rules page: the web dashboard's Routing rules card, natively.
// Same store fields and save semantics as the dashboard -- toggles
// save immediately (quickToggle), my lists + custom rules are staged
// in the widgets and committed by "Save rules & apply", rule-set
// packs can be refreshed or imported manually (URL / file) through
// the same core functions the HTTP handlers call.

var obLabels = []string{"Proxy", "Direct", "Block"}
var obValues = []string{"proxy", "direct", "block"}

func obIndex(v string) int {
	for i, x := range obValues {
		if x == v {
			return i
		}
	}
	return 0
}

// ruleModel lists the staged custom rules (read live from the store;
// edits are staged in the store copy only on Save -- see saveRules).
type ruleModel struct {
	walk.ListModelBase
	a *app
}

func (m *ruleModel) ItemCount() int { return len(m.a.stagedRules) }

func (m *ruleModel) Value(index int) interface{} {
	if index < 0 || index >= len(m.a.stagedRules) {
		return ""
	}
	r := m.a.stagedRules[index]
	kind := "Domain suffix"
	if r.Kind == "ip" {
		kind = "IP range"
	}
	return fmt.Sprintf("%s: %s  ->  %s", kind, r.Value, r.Outbound)
}

// initRulesPage wires the Rules page handlers and loads the values.
// Called once after the window is created; reloadRules re-reads the
// store every time the tab is shown.
func (a *app) initRulesPage() {
	if a.adsChk == nil {
		return
	}
	a.adsChk.CheckedChanged().Attach(func() { a.quickToggleRules() })
	a.gfwChk.CheckedChanged().Attach(func() { a.quickToggleRules() })
	a.autoChk.CheckedChanged().Attach(func() { a.quickToggleRules() })
	a.gwChk.CheckedChanged().Attach(func() { a.quickToggleRules() })
	a.autoUpdEd.ValueChanged().Attach(func() {
		if !a.rulesReady {
			return
		}
		a.store.Data.Cfg.AutoUpdateHr = int(a.autoUpdEd.Value())
		_ = a.store.Save()
		a.logf("Subscription auto-refresh interval: %d h", a.store.Data.Cfg.AutoUpdateHr)
		a.refreshLogs()
	})
	a.reloadRules()
	a.rulesReady = true
	a.refreshRuleSetInfo()
}

// reloadRules copies the store's rule settings into the widgets.
func (a *app) reloadRules() {
	if a.adsChk == nil {
		return
	}
	was := a.rulesReady
	a.rulesReady = false
	cfg := a.store.Data.Cfg
	a.adsChk.SetChecked(cfg.BlockAds)
	a.gfwChk.SetChecked(cfg.GFWList)
	a.autoChk.SetChecked(cfg.AutoStart)
	a.gwChk.SetChecked(cfg.GatewayMode)
	a.autoUpdEd.SetValue(float64(cfg.AutoUpdateHr))
	a.udText.SetText(cfg.UserDomains)
	a.uiText.SetText(cfg.UserIPs)
	_ = a.udObCmb.SetCurrentIndex(obIndex(cfg.UserDomainOb))
	_ = a.uiObCmb.SetCurrentIndex(obIndex(cfg.UserIPOb))
	a.stagedRules = append([]core.CustomRule{}, cfg.CustomRules...)
	if a.ruleModel != nil {
		a.ruleModel.PublishItemsReset()
	}
	a.rulesReady = was
}

// quickToggleRules persists the four toggles + the auto-refresh
// interval the moment they change, like the dashboard's quickToggle.
func (a *app) quickToggleRules() {
	if !a.rulesReady {
		return
	}
	cfg := &a.store.Data.Cfg
	cfg.BlockAds = a.adsChk.Checked()
	cfg.GFWList = a.gfwChk.Checked()
	cfg.AutoStart = a.autoChk.Checked()
	cfg.GatewayMode = a.gwChk.Checked()
	if err := a.store.Save(); err != nil {
		a.fail("Save settings failed: %v", err)
		return
	}
	a.logf("Settings saved (ads %v, gfw %v, auto-start %v)", cfg.BlockAds, cfg.GFWList, cfg.AutoStart)
	a.refreshLogs()
}

// validateRule checks one custom rule the way sing-box consumes it.
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

func (a *app) addCustomRule() {
	kind := "domain"
	if a.rKindCmb.CurrentIndex() == 1 {
		kind = "ip"
	}
	v := strings.TrimSpace(a.rValEd.Text())
	if err := validateRule(kind, v); err != nil {
		walk.MsgBox(a.mw, "Custom rules", err.Error(), walk.MsgBoxIconError|walk.MsgBoxOK)
		return
	}
	ob := "proxy"
	if i := a.rOutCmb.CurrentIndex(); i >= 0 && i < len(obValues) {
		ob = obValues[i]
	}
	a.stagedRules = append(a.stagedRules, core.CustomRule{Kind: kind, Value: v, Outbound: ob})
	a.ruleModel.PublishItemsReset()
	a.rValEd.SetText("")
}

func (a *app) deleteCustomRule() {
	i := a.ruleList.CurrentIndex()
	if i < 0 || i >= len(a.stagedRules) {
		a.fail("Custom rules: select a rule first")
		return
	}
	a.stagedRules = append(a.stagedRules[:i], a.stagedRules[i+1:]...)
	a.ruleModel.PublishItemsReset()
}

// saveRules commits my lists + custom rules + the auto-refresh
// interval, then offers a restart when running (dashboard semantics).
func (a *app) saveRules() {
	for _, r := range a.stagedRules {
		if err := validateRule(r.Kind, r.Value); err != nil {
			walk.MsgBox(a.mw, "Custom rules", err.Error(), walk.MsgBoxIconError|walk.MsgBoxOK)
			return
		}
	}
	if err := validateIPList(a.uiText.Text()); err != nil {
		walk.MsgBox(a.mw, "My lists", err.Error(), walk.MsgBoxIconError|walk.MsgBoxOK)
		return
	}
	cfg := &a.store.Data.Cfg
	cfg.UserDomains = a.udText.Text()
	cfg.UserDomainOb = obValues[a.comboIndex(a.udObCmb)]
	cfg.UserIPs = a.uiText.Text()
	cfg.UserIPOb = obValues[a.comboIndex(a.uiObCmb)]
	cfg.CustomRules = append([]core.CustomRule{}, a.stagedRules...)
	cfg.AutoUpdateHr = int(a.autoUpdEd.Value())
	if err := a.store.Save(); err != nil {
		a.fail("Save rules failed: %v", err)
		return
	}
	a.logf("Rules saved (%d custom rules)", len(cfg.CustomRules))
	a.notice = ""
	a.refreshStatus()
	a.refreshLogs()
	a.offerRestart("Rules saved. Restart sing-box to apply?")
}

func (a *app) comboIndex(c *walk.ComboBox) int {
	if i := c.CurrentIndex(); i >= 0 && i < len(obValues) {
		return i
	}
	return 0
}

// offerRestart asks (when running) and restarts sing-box so a saved
// change takes effect; the mixed port does not move, so the system
// proxy is unaffected.
func (a *app) offerRestart(prompt string) {
	if !a.mgr.Running() {
		return
	}
	if walk.MsgBox(a.mw, "sbgui", prompt, walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) != walk.DlgCmdYes {
		return
	}
	_ = a.mgr.Stop()
	if err := a.startProxy(); err != nil {
		a.fail("Restart failed: %v", err)
		return
	}
	a.logf("Restarted to apply changes")
	a.refreshStatus()
	a.refreshLogs()
}

// ---------- rule-set packs ----------

func (a *app) refreshRuleSetInfo() {
	if a.rsInfoLbl == nil {
		return
	}
	parts := []string{}
	for k, v := range core.RuleSetInfo(a.workDir) {
		parts = append(parts, k+": "+v)
	}
	if len(parts) == 0 {
		a.rsInfoLbl.SetText("no cached packs")
		return
	}
	a.rsInfoLbl.SetText(strings.Join(parts, " | "))
}

// refreshRuleSets force re-downloads the packs (backup + restore on
// failure, inside core.RefreshRuleSets), then offers a restart.
func (a *app) refreshRuleSets() {
	a.logf("Updating rule data...")
	a.refreshLogs()
	workDir := a.workDir
	go func() {
		err := core.RefreshRuleSets(workDir, func() error {
			return core.Regen(a.store.Data, workDir)
		})
		a.mw.Synchronize(func() {
			if err != nil {
				a.fail("Rule data update failed: %v", err)
				return
			}
			a.logf("Rule data updated")
			a.notice = ""
			a.refreshRuleSetInfo()
			a.refreshStatus()
			a.refreshLogs()
			a.offerRestart("Rule data updated. Restart sing-box to apply?")
		})
	}()
}

func (a *app) ruleSetTarget() string {
	targets := core.ImportableTargets
	if i := a.rsTgtCmb.CurrentIndex(); i >= 0 && i < len(targets) {
		return targets[i]
	}
	return targets[0]
}

// importRuleSetURL downloads the pasted URL as the selected pack.
func (a *app) importRuleSetURL() {
	u := strings.TrimSpace(a.rsUrlEd.Text())
	if u == "" {
		a.fail("Import pack: paste a download URL first")
		return
	}
	target := a.ruleSetTarget()
	a.logf("Downloading %s from %s ...", target, u)
	a.refreshLogs()
	go func() {
		err := core.ImportRuleSetFromURL(a.workDir, target, u)
		a.mw.Synchronize(func() {
			if err != nil {
				a.fail("Import failed: %v", err)
				return
			}
			a.logf("Imported %s", target)
			a.notice = ""
			a.refreshRuleSetInfo()
			a.refreshStatus()
			a.refreshLogs()
			a.offerRestart("Rule-set imported. Restart sing-box to apply?")
		})
	}()
}

// importRuleSetFile loads a user-picked file as the selected pack.
func (a *app) importRuleSetFile() {
	dlg := walk.FileDialog{
		Title:  "Import rule-set pack",
		Filter: "Rule-set files (*.srs;*.txt)|*.srs;*.txt|All files (*.*)|*.*",
	}
	accepted, err := dlg.ShowOpen(a.mw)
	if err != nil || !accepted {
		return
	}
	data, err := os.ReadFile(dlg.FilePath)
	if err != nil {
		a.fail("Import failed: %v", err)
		return
	}
	target := a.ruleSetTarget()
	if err := core.ImportRuleSetFile(a.workDir, target, data); err != nil {
		a.fail("Import failed: %v", err)
		return
	}
	a.logf("Imported %s from %s", target, dlg.FilePath)
	a.notice = ""
	a.refreshRuleSetInfo()
	a.refreshStatus()
	a.refreshLogs()
	a.offerRestart("Rule-set imported. Restart sing-box to apply?")
}
