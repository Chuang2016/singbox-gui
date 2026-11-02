package server

import "github.com/Chuang2016/singbox-gui/core"

// nodeHealth is a cached per-node measurement.
type nodeHealth struct {
	Value float64 `json:"value"` // delay: ms; speed: Mbps
	At    string  `json:"at"`
	OK    bool    `json:"ok"`
}

// Delay/speed measurement lives in core (core/clash.go) so the native
// GUI shares it; these wrappers keep the server's call sites unchanged.

func (s *Server) clashDelay(tag string) int {
	return core.ClashDelay(s.store.Data.Cfg.ClashPort, tag)
}

func (s *Server) checkAllDelays() map[string]int {
	return core.CheckAllDelays(s.store.Data.Cfg.ClashPort, s.store.Data.Nodes)
}

func (s *Server) speedTest(nodeID string) (float64, error) {
	return core.ClashSpeedTest(s.store, s.mgr, nodeID)
}

// TrafficTracker exposes the shared per-node traffic tracker so native
// front-ends running in the same process (e.g. the Walk shell) read the
// exact totals and rates the dashboard serves -- no second poller and
// no forked accounting.
func (s *Server) TrafficTracker() *core.TrafficTracker { return s.traffic }
