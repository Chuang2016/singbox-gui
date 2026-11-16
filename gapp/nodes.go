package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/Chuang2016/singbox-gui/core"
)

// Nodes page: multi-line import box, the node table (Name / Group /
// Type / Server / Latency / Speed / Traffic) and a collapsible log
// pane. Same semantics as gui/nodes.go: clicking a row selects the
// node (and live-switches the running proxy); clicking a sortable
// header (Name / Latency / Speed / Traffic) sorts — latency
// ascending, speed/traffic descending by default, unmeasured last.

var nodeColumns = []struct {
	title string
	width unit.Dp
}{
	{"Name", 230},
	{"Group", 100},
	{"Type", 80},
	{"Server", 170},
	{"Latency", 80},
	{"Speed", 95},
	{"Traffic", 100},
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

type nodesPage struct {
	importEd  widget.Editor
	importBtn widget.Clickable
	list      widget.List

	headerClicks []widget.Clickable
	rowClicks    []widget.Clickable

	testOneBtn widget.Clickable
	testAllBtn widget.Clickable
	speedBtn   widget.Clickable
	editBtn    widget.Clickable
	deleteBtn  widget.Clickable
	resetBtn   widget.Clickable
	logCk      widget.Bool
	logList    widget.List

	healthMu sync.Mutex
	delays   map[string]healthVal
	speeds   map[string]healthVal

	sortKey   string // "", "name", "latency", "speed", "traffic"
	sortDesc  bool
	selNodeID string // last node row clicked (edit/delete/test target)
}

func (p *nodesPage) init() {
	p.headerClicks = make([]widget.Clickable, len(nodeColumns))
	p.list.List.Axis = layout.Vertical
	p.logList.List.Axis = layout.Vertical
	p.delays = map[string]healthVal{}
	p.speeds = map[string]healthVal{}
}

// displayOrder returns store indices in display order, applying the
// current sort; missing measurements sort last (gui/ semantics).
func (p *nodesPage) displayOrder(a *App) []int {
	nodes := a.store.Data.Nodes
	order := make([]int, len(nodes))
	for i := range nodes {
		order[i] = i
	}
	if p.sortKey == "" {
		return order
	}
	key := func(i int) (float64, string) {
		n := nodes[i]
		switch p.sortKey {
		case "name":
			return 0, n.Tag
		case "latency":
			p.healthMu.Lock()
			h, ok := p.delays[n.ID]
			p.healthMu.Unlock()
			if !ok || !h.ok {
				return 1e18, ""
			}
			return h.val, ""
		case "speed":
			p.healthMu.Lock()
			h, ok := p.speeds[n.ID]
			p.healthMu.Unlock()
			if !ok || !h.ok {
				return -1, ""
			}
			return h.val, ""
		case "traffic":
			if a.tracker != nil {
				return float64(a.tracker.Total(n.ID)), ""
			}
		}
		return 0, ""
	}
	sort.SliceStable(order, func(x, y int) bool {
		fx, sx := key(order[x])
		fy, sy := key(order[y])
		if p.sortKey == "name" {
			if p.sortDesc {
				return sx > sy
			}
			return sx < sy
		}
		if p.sortDesc {
			return fx > fy
		}
		return fx < fy
	})
	return order
}

func (p *nodesPage) onHeaderClicked(col int) {
	k := sortKeyForCol(col)
	if k == "" {
		return
	}
	if p.sortKey == k {
		p.sortDesc = !p.sortDesc
	} else {
		p.sortKey = k
		p.sortDesc = k == "speed" || k == "traffic"
	}
}

func (p *nodesPage) layout(gtx layout.Context, a *App) layout.Dimensions {
	if p.importBtn.Clicked(gtx) {
		p.importLinks(a)
	}
	if p.testOneBtn.Clicked(gtx) {
		p.testLatencySelected(a)
	}
	if p.testAllBtn.Clicked(gtx) {
		p.testLatencyAll(a)
	}
	if p.speedBtn.Clicked(gtx) {
		p.testSpeedSelected(a)
	}
	if p.editBtn.Clicked(gtx) {
		a.editSelectedNode()
	}
	if p.deleteBtn.Clicked(gtx) {
		p.deleteSelectedNode(a)
	}
	if p.resetBtn.Clicked(gtx) {
		a.showConfirm("Reset traffic",
			"Reset cumulative traffic counters for all nodes?",
			func() {
				if a.tracker != nil {
					a.tracker.Reset()
					a.logf("Per-node traffic counters reset")
				}
			})
	}
	_ = p.logCk.Update(gtx)

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// import row
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Start}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							gtx.Constraints.Min.Y = gtx.Dp(unit.Dp(52))
							gtx.Constraints.Max.Y = gtx.Dp(unit.Dp(52))
							return widget.Border{Color: colDivider, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(gtx,
								material.Editor(a.th, &p.importEd, "Paste one or more share links (vless / trojan / ss / vmess / hysteria2 / tuic / anytls / privproto)...").Layout)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
						layout.Rigid(material.Button(a.th, &p.importBtn, "Import").Layout),
					)
				})
		}),
		// toolbar
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(material.Button(a.th, &p.testOneBtn, "Test latency").Layout),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(material.Button(a.th, &p.testAllBtn, "Test all").Layout),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(material.Button(a.th, &p.speedBtn, "Speed test").Layout),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(material.Button(a.th, &p.editBtn, "Edit node...").Layout),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(material.Button(a.th, &p.deleteBtn, "Delete node").Layout),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(material.Button(a.th, &p.resetBtn, "Reset traffic").Layout),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return layout.E.Layout(gtx, material.Label(a.th, a.th.TextSize, a.rateText).Layout)
						}),
					)
				})
		}),
		// table
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return p.layoutTable(gtx, a)
				})
		}),
		// log pane
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(10), Right: unit.Dp(10), Bottom: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					children := []layout.FlexChild{
						layout.Rigid(material.CheckBox(a.th, &p.logCk, "Log").Layout),
					}
					if p.logCk.Value {
						children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							gtx.Constraints.Max.Y = gtx.Dp(unit.Dp(120))
							return material.List(a.th, &p.logList).Layout(gtx, 1,
								func(gtx layout.Context, _ int) layout.Dimensions {
									return material.Label(a.th, unit.Sp(12), a.logText()).Layout(gtx)
								})
						}))
					}
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
				})
		}),
	)
}

