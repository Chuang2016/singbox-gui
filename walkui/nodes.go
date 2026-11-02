//go:build windows

package main

import (
	"fmt"
	"strings"

	"github.com/lxn/walk"

	"github.com/Chuang2016/singbox-gui/core"
)

// nodeModel backs the TableView with the shared store. Columns:
// 0 active marker, 1 name, 2 protocol, 3 latency, 4 traffic.
type nodeModel struct {
	walk.TableModelBase
	a *app
}

func (m *nodeModel) RowCount() int {
	return len(m.a.store.Data.Nodes)
}

func (m *nodeModel) Value(row, col int) interface{} {
	nodes := m.a.store.Data.Nodes
	if row < 0 || row >= len(nodes) {
		return ""
	}
	n := nodes[row]
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
		return m.a.trafficText(n.ID)
	}
	return ""
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
	row := a.table.CurrentIndex()
	nodes := a.store.Data.Nodes
	if row < 0 || row >= len(nodes) {
		return
	}
	a.selectNode(nodes[row].ID)
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
	for i, n := range a.store.Data.Nodes {
		if n.ID == a.store.Data.Cfg.ActiveNodeID {
			row = i
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
	row := a.table.CurrentIndex()
	nodes := a.store.Data.Nodes
	if row < 0 || row >= len(nodes) {
		a.fail("Latency test: select a node first")
		return
	}
	node := nodes[row]
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
