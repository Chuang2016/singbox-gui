package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/Chuang2016/singbox-gui/core"
	"github.com/Chuang2016/singbox-gui/web"
)

type Server struct {
	store   *core.Store
	mgr     *core.Manager
	workDir string
	mux     *http.ServeMux
}

func New(store *core.Store, mgr *core.Manager, workDir string) *Server {
	s := &Server{store: store, mgr: mgr, workDir: workDir, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

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
			"running": s.mgr.Running(),
			"nodes":   len(s.store.Data.Nodes),
			"logs":    s.mgr.Logs(),
		})
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
			writeJSON(w, s.store.Data.Nodes)
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
	m.HandleFunc("/api/nodes/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/nodes/")
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
	// bypass-cn mode needs local rule-sets; download via mirrors (no proxy needed)
	if s.store.Data.Cfg.Mode == "bypass-cn" {
		if err := core.EnsureRuleSets(s.workDir); err != nil {
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
	default:
		return fmt.Errorf("system proxy toggle only implemented on windows/darwin; set proxy manually to %s", addr)
	}
}
