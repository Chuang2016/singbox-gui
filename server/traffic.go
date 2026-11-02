package server

import "github.com/Chuang2016/singbox-gui/core"

// Per-node traffic accounting lives in core (core.TrafficTracker) so
// the native GUI shares the exact same poller and totals file; this
// file only keeps the server's historical names.
type trafficTracker = core.TrafficTracker

func newTrafficTracker(workDir string) *trafficTracker {
	return core.NewTrafficTracker(workDir)
}
