// sbgui-gapp: lightweight Gio front-end for the sbgui core engine.
//
// This is the low-memory sibling of the Fyne front-end in ../gui: the
// same pages, the same core calls, the same save semantics, rendered
// with gioui.org instead of Fyne/GLFW. Gio draws with its own GPU
// renderer and carries no widget-toolkit runtime, which is the whole
// point of this build (the Fyne build idles near 200 MB RSS; this one
// targets a small fraction of that).
//
// Layout follows gui/: a left navigation rail (Nodes / Subscriptions
// / Rules / Connections / Settings), a top bar with the connection
// controls, the page in the middle and a slim status bar at the
// bottom. All behaviour comes from the shared core package.
//
// Differences from the Fyne build, forced by Gio's toolkit surface:
//   - no system tray (Gio has none): closing the window quits, with
//     the same cleanup as the Fyne tray Quit (clear OS proxy, stop
//     sing-box, flush traffic totals);
//   - dialogs are modal cards drawn inside the window.
package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"gioui.org/app"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/Chuang2016/singbox-gui/core"
)

// demoLinks seed the list (in memory only, never saved) when the store
// is empty, so a first run shows the real core.ParseShareLink path.
// Same links as gui/main.go.
var demoLinks = []string{
	"vless://00000000-0000-0000-0000-000000000001@example.com:443?security=tls&sni=example.com#Demo-VLESS",
	"trojan://demopass@example.com:443?sni=example.com#Demo-Trojan",
	"hysteria2://demopass@example.com:443?insecure=1#Demo-HY2",
}

// gioVersion labels this front-end build.
const gioVersion = "1.1.0-gui-preview.12-gio"

const (
	pageNodes = "nodes"
	pageSubs  = "subs"
	pageRules = "rules"
	pageConns = "conns"
	pagePrefs = "settings"
)

// healthVal is one cached measurement (delay ms or speed Mbps) for the
// Nodes table, mirroring gui/'s delay/speed caches.
type healthVal struct {
	val float64
	ok  bool
	at  string
}

// modal is an in-window modal card (Gio has no native dialogs).
type modal interface {
	layoutModal(gtx layout.Context, a *App) layout.Dimensions
}

type App struct {
	w  *app.Window
	th *material.Theme

	workDir string
	store   *core.Store
	mgr     *core.Manager
	tracker *core.TrafficTracker
	initErr error

	// inbox marshals background completions onto the UI goroutine
	// (the fyne.Do equivalent).
	inboxMu sync.Mutex
	inbox   []func()

	notice   string // sticky status-bar message until the next action
	logMu    sync.Mutex
	uiLogs   []string
	sysProxy bool // we currently own the OS proxy settings
	wasRun   bool // last observed sing-box state
	busy     bool // a start/stop/switch is in flight

	page    string
	navBtns map[string]*widget.Clickable
	navIDs  []string
	connBtn widget.Clickable
	proxyCk widget.Bool

	// cached status-bar strings, refreshed once a second
	statusText string
	nodeText   string
	rateText   string
	lastTick   time.Time
	ticks      int

	modal     modal
	modalBg   widget.Clickable
	quitClean bool

	nodes nodesPage
	subs  subsPage
	rules rulesPage
	conns connsPage
}

func (a *App) post(fn func()) {
	a.inboxMu.Lock()
	a.inbox = append(a.inbox, fn)
	a.inboxMu.Unlock()
	a.w.Invalidate()
}

