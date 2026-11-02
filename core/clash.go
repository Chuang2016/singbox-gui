package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sync"
	"time"
)

// This file is the shared client for sing-box's Clash API: node delay
// and speed measurements, live selector switching and the live
// connections list. It used to live in server/ (health.go), which made
// it web-only; both front-ends now call the same code.

func ClashBase(clashPort int) string {
	return fmt.Sprintf("http://127.0.0.1:%d", clashPort)
}

func clashClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}

// ClashDelay asks the Clash API for the delay of one outbound
// ("node:<id>"). Returns delay in ms, or -1 when unreachable.
func ClashDelay(clashPort int, tag string) int {
	u := fmt.Sprintf("%s/proxies/%s/delay?timeout=5000&url=%s",
		ClashBase(clashPort), url.PathEscape(tag), url.QueryEscape("https://www.gstatic.com/generate_204"))
	resp, err := clashClient(8 * time.Second).Get(u)
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

// CheckAllDelays tests every node concurrently (max 10 in flight) and
// returns node ID -> delay ms (-1 = unreachable).
func CheckAllDelays(clashPort int, nodes []Node) map[string]int {
	type res struct {
		id    string
		delay int
	}
	out := make(map[string]int, len(nodes))
	sem := make(chan struct{}, 10)
	var wg sync.WaitGroup
	ch := make(chan res, len(nodes))
	for _, n := range nodes {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ch <- res{id, ClashDelay(clashPort, "node:"+id)}
		}(n.ID)
	}
	wg.Wait()
	close(ch)
	for r := range ch {
		out[r.id] = r.delay
	}
	return out
}

// SwitchSelector points the "proxy" selector at the given outbound
// name (e.g. "node:<id>") through the Clash API.
func SwitchSelector(clashPort int, name string) error {
	body, _ := json.Marshal(map[string]string{"name": name})
	req, err := http.NewRequest("PUT", ClashBase(clashPort)+"/proxies/proxy", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := clashClient(5 * time.Second).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("clash api: %s", resp.Status)
	}
	return nil
}

// ClashSpeedTest measures download throughput (Mbps) through one node:
// switch the proxy selector to it, download a test file via the local
// mixed inbound, then restore the previous selection. (Moved verbatim
// from server/health.go.)
func ClashSpeedTest(store *Store, mgr *Manager, nodeID string) (float64, error) {
	cfg := store.Data.Cfg
	if !mgr.Running() {
		return 0, fmt.Errorf("sing-box is not running")
	}
	if cfg.Mode == "global-direct" {
		return 0, fmt.Errorf("speed test needs a proxy mode (not global-direct)")
	}
	tag := ""
	for _, n := range store.Data.Nodes {
		if n.ID == nodeID {
			tag = "node:" + n.ID
		}
	}
	if tag == "" {
		return 0, fmt.Errorf("node not found")
	}
	base := ClashBase(cfg.ClashPort)
	client := clashClient(10 * time.Second)

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
	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", cfg.MixedPort))
	dl := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
	}
	for _, u := range []string{
		"https://speed.cloudflare.com/__down?bytes=10000000",
		"https://cachefly.cachefly.net/10mb.test",
	} {
		if resp, err := dl.Get(u); err == nil && resp.StatusCode == 200 {
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
	return 0, fmt.Errorf("download test failed (node may be down)")
}

// Connection is one live connection from the Clash API, flattened for
// display (dashboard table and GUI Connections page show the same
// facts).
type Connection struct {
	ID       string
	Process  string // process name (basename of the process path)
	Host     string // host or destination IP, with port
	Rule     string // matched rule, e.g. "DomainSuffix google.com"
	Outbound string // final chain element (selector or node tag)
	Upload   int64
	Download int64
	Start    string
}

// FetchConnections reads the live connection list. The dashboard shows
// the last 30, newest first; callers slice as they like.
func FetchConnections(clashPort int) ([]Connection, error) {
	resp, err := clashClient(3 * time.Second).Get(ClashBase(clashPort) + "/connections")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var body struct {
		Connections []struct {
			ID       string `json:"id"`
			Upload   int64  `json:"upload"`
			Download int64  `json:"download"`
			Start    string `json:"start"`
			Rule     string `json:"rule"`
			Payload  string `json:"rulePayload"`
			Chains   []string `json:"chains"`
			Metadata struct {
				Host          string `json:"host"`
				DestIP        string `json:"destinationIP"`
				DestPort      string `json:"destinationPort"`
				Process       string `json:"process"`
				ProcessPath   string `json:"processPath"`
			} `json:"metadata"`
		} `json:"connections"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	out := make([]Connection, 0, len(body.Connections))
	for _, c := range body.Connections {
		host := c.Metadata.Host
		if host == "" {
			host = c.Metadata.DestIP
		}
		if c.Metadata.DestPort != "" {
			host += ":" + c.Metadata.DestPort
		}
		proc := c.Metadata.Process
		if proc == "" && c.Metadata.ProcessPath != "" {
			proc = path.Base(c.Metadata.ProcessPath)
		}
		rule := c.Rule
		if c.Payload != "" {
			rule += " " + c.Payload
		}
		ob := ""
		if len(c.Chains) > 0 {
			ob = c.Chains[len(c.Chains)-1]
		}
		out = append(out, Connection{
			ID: c.ID, Process: proc, Host: host, Rule: rule,
			Outbound: ob, Upload: c.Upload, Download: c.Download, Start: c.Start,
		})
	}
	return out, nil
}
