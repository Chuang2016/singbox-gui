//go:build windows

package main

// Toolbar controls beyond Connect: the system-proxy toggle and the
// routing-mode selector. Both reuse the milestone-2 semantics from
// the (frozen) Fyne front-end, on top of the shared core.

// modeOrder maps combo indices to Settings.Mode values.
var modeOrder = []string{"bypass-cn", "global-proxy", "global-direct"}

func modeIndex(mode string) int {
	for i, m := range modeOrder {
		if m == mode {
			return i
		}
	}
	return 0
}

// onProxyToggle is the checkbox's CheckedChanged handler. It ignores
// events that merely reflect programmatic syncs (state already equal)
// and refuses to enable the OS proxy while nothing is listening.
func (a *app) onProxyToggle() {
	if a.proxyChk == nil {
		return
	}
	want := a.proxyChk.Checked()
	if want == a.sysProxy {
		return
	}
	if want && !a.mgr.Running() {
		a.proxyChk.SetChecked(false)
		a.fail("System proxy: connect first (nothing is listening on the mixed port)")
		return
	}
	a.setSysProxy(want)
}

// onModeChanged applies a new routing mode: persist, and if the
// proxy is running, regen + restart so it takes effect immediately
// (the system proxy is unaffected -- the mixed port does not move).
func (a *app) onModeChanged() {
	if !a.ready || a.modeCmb == nil {
		return
	}
	idx := a.modeCmb.CurrentIndex()
	if idx < 0 || idx >= len(modeOrder) {
		return
	}
	mode := modeOrder[idx]
	if mode == a.store.Data.Cfg.Mode {
		return
	}
	a.store.Data.Cfg.Mode = mode
	_ = a.store.Save()
	a.logf("Mode: %s", mode)
	if a.mgr.Running() {
		_ = a.mgr.Stop()
		if err := a.startProxy(); err != nil {
			a.fail("Mode switch failed (sing-box stopped): %v", err)
			return
		}
		a.logf("Mode applied (restarted)")
	}
	a.notice = ""
	a.refreshStatus()
}
