// sbgui-gui: native Fyne front-end for the sbgui core engine.
//
// Layout (milestone 3) follows the Qv2ray arrangement: a left
// navigation rail (Nodes / Subscriptions / Rules / Connections /
// Settings), a top bar with the connection controls, the page content
// in the middle and a slim status bar at the bottom. All behaviour
// (store, config build, sing-box process, system proxy, rules) comes
// from the shared core package -- the same code the web dashboard
// drives -- so the two front-ends cannot drift apart.
package main

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/Chuang2016/singbox-gui/core"
)

// demoLinks seed the list (in memory only, never saved) when the store
// is empty, so a first run shows the real core.ParseShareLink path.
var demoLinks = []string{
	"vless://00000000-0000-0000-0000-000000000001@example.com:443?security=tls&sni=example.com#Demo-VLESS",
	"trojan://demopass@example.com:443?sni=example.com#Demo-Trojan",
	"hysteria2://demopass@example.com:443?insecure=1#Demo-HY2",
}

const (
	pageNodes = "nodes"
	pageSubs  = "subs"
	pageRules = "rules"
	pageConns = "conns"
	pagePrefs = "settings"
)

// healthVal is one cached measurement (delay ms or speed Mbps) for the
// Nodes table, mirroring the web dashboard's delay/speed caches.
type healthVal struct {
	val float64
	ok  bool
	at  string
}

