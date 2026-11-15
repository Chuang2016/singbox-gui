//go:build windows

package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/lxn/walk"

	"github.com/Chuang2016/singbox-gui/core"
)

// nodeModel backs the TableView with the shared store. Columns:
// 0 active marker, 1 name, 2 protocol, 3 latency, 4 speed, 5 traffic.
// Rows follow the display order (header-click sorting), not the
// store order; every row-based handler must map through it.
type nodeModel struct {
	walk.TableModelBase
	a *app
}

func (m *nodeModel) RowCount() int {
	return len(m.a.store.Data.Nodes)
}

func (m *nodeModel) Value(row, col int) interface{} {
	order := m.a.displayOrder()
	if row < 0 || row >= len(order) {
		return ""
	}
	n := m.a.store.Data.Nodes[order[row]]
	switch col {
	case 0:
		if n.ID == m.a.store.Data.Cfg.ActiveNodeID {
			return "▶"
		}
		return ""
	case 1:
		return n.Tag
	case 2:
		return n.Protocol
	case 3:
		return m.a.latencyText(n.ID)
	case 4:
		return m.a.speedText(n.ID)
	case 5:
		return m.a.trafficText(n.ID)
	}
	return ""
}

// displayOrder returns store indices in the order the table shows
// them, applying the current sort (latency/speed/traffic/name from
// header clicks; missing measurements sort last, like the dashboard).
func (a *app) displayOrder() []int {
	nodes := a.store.Data.Nodes
	order := make([]int, len(nodes))
	for i := range nodes {
		order[i] = i
	}
	if a.sortKey == "" {
		return order
	}
	key := func(i int) (float64, string) {
		n := nodes[i]
		switch a.sortKey {
		case "name":
			return 0, n.Tag
		case "latency":
			a.delayMu.Lock()
			d, ok := a.delays[n.ID]
			a.delayMu.Unlock()
			if !ok || d < 0 {
				return 1e18, ""
			}
			return float64(d), ""
		case "speed":
			a.delayMu.Lock()
			s, ok := a.speeds[n.ID]
			okk := a.speedOK[n.ID]
			a.delayMu.Unlock()
			if !ok || !okk {
				return -1, ""
			}
			return s, ""
		case "traffic":
			if tr := a.srv.TrafficTracker(); tr != nil {
				return float64(tr.Total(n.ID)), ""
			}
		}
		return 0, ""
	}
	sort.SliceStable(order, func(x, y int) bool {
		fx, sx := key(order[x])
		fy, sy := key(order[y])
		if a.sortKey == "name" {
			if a.sortDesc {
				return sx > sy
			}
			return sx < sy
		}
		if a.sortDesc {
			return fx > fy
		}
		return fx < fy
	})
	return order
}

// onColumnClicked maps a header click to a sort: same key toggles
// direction; latency defaults ascending, speed/traffic descending
// (the dashboard's convention).
func (a *app) onColumnClicked(col int) {
	keys := map[int]string{1: "name", 3: "latency", 4: "speed", 5: "traffic"}
	k, ok := keys[col]
	if !ok {
		return
	}
	if a.sortKey == k {
		a.sortDesc = !a.sortDesc
	} else {
		a.sortKey = k
		a.sortDesc = k == "speed" || k == "traffic"
	}
	a.model.PublishRowsReset()
}

// selectedNode maps the table's current display row to its node.
func (a *app) selectedNode() *core.Node {
	if a.table == nil {
		return nil
	}
	row := a.table.CurrentIndex()
	order := a.displayOrder()
	if row < 0 || row >= len(order) {
		return nil
	}
	return &a.store.Data.Nodes[order[row]]
}

// speedText renders the cached speed-test result ("12.3 Mbps",
// "fail", or "-" when never measured).
func (a *app) speedText(nodeID string) string {
	a.delayMu.Lock()
	defer a.delayMu.Unlock()
	s, ok := a.speeds[nodeID]
	if !ok {
		return "-"
	}
	if !a.speedOK[nodeID] {
		return "fail"
	}
	return fmt.Sprintf("%.1f Mbps", s)
}

// latencyText renders the cached delay ("86 ms", "timeout", or "-"
// when never measured; measurements come from the latency commit's
// tests, driven by core.ClashDelay like the dashboard).
func (a *app) latencyText(nodeID string) string {
	a.delayMu.Lock()
	defer a.delayMu.Unlock()
	d, ok := a.delays[nodeID]
	if !ok {
		return "-"
	}
	if d < 0 {
		return "timeout"
	}
	return fmt.Sprintf("%d ms", d)
}

// trafficText renders the cumulative per-node traffic from the
// process-wide shared tracker (the same one the web back-end polls).
func (a *app) trafficText(nodeID string) string {
	tr := a.srv.TrafficTracker()
	if tr == nil {
		return "-"
	}
	return humanBytes(tr.Total(nodeID))
}

