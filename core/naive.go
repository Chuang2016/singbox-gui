package core

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

// naive node support.
//
// A naive node is not a sing-box outbound either: the standalone
// naive client (klzgrad/naiveproxy) terminates the tunnel and
// exposes a local SOCKS5 port. The arrangement mirrors privproto
// (see core/privproto.go):
//
//   - the stored node fragment (type "naive", see links.go) carries
//     server / server_port / username / password / network for the
//     client;
//   - BuildConfig renders the node as a plain socks outbound to a
//     freshly probed local port;
//   - the Manager launches one naive client per naive node before
//     it starts sing-box, waits for each SOCKS port to accept, and
//     kills every client again when the session ends.
//
// IPv4 pinning: when a node's server is a domain name, Start
// resolves its A record and writes a "host-resolver-rules" entry
// (MAP <domain> <ipv4>) into the client config. naiveproxy's
// Chromium resolver otherwise prefers AAAA whenever one exists,
// which black-holes the tunnel on networks without working IPv6
// (the connection is attempted over v6 and never falls back in
// time). The proxy URL keeps the original domain, so TLS SNI and
// certificate verification are unaffected. IP-literal servers,
// failed lookups and AAAA-only domains get no rule and launch
// exactly as before.
//
// One deliberate difference from privproto: the client is launched
// with a JSON config file (naive accepts `naive <config.json>` as
// its only argument, equivalent to --listen/--proxy flags) instead
// of command-line flags, so the upstream password stays out of the
// process argument list -- the same 0600 temp-file treatment the
// privproto client config gets.

// IsNaiveNode reports whether a node is served by a naive-client
// subprocess rather than by sing-box itself.
func IsNaiveNode(n Node) bool {
	if n.Protocol == "naive" {
		return true
	}
	t, _ := n.Outbound["type"].(string)
	return t == "naive"
}

