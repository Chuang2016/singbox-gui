package main

import (
	"fmt"
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

// Nodes page: multi-line import box, the node table (Name / Group /
// Type / Server / Latency / Speed / Traffic) and a collapsible log
// pane. Clicking a row selects the node (and live-switches the
// running proxy); clicking a sortable header (Name / Latency /
// Speed / Traffic) sorts, the Walk front-end's convention: latency
// ascending, speed/traffic descending by default, unmeasured last.

var nodeColumns = []struct {
	title string
	width float32
}{
	{"Name", 250},
	{"Group", 110},
	{"Type", 90},
	{"Server", 170},
	{"Latency", 90},
	{"Speed", 100},
	{"Traffic", 110},
}

// sortKeyForCol maps a table column to its sort key ("" = not
// sortable).
func sortKeyForCol(col int) string {
	switch col {
	case 0:
		return "name"
	case 4:
		return "latency"
	case 5:
		return "speed"
	case 6:
		return "traffic"
	}
	return ""
}

// displayOrder returns store indices in the order the table shows
// them, applying the current sort; missing measurements sort last,
// like the Walk front-end and the dashboard.
func (g *guiState) displayOrder() []int {
	nodes := g.store.Data.Nodes
	order := make([]int, len(nodes))
	for i := range nodes {
		order[i] = i
	}
	if g.sortKey == "" {
		return order
	}
	key := func(i int) (float64, string) {
		n := nodes[i]
		switch g.sortKey {
		case "name":
			return 0, n.Tag
		case "latency":
			g.healthMu.Lock()
			h, ok := g.delays[n.ID]
			g.healthMu.Unlock()
			if !ok || !h.ok {
				return 1e18, ""
			}
			return h.val, ""
		case "speed":
			g.healthMu.Lock()
			h, ok := g.speeds[n.ID]
			g.healthMu.Unlock()
			if !ok || !h.ok {
				return -1, ""
			}
			return h.val, ""
		case "traffic":
			if g.tracker != nil {
				return float64(g.tracker.Total(n.ID)), ""
			}
		}
		return 0, ""
	}
	sort.SliceStable(order, func(x, y int) bool {
		fx, sx := key(order[x])
		fy, sy := key(order[y])
		if g.sortKey == "name" {
			if g.sortDesc {
				return sx > sy
			}
			return sx < sy
		}
		if g.sortDesc {
			return fx > fy
		}
		return fx < fy
	})
	return order
}

// onHeaderClicked maps a header click to a sort: same key toggles
// direction; latency defaults ascending, speed/traffic descending.
func (g *guiState) onHeaderClicked(col int) {
	k := sortKeyForCol(col)
	if k == "" {
		return
	}
	if g.sortKey == k {
		g.sortDesc = !g.sortDesc
	} else {
		g.sortKey = k
		g.sortDesc = k == "speed" || k == "traffic"
	}
	if g.nodeTable != nil {
		g.nodeTable.Refresh()
	}
}

// groupText renders a node's source: "Manual" or its subscription's
// name.
func (g *guiState) groupText(n core.Node) string {
	if strings.HasPrefix(n.Source, "sub:") {
		id := strings.TrimPrefix(n.Source, "sub:")
		for _, s := range g.store.Data.Subs {
			if s.ID == id {
				return s.Name
			}
		}
		return "Subscription"
	}
	return "Manual"
}

// serverText renders the outbound's server:port. The port may be a
// float64 (store loaded from JSON) or an int (freshly parsed link),
// depending on where the outbound map came from.
func serverText(n core.Node) string {
	srv, _ := n.Outbound["server"].(string)
	if srv == "" {
		return "-"
	}
	switch p := n.Outbound["server_port"].(type) {
	case float64:
		return srv + ":" + strconv.Itoa(int(p))
	case int:
		return srv + ":" + strconv.Itoa(p)
	case int64:
		return srv + ":" + strconv.FormatInt(p, 10)
	}
	return srv
}

func (g *guiState) buildNodesPage() fyne.CanvasObject {
	// --- import row (multi-line: share links and whole
	// subscription dumps, parsed line by line like the dashboard) ---
	importEntry := widget.NewMultiLineEntry()
	importEntry.SetPlaceHolder("Paste one or more share links (vless / trojan / ss / vmess / hysteria2 / tuic / anytls / privproto / naive)...")
	importEntry.SetMinRowsVisible(2)
	importBtn := widget.NewButton("Import", func() { g.importLinks(importEntry) })
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
				title := nodeColumns[id.Col].title
				if k := sortKeyForCol(id.Col); k != "" && k == g.sortKey {
					if g.sortDesc {
						title += " ▼"
					} else {
						title += " ▲"
					}
				}
				lbl.SetText(title)
				return
			}
			order := g.displayOrder()
			if id.Row-1 >= len(order) {
				lbl.SetText("")
				return
			}
			n := g.store.Data.Nodes[order[id.Row-1]]
			switch id.Col {
			case 0:
				name := n.Tag
				if n.ID == g.store.Data.Cfg.ActiveNodeID {
					name = "▶ " + name
					lbl.TextStyle = fyne.TextStyle{Bold: true}
				}
				lbl.SetText(name)
			case 1:
				lbl.SetText(g.groupText(n))
			case 2:
				lbl.SetText(n.Protocol)
			case 3:
				lbl.SetText(serverText(n))
			case 4:
				lbl.SetText(g.latencyText(n.ID))
			case 5:
				lbl.SetText(g.speedText(n.ID))
			case 6:
				lbl.SetText(g.trafficText(n.ID))
			}
		},
	)
	for i, c := range nodeColumns {
		g.nodeTable.SetColumnWidth(i, c.width)
	}
	g.nodeTable.OnSelected = func(id widget.TableCellID) {
		if id.Row == 0 {
			g.onHeaderClicked(id.Col)
			return
		}
		order := g.displayOrder()
		if id.Row-1 < len(order) {
			n := g.store.Data.Nodes[order[id.Row-1]]
			g.selNodeID = n.ID
			g.selectNode(n.ID)
		}
	}

	// --- action toolbar (delay / speed via the Clash API, edit,
	// delete, traffic reset) plus the live rate readout ---
	testOneBtn := widget.NewButton("Test latency", func() { g.testLatencySelected() })
	testAllBtn := widget.NewButton("Test all", func() { g.testLatencyAll() })
	speedBtn := widget.NewButton("Speed test", func() { g.testSpeedSelected() })
	editBtn := widget.NewButton("Edit node...", func() { g.editSelectedNode() })
	deleteBtn := widget.NewButton("Delete node", func() { g.deleteSelectedNode() })
	resetTrafficBtn := widget.NewButton("Reset traffic", func() {
		dialog.ShowConfirm("Reset traffic",
			"Reset cumulative traffic counters for all nodes?",
			func(ok bool) {
				if !ok {
					return
				}
				if g.tracker != nil {
					g.tracker.Reset()
					g.logf("Per-node traffic counters reset")
					g.nodeTable.Refresh()
				}
			}, g.win)
	})
	g.nodesRate = widget.NewLabel("")
	testRow := container.NewBorder(nil, nil, nil,
		container.NewHBox(g.nodesRate),
		container.NewHBox(testOneBtn, testAllBtn, speedBtn, editBtn, deleteBtn, resetTrafficBtn))

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