func (a *App) drainInbox() {
	a.inboxMu.Lock()
	fns := a.inbox
	a.inbox = nil
	a.inboxMu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

func (a *App) activeNode() *core.Node {
	for i := range a.store.Data.Nodes {
		if a.store.Data.Nodes[i].ID == a.store.Data.Cfg.ActiveNodeID {
			return &a.store.Data.Nodes[i]
		}
	}
	if len(a.store.Data.Nodes) > 0 {
		return &a.store.Data.Nodes[0]
	}
	return nil
}

// logf appends a timestamped GUI-side line to the log pane.
func (a *App) logf(format string, args ...any) {
	a.logMu.Lock()
	defer a.logMu.Unlock()
	line := time.Now().Format("15:04:05") + " " + fmt.Sprintf(format, args...)
	a.uiLogs = append(a.uiLogs, line)
	if len(a.uiLogs) > 200 {
		a.uiLogs = a.uiLogs[len(a.uiLogs)-200:]
	}
}

// fail parks an error in the sticky notice and the log.
func (a *App) fail(format string, args ...any) {
	a.notice = fmt.Sprintf(format, args...)
	a.logf("%s", a.notice)
}

// setSysProxy switches the OS proxy through the shared core
// implementation; on failure the toggle reverts to the real state.
func (a *App) setSysProxy(on bool) {
	port := a.store.Data.Cfg.MixedPort
	if err := core.SetSystemProxy(on, port); err != nil {
		a.logf("System proxy: %v", err)
		a.proxyCk.Value = a.sysProxy
		return
	}
	a.sysProxy = on
	if on {
		a.logf("System proxy on (127.0.0.1:%d)", port)
	} else {
		a.logf("System proxy off")
	}
}

// clearSysProxy turns the OS proxy back off if we turned it on.
func (a *App) clearSysProxy() {
	if !a.sysProxy {
		return
	}
	if err := core.SetSystemProxy(false, a.store.Data.Cfg.MixedPort); err != nil {
		a.logf("System proxy clear failed: %v", err)
		return
	}
	a.sysProxy = false
	a.proxyCk.Value = false
	a.logf("System proxy off")
}

// startProxy regenerates the config, then starts the subprocess.
func (a *App) startProxy() error {
	if err := core.Regen(a.store.Data, a.workDir); err != nil {
		return err
	}
	return a.mgr.Start()
}

// toggle connects or disconnects (async: mgr.Start blocks on the
// sing-box config check, so it runs off the UI goroutine).
func (a *App) toggle() {
	if a.busy {
		return
	}
	starting := !a.mgr.Running()
	if starting {
		if n := a.activeNode(); n != nil {
			a.store.Data.Cfg.ActiveNodeID = n.ID
			_ = a.store.Save()
		}
	}
	a.busy = true
	go func() {
		var err error
		if starting {
			err = a.startProxy()
		} else {
			err = a.mgr.Stop()
		}
		a.post(func() {
			a.busy = false
			if err != nil {
				what := "Start"
				if !starting {
					what = "Stop"
				}
				a.fail("%s failed: %v", what, err)
				return
			}
			if !starting {
				a.clearSysProxy()
			}
			a.notice = ""
		})
	}()
}

// selectNode marks a node active; while running it live-switches with
// rollback on failure, exactly like gui/'s selectNode.
func (a *App) selectNode(id string) {
	prevID := a.store.Data.Cfg.ActiveNodeID
	if id == prevID || a.busy {
		return
	}
	a.store.Data.Cfg.ActiveNodeID = id
	_ = a.store.Save()
	if !a.mgr.Running() {
		return
	}
	tag := id
	if n := a.activeNode(); n != nil {
		tag = n.Tag
	}
	a.logf("Switching to %s ...", tag)
	a.busy = true
	go func() {
		_ = a.mgr.Stop()
		err := a.startProxy()
		a.post(func() {
			a.busy = false
			if err != nil {
				a.store.Data.Cfg.ActiveNodeID = prevID
				_ = a.store.Save()
				a.fail("Switch to %s failed (rolled back): %v", tag, err)
				return
			}
			a.logf("Switched to %s", tag)
			a.notice = ""
		})
	}()
}

// restartProxy applies new settings: stop, regen, start again.
func (a *App) restartProxy() {
	if a.busy {
		return
	}
	a.busy = true
	go func() {
		_ = a.mgr.Stop()
		err := a.startProxy()
		a.post(func() {
			a.busy = false
			if err != nil {
				a.notice = "Restart failed: " + err.Error()
				a.logf("%s", a.notice)
			} else {
				a.notice = ""
				a.logf("sing-box restarted to apply new rules")
			}
		})
	}()
}

// offerRestart asks (when running) and restarts sing-box so a saved
// change takes effect.
func (a *App) offerRestart(prompt string) {
	if !a.mgr.Running() {
		return
	}
	a.showConfirm("sbgui", prompt, func() { a.restartProxy() })
}

// tick runs the once-a-second refresh: process state, status-bar
// strings, rates, subscription auto-refresh, connection polling.
func (a *App) tick() {
	now := time.Now()
	if now.Sub(a.lastTick) < 900*time.Millisecond {
		return
	}
	a.lastTick = now
	a.ticks++

	running := a.mgr.Running()
	if a.wasRun && !running && a.sysProxy {
		a.clearSysProxy() // sing-box died on its own: free the OS proxy
	}
	a.wasRun = running

	node := "none"
	if n := a.activeNode(); n != nil {
		node = n.Tag
	}
	a.nodeText = "Node: " + node
	state := "Stopped"
	if running {
		state = "Running"
	}
	txt := fmt.Sprintf("Status: %s  |  Mixed port: %d  |  Mode: %s",
		state, a.store.Data.Cfg.MixedPort, a.store.Data.Cfg.Mode)
	if a.notice != "" {
		txt += "  |  " + a.notice
	}
	a.statusText = txt

	a.rateText = ""
	if a.tracker != nil && running {
		up, down := a.tracker.Rates()
		a.rateText = fmt.Sprintf("Down %s  |  Up %s", humanRate(down), humanRate(up))
	}
	a.subs.maybeAutoRefresh(a)
	if a.page == pageConns {
		a.conns.maybeFetch(a)
	}
}

// quit performs the window-close cleanup (Fyne tray-Quit equivalent).
func (a *App) quit() {
	if a.quitClean {
		return
	}
	a.quitClean = true
	if a.sysProxy {
		_ = core.SetSystemProxy(false, a.store.Data.Cfg.MixedPort)
		a.sysProxy = false
	}
	if a.mgr != nil && a.mgr.Running() {
		_ = a.mgr.Stop()
	}
	if a.tracker != nil {
		a.tracker.Save()
	}
}

func (a *App) run() {
	var ops op.Ops
	for {
		switch e := a.w.Event().(type) {
		case app.DestroyEvent:
			a.quit()
			return
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			a.drainInbox()
			a.tick()
			a.layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

// layout draws the shell: top bar, nav rail + page, status bar, and
// any modal on top.
func (a *App) layout(gtx layout.Context) layout.Dimensions {
	if a.initErr != nil {
		return layout.Center.Layout(gtx, material.Label(a.th, a.th.TextSize,
			"Failed to open store: "+a.initErr.Error()).Layout)
	}
	// Gio does not clear the window with the theme color; paint
	// the background explicitly before any content.
	paint.FillShape(gtx.Ops, a.th.Bg, clip.Rect{Max: gtx.Constraints.Max}.Op())
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(a.layoutTopBar),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
						layout.Rigid(a.layoutNav),
						layout.Flexed(1, a.layoutPage),
					)
				}),
				layout.Rigid(a.layoutStatusBar),
			)
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			if a.modal == nil {
				return layout.Dimensions{}
			}
			return a.layoutModal(gtx)
		}),
	)
}

