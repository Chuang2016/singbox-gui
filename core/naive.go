package core

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
}

type naiveClient struct {
	spec    naiveSpec
	cmd     *exec.Cmd
	cfgPath string
	done    chan struct{} // closed once the process has exited
	waitErr error         // valid after done is closed
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
		specs = append(specs, naiveSpec{
			nodeID: n.ID, tag: n.Tag, proxy: proxy, port: socksPort,
		})
	}
	return specs, nil
}

// startNaiveClients launches one naive client per naive node and
// waits until each local SOCKS port accepts a connection. Failures
// are per-node: a node whose client cannot start is recorded in
// the Manager's NodeErrors and logged, the remaining clients
// still launch, and sing-box starts either way.
func (m *Manager) startNaiveClients() {
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
		if err := m.startOneNaiveClient(bin, spec); err != nil {
			m.dropNaiveClient(spec.nodeID)
			m.setNodeError(spec.nodeID, err)
			m.pushLog(fmt.Sprintf("naive: node %s failed: %v", spec.tag, err))
			continue
		}
		m.pushLog(fmt.Sprintf("naive: node %s via 127.0.0.1:%d", spec.tag, spec.port))
	}
}

// dropNaiveClient terminates and forgets the one client belonging
// to nodeID (a client that failed its startup wait). Other
// clients are untouched.
func (m *Manager) dropNaiveClient(nodeID string) {
	m.nvMu.Lock()
	var dropped *naiveClient
	kept := make([]*naiveClient, 0, len(m.nvClients))
	for _, nc := range m.nvClients {
		if nc.spec.nodeID == nodeID {
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
	if dropped.cmd.Process != nil {
		_ = dropped.cmd.Process.Kill()
	}
	select {
	case <-dropped.done:
	case <-time.After(3 * time.Second):
	}
	_ = os.Remove(dropped.cfgPath)
}

func (m *Manager) startOneNaiveClient(bin string, spec naiveSpec) error {
	cfgJSON, err := json.Marshal(map[string]string{
		"listen": fmt.Sprintf("socks://127.0.0.1:%d", spec.port),
		"proxy":  spec.proxy,
	})
	if err != nil {
		return err
	}
	// The config file carries the password: same 0600 treatment as
	// store.json, and removed again by stopNaiveClients.
	cfgPath := filepath.Join(m.workDir, "naive-"+spec.nodeID+".json")
	if err := os.WriteFile(cfgPath, cfgJSON, 0600); err != nil {
		return fmt.Errorf("naive: write client config: %w", err)
	}
	cmd := quietCommand(bin, cfgPath)
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		_ = os.Remove(cfgPath)
		return fmt.Errorf("naive: start client for node %s: %w", spec.tag, err)
	}
	nc := &naiveClient{spec: spec, cmd: cmd, cfgPath: cfgPath, done: make(chan struct{})}
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
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("naive: client for node %s did not open port %d within 15s", spec.tag, spec.port)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// stopNaiveClients terminates every launched naive client and
// removes its temp config file. Idempotent: safe to call from every
// teardown path (Stop, sing-box exit watcher, app shutdown).
func (m *Manager) stopNaiveClients() {
	m.nvMu.Lock()
	clients := m.nvClients
	m.nvClients = nil
	m.nvMu.Unlock()
	for _, nc := range clients {
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