func (p *nodesPage) layoutTable(gtx layout.Context, a *App) layout.Dimensions {
	order := p.displayOrder(a)
	for len(p.rowClicks) < len(order) {
		p.rowClicks = append(p.rowClicks, widget.Clickable{})
	}
	return material.List(a.th, &p.list).Layout(gtx, len(order)+1,
		func(gtx layout.Context, idx int) layout.Dimensions {
			if idx == 0 {
				return p.layoutHeader(gtx, a)
			}
			row := idx - 1
			n := a.store.Data.Nodes[order[row]]
			if p.rowClicks[row].Clicked(gtx) {
				p.selNodeID = n.ID
				a.selectNode(n.ID)
			}
			active := n.ID == a.store.Data.Cfg.ActiveNodeID
			selected := n.ID == p.selNodeID
			content := func(gtx layout.Context) layout.Dimensions {
				return material.Clickable(gtx, &p.rowClicks[row], func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							name := n.Tag
							if active {
								name = "▶ " + name
							}
							return cellLabel(a.th, nodeColumns[0].width, name, active)(gtx)
						}),
						layout.Rigid(cellLabel(a.th, nodeColumns[1].width, a.groupText(n), false)),
						layout.Rigid(cellLabel(a.th, nodeColumns[2].width, n.Protocol, false)),
						layout.Rigid(cellLabel(a.th, nodeColumns[3].width, serverText(n), false)),
						layout.Rigid(cellLabel(a.th, nodeColumns[4].width, p.latencyText(n.ID), false)),
						layout.Rigid(cellLabel(a.th, nodeColumns[5].width, p.speedText(n.ID), false)),
						layout.Rigid(cellLabel(a.th, nodeColumns[6].width, p.trafficText(a, n.ID), false)),
					)
				})
			}
			return layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					if selected || active {
						return layout.Stack{}.Layout(gtx,
							layout.Expanded(bgWidget(colRowSel)),
							layout.Stacked(content),
						)
					}
					return content(gtx)
				})
		})
}

func (p *nodesPage) layoutHeader(gtx layout.Context, a *App) layout.Dimensions {
	children := make([]layout.FlexChild, 0, len(nodeColumns))
	for col := range nodeColumns {
		col := col
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if p.headerClicks[col].Clicked(gtx) {
				p.onHeaderClicked(col)
			}
			title := nodeColumns[col].title
			if k := sortKeyForCol(col); k != "" && k == p.sortKey {
				if p.sortDesc {
					title += " ▼"
				} else {
					title += " ▲"
				}
			}
			return material.Clickable(gtx, &p.headerClicks[col],
				cellLabel(a.th, nodeColumns[col].width, title, true))
		}))
	}
	return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(4)}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
		})
}

// importLinks parses the import box line by line (gui/ semantics).
func (p *nodesPage) importLinks(a *App) {
	text := strings.TrimSpace(p.importEd.Text())
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
		a.store.Data.Nodes = append(a.store.Data.Nodes, core.Node{
			ID: core.NewID("n"), Tag: core.LinkTag(line, proto),
			Protocol: proto, Outbound: ob, Source: "manual",
		})
		added++
	}
	if added > 0 {
		_ = a.store.Save()
		p.importEd.SetText("")
		a.logf("Imported %d node(s)", added)
		a.notice = ""
	}
	if failed > 0 {
		a.fail("Import: %d line(s) could not be parsed", failed)
	}
	if added == 0 && failed == 0 {
		a.fail("Import: nothing to import")
	}
}

