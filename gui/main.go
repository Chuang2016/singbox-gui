// sbgui-gui: native Fyne front-end for the sbgui core engine (milestone 1 PoC).
//
// This is a proof of concept: it reuses core/ as-is (store, link parsing,
// config building, sing-box process management) and replaces the browser
// dashboard with a real window + system tray. Feature parity with the web
// dashboard (subscriptions, traffic charts, connections table,
// urltest) is future work -- see MILESTONE1.md.
package main

import (
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"github.com/Chuang2016/singbox-gui/core"
)

// demoLinks seed the list (in memory only, never saved) when the store is
// empty, so the PoC shows the real core.ParseShareLink path on first run.
var demoLinks = []string{
	"vless://00000000-0000-0000-0000-000000000001@example.com:443?security=tls&sni=example.com#Demo-VLESS",
	"trojan://demopass@example.com:443?sni=example.com#Demo-Trojan",
	"hysteria2://demopass@example.com:443?insecure=1#Demo-HY2",
}

type guiState struct {
	workDir string
	store   *core.Store
	mgr     *core.Manager

	status *widget.Label
	logs   *widget.Label
	list   *widget.List
	connBt *widget.Button

	notice string   // sticky message kept in the status line until the next action
	uiLogs []string // GUI-side log lines, shown together with the sing-box log

	proxySw    *widget.Check // "System proxy" toggle
	sysProxy   bool          // we currently own the OS proxy settings
	wasRunning bool          // last observed sing-box state (auto-clear check)
}

func (g *guiState) activeNode() *core.Node {
	for i := range g.store.Data.Nodes {
		if g.store.Data.Nodes[i].ID == g.store.Data.Cfg.ActiveNodeID {
			return &g.store.Data.Nodes[i]
		}
	}
	if len(g.store.Data.Nodes) > 0 {
		return &g.store.Data.Nodes[0]
	}
	return nil
}

// logf appends a timestamped GUI-side line to the log pane (the pane also
// shows the sing-box subprocess log via Manager.Logs).
func (g *guiState) logf(format string, args ...any) {
	line := time.Now().Format("15:04:05") + " " + fmt.Sprintf(format, args...)
	g.uiLogs = append(g.uiLogs, line)
	if len(g.uiLogs) > 100 {
		g.uiLogs = g.uiLogs[len(g.uiLogs)-100:]
	}
}

// setSysProxy switches the OS proxy through the shared core
// implementation (the same one the web dashboard uses) and keeps the
// toggle, the internal state and the log in line. On failure the toggle
// reverts to the real state.
func (g *guiState) setSysProxy(on bool) {
	port := g.store.Data.Cfg.MixedPort
	if err := core.SetSystemProxy(on, port); err != nil {
		g.logf("System proxy: %v", err)
		g.proxySw.SetChecked(g.sysProxy)
		g.refreshStatus()
		return
	}
	g.sysProxy = on
	if on {
		g.logf("System proxy on (127.0.0.1:%d)", port)
	} else {
		g.logf("System proxy off")
	}
	g.refreshStatus()
}

// clearSysProxy turns the OS proxy back off if we turned it on. Used on
// Disconnect and before Quit, so the OS never keeps pointing at a port
// where nothing listens anymore.
func (g *guiState) clearSysProxy() {
	if !g.sysProxy {
		return
	}
	if err := core.SetSystemProxy(false, g.store.Data.Cfg.MixedPort); err != nil {
		g.logf("System proxy clear failed: %v", err)
		return
	}
	g.sysProxy = false
	g.proxySw.SetChecked(false)
	g.logf("System proxy off")
}

