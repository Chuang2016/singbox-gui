package core

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Manager controls the sing-box subprocess and, for privproto and
// naive nodes, the external client subprocesses that front it
// (see privproto.go and naive.go).
type Manager struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	workDir string
	logs    []string
	running bool

	ppMu      sync.Mutex
	ppClients []*privprotoClient

	nvMu      sync.Mutex
	nvClients []*naiveClient

	extErrMu sync.Mutex
	nodeErrs map[string]string
}

func NewManager(workDir string) *Manager {
	return &Manager{workDir: workDir, logs: []string{}}
}

func singboxBin(workDir string) string {
	name := "sing-box"
	if runtime.GOOS == "windows" {
		name = "sing-box.exe"
	}
	p := filepath.Join(workDir, name)
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return name // fall back to PATH
}

// Check validates config.json with `sing-box check`.
func (m *Manager) Check() (string, error) {
	bin := singboxBin(m.workDir)
	out, err := quietCommand(bin, "check", "-c", filepath.Join(m.workDir, "config.json")).CombinedOutput()
	return string(out), err
}

func (m *Manager) pushLog(line string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, line)
	if len(m.logs) > 500 {
		m.logs = m.logs[len(m.logs)-500:]
	}
}

func (m *Manager) Logs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]string, len(m.logs))
	copy(cp, m.logs)
	return cp
}

func (m *Manager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// NodeErrors returns the per-node startup errors recorded for
// nodes served by external client subprocesses (privproto /
// naive) in the current session, keyed by node ID. A node listed
// here is down because its client could not start; every other
// node is unaffected. The same failures are in the log.
func (m *Manager) NodeErrors() map[string]string {
	m.extErrMu.Lock()
	defer m.extErrMu.Unlock()
	out := make(map[string]string, len(m.nodeErrs))
	for id, e := range m.nodeErrs {
		out[id] = e
	}
	return out
}

func (m *Manager) setNodeError(id string, err error) {
	m.extErrMu.Lock()
	defer m.extErrMu.Unlock()
	if m.nodeErrs == nil {
		m.nodeErrs = map[string]string{}
	}
	m.nodeErrs[id] = err.Error()
}

func (m *Manager) clearNodeErrors() {
	m.extErrMu.Lock()
	defer m.extErrMu.Unlock()
	m.nodeErrs = map[string]string{}
}

// Start launches sing-box. Returns error if already running or check fails.
func (m *Manager) Start() error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return fmt.Errorf("already running")
	}
	m.mu.Unlock()

	if out, err := m.Check(); err != nil {
		return fmt.Errorf("config check failed: %v\n%s", err, out)
	}
	// privproto and naive clients must be accepting before sing-box
	// starts routing through their socks outbounds. A client that
	// fails to start (binary missing, bad node parameters, ...)
	// takes down only its own node: the failure is recorded per
	// node (see NodeErrors) and logged, and sing-box starts
	// regardless so the remaining nodes keep working.
	m.clearNodeErrors()
	m.startPrivprotoClients()
	m.startNaiveClients()
	bin := singboxBin(m.workDir)
	cmd := quietCommand(bin, "run", "-c", filepath.Join(m.workDir, "config.json"))
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		m.stopPrivprotoClients()
		m.stopNaiveClients()
		return fmt.Errorf("start sing-box: %w (is sing-box binary next to sbgui?)", err)
	}
	m.mu.Lock()
	m.cmd = cmd
	m.running = true
	m.mu.Unlock()

	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			m.pushLog(strings.TrimSpace(sc.Text()))
		}
	}()
	go func() {
		err := cmd.Wait()
		m.mu.Lock()
		m.running = false
		m.cmd = nil
		m.mu.Unlock()
		// sing-box is gone (stopped or died on its own): the
		// external clients it fronted have nothing left to serve.
		m.stopPrivprotoClients()
		m.stopNaiveClients()
		if err != nil {
			m.pushLog(fmt.Sprintf("sing-box exited: %v", err))
		} else {
			m.pushLog("sing-box exited")
		}
	}()
	// give it a moment to fail fast on bad config
	time.Sleep(800 * time.Millisecond)
	m.mu.Lock()
	ok := m.running
	m.mu.Unlock()
	if !ok {
		m.stopPrivprotoClients()
		m.stopNaiveClients()
		logs := m.Logs()
		tail := ""
		if len(logs) > 0 {
			tail = logs[len(logs)-1]
		}
		return fmt.Errorf("sing-box exited immediately: %s", tail)
	}
	return nil
}

func (m *Manager) Stop() error {
	m.mu.Lock()
	cmd := m.cmd
	m.mu.Unlock()
	// The external clients belong to the session: they are torn
	// down even when sing-box itself is already gone.
	defer m.stopPrivprotoClients()
	defer m.stopNaiveClients()
	if cmd == nil || cmd.Process == nil {
		return fmt.Errorf("not running")
	}
	if err := cmd.Process.Kill(); err != nil {
		return err
	}
	// wait for the waiter goroutine to flip state
	for i := 0; i < 50; i++ {
		m.mu.Lock()
		r := m.running
		m.mu.Unlock()
		if !r {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("stop timeout")
}
