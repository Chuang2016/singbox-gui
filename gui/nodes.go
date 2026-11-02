package main

import (
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/Chuang2016/singbox-gui/core"
)

// Nodes page: import box, the node table (Name / Protocol / Latency /
// Speed / Traffic, Qv2ray-style) and a collapsible log pane. Clicking
// a row selects the node (and live-switches the running proxy).

var nodeColumns = []struct {
	title string
	width float32
}{
	{"Name", 330},
	{"Protocol", 110},
	{"Latency", 120},
	{"Speed", 120},
	{"Traffic", 130},
}

func (g *guiState) buildNodesPage() fyne.CanvasObject {
	// --- import row ---
	importEntry := widget.NewEntry()
	importEntry.SetPlaceHolder("Paste a share link (vless://, trojan://, hysteria2://, ...) and press Import")
	importBtn := widget.NewButton("Import link", func() {
		link := strings.TrimSpace(importEntry.Text)
		if link == "" {
			return
		}
		proto, ob, err := core.ParseShareLink(link)
		if err != nil {
			g.fail("Import failed: %v", err)
			return
		}
		g.store.Data.Nodes = append(g.store.Data.Nodes, core.Node{
			ID: core.NewID("n"), Tag: core.LinkTag(link, proto),
			Protocol: proto, Outbound: ob, Source: "manual",
		})
		_ = g.store.Save()
		g.notice = ""
		importEntry.SetText("")
		g.logf("Imported node (%s)", proto)
		if g.nodeTable != nil {
			g.nodeTable.Refresh()
		}
		g.refreshStatus()
	})
	importRow := container.NewBorder(nil, nil, nil, importBtn, importEntry)

	// --- node table (row 0 is the header) ---
	g.nodeTable = widget.NewTable(
		func() (int, int) { return len(g.store.Data.Nodes) + 1, len(nodeColumns) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.TableCellID, o fyne.CanvasObject) {
			lbl := o.(*widget.Label)
			lbl.TextStyle = fyne.TextStyle{}
			if id.Row == 0 {
				lbl.TextStyle = fyne.TextStyle{Bold: true}
				lbl.SetText(nodeColumns[id.Col].title)
				return
			}
			n := g.store.Data.Nodes[id.Row-1]
			switch id.Col {
			case 0:
				name := n.Tag
				if n.ID == g.store.Data.Cfg.ActiveNodeID {
					name = "▶ " + name
					lbl.TextStyle = fyne.TextStyle{Bold: true}
				}
				lbl.SetText(name)
			case 1:
				lbl.SetText(n.Protocol)
			case 2:
				lbl.SetText(g.latencyText(n.ID))
			case 3:
				lbl.SetText(g.speedText(n.ID))
			case 4:
				lbl.SetText(g.trafficText(n.ID))
			}
		},
	)
	for i, c := range nodeColumns {
		g.nodeTable.SetColumnWidth(i, c.width)
	}
	g.nodeTable.OnSelected = func(id widget.TableCellID) {
		if id.Row >= 1 && id.Row-1 < len(g.store.Data.Nodes) {
			g.selectNode(g.store.Data.Nodes[id.Row-1].ID)
		}
	}

	// --- measurement toolbar (delay / speed via the Clash API) ---
	testOneBtn := widget.NewButton("Test latency", func() { g.testLatencySelected() })
	testAllBtn := widget.NewButton("Test all", func() { g.testLatencyAll() })
	speedBtn := widget.NewButton("Speed test", func() { g.testSpeedSelected() })
	resetTrafficBtn := widget.NewButton("Reset traffic", func() {
		if g.tracker != nil {
			g.tracker.Reset()
			g.logf("Per-node traffic counters reset")
			g.nodeTable.Refresh()
		}
	})
	testRow := container.NewHBox(testOneBtn, testAllBtn, speedBtn, resetTrafficBtn)

	// --- collapsible log pane ---
	g.logLabel = widget.NewLabel("")
	g.logLabel.Wrapping = fyne.TextWrapBreak
	logScroll := container.NewVScroll(g.logLabel)
	logScroll.SetMinSize(fyne.NewSize(0, 120))
	logScroll.Hide()
	logCheck := widget.NewCheck("Log", func(on bool) {
		if on {
			logScroll.Show()
		} else {
			logScroll.Hide()
		}
	})
	logBox := container.NewVBox(logCheck, logScroll)

	top := container.NewVBox(importRow, testRow)
	return container.NewBorder(top, logBox, nil, nil, g.nodeTable)
}