// importLinks parses the import box line by line, like the Walk
// front-end and the web back-end's /api/nodes POST.
func (g *guiState) importLinks(entry *widget.Entry) {
	text := strings.TrimSpace(entry.Text)
	if text == "" {
		return
	}
	added, failed := 0, 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		proto, ob, err := core.ParseShareLink(line)
		if err != nil {
			failed++
			continue
		}
		g.store.Data.Nodes = append(g.store.Data.Nodes, core.Node{
			ID: core.NewID("n"), Tag: core.LinkTag(line, proto),
			Protocol: proto, Outbound: ob, Source: "manual",
		})
		added++
	}
	if added > 0 {
		_ = g.store.Save()
		entry.SetText("")
		g.logf("Imported %d node(s)", added)
		g.notice = ""
		if g.nodeTable != nil {
			g.nodeTable.Refresh()
		}
		g.refreshStatus()
	}
	if failed > 0 {
		g.fail("Import: %d line(s) could not be parsed", failed)
	}
	if added == 0 && failed == 0 {
		g.fail("Import: nothing to import")
	}
}

// targetNode resolves the node the toolbar actions aim at: the last
// row clicked, falling back to the active node.
func (g *guiState) targetNode() *core.Node {
	if g.selNodeID != "" {
		for i := range g.store.Data.Nodes {
			if g.store.Data.Nodes[i].ID == g.selNodeID {
				return &g.store.Data.Nodes[i]
			}
		}
	}
	return g.activeNode()
}

