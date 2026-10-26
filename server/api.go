package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Chuang2016/singbox-gui/core"
	"github.com/Chuang2016/singbox-gui/web"
)

type Server struct {
	store   *core.Store
	mgr     *core.Manager
	workDir string
	mux     *http.ServeMux

	healthMu sync.Mutex
	delays   map[string]nodeHealth // nodeID -> delay
	speeds   map[string]nodeHealth // nodeID -> speed

	traffic *trafficTracker // per-node cumulative bytes
}

func New(store *core.Store, mgr *core.Manager, workDir string) *Server {
	s := &Server{store: store, mgr: mgr, workDir: workDir, mux: http.NewServeMux(),
		delays: map[string]nodeHealth{}, speeds: map[string]nodeHealth{},
		traffic: newTrafficTracker(workDir)}
	s.routes()
	go s.traffic.loop(s)
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

// StartProxy regenerates the config and starts sing-box (same as POST /api/start).
func (s *Server) StartProxy() error {
	if err := s.regen(); err != nil {
		return err
	}
	return s.mgr.Start()
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	w.WriteHeader(http.StatusBadRequest)
	writeJSON(w, map[string]string{"error": err.Error()})
}

func (s *Server) routes() {
	m := s.mux
	// dashboard
	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(web.DashboardHTML)
	})

	m.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"running":  s.mgr.Running(),
			"nodes":    len(s.store.Data.Nodes),
			"logs":     s.mgr.Logs(),
			"work_dir": s.workDir,
			"rulesets": core.RuleSetInfo(s.workDir),
		})
	})

	// quit sbgui (stop sing-box child first)
	m.HandleFunc("/api/quit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		go func() {
			time.Sleep(300 * time.Millisecond)
			_ = s.mgr.Stop()
			s.traffic.save()
			os.Exit(0)
		}()
		writeJSON(w, map[string]string{"ok": "bye"})
	})

	// force re-download rule-set packs (bypass 7-day cache), then rebuild config.
	// Files are backed up first and restored if the re-download fails, so a
	// network outage can never leave the install without usable rule-sets.
	m.HandleFunc("/api/rulesets/refresh", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		dir := core.RuleSetDir(s.workDir)
		names := []string{"geosite-cn.srs", "geoip-cn.srs", "geosite-category-ads-all.srs", "gfwlist.json"}
		for _, f := range names {
			_ = os.Rename(filepath.Join(dir, f), filepath.Join(dir, f+".bak"))
		}
		if err := s.regen(); err != nil {
			for _, f := range names { // restore backups
				_ = os.Rename(filepath.Join(dir, f+".bak"), filepath.Join(dir, f))
			}
			writeErr(w, err)
			return
		}
		for _, f := range names {
			_ = os.Remove(filepath.Join(dir, f+".bak"))
		}
		writeJSON(w, map[string]any{"ok": "refreshed", "rulesets": core.RuleSetInfo(s.workDir)})
	})

	// manually import a rule-set from a user-pasted URL (any reachable mirror)
	m.HandleFunc("/api/rulesets/import-url", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		var req struct {
			Target string `json:"target"`
			URL    string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, err)
			return
		}
		if err := core.ImportRuleSetFromURL(s.workDir, req.Target, strings.TrimSpace(req.URL)); err != nil {
			writeErr(w, err)
			return
		}
		_ = s.regen() // best effort: import counts even if config can't build yet (e.g. no nodes)
		writeJSON(w, map[string]any{"ok": "imported", "rulesets": core.RuleSetInfo(s.workDir)})
	})

	// manually upload a rule-set file the user downloaded themselves
	m.HandleFunc("/api/rulesets/import-file", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			writeErr(w, err)
			return
		}
		target := r.FormValue("target")
		f, _, err := r.FormFile("file")
		if err != nil {
			writeErr(w, err)
			return
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, 64<<20))
		if err != nil {
			writeErr(w, err)
			return
		}
		if err := core.ImportRuleSetFile(s.workDir, target, data); err != nil {
			writeErr(w, err)
			return
		}
		_ = s.regen() // best effort, same as import-url
		writeJSON(w, map[string]any{"ok": "imported", "rulesets": core.RuleSetInfo(s.workDir)})
	})
	m.HandleFunc("/api/start", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		if err := s.regen(); err != nil {
			writeErr(w, err)
			return
		}
		if err := s.mgr.Start(); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]string{"ok": "started"})
	})
	m.HandleFunc("/api/stop", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		if err := s.mgr.Stop(); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]string{"ok": "stopped"})
	})
	m.HandleFunc("/api/restart", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		_ = s.mgr.Stop()
		if err := s.regen(); err != nil {
			writeErr(w, err)
			return
		}
		if err := s.mgr.Start(); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]string{"ok": "restarted"})
	})

	// nodes
	m.HandleFunc("/api/nodes", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			s.healthMu.Lock()
			out := make([]any, 0, len(s.store.Data.Nodes))
			for _, n := range s.store.Data.Nodes {
				m := map[string]any{
					"id": n.ID, "tag": n.Tag, "protocol": n.Protocol, "source": n.Source,
				}
				if h, ok := s.delays[n.ID]; ok {
					m["delay_ms"] = h.Value
					m["delay_at"] = h.At
					m["delay_ok"] = h.OK
				}
				if h, ok := s.speeds[n.ID]; ok {
					m["speed_mbps"] = h.Value
					m["speed_at"] = h.At
					m["speed_ok"] = h.OK
				}
				m["traffic_bytes"] = s.traffic.total(n.ID)
				out = append(out, m)
			}
			s.healthMu.Unlock()
			writeJSON(w, out)
		case "POST": // import links: {"text": "..."}
			var req struct {
				Text string `json:"text"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeErr(w, err)
				return
			}
			added, failed := 0, 0
			for _, line := range strings.Split(req.Text, "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				proto, ob, err := core.ParseShareLink(line)
				if err != nil {
					failed++
					continue
				}
				tag := core.LinkTag(line, proto)
				if tag == "" {
					tag = fmt.Sprintf("%s-%d", proto, len(s.store.Data.Nodes)+1)
				}
				s.store.Data.Nodes = append(s.store.Data.Nodes, core.Node{
					ID: core.NewID("n"), Tag: tag, Protocol: proto,
					Outbound: ob, Source: "manual",
				})
				added++
			}
			_ = s.store.Save()
			writeJSON(w, map[string]any{"added": added, "failed": failed})
		default:
			w.WriteHeader(405)
		}
	})
	// delay check for all nodes (via sing-box Clash API)
	// reset cumulative per-node traffic counters
	m.HandleFunc("/api/nodes/reset-traffic", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		s.traffic.reset()
		writeJSON(w, map[string]string{"ok": "reset"})
	})

	m.HandleFunc("/api/nodes/check-delay", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		if !s.mgr.Running() {
			writeErr(w, fmt.Errorf("sing-box is not running"))
			return
		}
		results := s.checkAllDelays()
		now := time.Now().Format("15:04:05")
		s.healthMu.Lock()
		for id, d := range results {
			s.delays[id] = nodeHealth{Value: float64(d), At: now, OK: d >= 0}
		}
		s.healthMu.Unlock()
		writeJSON(w, map[string]any{"delays": results, "at": now})
	})

	// speed test for one node: /api/nodes/{id}/speed
	m.HandleFunc("/api/nodes/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/api/nodes/")
		parts := strings.SplitN(rest, "/", 2)
		if len(parts) == 2 && parts[1] == "speed" && r.Method == "POST" {
			mbps, err := s.speedTest(parts[0])
			now := time.Now().Format("15:04:05")
			s.healthMu.Lock()
			if err == nil {
				s.speeds[parts[0]] = nodeHealth{Value: mbps, At: now, OK: true}
			} else {
				s.speeds[parts[0]] = nodeHealth{Value: 0, At: now, OK: false}
			}
			s.healthMu.Unlock()
			if err != nil {
				writeErr(w, err)
				return
			}
			writeJSON(w, map[string]any{"speed_mbps": mbps, "at": now})
			return
		}
		// node detail / edit: GET or PUT /api/nodes/{id}
		if len(parts) == 1 && (r.Method == "GET" || r.Method == "PUT") {
			id := parts[0]
			idx := -1
			for i := range s.store.Data.Nodes {
				if s.store.Data.Nodes[i].ID == id {
					idx = i
					break
				}
			}
			if idx < 0 {
				writeErr(w, fmt.Errorf("node not found"))
				return
			}
			if r.Method == "GET" {
				n := s.store.Data.Nodes[idx]
				writeJSON(w, map[string]any{"id": n.ID, "name": n.Tag, "protocol": n.Protocol, "outbound": n.Outbound})
				return
			}
			var req struct {
				Name     string         `json:"name"`
				Outbound map[string]any `json:"outbound"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeErr(w, err)
				return
			}
			if req.Outbound == nil {
				writeErr(w, fmt.Errorf("outbound must be a JSON object"))
				return
			}
			if t, _ := req.Outbound["type"].(string); t == "" {
				writeErr(w, fmt.Errorf("outbound.type is required"))
				return
			}
			delete(req.Outbound, "tag") // internal tag is forced at config build
			// snapshot for rollback: a rejected edit must not persist
			old := s.store.Data.Nodes[idx]
			oldOutbound := map[string]any{}
			if raw, err := json.Marshal(old.Outbound); err == nil {
				_ = json.Unmarshal(raw, &oldOutbound)
			}
			rollback := func() {
				s.store.Data.Nodes[idx].Outbound = oldOutbound
				s.store.Data.Nodes[idx].Tag = old.Tag
				s.store.Data.Nodes[idx].Protocol = old.Protocol
				_ = s.store.Save()
				_ = s.regen()
			}
			s.store.Data.Nodes[idx].Outbound = req.Outbound
			s.store.Data.Nodes[idx].Protocol, _ = req.Outbound["type"].(string)
			if strings.TrimSpace(req.Name) != "" {
				s.store.Data.Nodes[idx].Tag = strings.TrimSpace(req.Name)
			}
			_ = s.store.Save()
			if err := s.regen(); err != nil {
				rollback()
				writeErr(w, err)
				return
			}
			// validate the rebuilt config so a bad edit is reported immediately
			// (skipped silently when no sing-box binary exists yet)
			if out, err := s.mgr.Check(); err != nil {
				var execErr *exec.Error
				if !errors.As(err, &execErr) {
					rollback()
					writeErr(w, fmt.Errorf("edit rejected by sing-box (rolled back): %v %s", err, out))
					return
				}
			}
			writeJSON(w, map[string]string{"ok": "saved"})
			return
		}
		// fall through to the existing DELETE handler below
		id := rest
		if r.Method != "DELETE" {
			w.WriteHeader(405)
			return
		}
		kept := s.store.Data.Nodes[:0]
		for _, n := range s.store.Data.Nodes {
			if n.ID != id {
				kept = append(kept, n)
			}
		}
		s.store.Data.Nodes = kept
		_ = s.store.Save()
		writeJSON(w, map[string]string{"ok": "deleted"})
	})

	// subscriptions
	m.HandleFunc("/api/subs", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			writeJSON(w, s.store.Data.Subs)
		case "POST":
			var req struct {
				Name string `json:"name"`
				URL  string `json:"url"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeErr(w, err)
				return
			}
			sub := core.Subscription{ID: core.NewID("sub"), Name: req.Name, URL: req.URL}
			s.store.Data.Subs = append(s.store.Data.Subs, sub)
			_ = s.store.Save()
			added, err := s.refreshSub(sub.ID)
			if err != nil {
				writeJSON(w, map[string]any{"sub": sub, "warning": err.Error()})
				return
			}
			writeJSON(w, map[string]any{"sub": sub, "added": added})
		default:
			w.WriteHeader(405)
		}
	})
	m.HandleFunc("/api/subs/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/api/subs/")
		parts := strings.SplitN(rest, "/", 2)
		id := parts[0]
		action := ""
		if len(parts) > 1 {
			action = parts[1]
		}
		if r.Method == "DELETE" && action == "" {
			kept := s.store.Data.Subs[:0]
			for _, sb := range s.store.Data.Subs {
				if sb.ID != id {
					kept = append(kept, sb)
				}
			}
			s.store.Data.Subs = kept
			// drop its nodes
			kn := s.store.Data.Nodes[:0]
			for _, n := range s.store.Data.Nodes {
				if n.Source != "sub:"+id {
					kn = append(kn, n)
				}
			}
			s.store.Data.Nodes = kn
			_ = s.store.Save()
			writeJSON(w, map[string]string{"ok": "deleted"})
			return
		}
		if r.Method == "POST" && action == "refresh" {
			added, err := s.refreshSub(id)
			if err != nil {
				writeErr(w, err)
				return
			}
			writeJSON(w, map[string]any{"added": added})
			return
		}
		w.WriteHeader(405)
	})

	// settings
	m.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			writeJSON(w, s.store.Data.Cfg)
		case "PUT":
			var cfg core.Settings
			if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
				writeErr(w, err)
				return
			}
			if cfg.GatewayMode != s.store.Data.Cfg.GatewayMode {
				if cfg.GatewayMode {
					if err := core.SetupGateway(core.GatewayTproxyPort); err != nil {
						writeErr(w, err)
						return
					}
				} else {
					_ = core.TeardownGateway()
				}
				// tproxy inbound changed -> regen config, restart if running
				s.store.Data.Cfg = cfg
				_ = s.store.Save()
				if err := s.regen(); err != nil {
					writeErr(w, err)
					return
				}
				if s.mgr.Running() {
					_ = s.mgr.Stop()
					if err := s.mgr.Start(); err != nil {
						writeErr(w, err)
						return
					}
				}
				writeJSON(w, map[string]string{"ok": "saved"})
				return
			}
			s.store.Data.Cfg = cfg
			_ = s.store.Save()
			writeJSON(w, map[string]string{"ok": "saved"})
		default:
			w.WriteHeader(405)
		}
	})

	// select active node -> switch sing-box selector via clash api
	m.HandleFunc("/api/select", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		var req struct {
			NodeID string `json:"node_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, err)
			return
		}
		var tag string
		for _, n := range s.store.Data.Nodes {
			if n.ID == req.NodeID {
				tag = "node:" + n.ID
				break
			}
		}
		if tag == "" {
			writeErr(w, fmt.Errorf("node not found"))
			return
		}
		s.store.Data.Cfg.ActiveNodeID = req.NodeID
		_ = s.store.Save()
		// try live switch through clash api (best effort)
		clashBase := fmt.Sprintf("http://127.0.0.1:%d", s.store.Data.Cfg.ClashPort)
		body, _ := json.Marshal(map[string]string{"name": tag})
		req2, _ := http.NewRequest("PUT", clashBase+"/proxies/proxy", strings.NewReader(string(body)))
		req2.Header.Set("Content-Type", "application/json")
		client := &http.Client{Timeout: 5 * time.Second}
		if resp, err := client.Do(req2); err == nil {
			resp.Body.Close()
		}
		writeJSON(w, map[string]string{"ok": "selected", "tag": tag})
	})

	// system proxy toggle
	m.HandleFunc("/api/sysproxy", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		var req struct {
			Enable bool `json:"enable"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, err)
			return
		}
		if err := setSystemProxy(req.Enable, s.store.Data.Cfg.MixedPort); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]string{"ok": "done"})
	})

	// clash api reverse proxy (REST + websocket)
	m.HandleFunc("/api/clash/", s.clashProxy())
}

func (s *Server) regen() error {
	// local rule-sets are downloaded by sbgui via mirrors (no proxy needed)
	if s.store.Data.Cfg.Mode == "bypass-cn" {
		if err := core.EnsureRuleSets(s.workDir); err != nil {
			return err
		}
	}
	if s.store.Data.Cfg.BlockAds {
		if err := core.EnsureExtraRuleSets(s.workDir, []string{"geosite-category-ads-all"}); err != nil {
			return err
		}
	}
	if s.store.Data.Cfg.GFWList {
		if err := core.EnsureGFWList(s.workDir); err != nil {
			return err
		}
	}
	_, err := core.BuildConfig(s.store.Data, s.workDir)
	return err
}

func (s *Server) refreshSub(id string) (int, error) {
	var sub *core.Subscription
	for i := range s.store.Data.Subs {
		if s.store.Data.Subs[i].ID == id {
			sub = &s.store.Data.Subs[i]
			break
		}
	}
	if sub == nil {
		return 0, fmt.Errorf("subscription not found")
	}
	obs, tags, protos, err := core.FetchSubscription(sub.URL)
	if err != nil {
		return 0, err
	}
	// drop old nodes from this sub
	kept := s.store.Data.Nodes[:0]
	for _, n := range s.store.Data.Nodes {
		if n.Source != "sub:"+id {
			kept = append(kept, n)
		}
	}
	s.store.Data.Nodes = kept
	for i, ob := range obs {
		s.store.Data.Nodes = append(s.store.Data.Nodes, core.Node{
			ID: core.NewID("n"), Tag: tags[i], Protocol: protos[i],
			Outbound: ob, Source: "sub:" + id,
		})
	}
	sub.Count = len(obs)
	sub.UpdatedAt = time.Now().Format("2006-01-02 15:04")
	_ = s.store.Save()
	return len(obs), nil
}

func (s *Server) clashProxy() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target := &url.URL{
			Scheme: "http",
			Host:   fmt.Sprintf("127.0.0.1:%d", s.store.Data.Cfg.ClashPort),
			Path:   strings.TrimPrefix(r.URL.Path, "/api/clash"),
		}
		target.RawQuery = r.URL.RawQuery
		proxy := &httputil.ReverseProxy{
			Director: func(req *http.Request) {
				req.URL = target
				req.Host = target.Host
			},
			ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
				http.Error(w, "clash api unreachable (is sing-box running?)", http.StatusBadGateway)
			},
		}
		proxy.ServeHTTP(w, r)
	}
}

func setSystemProxy(enable bool, port int) error {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	switch runtime.GOOS {
	case "windows":
		key := `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`
		if enable {
			if out, err := exec.Command("reg", "add", key, "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "1", "/f").CombinedOutput(); err != nil {
				return fmt.Errorf("%v: %s", err, out)
			}
			if out, err := exec.Command("reg", "add", key, "/v", "ProxyServer", "/t", "REG_SZ", "/d", addr, "/f").CombinedOutput(); err != nil {
				return fmt.Errorf("%v: %s", err, out)
			}
		} else {
			if out, err := exec.Command("reg", "add", key, "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "0", "/f").CombinedOutput(); err != nil {
				return fmt.Errorf("%v: %s", err, out)
			}
		}
		return nil
	case "darwin":
		svc := "Wi-Fi"
		if enable {
			if out, err := exec.Command("networksetup", "-setwebproxy", svc, "127.0.0.1", fmt.Sprint(port)).CombinedOutput(); err != nil {
				return fmt.Errorf("%v: %s", err, out)
			}
			if out, err := exec.Command("networksetup", "-setsecurewebproxy", svc, "127.0.0.1", fmt.Sprint(port)).CombinedOutput(); err != nil {
				return fmt.Errorf("%v: %s", err, out)
			}
			if out, err := exec.Command("networksetup", "-setsocksfirewallproxy", svc, "127.0.0.1", fmt.Sprint(port)).CombinedOutput(); err != nil {
				return fmt.Errorf("%v: %s", err, out)
			}
		} else {
			for _, a := range [][]string{{"-setwebproxystate", svc, "off"}, {"-setsecurewebproxystate", svc, "off"}, {"-setsocksfirewallproxystate", svc, "off"}} {
				if out, err := exec.Command("networksetup", a...).CombinedOutput(); err != nil {
					return fmt.Errorf("%v: %s", err, out)
				}
			}
		}
		return nil
	case "linux":
		// GNOME desktop via gsettings
		if _, err := exec.LookPath("gsettings"); err != nil {
			return fmt.Errorf("gsettings not found (non-GNOME desktop?); set http_proxy/https_proxy manually to http://%s", addr)
		}
		run := func(args ...string) error {
			if out, err := exec.Command("gsettings", args...).CombinedOutput(); err != nil {
				return fmt.Errorf("%v: %s", err, out)
			}
			return nil
		}
		if enable {
			for _, a := range [][]string{
				{"set", "org.gnome.system.proxy", "mode", "'manual'"},
				{"set", "org.gnome.system.proxy.http", "host", "'127.0.0.1'"},
				{"set", "org.gnome.system.proxy.http", "port", fmt.Sprint(port)},
				{"set", "org.gnome.system.proxy.https", "host", "'127.0.0.1'"},
				{"set", "org.gnome.system.proxy.https", "port", fmt.Sprint(port)},
				{"set", "org.gnome.system.proxy.socks", "host", "'127.0.0.1'"},
				{"set", "org.gnome.system.proxy.socks", "port", fmt.Sprint(port)},
				{"set", "org.gnome.system.proxy", "ignore-hosts", "['localhost', '127.0.0.0/8']"},
			} {
				if err := run(a...); err != nil {
					return err
				}
			}
		} else {
			if err := run("set", "org.gnome.system.proxy", "mode", "'none'"); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("system proxy toggle only implemented on windows/darwin/linux; set proxy manually to %s", addr)
	}
}
