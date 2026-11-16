package main

import (
	"fmt"

	"github.com/Chuang2016/singbox-gui/core"
)

// Engine wiring for the GUI process: per-node traffic tracking and the
// live rate readout in the status bar. The tracker lives in core
// (core.TrafficTracker, shared with the web dashboard); this file owns
// the GUI-side lifecycle.

func (g *guiState) initEngine() {
	g.tracker = core.NewTrafficTracker(g.workDir)
	go g.tracker.Loop(g.mgr.Running, func() int { return g.store.Data.Cfg.ClashPort })
}

func (g *guiState) stopEngine() {
	if g.tracker != nil {
		g.tracker.Save()
	}
}

// refreshRates updates the bottom-right rate readout (once a second)
// from the shared traffic tracker; the same text is mirrored next to
// the Nodes page actions, like the Walk front-end does.
func (g *guiState) refreshRates() {
	txt := ""
	if g.tracker != nil && g.mgr.Running() {
		up, down := g.tracker.Rates()
		txt = fmt.Sprintf("Down %s  |  Up %s", humanRate(down), humanRate(up))
	}
	if g.rateTx != nil {
		g.rateTx.SetText(txt)
	}
	if g.nodesRate != nil {
		g.nodesRate.SetText(txt)
	}
}

// humanBytes formats a byte count the way the dashboard does
// ("1.2 MB", values stay in the unit they cross into).
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

// humanRate formats bytes/second for the status bar.
func humanRate(bps float64) string {
	if bps < 1024 {
		return fmt.Sprintf("%d B/s", int64(bps))
	}
	return humanBytes(int64(bps)) + "/s"
}