func (a *App) layoutTopBar(gtx layout.Context) layout.Dimensions {
	if a.connBtn.Clicked(gtx) {
		a.toggle()
	}
	if a.proxyCk.Update(gtx) {
		if a.proxyCk.Value && !a.mgr.Running() {
			a.logf("System proxy: connect first")
			a.proxyCk.Value = false
		} else {
			a.setSysProxy(a.proxyCk.Value)
		}
	}
	connLabel := "Connect"
	if a.mgr.Running() {
		connLabel = "Disconnect"
	}
	return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(a.th, unit.Sp(17), "sbgui")
					l.Font.Weight = font.Bold
					return l.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return material.Label(a.th, a.th.TextSize, a.nodeText).Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return material.CheckBox(a.th, &a.proxyCk, "System proxy").Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
				layout.Rigid(material.Button(a.th, &a.connBtn, connLabel).Layout),
			)
		})
}

func (a *App) layoutNav(gtx layout.Context) layout.Dimensions {
	labels := map[string]string{
		pageNodes: "Nodes", pageSubs: "Subscriptions", pageRules: "Rules",
		pageConns: "Connections", pagePrefs: "Settings",
	}
	children := make([]layout.FlexChild, 0, len(a.navIDs))
	for _, id := range a.navIDs {
		id := id
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if a.navBtns[id].Clicked(gtx) {
				a.page = id
				if id == pageRules {
					a.rules.reload()
				}
			}
			btn := material.Button(a.th, a.navBtns[id], labels[id])
			if a.page != id {
				btn.Background = a.th.Bg
				btn.Color = a.th.Fg
			}
			return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, btn.Layout)
		}))
	}
	gtx.Constraints.Min.X = gtx.Dp(unit.Dp(128))
	gtx.Constraints.Max.X = gtx.Dp(unit.Dp(128))
	return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
}

