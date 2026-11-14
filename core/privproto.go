package core

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// privproto node support.
//
// A privproto node is not a sing-box outbound: the standalone
// privproto-client binary terminates the tunnel and exposes a local
// SOCKS5 port. sbgui's half of the bargain:
//
//   - the stored node fragment (type "privproto", see links.go)
//     carries server / server_port / password for the client;
//   - BuildConfig renders the node as a plain socks outbound to a
//     freshly probed local port;
//   - the Manager launches one privproto-client per privproto node
//     before it starts sing-box, waits for each SOCKS port to
//     accept, and kills every client again when the session ends --
//     the clients are part of the managed session, never orphans.
//
// So all routing (presets, custom rules, urltest, live selector
// switches) works exactly as for native nodes: sing-box just sees
// a socks outbound.

// IsPrivprotoNode reports whether a node is served by a
// privproto-client subprocess rather than by sing-box itself.
func IsPrivprotoNode(n Node) bool {
	if n.Protocol == "privproto" {
		return true
	}
	t, _ := n.Outbound["type"].(string)
	return t == "privproto"
}

// privprotoClientBin locates the privproto-client binary: next to
// the sbgui executable first, then the work dir (the other place
// sing-box is looked up), then PATH.
func privprotoClientBin(workDir string) (string, error) {
	name := "privproto-client"
	if runtime.GOOS == "windows" {
		name = "privproto-client.exe"
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
	return "", fmt.Errorf("privproto client binary not found: place %s next to the sbgui executable or in PATH", name)
}

// privprotoSpec is everything needed to launch one client.
type privprotoSpec struct {
	nodeID   string
	tag      string
	server   string // host:port of the privproto server
	password string
	port     int // local SOCKS5 port from the generated config
}

type privprotoClient struct {
	spec    privprotoSpec
	cmd     *exec.Cmd
	cfgPath string
	done    chan struct{} // closed once the process has exited
	waitErr error         // valid after done is closed
}

// privprotoSpecs derives the launch specs for the current session:
// node parameters come from the store, the local SOCKS ports from
// the config.json BuildConfig just wrote (outbound tag "node:<id>").
func (m *Manager) privprotoSpecs() ([]privprotoSpec, error) {
	store, err := OpenStore(m.workDir)
	if err != nil {
		return nil, err
	}
	var ppNodes []Node
	for _, n := range store.Data.Nodes {
		if IsPrivprotoNode(n) {
			ppNodes = append(ppNodes, n)
		}
	}
	if len(ppNodes) == 0 {
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
		return nil, fmt.Errorf("privproto: parse generated config: %w", err)
	}
	portOf := map[string]int{}
	for _, ob := range cfg.Outbounds {
		if ob.Type == "socks" {
			portOf[ob.Tag] = ob.ServerPort
		}
	}
	specs := make([]privprotoSpec, 0, len(ppNodes))
	for _, n := range ppNodes {
		host, _ := n.Outbound["server"].(string)
		port := anyInt(n.Outbound["server_port"])
		pw, _ := n.Outbound["password"].(string)
		if host == "" || port == 0 || pw == "" {
			return nil, fmt.Errorf("privproto: node %s is missing server, port or password", n.Tag)
		}
		socksPort, ok := portOf["node:"+n.ID]
		if !ok || socksPort == 0 {
			return nil, fmt.Errorf("privproto: node %s has no socks outbound in the generated config", n.Tag)
		}
		specs = append(specs, privprotoSpec{
			nodeID: n.ID, tag: n.Tag,
			server:   net.JoinHostPort(host, fmt.Sprint(port)),
			password: pw, port: socksPort,
		})
	}
	return specs, nil
}

// startPrivprotoClients launches one privproto-client per privproto
// node and waits until each local SOCKS port accepts a connection.
// On any failure everything launched so far is torn down again.
func (m *Manager) startPrivprotoClients() error {
	specs, err := m.privprotoSpecs()
	if err != nil {
		return err
	}
	if len(specs) == 0 {
		return nil
	}
	bin, err := privprotoClientBin(m.workDir)
	if err != nil {
		return err
	}
	for _, spec := range specs {
		if err := m.startOnePrivprotoClient(bin, spec); err != nil {
			m.stopPrivprotoClients()
			return err
		}
		m.pushLog(fmt.Sprintf("privproto: node %s via 127.0.0.1:%d", spec.tag, spec.port))
	}
	return nil
}

func (m *Manager) startOnePrivprotoClient(bin string, spec privprotoSpec) error {
	cfgJSON, err := json.Marshal(map[string]string{
		"server":   spec.server,
		"password": spec.password,
		"listen":   fmt.Sprintf("127.0.0.1:%d", spec.port),
	})
	if err != nil {
		return err
	}
	// The config file carries the password: same 0600 treatment as
	// store.json, and removed again by stopPrivprotoClients.
	cfgPath := filepath.Join(m.workDir, "privproto-"+spec.nodeID+".json")
	if err := os.WriteFile(cfgPath, cfgJSON, 0600); err != nil {
		return fmt.Errorf("privproto: write client config: %w", err)
	}
	cmd := quietCommand(bin, "-config", cfgPath)
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		_ = os.Remove(cfgPath)
		return fmt.Errorf("privproto: start client for node %s: %w", spec.tag, err)
	}
	pc := &privprotoClient{spec: spec, cmd: cmd, cfgPath: cfgPath, done: make(chan struct{})}
	m.ppMu.Lock()
	m.ppClients = append(m.ppClients, pc)
	m.ppMu.Unlock()
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			m.pushLog("privproto: " + strings.TrimSpace(sc.Text()))
		}
	}()
	go func() {
		pc.waitErr = cmd.Wait()
		close(pc.done)
	}()
	// The client binds its SOCKS port as soon as it is up (the
	// tunnel session to the server is established asynchronously
	// by the client itself, like sing-box starting before its
	// outbounds are dialed).
	deadline := time.Now().Add(15 * time.Second)
	for {
		select {
		case <-pc.done:
			return fmt.Errorf("privproto: client for node %s exited during startup: %v", spec.tag, pc.waitErr)
		default:
		}
		if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", spec.port), 200*time.Millisecond); err == nil {
			c.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("privproto: client for node %s did not open port %d within 15s", spec.tag, spec.port)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// stopPrivprotoClients terminates every launched privproto-client
// and removes its temp config file. Idempotent: safe to call from
// every teardown path (Stop, sing-box exit watcher, app shutdown).
func (m *Manager) stopPrivprotoClients() {
	m.ppMu.Lock()
	clients := m.ppClients
	m.ppClients = nil
	m.ppMu.Unlock()
	for _, pc := range clients {
		if pc.cmd.Process != nil {
			_ = pc.cmd.Process.Kill()
		}
		select {
		case <-pc.done:
		case <-time.After(3 * time.Second):
		}
		_ = os.Remove(pc.cfgPath)
	}
}