// targetNode resolves the node the toolbar actions aim at: the last
// row clicked, falling back to the active node.
func (p *nodesPage) targetNode(a *App) *core.Node {
	if p.selNodeID != "" {
		for i := range a.store.Data.Nodes {
			if a.store.Data.Nodes[i].ID == p.selNodeID {
				return &a.store.Data.Nodes[i]
			}
		}
	}
	return a.activeNode()
}

// --- delay / speed measurement (shared core Clash API helpers; both
// need a running sing-box, exactly like gui/) ---

func (p *nodesPage) testLatencySelected(a *App) {
	n := p.targetNode(a)
	if n == nil {
		a.fail("Latency test: select a node first")
		return
	}
	if !a.mgr.Running() {
		a.fail("Latency test: sing-box is not running")
		return
	}
	node := *n
	go func() {
		d := core.ClashDelay(a.store.Data.Cfg.ClashPort, "node:"+node.ID)
		p.healthMu.Lock()
		p.delays[node.ID] = healthVal{val: float64(d), ok: d >= 0, at: time.Now().Format("15:04:05")}
		p.healthMu.Unlock()
		a.post(func() {
			if d >= 0 {
				a.logf("Latency %s: %d ms", node.Tag, d)
			} else {
				a.logf("Latency %s: timeout", node.Tag)
			}
		})
	}()
}

func (p *nodesPage) testLatencyAll(a *App) {
	if !a.mgr.Running() {
		a.fail("Latency test: sing-box is not running")
		return
	}
	a.logf("Testing latency of %d nodes...", len(a.store.Data.Nodes))
	go func() {
		results := core.CheckAllDelays(a.store.Data.Cfg.ClashPort, a.store.Data.Nodes)
		p.healthMu.Lock()
		for id, d := range results {
			p.delays[id] = healthVal{val: float64(d), ok: d >= 0, at: time.Now().Format("15:04:05")}
		}
		p.healthMu.Unlock()
		a.post(func() { a.logf("Latency test done") })
	}()
}

func (p *nodesPage) testSpeedSelected(a *App) {
	n := p.targetNode(a)
	if n == nil {
		a.fail("Speed test: select a node first")
		return
	}
	if !a.mgr.Running() {
		a.fail("Speed test: sing-box is not running")
		return
	}
	node := *n
	a.showConfirm("Speed test",
		fmt.Sprintf("The speed test briefly switches the proxy to %q, downloads a test file, then switches back. Continue?", node.Tag),
		func() {
			a.logf("Speed test running (%s)...", node.Tag)
			go func() {
				mbps, err := core.ClashSpeedTest(a.store, a.mgr, node.ID)
				p.healthMu.Lock()
				p.speeds[node.ID] = healthVal{val: mbps, ok: err == nil, at: time.Now().Format("15:04:05")}
				p.healthMu.Unlock()
				a.post(func() {
					if err != nil {
						a.fail("Speed test failed: %v", err)
					} else {
						a.logf("Speed %s: %.1f Mbps", node.Tag, mbps)
						a.notice = ""
					}
				})
			}()
		})
}

// deleteSelectedNode removes the targeted node after a confirmation
// (via core.DeleteNode). If it was the active node, the selection
// falls back to the first remaining node.
func (p *nodesPage) deleteSelectedNode(a *App) {
	n := p.targetNode(a)
	if n == nil {
		a.fail("Delete node: select a node first")
		return
	}
	node := *n
	a.showConfirm("Delete node", fmt.Sprintf("Delete %q?", node.Tag), func() {
		wasActive := a.store.Data.Cfg.ActiveNodeID == node.ID
		if !core.DeleteNode(a.store, node.ID) {
			a.fail("Delete node: node not found")
			return
		}
		p.healthMu.Lock()
		delete(p.delays, node.ID)
		delete(p.speeds, node.ID)
		p.healthMu.Unlock()
		if p.selNodeID == node.ID {
			p.selNodeID = ""
		}
		if wasActive {
			a.store.Data.Cfg.ActiveNodeID = ""
			if len(a.store.Data.Nodes) > 0 {
				a.store.Data.Cfg.ActiveNodeID = a.store.Data.Nodes[0].ID
			}
			_ = a.store.Save()
		}
		a.logf("Deleted node %s", node.Tag)
		if a.mgr.Running() {
			a.logf("Node list changed while running: restart to apply")
		}
		a.notice = ""
	})
}

func (p *nodesPage) latencyText(nodeID string) string {
	p.healthMu.Lock()
	defer p.healthMu.Unlock()
	h, ok := p.delays[nodeID]
	if !ok {
		return "-"
	}
	if !h.ok {
		return "timeout"
	}
	return fmt.Sprintf("%d ms", int(h.val))
}

func (p *nodesPage) speedText(nodeID string) string {
	p.healthMu.Lock()
	defer p.healthMu.Unlock()
	h, ok := p.speeds[nodeID]
	if !ok {
		return "-"
	}
	if !h.ok {
		return "failed"
	}
	return fmt.Sprintf("%.1f Mbps", h.val)
}

func (p *nodesPage) trafficText(a *App, nodeID string) string {
	if a.tracker == nil {
		return "-"
	}
	return humanBytes(a.tracker.Total(nodeID))
}