func (g *guiState) refreshStatus() {
	running := g.mgr.Running()
	// sing-box stopped on its own while we own the OS proxy: clear it,
	// so the desktop does not keep pointing at a dead port.
	if g.wasRunning && !running && g.sysProxy {
		g.clearSysProxy()
	}
	g.wasRunning = running
	state := "Stopped"
	if running {
		state = "Running"
		g.connBt.SetText("Disconnect")
	} else {
		g.connBt.SetText("Connect")
	}
	if g.proxySw != nil {
		// the toggle is only meaningful while connected (or while on,
		// so it can always be switched back off by hand)
		if running || g.sysProxy {
			g.proxySw.Enable()
		} else {
			g.proxySw.Disable()
		}
	}
	node := "none"
	if n := g.activeNode(); n != nil {
		node = n.Tag
	}
	txt := fmt.Sprintf("Status: %s  |  Active node: %s  |  Mixed port: %d  |  Mode: %s",
		state, node, g.store.Data.Cfg.MixedPort, g.store.Data.Cfg.Mode)
	if g.notice != "" {
		txt += "  |  " + g.notice
	}
	g.status.SetText(txt)
	lines := append(g.mgr.Logs(), g.uiLogs...)
	if len(lines) > 14 {
		lines = lines[len(lines)-14:]
	}
	g.logs.SetText(strings.Join(lines, "\n"))
}

// startProxy mirrors server.Server.StartProxy: regen the config, then
// start the subprocess.
func (g *guiState) startProxy() error {
	if err := g.regen(); err != nil {
		return err
	}
	return g.mgr.Start()
}

// regen mirrors server.Server.regen: make sure every rule-set pack the
// config references is downloaded, then rebuild config.json. The web
// build ensures the CN packs (bypass-cn mode), the ads pack (BlockAds)
// and the GFW list (GFWList); the GUI originally only fetched the CN
// packs, so on a fresh install the first Connect referenced missing
// .srs files and sing-box refused to start.
func (g *guiState) regen() error {
	if g.store.Data.Cfg.Mode == "bypass-cn" {
		if err := core.EnsureRuleSets(g.workDir); err != nil {
			return err
		}
	}
	if g.store.Data.Cfg.BlockAds {
		if err := core.EnsureExtraRuleSets(g.workDir, []string{"geosite-category-ads-all"}); err != nil {
			return err
		}
	}
	if g.store.Data.Cfg.GFWList {
		if err := core.EnsureGFWList(g.workDir); err != nil {
			return err
		}
	}
	_, err := core.BuildConfig(g.store.Data, g.workDir)
	return err
}

func (g *guiState) toggle() {
	var err error
	starting := !g.mgr.Running()
	if !starting {
		err = g.mgr.Stop()
	} else {
		if n := g.activeNode(); n != nil {
			g.store.Data.Cfg.ActiveNodeID = n.ID
		}
		err = g.startProxy()
	}
	if err != nil {
		what := "Start"
		if !starting {
			what = "Stop"
		}
		// Keep the failure visible: the 1s status refresh must not wipe
		// it, so park it in the sticky notice and in the log pane.
		g.notice = what + " failed: " + err.Error()
		g.logf("%s", g.notice)
		g.refreshStatus()
		return
	}
	if !starting {
		g.clearSysProxy()
	}
	g.notice = ""
	g.refreshStatus()
}