// startProxy regenerates the config, then starts the subprocess
// (mirrors server.Server.StartProxy via the shared core.Regen).
func (a *app) startProxy() error {
	if err := core.Regen(a.store.Data, a.workDir); err != nil {
		return err
	}
	return a.mgr.Start()
}

// toggle is the Connect/Disconnect button.
func (a *app) toggle() {
	starting := !a.mgr.Running()
	var err error
	if !starting {
		err = a.mgr.Stop()
	} else {
		if len(a.store.Data.Nodes) == 0 {
			a.fail("Nothing to connect: import a share link or add a subscription first")
			return
		}
		if n := a.activeNode(); n != nil {
			a.store.Data.Cfg.ActiveNodeID = n.ID
			_ = a.store.Save()
		}
		err = a.startProxy()
	}
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
		a.logf("Disconnected")
	} else {
		a.logf("Connected (%s)", a.activeNode().Tag)
	}
	a.notice = ""
	a.refreshStatus()
	a.model.PublishRowsReset()
}

// onRowSelected is the table's selection-changed handler.
func (a *app) onRowSelected() {
	if a.suppressSelect || a.table == nil {
		return
	}
	if n := a.selectedNode(); n != nil {
		a.selectNode(n.ID)
	}
}

// selectNode marks a node active (click in the node table).
//
// While stopped it only changes the selection. While running it
// switches immediately, the way the web dashboard does: set active,
// regenerate the config, restart sing-box. The system proxy stays on
// across the switch (the mixed port does not change). If the restart
// fails, the selection is rolled back to the previous node and the
// failure is parked in the sticky notice -- the UI must never claim
// to be connected to a node that is not actually running. (Model
// carried over from the frozen Fyne front-end, commit 7e86bd5.)
func (a *app) selectNode(id string) {
	prevID := a.store.Data.Cfg.ActiveNodeID
	if id == prevID {
		return // clicking the already-active node changes nothing
	}
	a.store.Data.Cfg.ActiveNodeID = id
	_ = a.store.Save()
	if !a.mgr.Running() {
		a.refreshStatus()
		a.model.PublishRowsReset()
		return
	}
	tag := id
	if n := a.activeNode(); n != nil {
		tag = n.Tag
	}
	a.logf("Switching to %s ...", tag)
	_ = a.mgr.Stop()
	if err := a.startProxy(); err != nil {
		// Roll the selection back: sing-box is stopped and still
		// configured for the previous node on the next manual start.
		a.store.Data.Cfg.ActiveNodeID = prevID
		_ = a.store.Save()
		a.fail("Switch to %s failed (rolled back): %v", tag, err)
		a.model.PublishRowsReset()
		a.syncTableSelection()
		return
	}
	a.logf("Switched to %s", tag)
	a.notice = ""
	a.refreshStatus()
	a.model.PublishRowsReset()
}

// syncTableSelection highlights the row of the active node without
// triggering the switch logic.
func (a *app) syncTableSelection() {
	if a.table == nil {
		return
	}
	row := -1
	for disp, idx := range a.displayOrder() {
		if a.store.Data.Nodes[idx].ID == a.store.Data.Cfg.ActiveNodeID {
			row = disp
			break
		}
	}
	if row < 0 && len(a.store.Data.Nodes) > 0 {
		row = 0
	}
	a.suppressSelect = true
	_ = a.table.SetCurrentIndex(row)
	a.suppressSelect = false
}

// importLinks parses the import box line by line (share links and
// whole subscription dumps), like the web back-end's /api/nodes POST.
func (a *app) importLinks() {
	text := strings.TrimSpace(a.importEd.Text())
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
		node := core.Node{
			ID: core.NewID("n"), Tag: core.LinkTag(line, proto),
			Protocol: proto, Outbound: ob, Source: "manual",
		}
		a.store.Data.Nodes = append(a.store.Data.Nodes, node)
		added++
	}
	if added > 0 {
		_ = a.store.Save()
		a.importEd.SetText("")
		a.lastNodeCount = len(a.store.Data.Nodes)
		a.model.PublishRowsReset()
		a.syncTableSelection()
		a.logf("Imported %d node(s)", added)
		a.notice = ""
		a.refreshStatus()
	}
	if failed > 0 {
		a.fail("Import: %d line(s) could not be parsed", failed)
	}
	if added == 0 && failed == 0 {
		a.fail("Import: nothing to import")
	}
}

// humanBytes / humanRate format sizes the way the dashboard does.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func humanRate(bps float64) string {
	if bps < 1024 {
		return fmt.Sprintf("%d B/s", int64(bps))
	}
	return humanBytes(int64(bps)) + "/s"
}

// ---------- latency tests (core.ClashDelay / core.CheckAllDelays) ----------

