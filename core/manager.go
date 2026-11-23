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
//
// Session generations: every Start begins a new generation
// (Manager.gen). External clients are registered under the
// generation that launched them, and every teardown path -- Stop,
// the sing-box exit watcher, Start's own error paths -- names the
// generation it belongs to and only ever touches that
// generation's clients.
//
// This closes a restart race. Switching nodes is Stop immediately
// followed by Start, while the old session's sing-box watcher
// goroutine tears the external clients down asynchronously, and
// Stop returns as soon as the running flag flips -- before that
// watcher has necessarily run. The teardown used to drain
// whatever happened to be in the shared client lists when it
// executed, so a slow watcher could kill the NEW session's
// clients seconds after they started (the processes were gone,
// sing-box got connection refused on their local SOCKS ports)
// and delete their temp configs, which share the same per-node
// file name across sessions. Generation-scoped teardown makes
// the old session's cleanup a no-op for the new session's
// clients; the temp config files are generation-suffixed for
// the same reason.
type Manager struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	workDir string
	logs    []string
	running bool
	gen     uint64

	ppMu      sync.Mutex
	ppClients []*privprotoClient

	nvMu      sync.Mutex
	nvClients []*naiveClient

	extErrMu sync.Mutex
	nodeErrs map[string]string
}

func NewManager(workDir string) *Manager {
	m := &Manager{workDir: workDir, logs: []string{}}
	// App start: clear out a sing-box orphaned by a previous
	// run that was force-quit, before it can block this run's
	// ports (see stale.go for the exact signature).
	m.cleanupStaleSingbox()
	return m
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

	// A leftover from a force-quit run may have appeared while
	// this instance was open; clear it before it can block the
	// ports (the sweep never touches a live-owned sing-box).
	m.cleanupStaleSingbox()

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
	m.mu.Lock()
	m.gen++
	gen := m.gen
	m.mu.Unlock()
	m.startPrivprotoClients(gen)
	m.startNaiveClients(gen)
	bin := singboxBin(m.workDir)
	cmd := quietCommand(bin, "run", "-c", filepath.Join(m.workDir, "config.json"))
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		m.stopPrivprotoClients(gen)
		m.stopNaiveClients(gen)
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
		if m.cmd == cmd {
			m.running = false
			m.cmd = nil
		}
		m.mu.Unlock()
		// sing-box is gone (stopped or died on its own): the
		// external clients it fronted have nothing left to serve.
		// Generation-scoped: this watcher can run late, after a
		// new session has already started, and must not touch the
		// new session's clients or state.
		m.stopPrivprotoClients(gen)
		m.stopNaiveClients(gen)
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
		m.stopPrivprotoClients(gen)
		m.stopNaiveClients(gen)
		logs := m.Logs()
		tail := ""
		if len(logs) > 0 {
			tail = logs[len(logs)-1]
		}
		return fmt.Errorf("sing-box exited immediately: %s", tail)
	}
	return nil
}

// currentGen returns the current session generation.
func (m *Manager) currentGen() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.gen
}

func (m *Manager) Stop() error {
	m.mu.Lock()
	cmd := m.cmd
	gen := m.gen
	m.mu.Unlock()
	// The external clients belong to the session: they are torn
	// down even when sing-box itself is already gone. The
	// teardown names this session's generation, so it can never
	// touch a newer session's clients.
	defer m.stopPrivprotoClients(gen)
	defer m.stopNaiveClients(gen)
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