type guiState struct {
	app fyne.App
	win fyne.Window

	workDir string
	store   *core.Store
	mgr     *core.Manager

	notice  string   // sticky message in the status bar until the next action
	uiLogs  []string // GUI-side log lines, shown with the sing-box log
	logMu   sync.Mutex
	sysProxy   bool // we currently own the OS proxy settings
	wasRunning bool // last observed sing-box state (auto-clear check)

	// shell widgets
	connBt   *widget.Button
	proxySw  *widget.Check
	curNode  *widget.Label
	statusTx *widget.Label
	rateTx   *widget.Label
	pages    map[string]fyne.CanvasObject
	stack    *fyne.Container
	navBt    map[string]*widget.Button
	current  string
	ticks    int

	// nodes page
	nodeTable *widget.Table
	logLabel  *widget.Label
	healthMu  sync.Mutex
	delays    map[string]healthVal
	speeds    map[string]healthVal

	// connections page
	connTable    *widget.Table
	conns        []core.Connection
	connNote     *widget.Label
	connFetching bool

	// traffic (tracker created by initEngine)
	tracker *core.TrafficTracker

	// subscriptions page
	subList         *widget.List
	subSel          int
	lastAutoRefresh time.Time

	// rules page
	rulesEd *rulesEditor
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

// logf appends a timestamped GUI-side line to the log pane.
func (g *guiState) logf(format string, args ...any) {
	g.logMu.Lock()
	defer g.logMu.Unlock()
	line := time.Now().Format("15:04:05") + " " + fmt.Sprintf(format, args...)
	g.uiLogs = append(g.uiLogs, line)
	if len(g.uiLogs) > 200 {
		g.uiLogs = g.uiLogs[len(g.uiLogs)-200:]
	}
}

// fail parks an error in the sticky notice and the log: the periodic
// refresh must never silently wipe a failure (milestone-2 lesson).
func (g *guiState) fail(format string, args ...any) {
	g.notice = fmt.Sprintf(format, args...)
	g.logf("%s", g.notice)
	g.refreshStatus()
}

// setSysProxy switches the OS proxy through the shared core
// implementation and keeps toggle/state/log in line; on failure the
// toggle reverts to the real state.
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
// Disconnect, on sing-box exiting by itself, and before Quit, so the OS
// never keeps pointing at a port where nothing listens anymore.
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

// startProxy regenerates the config, then starts the subprocess
// (mirrors server.Server.StartProxy via the shared core.Regen).
func (g *guiState) startProxy() error {
	if err := core.Regen(g.store.Data, g.workDir); err != nil {
		return err
	}
	return g.mgr.Start()
}

func (g *guiState) toggle() {
	starting := !g.mgr.Running()
	var err error
	if !starting {
		err = g.mgr.Stop()
	} else {
		if n := g.activeNode(); n != nil {
			g.store.Data.Cfg.ActiveNodeID = n.ID
			_ = g.store.Save()
		}
		err = g.startProxy()
	}
	if err != nil {
		what := "Start"
		if !starting {
			what = "Stop"
		}
		g.fail("%s failed: %v", what, err)
		return
	}
	if !starting {
		g.clearSysProxy()
	}
	g.notice = ""
	g.refreshStatus()
}

// selectNode marks a node active (click in the Nodes table).
//
// While stopped it only changes the selection, as before. While
// running it switches immediately, the way the web dashboard's
// restart flow does: set active, regenerate the config, restart
// sing-box. The system proxy stays on across the switch (the mixed
// port does not change). If the restart fails, the selection is
// rolled back to the previous node and the failure is parked in the
// sticky notice -- the UI must never claim to be connected to a node
// that is not actually running.
func (g *guiState) selectNode(id string) {
	prevID := g.store.Data.Cfg.ActiveNodeID
	if id == prevID {
		return // clicking the already-active node changes nothing
	}
	g.store.Data.Cfg.ActiveNodeID = id
	_ = g.store.Save()
	if !g.mgr.Running() {
		g.refreshStatus()
		if g.nodeTable != nil {
			g.nodeTable.Refresh()
		}
		return
	}
	tag := id
	if n := g.activeNode(); n != nil {
		tag = n.Tag
	}
	g.logf("Switching to %s ...", tag)
	_ = g.mgr.Stop()
	if err := g.startProxy(); err != nil {
		// Roll the selection back: sing-box is stopped and still
		// configured for the previous node on the next manual start.
		g.store.Data.Cfg.ActiveNodeID = prevID
		_ = g.store.Save()
		g.fail("Switch to %s failed (rolled back): %v", tag, err)
		if g.nodeTable != nil {
			g.nodeTable.Refresh()
		}
		return
	}
	g.logf("Switched to %s", tag)
	g.notice = ""
	g.refreshStatus()
	if g.nodeTable != nil {
		g.nodeTable.Refresh()
	}
}

// showPage switches the visible page and the nav highlight.
func (g *guiState) showPage(name string) {
	g.current = name
	for n, p := range g.pages {
		if n == name {
			p.Show()
		} else {
			p.Hide()
		}
	}
	for n, b := range g.navBt {
		if n == name {
			b.Importance = widget.HighImportance
		} else {
			b.Importance = widget.MediumImportance
		}
		b.Refresh()
	}
	if name == pageRules && g.rulesEd != nil {
		g.rulesEd.reload()
	}
	g.stack.Refresh()
	g.refreshPage()
}

// refreshPage refreshes the visible page's dynamic content.
func (g *guiState) refreshPage() {
	switch g.current {
	case pageNodes:
		if g.nodeTable != nil {
			g.nodeTable.Refresh()
		}
		g.refreshLogs()
	case pageConns:
		g.maybeFetchConns()
		if g.connTable != nil {
			g.connTable.Refresh()
		}
	}
}

// refreshLogs renders sing-box + GUI log lines into the Nodes log pane.
func (g *guiState) refreshLogs() {
	if g.logLabel == nil {
		return
	}
	g.logMu.Lock()
	guiLines := append([]string{}, g.uiLogs...)
	g.logMu.Unlock()
	lines := append(g.mgr.Logs(), guiLines...)
	if len(lines) > 16 {
		lines = lines[len(lines)-16:]
	}
	g.logLabel.SetText(strings.Join(lines, "\n"))
}

// refreshStatus runs once a second: process state, top bar, status bar,
// and the visible page.
func (g *guiState) refreshStatus() {
	g.ticks++
	running := g.mgr.Running()
	if g.wasRunning && !running && g.sysProxy {
		g.clearSysProxy() // sing-box died on its own: free the OS proxy
	}
	g.wasRunning = running

	if running {
		g.connBt.SetText("Disconnect")
	} else {
		g.connBt.SetText("Connect")
	}
	if g.proxySw != nil {
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
	g.curNode.SetText("Node: " + node)

	state := "Stopped"
	if running {
		state = "Running"
	}
	txt := fmt.Sprintf("Status: %s  |  Mixed port: %d  |  Mode: %s",
		state, g.store.Data.Cfg.MixedPort, g.store.Data.Cfg.Mode)
	if g.notice != "" {
		txt += "  |  " + g.notice
	}
	g.statusTx.SetText(txt)
	g.refreshRates()
	g.maybeAutoRefreshSubs()
	g.refreshPage()
}

func main() {
	a := app.NewWithID("org.sbgui.native")
	applyTheme(a)
	w := a.NewWindow("sbgui — sing-box manager")
	w.Resize(fyne.NewSize(1060, 680))

	workDir := core.WorkDir()
	store, err := core.OpenStore(workDir)
	if err != nil {
		w.SetContent(widget.NewLabel("Failed to open store: " + err.Error()))
		w.ShowAndRun()
		return
	}
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
		}
		if store.Data.Cfg.ActiveNodeID == "" && len(store.Data.Nodes) > 0 {
			store.Data.Cfg.ActiveNodeID = store.Data.Nodes[0].ID
		}
	}

	g := &guiState{
		app: a, win: w, workDir: workDir, store: store,
		mgr:     core.NewManager(workDir),
		current: pageNodes,
		pages:   map[string]fyne.CanvasObject{},
		navBt:   map[string]*widget.Button{},
		delays:  map[string]healthVal{},
		speeds:  map[string]healthVal{},
	}
	g.initEngine()

	// --- top bar: title, current node, system proxy, connect ---
	g.curNode = widget.NewLabel("")
	g.connBt = widget.NewButton("Connect", g.toggle)
	g.connBt.Importance = widget.HighImportance
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
	topBar := container.NewBorder(nil, nil,
		widget.NewLabelWithStyle("sbgui", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewHBox(g.proxySw, g.connBt),
		g.curNode,
	)

	// --- left navigation rail ---
	navItems := []struct{ id, label string }{
		{pageNodes, "Nodes"},
		{pageSubs, "Subscriptions"},
		{pageRules, "Rules"},
		{pageConns, "Connections"},
		{pagePrefs, "Settings"},
	}
	navBox := container.NewVBox()
	for _, it := range navItems {
		it := it
		b := widget.NewButton(it.label, func() { g.showPage(it.id) })
		b.Alignment = widget.ButtonAlignLeading
		g.navBt[it.id] = b
		navBox.Add(b)
	}
	navBox.Add(layout.NewSpacer())

	// --- pages ---
	g.pages[pageNodes] = g.buildNodesPage()
	g.pages[pageSubs] = g.buildSubsPage()
	g.pages[pageRules] = g.buildRulesPage()
	g.pages[pageConns] = g.buildConnsPage()
	g.pages[pagePrefs] = g.buildSettingsPage()
	stackObjs := make([]fyne.CanvasObject, 0, len(navItems))
	for _, it := range navItems {
		stackObjs = append(stackObjs, g.pages[it.id])
	}
	g.stack = container.NewStack(stackObjs...)

	// --- bottom status bar ---
	g.statusTx = widget.NewLabel("")
	g.rateTx = widget.NewLabel("")
	bottomBar := container.NewBorder(nil, nil, g.statusTx, g.rateTx, nil)

	w.SetContent(container.NewBorder(topBar, bottomBar, navBox, nil, g.stack))

	g.showPage(pageNodes)
	g.refreshStatus()

	// Auto-start the proxy on launch when the setting says so
	// (the web build honours cfg.AutoStart the same way).
	if store.Data.Cfg.AutoStart && len(store.Data.Nodes) > 0 {
		if err := g.startProxy(); err != nil {
			g.fail("Auto-start failed: %v", err)
		} else {
			g.logf("Proxy auto-started")
			g.refreshStatus()
		}
	}

	// System tray: Show / Quit. Closing the window hides to the tray.
	if desk, ok := a.(desktop.App); ok {
		menu := fyne.NewMenu("sbgui",
			fyne.NewMenuItem("Show", func() { w.Show() }),
			fyne.NewMenuItem("Quit", func() {
				g.quit()
			}),
		)
		desk.SetSystemTrayMenu(menu)
		w.SetCloseIntercept(func() { w.Hide() })
	}

	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for range t.C {
			fyne.Do(g.refreshStatus)
		}
	}()

	w.ShowAndRun()
}

// quit stops the engine in the right order: clear the OS proxy first,
// then stop sing-box, then flush per-node traffic totals.
func (g *guiState) quit() {
	if g.sysProxy {
		_ = core.SetSystemProxy(false, g.store.Data.Cfg.MixedPort)
		g.sysProxy = false
	}
	if g.mgr.Running() {
		_ = g.mgr.Stop()
	}
	g.stopEngine()
	g.app.Quit()
}