func (a *App) layoutPage(gtx layout.Context) layout.Dimensions {
	switch a.page {
	case pageSubs:
		return a.subs.layout(gtx, a)
	case pageRules:
		return a.rules.layout(gtx, a)
	case pageConns:
		return a.conns.layout(gtx, a)
	case pagePrefs:
		return layoutSettings(gtx, a)
	default:
		return a.nodes.layout(gtx, a)
	}
}

func (a *App) layoutStatusBar(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(6), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return material.Label(a.th, unit.Sp(12.5), a.statusText).Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return material.Label(a.th, unit.Sp(12.5), a.rateText).Layout(gtx)
				}),
			)
		})
}

// logText renders the last lines of sing-box + GUI logs for the
// Nodes page log pane.
func (a *App) logText() string {
	a.logMu.Lock()
	guiLines := append([]string{}, a.uiLogs...)
	a.logMu.Unlock()
	lines := append(a.mgr.Logs(), guiLines...)
	if len(lines) > 16 {
		lines = lines[len(lines)-16:]
	}
	return strings.Join(lines, "\n")
}

func main() {
	workDir := core.WorkDir()
	store, err := core.OpenStore(workDir)

	a := &App{
		workDir: workDir,
		store:   store,
		initErr: err,
		th:      newTheme(),
		page:    pageNodes,
		navBtns: map[string]*widget.Clickable{},
		navIDs:  []string{pageNodes, pageSubs, pageRules, pageConns, pagePrefs},
	}
	for _, id := range a.navIDs {
		a.navBtns[id] = &widget.Clickable{}
	}
	a.w = new(app.Window)
	a.w.Option(app.Title("sbgui — sing-box manager "+gioVersion),
		app.Size(unit.Dp(1060), unit.Dp(680)))

	if err == nil {
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
		a.mgr = core.NewManager(workDir)
		a.tracker = core.NewTrafficTracker(workDir)
		go a.tracker.Loop(a.mgr.Running, func() int { return a.store.Data.Cfg.ClashPort })
		a.nodes.init()
		a.subs.init()
		a.rules.init(a)
		a.conns.init()

		// Auto-start the proxy on launch when the setting says so.
		if store.Data.Cfg.AutoStart && len(store.Data.Nodes) > 0 {
			go func() {
				if err := a.startProxy(); err != nil {
					a.post(func() { a.fail("Auto-start failed: %v", err) })
				} else {
					a.post(func() { a.logf("Proxy auto-started") })
				}
			}()
		}
	}

	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for range t.C {
			a.w.Invalidate()
		}
	}()
	go func() {
		a.run()
		os.Exit(0)
	}()
	app.Main()
}
