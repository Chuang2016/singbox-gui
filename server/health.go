package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// nodeHealth is a cached per-node measurement.
type nodeHealth struct {
	Value float64 `json:"value"` // delay: ms; speed: Mbps
	At    string  `json:"at"`
	OK    bool    `json:"ok"`
}

func (s *Server) clashBase() string {
	return fmt.Sprintf("http://127.0.0.1:%d", s.store.Data.Cfg.ClashPort)
}

// clashDelay asks sing-box's Clash API for the delay of one outbound.
// Returns delay in ms, or -1 when the node is unreachable.
func (s *Server) clashDelay(tag string) int {
	u := fmt.Sprintf("%s/proxies/%s/delay?timeout=5000&url=%s",
		s.clashBase(), url.PathEscape(tag), url.QueryEscape("https://www.gstatic.com/generate_204"))
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(u)
	if err != nil {
		return -1
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return -1
	}
	var out struct {
		Delay int `json:"delay"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return -1
	}
	return out.Delay
}

// checkAllDelays tests every node concurrently (max 10 in flight).
func (s *Server) checkAllDelays() map[string]int {
	type res struct {
		id    string
		delay int
	}
	nodes := s.store.Data.Nodes
	out := make(map[string]int, len(nodes))
	sem := make(chan struct{}, 10)
	var wg sync.WaitGroup
	ch := make(chan res, len(nodes))
	for _, n := range nodes {
		wg.Add(1)
		go func(id, tag string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ch <- res{id, s.clashDelay("node:" + id)}
		}(n.ID, n.Tag)
	}
	wg.Wait()
	close(ch)
	for r := range ch {
		out[r.id] = r.delay
	}
	return out
}

// speedTest measures download throughput (Mbps) through one node:
// switch the proxy selector to it, download a test file via the local
// mixed inbound, then restore the previous selection.
func (s *Server) speedTest(nodeID string) (float64, error) {
	if !s.mgr.Running() {
		return 0, fmt.Errorf("sing-box is not running")
	}
	if s.store.Data.Cfg.Mode == "global-direct" {
		return 0, fmt.Errorf("speed test needs a proxy mode (not global-direct)")
	}
	var tag string
	for _, n := range s.store.Data.Nodes {
		if n.ID == nodeID {
			tag = "node:" + n.ID
		}
	}
	if tag == "" {
		return 0, fmt.Errorf("node not found")
	}
	base := s.clashBase()
	client := &http.Client{Timeout: 10 * time.Second}

	// current selector choice
	prev := ""
	if resp, err := client.Get(base + "/proxies/proxy"); err == nil {
		var p struct {
			Now string `json:"now"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&p)
		resp.Body.Close()
		prev = p.Now
	}
	// switch to target
	switchSelector := func(name string) {
		body, _ := json.Marshal(map[string]string{"name": name})
		req, _ := http.NewRequest("PUT", base+"/proxies/proxy", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if resp, err := client.Do(req); err == nil {
			resp.Body.Close()
		}
	}
	switchSelector(tag)
	defer func() {
		if prev != "" {
			switchSelector(prev)
		}
	}()
	time.Sleep(500 * time.Millisecond) // let the selector settle

	// download through the local mixed inbound (HTTP proxy)
	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", s.store.Data.Cfg.MixedPort))
	dl := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
	}
	var dlURL string
	for _, u := range []string{
		"https://speed.cloudflare.com/__down?bytes=10000000",
		"https://cachefly.cachefly.net/10mb.test",
	} {
		if resp, err := dl.Get(u); err == nil && resp.StatusCode == 200 {
			dlURL = u
			start := time.Now()
			n, _ := io.Copy(io.Discard, io.LimitReader(resp.Body, 12<<20))
			elapsed := time.Since(start).Seconds()
			resp.Body.Close()
			if elapsed > 0.2 && n > 0 {
				mbps := float64(n) / elapsed * 8 / 1e6
				return mbps, nil
			}
			break
		} else if resp != nil {
			resp.Body.Close()
		}
	}
	_ = dlURL
	return 0, fmt.Errorf("download test failed (node may be down)")
}