// naiveClientBin locates the naive binary: next to the sbgui
// executable first, then the work dir, then PATH.
func naiveClientBin(workDir string) (string, error) {
	name := "naive"
	if runtime.GOOS == "windows" {
		name = "naive.exe"
	}
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	dirs = append(dirs, workDir)
	for _, d := range dirs {
		p := filepath.Join(d, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("naive client binary not found: place %s next to the sbgui executable or in PATH", name)
}

// naiveSpec is everything needed to launch one client.
type naiveSpec struct {
	nodeID string
	tag    string
	proxy  string // full upstream URL for the client's "proxy" option
	port   int    // local SOCKS5 port from the generated config
	rules  string // "host-resolver-rules" value pinning the server domain to IPv4, "" = none
}

type naiveClient struct {
	spec    naiveSpec
	cmd     *exec.Cmd
	cfgPath string
	done    chan struct{} // closed once the process has exited
	waitErr error         // valid after done is closed
	gen     uint64        // session generation that launched this client

	// intentional is set by every teardown path before it kills
	// the process, so the exit watcher can tell sbgui's own kills
	// (expected, silent) from the client dying on its own (a real
	// failure, logged with its exit status).
	intentional atomic.Bool
}

// naiveProxyURL rebuilds the upstream proxy URL (https:// or
// quic://, credentials escaped) from a stored naive fragment.
func naiveProxyURL(ob map[string]any) (string, error) {
	host, _ := ob["server"].(string)
	port := anyInt(ob["server_port"])
	user, _ := ob["username"].(string)
	pw, _ := ob["password"].(string)
	network, _ := ob["network"].(string)
	if network != "quic" {
		network = "https"
	}
	if host == "" || port == 0 || user == "" || pw == "" {
		return "", fmt.Errorf("missing server, port, username or password")
	}
	u := url.URL{
		Scheme: network,
		User:   url.UserPassword(user, pw),
		Host:   net.JoinHostPort(host, fmt.Sprint(port)),
	}
	return u.String(), nil
}

// naiveLookupIP is the A/AAAA lookup used for IPv4 pinning; a
// variable so tests can stub DNS.
var naiveLookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip4", host)
}

// naivePinIPv4 returns a Chromium host-resolver-rules value that
// statically maps host to its first IPv4 (A) address, e.g.
// "MAP example.com 203.0.113.7". It returns "" whenever pinning
// does not apply: host is empty or an IP literal (v4 or v6), the
// lookup fails, or the name has no A record.
func naivePinIPv4(host string) string {
	if host == "" || net.ParseIP(host) != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ips, err := naiveLookupIP(ctx, host)
	if err != nil {
		return ""
	}
	for _, ip := range ips {
		if ip4 := ip.To4(); ip4 != nil {
			return "MAP " + host + " " + ip4.String()
		}
	}
	return ""
}

// naiveSpecs derives the launch specs for the current session: node
// parameters come from the store, the local SOCKS ports from the
// config.json BuildConfig just wrote (outbound tag "node:<id>").
func (m *Manager) naiveSpecs() ([]naiveSpec, error) {
	store, err := OpenStore(m.workDir)
	if err != nil {
		return nil, err
	}
	var nvNodes []Node
	for _, n := range store.Data.Nodes {
		if IsNaiveNode(n) {
			nvNodes = append(nvNodes, n)
		}
	}
	if len(nvNodes) == 0 {
		return nil, nil
	}
	raw, err := os.ReadFile(filepath.Join(m.workDir, "config.json"))
	if err != nil {
		return nil, err
	}
	var cfg struct {
		Outbounds []struct {
			Tag        string `json:"tag"`
			Type       string `json:"type"`
			ServerPort int    `json:"server_port"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("naive: parse generated config: %w", err)
	}
	portOf := map[string]int{}
	for _, ob := range cfg.Outbounds {
		if ob.Type == "socks" {
			portOf[ob.Tag] = ob.ServerPort
		}
	}
	specs := make([]naiveSpec, 0, len(nvNodes))
	for _, n := range nvNodes {
		proxy, err := naiveProxyURL(n.Outbound)
		if err != nil {
			return nil, fmt.Errorf("naive: node %s: %w", n.Tag, err)
		}
		socksPort, ok := portOf["node:"+n.ID]
		if !ok || socksPort == 0 {
			return nil, fmt.Errorf("naive: node %s has no socks outbound in the generated config", n.Tag)
		}
		server, _ := n.Outbound["server"].(string)
		specs = append(specs, naiveSpec{
			nodeID: n.ID, tag: n.Tag, proxy: proxy, port: socksPort,
			rules: naivePinIPv4(server),
		})
	}
	return specs, nil
}

// startNaiveClients launches one naive client per naive node and
// waits until each local SOCKS port accepts a connection. Failures
// are per-node: a node whose client cannot start is recorded in
// the Manager's NodeErrors and logged, the remaining clients
// still launch, and sing-box starts either way.
func (m *Manager) startNaiveClients(gen uint64) {
	specs, err := m.naiveSpecs()
	if err != nil {
		m.pushLog(fmt.Sprintf("naive: %v", err))
		return
	}
	if len(specs) == 0 {
		return
	}
	bin, err := naiveClientBin(m.workDir)
	if err != nil {
		for _, spec := range specs {
			m.setNodeError(spec.nodeID, err)
			m.pushLog(fmt.Sprintf("naive: node %s failed: %v", spec.tag, err))
		}
		return
	}
	for _, spec := range specs {
		if err := m.startOneNaiveClient(bin, spec, gen); err != nil {
			m.dropNaiveClient(gen, spec.nodeID)
			m.setNodeError(spec.nodeID, err)
			m.pushLog(fmt.Sprintf("naive: node %s failed: %v", spec.tag, err))
			continue
		}
		m.pushLog(fmt.Sprintf("naive: node %s via 127.0.0.1:%d", spec.tag, spec.port))
		if spec.rules != "" {
			m.pushLog(fmt.Sprintf("naive: node %s pins %s", spec.tag, spec.rules))
		}
	}
}

// dropNaiveClient terminates and forgets the one client belonging
// to nodeID in generation gen (a client that failed its startup
// wait). Clients of other generations -- and other nodes -- are
// untouched.
func (m *Manager) dropNaiveClient(gen uint64, nodeID string) {
	m.nvMu.Lock()
	var dropped *naiveClient
	kept := make([]*naiveClient, 0, len(m.nvClients))
	for _, nc := range m.nvClients {
		if nc.gen == gen && nc.spec.nodeID == nodeID {
			dropped = nc
		} else {
			kept = append(kept, nc)
		}
	}
	m.nvClients = kept
	m.nvMu.Unlock()
	if dropped == nil {
		return
	}
	dropped.intentional.Store(true)
	if dropped.cmd.Process != nil {
		_ = dropped.cmd.Process.Kill()
	}
	select {
	case <-dropped.done:
	case <-time.After(3 * time.Second):
	}
	_ = os.Remove(dropped.cfgPath)
}

// watchNaiveExit reports a client that dies on its own after a
// successful start. Teardown kills are expected and stay silent
// (every teardown path marks the client intentional first). An
// unmarked exit is a real failure of that node -- e.g. the client
// crashing the moment the first tunnel is dialed -- so it is
// logged with the process's exit status verbatim (the raw exit
// code is itself diagnostic: a Chromium CHECK failure aborts
// with 0x80000003, for instance) and recorded as a node error.
// The node error is only recorded while the client still
// belongs to the current session generation; a stale watcher
// from an old generation just logs.
func (m *Manager) watchNaiveExit(nc *naiveClient) {
	<-nc.done
	if nc.intentional.Load() {
		return
	}
	m.pushLog(fmt.Sprintf("naive: node %s exited: %v", nc.spec.tag, nc.waitErr))
	if nc.gen == m.currentGen() {
		m.setNodeError(nc.spec.nodeID, fmt.Errorf("naive client exited: %v", nc.waitErr))
	}
}

func (m *Manager) startOneNaiveClient(bin string, spec naiveSpec, gen uint64) error {
	cfgMap := map[string]string{
		"listen": fmt.Sprintf("socks://127.0.0.1:%d", spec.port),
		"proxy":  spec.proxy,
		// naiveproxy logs nothing by default (privacy). An empty
		// "log" value sends its log to stderr instead, which the
		// scanner goroutine below forwards into the sbgui log --
		// without it a failing or crashing client is silent, and
		// its FATAL "Check failed" line (the key to a crash) is
		// lost.
		"log": "",
	}
	if spec.rules != "" {
		cfgMap["host-resolver-rules"] = spec.rules
	}
	cfgJSON, err := json.Marshal(cfgMap)
	if err != nil {
		return err
	}
	// The config file carries the password: same 0600 treatment as
	// store.json, and removed again by stopNaiveClients. The name
	// carries the session generation so one generation's teardown
	// can never delete the file a newer session's client is using
	// (the path would otherwise be identical across sessions).
	cfgPath := filepath.Join(m.workDir, fmt.Sprintf("naive-%s-g%d.json", spec.nodeID, gen))
	if err := os.WriteFile(cfgPath, cfgJSON, 0600); err != nil {
		return fmt.Errorf("naive: write client config: %w", err)
	}
	cmd := quietCommand(bin, cfgPath)
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		_ = os.Remove(cfgPath)
		return fmt.Errorf("naive: start client for node %s: %w", spec.tag, err)
	}
	nc := &naiveClient{spec: spec, cmd: cmd, cfgPath: cfgPath, done: make(chan struct{}), gen: gen}
	m.nvMu.Lock()
	m.nvClients = append(m.nvClients, nc)
	m.nvMu.Unlock()
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			m.pushLog("naive: " + strings.TrimSpace(sc.Text()))
		}
	}()
	go func() {
		nc.waitErr = cmd.Wait()
		close(nc.done)
	}()
	// The client binds its SOCKS port as soon as it is up (the
	// tunnel to the server is established per connection, like
	// sing-box starting before its outbounds are dialed).
	deadline := time.Now().Add(15 * time.Second)
	for {
		select {
		case <-nc.done:
			return fmt.Errorf("naive: client for node %s exited during startup: %v", spec.tag, nc.waitErr)
		default:
		}
		if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", spec.port), 200*time.Millisecond); err == nil {
			c.Close()
			go m.watchNaiveExit(nc)
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("naive: client for node %s did not open port %d within 15s", spec.tag, spec.port)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// stopNaiveClients terminates the naive clients of session
// generation gen and removes their temp config files. Clients of
// other generations are left running: teardown is routinely
// raced by a following Start (node switching), and the old
// session's cleanup must not kill the new session's clients.
// Idempotent: safe to call from every teardown path (Stop,
// sing-box exit watcher, app shutdown).
func (m *Manager) stopNaiveClients(gen uint64) {
	m.nvMu.Lock()
	var mine []*naiveClient
	kept := make([]*naiveClient, 0, len(m.nvClients))
	for _, nc := range m.nvClients {
		if nc.gen == gen {
			mine = append(mine, nc)
		} else {
			kept = append(kept, nc)
		}
	}
	m.nvClients = kept
	m.nvMu.Unlock()
	for _, nc := range mine {
		nc.intentional.Store(true)
		if nc.cmd.Process != nil {
			_ = nc.cmd.Process.Kill()
		}
		select {
		case <-nc.done:
		case <-time.After(3 * time.Second):
		}
		_ = os.Remove(nc.cfgPath)
	}
}