// --- delay / speed measurement (shared core Clash API helpers; both
// need a running sing-box, exactly like the web dashboard) ---

func (g *guiState) testLatencySelected() {
	n := g.targetNode()
	if n == nil {
		g.fail("Latency test: select a node first")
		return
	}
	if !g.mgr.Running() {
		g.fail("Latency test: sing-box is not running")
		return
	}
	node := *n
	go func() {
		d := core.ClashDelay(g.store.Data.Cfg.ClashPort, "node:"+node.ID)
		fyne.Do(func() {
			g.healthMu.Lock()
			g.delays[node.ID] = healthVal{val: float64(d), ok: d >= 0, at: time.Now().Format("15:04:05")}
			g.healthMu.Unlock()
			if d >= 0 {
				g.logf("Latency %s: %d ms", node.Tag, d)
			} else {
				g.logf("Latency %s: timeout", node.Tag)
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
	n := g.targetNode()
	if n == nil {
		g.fail("Speed test: select a node first")
		return
	}
	if !g.mgr.Running() {
		g.fail("Speed test: sing-box is not running")
		return
	}
	node := *n
	dialog.ShowConfirm("Speed test",
		fmt.Sprintf("The speed test briefly switches the proxy to %q, downloads a test file, then switches back. Continue?", node.Tag),
		func(ok bool) {
			if !ok {
				return
			}
			g.logf("Speed test running (%s)...", node.Tag)
			go func() {
				mbps, err := core.ClashSpeedTest(g.store, g.mgr, node.ID)
				fyne.Do(func() {
					g.healthMu.Lock()
					g.speeds[node.ID] = healthVal{val: mbps, ok: err == nil, at: time.Now().Format("15:04:05")}
					g.healthMu.Unlock()
					if err != nil {
						g.fail("Speed test failed: %v", err)
					} else {
						g.logf("Speed %s: %.1f Mbps", node.Tag, mbps)
						g.notice = ""
						g.refreshStatus()
					}
					g.nodeTable.Refresh()
				})
			}()
		}, g.win)
}

// deleteSelectedNode removes the targeted node after a confirmation
// (the dashboard's Del button, via core.DeleteNode). If it was the
// active node, the selection falls back to the first remaining node.
func (g *guiState) deleteSelectedNode() {
	n := g.targetNode()
	if n == nil {
		g.fail("Delete node: select a node first")
		return
	}
	node := *n
	dialog.ShowConfirm("Delete node",
		fmt.Sprintf("Delete %q?", node.Tag),
		func(ok bool) {
			if !ok {
				return
			}
			wasActive := g.store.Data.Cfg.ActiveNodeID == node.ID
			if !core.DeleteNode(g.store, node.ID) {
				g.fail("Delete node: node not found")
				return
			}
			g.healthMu.Lock()
			delete(g.delays, node.ID)
			delete(g.speeds, node.ID)
			g.healthMu.Unlock()
			if g.selNodeID == node.ID {
				g.selNodeID = ""
			}
			if wasActive {
				g.store.Data.Cfg.ActiveNodeID = ""
				if len(g.store.Data.Nodes) > 0 {
					g.store.Data.Cfg.ActiveNodeID = g.store.Data.Nodes[0].ID
				}
				_ = g.store.Save()
			}
			if g.nodeTable != nil {
				g.nodeTable.Refresh()
			}
			g.logf("Deleted node %s", node.Tag)
			if g.mgr.Running() {
				g.logf("Node list changed while running: restart to apply")
			}
			g.notice = ""
			g.refreshStatus()
		}, g.win)
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
	return fmt.Sprintf("%.1f Mbps", h.val)
}

// trafficText renders the cumulative per-node traffic.
func (g *guiState) trafficText(nodeID string) string {
	if g.tracker == nil {
		return "-"
	}
	return humanBytes(g.tracker.Total(nodeID))
}
