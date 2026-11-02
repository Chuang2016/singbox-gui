//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"

	"github.com/Chuang2016/singbox-gui/core"
	"github.com/Chuang2016/singbox-gui/server"
)

// app is the Walk front-end state. All widget access happens on the UI
// goroutine (walk's message loop); background work posts back through
// mw.Synchronize -- see postLog/postFail.
type app struct {
	store   *core.Store
	mgr     *core.Manager
	srv     *server.Server
	workDir string
	port    int

	icon  *walk.Icon
	model *nodeModel

	mw       *walk.MainWindow
	ni       *walk.NotifyIcon
	table    *walk.TableView
	connBtn  *walk.PushButton
	proxyChk *walk.CheckBox
	modeCmb  *walk.ComboBox
	importEd *walk.LineEdit
	logEd    *walk.TextEdit
	railBox  *walk.Composite

	sbStatus *walk.StatusBarItem
	sbNode   *walk.StatusBarItem
	sbPorts  *walk.StatusBarItem
	sbRates  *walk.StatusBarItem

	logMu  sync.Mutex
	uiLogs []string
	notice string // sticky error, cleared by the next successful action

	sysProxy   bool // we currently own the OS proxy settings
	wasRunning bool // last observed sing-box state (auto-clear check)

	ready          bool // toolbar handlers armed (after initial sync)
	suppressSelect bool // ignore selection events we caused ourselves
	quitting       bool
	closed         bool
	done           chan struct{}

	lastLogKey     string
	lastNodeCount  int
	lastTrafficSum int64

	delayMu sync.Mutex
	delays  map[string]int // nodeID -> ms, -1 = timeout; absent = untested

	subList         *walk.ListBox
	subModel        *subModel
	lastAutoRefresh time.Time
}

func newApp(store *core.Store, mgr *core.Manager, srv *server.Server, workDir string, port int) *app {
	a := &app{
		store: store, mgr: mgr, srv: srv, workDir: workDir, port: port,
		done:   make(chan struct{}),
		delays: map[string]int{},
	}
	a.model = &nodeModel{a: a}
	a.icon = loadIcon()
	return a
}

func (a *app) buildWindow() error {
	def := MainWindow{
		AssignTo: &a.mw,
		Title:    "sbgui",
		MinSize:  Size{Width: 760, Height: 500},
		Size:     Size{Width: 1020, Height: 660},
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
					PushButton{
						Text:      "Test node",
						OnClicked: func() { a.testLatencySelected() },
					},
					PushButton{
						Text:      "Test all",
						OnClicked: func() { a.testLatencyAll() },
					},
					HSpacer{},
					PushButton{
						Text:      "Open web back-end",
						OnClicked: func() { a.openInBrowser() },
					},
				},
			},
			HSplitter{
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
						AssignTo: &a.table,
						Model:    a.model,
						Columns: []TableViewColumn{
							{Title: "", Width: 26},
							{Title: "Name", Width: 300},
							{Title: "Protocol", Width: 90},
							{Title: "Latency", Width: 84},
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
						CueBanner: "Paste one or more share links (vless / trojan / ss / vmess / hysteria2 / tuic)...",
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
	}
	if err := def.Create(); err != nil {
		return err
	}
	if a.icon != nil {
		_ = a.mw.SetIcon(a.icon)
	}
	_ = a.mw.SetTitle("sbgui " + version)
	a.table.CurrentIndexChanged().Attach(func() { a.onRowSelected() })
	a.proxyChk.CheckedChanged().Attach(func() { a.onProxyToggle() })
	a.modeCmb.CurrentIndexChanged().Attach(func() { a.onModeChanged() })
	_ = a.modeCmb.SetCurrentIndex(modeIndex(a.store.Data.Cfg.Mode))
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
	a.lastNodeCount = len(a.store.Data.Nodes)
	a.syncTableSelection()
	a.refreshStatus()
	return nil
}

// ---------- tray ----------

