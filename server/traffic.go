package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Per-node traffic accounting.
//
// sing-box's Clash API only exposes live per-connection byte counters, so we
// poll /connections every 250ms and fold per-connection counters into per-node
// totals. Totals persist in workDir/traffic.json (node ID -> bytes up+down),
// independent of store.json so the poller never contends on the store lock.

type connSeen struct{ up, down int64 }

type trafficTracker struct {
	mu     sync.Mutex
	path   string
	totals map[string]int64
	last   map[string]connSeen
	dirty  bool
}

func newTrafficTracker(workDir string) *trafficTracker {
	t := &trafficTracker{
		path:   filepath.Join(workDir, "traffic.json"),
		totals: map[string]int64{},
		last:   map[string]connSeen{},
	}
	if raw, err := os.ReadFile(t.path); err == nil {
		_ = json.Unmarshal(raw, &t.totals)
		if t.totals == nil {
			t.totals = map[string]int64{}
		}
	}
	return t
}

func (t *trafficTracker) total(nodeID string) int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.totals[nodeID]
}

func (t *trafficTracker) reset() {
	t.mu.Lock()
	t.totals = map[string]int64{}
	t.mu.Unlock()
	t.save()
}

func (t *trafficTracker) save() {
	t.mu.Lock()
	raw, err := json.Marshal(t.totals)
	t.dirty = false
	t.mu.Unlock()
	if err != nil {
		return
	}
	tmp := t.path + ".tmp"
	if os.WriteFile(tmp, raw, 0600) == nil {
		_ = os.Rename(tmp, t.path)
	}
}

// pollOnce fetches live connections and folds byte counters into node totals.
// A connection contributes its full counters the first time it is seen, then
// only the growth on later polls. Connections that open and close entirely
// between two polls (250ms apart) can be missed — totals are a close estimate,
// not an audit log.
func (t *trafficTracker) pollOnce(clashPort int) {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/connections", clashPort))
	if err != nil {
		return
	}
	defer resp.Body.Close()
	var body struct {
		Connections []struct {
			ID       string   `json:"id"`
			Upload   int64    `json:"upload"`
			Download int64    `json:"download"`
			Chains   []string `json:"chains"`
		} `json:"connections"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	seen := make(map[string]connSeen, len(body.Connections))
	for _, c := range body.Connections {
		seen[c.ID] = connSeen{c.Upload, c.Download}
		var delta int64
		if prev, ok := t.last[c.ID]; ok {
			delta = (c.Upload - prev.up) + (c.Download - prev.down)
		} else {
			delta = c.Upload + c.Download // first sighting: count what already flowed
		}
		if delta <= 0 {
			continue
		}
		for _, ch := range c.Chains {
			if strings.HasPrefix(ch, "node:") {
				t.totals[strings.TrimPrefix(ch, "node:")] += delta
				t.dirty = true
				break
			}
		}
	}
	t.last = seen
}

// loop polls while sing-box runs and persists totals about every 30s.
func (t *trafficTracker) loop(s *Server) {
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	rounds := 0
	for range tick.C {
		if s.mgr.Running() {
			t.pollOnce(s.store.Data.Cfg.ClashPort)
		} else {
			t.mu.Lock()
			if len(t.last) > 0 {
				t.last = map[string]connSeen{}
			}
			t.mu.Unlock()
		}
		rounds++
		if rounds >= 120 {
			rounds = 0
			t.mu.Lock()
			d := t.dirty
			t.mu.Unlock()
			if d {
				t.save()
			}
		}
	}
}