// --- delay / speed measurement (shared core Clash API helpers; both
// need a running sing-box, exactly like the web dashboard) ---

func (g *guiState) selectedNodeID() string {
	if n := g.activeNode(); n != nil {
		return n.ID
	}
	return ""
}

func (g *guiState) testLatencySelected() {
	id := g.selectedNodeID()
	if id == "" {
		return
	}
	if !g.mgr.Running() {
		g.fail("Latency test: sing-box is not running")
		return
	}
	go func() {
		d := core.ClashDelay(g.store.Data.Cfg.ClashPort, "node:"+id)
		fyne.Do(func() {
			g.healthMu.Lock()
			g.delays[id] = healthVal{val: float64(d), ok: d >= 0, at: time.Now().Format("15:04:05")}
			g.healthMu.Unlock()
			if d >= 0 {
				g.logf("Latency %s: %d ms", id, d)
			} else {
				g.logf("Latency %s: timeout", id)
			}
			g.nodeTable.Refresh()
		})
	}()
}

func (g *guiState) testLatencyAll() {
	if !g.mgr.Running() {
		g.fail("Latency test: sing-box is not running")
		return
	}
	g.logf("Testing latency of %d nodes...", len(g.store.Data.Nodes))
	go func() {
		results := core.CheckAllDelays(g.store.Data.Cfg.ClashPort, g.store.Data.Nodes)
		fyne.Do(func() {
			g.healthMu.Lock()
			for id, d := range results {
				g.delays[id] = healthVal{val: float64(d), ok: d >= 0, at: time.Now().Format("15:04:05")}
			}
			g.healthMu.Unlock()
			g.logf("Latency test done")
			g.nodeTable.Refresh()
		})
	}()
}

func (g *guiState) testSpeedSelected() {
	id := g.selectedNodeID()
	if id == "" {
		return
	}
	if !g.mgr.Running() {
		g.fail("Speed test: sing-box is not running")
		return
	}
	g.logf("Speed test running (switches the selector to the node, then restores it)...")
	go func() {
		mbps, err := core.ClashSpeedTest(g.store, g.mgr, id)
		fyne.Do(func() {
			g.healthMu.Lock()
			if err == nil {
				g.speeds[id] = healthVal{val: mbps, ok: true, at: time.Now().Format("15:04:05")}
			} else {
				g.speeds[id] = healthVal{val: 0, ok: false, at: time.Now().Format("15:04:05")}
			}
			g.healthMu.Unlock()
			if err != nil {
				g.fail("Speed test failed: %v", err)
			} else {
				g.logf("Speed %s: %.1f Mbps", id, mbps)
				g.notice = ""
				g.refreshStatus()
			}
			g.nodeTable.Refresh()
		})
	}()
}

// latencyText renders the cached delay for the table ("86 ms",
// "timeout", or "-" when never measured).
func (g *guiState) latencyText(nodeID string) string {
	g.healthMu.Lock()
	defer g.healthMu.Unlock()
	h, ok := g.delays[nodeID]
	if !ok {
		return "-"
	}
	if !h.ok {
		return "timeout"
	}
	return fmt.Sprintf("%d ms", int(h.val))
}

// speedText renders the cached speed-test result.
func (g *guiState) speedText(nodeID string) string {
	g.healthMu.Lock()
	defer g.healthMu.Unlock()
	h, ok := g.speeds[nodeID]
	if !ok {
		return "-"
	}
	if !h.ok {
		return "failed"
	}
	return fmt.Sprintf("%.0f Mbps", h.val)
}

// trafficText renders the cumulative per-node traffic (tracker wired
// in the traffic commit; "-" until then).
func (g *guiState) trafficText(nodeID string) string {
	if g.tracker == nil {
		return "-"
	}
	return humanBytes(g.tracker.Total(nodeID))
}