func (a *app) initTray() {
	ni, err := walk.NewNotifyIcon(a.mw)
	if err != nil {
		return // no tray host: the window alone still works
	}
	a.ni = ni
	if a.icon != nil {
		_ = ni.SetIcon(a.icon)
	}
	_ = ni.SetToolTip("sbgui - sing-box manager")

	showAct := walk.NewAction()
	_ = showAct.SetText("Show")
	showAct.Triggered().Attach(func() { a.showWindow() })
	browserAct := walk.NewAction()
	_ = browserAct.SetText("Open in browser")
	browserAct.Triggered().Attach(func() { a.openInBrowser() })
	quitAct := walk.NewAction()
	_ = quitAct.SetText("Quit")
	quitAct.Triggered().Attach(func() { a.quit() })
	_ = ni.ContextMenu().Actions().Add(showAct)
	_ = ni.ContextMenu().Actions().Add(browserAct)
	_ = ni.ContextMenu().Actions().Add(quitAct)

	ni.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			a.showWindow()
		}
	})
	_ = ni.SetVisible(true)
}

func (a *app) showWindow() {
	if a.mw == nil {
		return
	}
	a.mw.Show()
	_ = a.mw.SetFocus()
}

// openInBrowser opens the full web back-end in the default browser --
// the second face of this same process.
func (a *app) openInBrowser() {
	url := fmt.Sprintf("http://127.0.0.1:%d", a.port)
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

// ---------- logging / sticky errors ----------

// logf appends a timestamped front-end line to the log pane.
func (a *app) logf(format string, args ...any) {
	a.logMu.Lock()
	defer a.logMu.Unlock()
	line := time.Now().Format("15:04:05") + " " + fmt.Sprintf(format, args...)
	a.uiLogs = append(a.uiLogs, line)
	if len(a.uiLogs) > 200 {
		a.uiLogs = a.uiLogs[len(a.uiLogs)-200:]
	}
}

// fail parks an error in the sticky notice and the log: the periodic
// refresh must never silently wipe a failure (milestone-2 lesson).
func (a *app) fail(format string, args ...any) {
	a.notice = fmt.Sprintf(format, args...)
	a.logf("%s", a.notice)
	a.refreshStatus()
}

// postLog / postFail are the goroutine-safe variants (they hop onto
// the UI goroutine). Never call them from the UI goroutine itself.
func (a *app) postLog(format string, args ...any) {
	a.mw.Synchronize(func() {
		a.logf(format, args...)
		a.refreshLogs()
	})
}

func (a *app) postFail(format string, args ...any) {
	a.mw.Synchronize(func() { a.fail(format, args...) })
}

// refreshLogs rewrites the log pane when its content changed: our own
// lines first, then the sing-box manager's ring buffer.
func (a *app) refreshLogs() {
	if a.logEd == nil {
		return
	}
	a.logMu.Lock()
	own := make([]string, len(a.uiLogs))
	copy(own, a.uiLogs)
	a.logMu.Unlock()
	mgrLogs := a.mgr.Logs()
	key := fmt.Sprintf("%d/%d|%s|%s", len(own), len(mgrLogs), lastLine(own), lastLine(mgrLogs))
	if key == a.lastLogKey {
		return
	}
	a.lastLogKey = key
	var b strings.Builder
	for _, l := range own {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	if len(own) > 0 && len(mgrLogs) > 0 {
		b.WriteString("--- sing-box ---\n")
	}
	for _, l := range mgrLogs {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	text := b.String()
	a.logEd.SetText(text)
	a.logEd.SetTextSelection(len(text), len(text))
}

func lastLine(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}

// ---------- status ----------

func (a *app) activeNode() *core.Node {
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

func (a *app) refreshStatus() {
	if a.mw == nil {
		return
	}
	running := a.mgr.Running()
	switch {
	case a.notice != "":
		a.sbStatus.SetText("Error (see log)")
	case running:
		a.sbStatus.SetText("Running")
	default:
		a.sbStatus.SetText("Stopped")
	}
	node := "none"
	if n := a.activeNode(); n != nil {
		node = n.Tag
	}
	a.sbNode.SetText("Node: " + node)
	cfg := a.store.Data.Cfg
	a.sbPorts.SetText(fmt.Sprintf("Mixed %d | Clash %d | Web %d", cfg.MixedPort, cfg.ClashPort, a.port))
	if a.connBtn != nil {
		if running {
			a.connBtn.SetText("Disconnect")
		} else {
			a.connBtn.SetText("Connect")
		}
	}
	if a.proxyChk != nil {
		a.proxyChk.SetEnabled(running || a.sysProxy)
	}
}

// ---------- system proxy lifecycle (shared core implementation) ----------

// setSysProxy switches the OS proxy through core.SetSystemProxy and
// keeps state/log in line; on failure the state (and the toggle, via
// refreshStatus) reverts to reality.
func (a *app) setSysProxy(on bool) {
	port := a.store.Data.Cfg.MixedPort
	if err := core.SetSystemProxy(on, port); err != nil {
		a.logf("System proxy: %v", err)
		a.syncProxyCheckbox()
		a.refreshStatus()
		return
	}
	a.sysProxy = on
	if on {
		a.logf("System proxy on (127.0.0.1:%d)", port)
	} else {
		a.logf("System proxy off")
	}
	a.syncProxyCheckbox()
	a.refreshStatus()
}

// clearSysProxy turns the OS proxy back off if we turned it on. Used
// on Disconnect, on sing-box exiting by itself, and before Quit, so
// the OS never keeps pointing at a port where nothing listens.
func (a *app) clearSysProxy() {
	if !a.sysProxy {
		return
	}
	if err := core.SetSystemProxy(false, a.store.Data.Cfg.MixedPort); err != nil {
		a.logf("System proxy clear failed: %v", err)
		return
	}
	a.sysProxy = false
	a.syncProxyCheckbox()
	a.logf("System proxy off")
}

// syncProxyCheckbox reflects the real system-proxy state in the
// toolbar toggle (nil until the controls commit adds the widget).
func (a *app) syncProxyCheckbox() {
	// a.closed: the window is gone (post-teardown cleanup) -- the
	// engine state still gets fixed, the widget must not be touched.
	if a.proxyChk != nil && !a.closed {
		a.proxyChk.SetChecked(a.sysProxy)
	}
}

// ---------- 1s ticker ----------

func (a *app) startTicker() {
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-a.done:
				return
			case <-t.C:
				a.mw.Synchronize(func() {
					if !a.closed {
						a.tick()
					}
				})
			}
		}
	}()
}

func (a *app) tick() {
	running := a.mgr.Running()
	if a.wasRunning && !running && a.sysProxy {
		a.clearSysProxy() // sing-box died on its own: free the OS proxy
	}
	a.wasRunning = running
	a.refreshStatus()

	if tr := a.srv.TrafficTracker(); tr != nil {
		if running {
			up, down := tr.Rates()
			a.sbRates.SetText(fmt.Sprintf("Down %s | Up %s", humanRate(down), humanRate(up)))
		} else if a.sbRates.Text() != "" {
			a.sbRates.SetText("")
		}
		var sum int64
		for _, n := range a.store.Data.Nodes {
			sum += tr.Total(n.ID)
		}
		if sum != a.lastTrafficSum {
			a.lastTrafficSum = sum
			a.model.PublishRowsReset()
		}
	}

	// The node set may change behind our back (web back-end edits,
	// subscription refresh): keep the table in sync.
	if len(a.store.Data.Nodes) != a.lastNodeCount {
		a.lastNodeCount = len(a.store.Data.Nodes)
		a.model.PublishRowsReset()
		a.syncTableSelection()
	}
	a.refreshLogs()
	a.maybeAutoRefreshSubs()
}

// ---------- quit ----------

// quit stops the engine in the right order: clear the OS proxy first,
// then stop sing-box, then flush per-node traffic totals.
func (a *app) quit() {
	a.quitting = true
	a.cleanupEngine()
	if a.ni != nil {
		_ = a.ni.SetVisible(false)
		a.ni.Dispose()
		a.ni = nil
	}
	a.mw.Close()
}

// shutdown runs after the message loop ends (also when the window is
// destroyed without going through quit): idempotent engine cleanup.
func (a *app) shutdown() {
	a.closed = true
	select {
	case <-a.done:
	default:
		close(a.done)
	}
	a.cleanupEngine()
}

func (a *app) cleanupEngine() {
	a.clearSysProxy()
	if a.mgr.Running() {
		_ = a.mgr.Stop()
	}
	if tr := a.srv.TrafficTracker(); tr != nil {
		tr.Save()
	}
}