func main() {
	a := app.NewWithID("org.sbgui.native")
	w := a.NewWindow("sbgui — sing-box manager (native PoC)")
	w.Resize(fyne.NewSize(760, 520))

	workDir := core.WorkDir()
	store, err := core.OpenStore(workDir)
	if err != nil {
		w.SetContent(widget.NewLabel("Failed to open store: " + err.Error()))
		w.ShowAndRun()
		return
	}
	seeded := false
	if len(store.Data.Nodes) == 0 {
		for _, link := range demoLinks {
			proto, ob, err := core.ParseShareLink(link)
			if err != nil {
				continue
			}
			store.Data.Nodes = append(store.Data.Nodes, core.Node{
				ID: core.NewID("n"), Tag: core.LinkTag(link, proto),
				Protocol: proto, Outbound: ob, Source: "manual",
			})
			seeded = true
		}
		if seeded && store.Data.Cfg.ActiveNodeID == "" && len(store.Data.Nodes) > 0 {
			store.Data.Cfg.ActiveNodeID = store.Data.Nodes[0].ID
		}
	}

	g := &guiState{workDir: workDir, store: store, mgr: core.NewManager(workDir)}
	g.status = widget.NewLabel("")
	g.logs = widget.NewLabel("")
	g.logs.Wrapping = fyne.TextWrapBreak
	g.connBt = widget.NewButton("Connect", g.toggle)
	g.proxySw = widget.NewCheck("System proxy", func(on bool) {
		if on && !g.mgr.Running() {
			g.logf("System proxy: connect first")
			g.proxySw.SetChecked(false)
			g.refreshStatus()
			return
		}
		g.setSysProxy(on)
	})
	g.proxySw.Disable()
	rulesBt := widget.NewButton("Rules...", func() { g.openRulesWindow() })

	g.list = widget.NewList(
		func() int { return len(g.store.Data.Nodes) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			n := g.store.Data.Nodes[i]
			mark := ""
			if n.ID == g.store.Data.Cfg.ActiveNodeID {
				mark = "  [active]"
			}
			o.(*widget.Label).SetText(fmt.Sprintf("%s  ·  %s%s", n.Tag, n.Protocol, mark))
		},
	)
	g.list.OnSelected = func(id widget.ListItemID) {
		g.store.Data.Cfg.ActiveNodeID = g.store.Data.Nodes[id].ID
		_ = g.store.Save()
		g.refreshStatus()
		g.list.Refresh()
	}

	importEntry := widget.NewEntry()
	importEntry.SetPlaceHolder("Paste a share link (vless://, trojan://, hysteria2://, ...) and press Import")
	importBtn := widget.NewButton("Import link", func() {
		link := strings.TrimSpace(importEntry.Text)
		if link == "" {
			return
		}
		proto, ob, err := core.ParseShareLink(link)
		if err != nil {
			g.notice = "Import failed: " + err.Error()
			g.logf("%s", g.notice)
			g.refreshStatus()
			return
		}
		g.store.Data.Nodes = append(g.store.Data.Nodes, core.Node{
			ID: core.NewID("n"), Tag: core.LinkTag(link, proto),
			Protocol: proto, Outbound: ob, Source: "manual",
		})
		_ = g.store.Save()
		g.notice = ""
		importEntry.SetText("")
		g.list.Refresh()
		g.refreshStatus()
	})

	header := container.NewVBox(
		widget.NewLabelWithStyle("sbgui native GUI — milestone 1 PoC", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		g.status,
	)
	bottom := container.NewVBox(
		container.NewBorder(nil, nil, nil, importBtn, importEntry),
		container.NewHBox(g.connBt, g.proxySw, rulesBt),
		widget.NewLabelWithStyle("sing-box log", fyne.TextAlignLeading, fyne.TextStyle{Italic: true}),
		container.NewVScroll(g.logs),
	)
	w.SetContent(container.NewBorder(header, bottom, nil, nil, g.list))
	g.refreshStatus()
	if seeded {
		g.status.SetText(g.status.Text + "  |  (demo nodes seeded — store was empty)")
	}

	// System tray: Show / Quit. Closing the window hides it to the tray.
	if desk, ok := a.(desktop.App); ok {
		menu := fyne.NewMenu("sbgui",
			fyne.NewMenuItem("Show", func() { w.Show() }),
			fyne.NewMenuItem("Quit", func() {
				// like the web build's /api/quit: stop sing-box first,
				// and never leave the OS proxy pointing at a dead port
				if g.sysProxy {
					_ = core.SetSystemProxy(false, g.store.Data.Cfg.MixedPort)
					g.sysProxy = false
				}
				if g.mgr.Running() {
					_ = g.mgr.Stop()
				}
				a.Quit()
			}),
		)
		desk.SetSystemTrayMenu(menu)
		w.SetCloseIntercept(func() { w.Hide() })
	}

	// Poll process state + logs once a second.
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for range t.C {
			fyne.Do(g.refreshStatus)
		}
	}()

	w.ShowAndRun()
}