// testLatencySelected measures the highlighted node's delay through
// the Clash API (sing-box must be running).
func (a *app) testLatencySelected() {
	if !a.mgr.Running() {
		a.fail("Latency test: sing-box is not running")
		return
	}
	n := a.selectedNode()
	if n == nil {
		a.fail("Latency test: select a node first")
		return
	}
	node := *n
	go func() {
		d := core.ClashDelay(a.store.Data.Cfg.ClashPort, "node:"+node.ID)
		a.mw.Synchronize(func() {
			a.delayMu.Lock()
			a.delays[node.ID] = d
			a.delayMu.Unlock()
			if d >= 0 {
				a.logf("Latency %s: %d ms", node.Tag, d)
			} else {
				a.logf("Latency %s: timeout", node.Tag)
			}
			a.model.PublishRowsReset()
			a.refreshLogs()
		})
	}()
}

// testLatencyAll measures every node's delay (the dashboard's
// check-all, shared from core).
func (a *app) testLatencyAll() {
	if !a.mgr.Running() {
		a.fail("Latency test: sing-box is not running")
		return
	}
	a.logf("Testing latency of %d nodes...", len(a.store.Data.Nodes))
	a.refreshLogs()
	go func() {
		results := core.CheckAllDelays(a.store.Data.Cfg.ClashPort, a.store.Data.Nodes)
		a.mw.Synchronize(func() {
			a.delayMu.Lock()
			for id, d := range results {
				a.delays[id] = d
			}
			a.delayMu.Unlock()
			a.logf("Latency test done")
			a.model.PublishRowsReset()
			a.refreshLogs()
		})
	}()
}

// ---------- speed test / delete / traffic reset ----------

// testSpeedSelected measures the highlighted node's download speed
// through core.ClashSpeedTest (it briefly switches the selector to
// the node, then restores it -- same as the dashboard, with the same
// confirmation).
func (a *app) testSpeedSelected() {
	if !a.mgr.Running() {
		a.fail("Speed test: sing-box is not running")
		return
	}
	n := a.selectedNode()
	if n == nil {
		a.fail("Speed test: select a node first")
		return
	}
	node := *n
	if walk.MsgBox(a.mw, "Speed test",
		"The speed test briefly switches the proxy to \""+node.Tag+"\", downloads a test file, then switches back. Continue?",
		walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) != walk.DlgCmdYes {
		return
	}
	a.logf("Speed test running (%s)...", node.Tag)
	a.refreshLogs()
	go func() {
		mbps, err := core.ClashSpeedTest(a.store, a.mgr, node.ID)
		a.mw.Synchronize(func() {
			a.delayMu.Lock()
			a.speeds[node.ID] = mbps
			a.speedOK[node.ID] = err == nil
			a.delayMu.Unlock()
			if err != nil {
				a.fail("Speed test failed: %v", err)
			} else {
				a.logf("Speed %s: %.1f Mbps", node.Tag, mbps)
				a.notice = ""
				a.refreshStatus()
			}
			a.model.PublishRowsReset()
			a.refreshLogs()
		})
	}()
}

// deleteSelectedNode removes the highlighted node after a
// confirmation (the dashboard's Del button). If it was the active
// node, the selection falls back to the first remaining node.
func (a *app) deleteSelectedNode() {
	n := a.selectedNode()
	if n == nil {
		a.fail("Delete node: select a node first")
		return
	}
	node := *n
	if walk.MsgBox(a.mw, "Delete node",
		"Delete \""+node.Tag+"\"?",
		walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) != walk.DlgCmdYes {
		return
	}
	wasActive := a.store.Data.Cfg.ActiveNodeID == node.ID
	if !core.DeleteNode(a.store, node.ID) {
		a.fail("Delete node: node not found")
		return
	}
	a.delayMu.Lock()
	delete(a.delays, node.ID)
	delete(a.speeds, node.ID)
	delete(a.speedOK, node.ID)
	a.delayMu.Unlock()
	if wasActive {
		a.store.Data.Cfg.ActiveNodeID = ""
		if len(a.store.Data.Nodes) > 0 {
			a.store.Data.Cfg.ActiveNodeID = a.store.Data.Nodes[0].ID
		}
		_ = a.store.Save()
	}
	a.lastNodeCount = len(a.store.Data.Nodes)
	a.model.PublishRowsReset()
	a.syncTableSelection()
	a.logf("Deleted node %s", node.Tag)
	if a.mgr.Running() {
		a.logf("Node list changed while running: restart to apply")
	}
	a.notice = ""
	a.refreshStatus()
	a.refreshLogs()
}

// resetTraffic zeroes the cumulative per-node counters (the
// dashboard's Reset traffic button).
func (a *app) resetTraffic() {
	if walk.MsgBox(a.mw, "Reset traffic",
		"Reset cumulative traffic counters for all nodes?",
		walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) != walk.DlgCmdYes {
		return
	}
	if tr := a.srv.TrafficTracker(); tr != nil {
		tr.Reset()
	}
	a.lastTrafficSum = 0
	a.model.PublishRowsReset()
	a.logf("Traffic counters reset")
	a.refreshLogs()
}
