package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The tests below pin down session-generation isolation: a
// teardown that names generation N must never kill, unregister
// or delete the config of a client belonging to generation N+1,
// even for the same node. They use stand-in sleep processes in
// place of the real external clients.

// fakeNaiveClient registers a stand-in naive client under the
// given generation, mirroring how startOneNaiveClient registers
// the real thing (process + Wait goroutine + temp config file).
func fakeNaiveClient(t *testing.T, m *Manager, gen uint64, nodeID string) *naiveClient {
	t.Helper()
	cfgPath := filepath.Join(m.workDir, fmt.Sprintf("naive-%s-g%d.json", nodeID, gen))
	if err := os.WriteFile(cfgPath, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	nc := &naiveClient{
		spec:    naiveSpec{nodeID: nodeID, tag: "tag-" + nodeID},
		cmd:     cmd,
		cfgPath: cfgPath,
		done:    make(chan struct{}),
		gen:     gen,
	}
	go func() {
		nc.waitErr = cmd.Wait()
		close(nc.done)
	}()
	m.nvMu.Lock()
	m.nvClients = append(m.nvClients, nc)
	m.nvMu.Unlock()
	return nc
}

func fakePrivprotoClient(t *testing.T, m *Manager, gen uint64, nodeID string) *privprotoClient {
	t.Helper()
	cfgPath := filepath.Join(m.workDir, fmt.Sprintf("privproto-%s-g%d.json", nodeID, gen))
	if err := os.WriteFile(cfgPath, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pc := &privprotoClient{
		spec:    privprotoSpec{nodeID: nodeID, tag: "tag-" + nodeID},
		cmd:     cmd,
		cfgPath: cfgPath,
		done:    make(chan struct{}),
		gen:     gen,
	}
	go func() {
		pc.waitErr = cmd.Wait()
		close(pc.done)
	}()
	m.ppMu.Lock()
	m.ppClients = append(m.ppClients, pc)
	m.ppMu.Unlock()
	return pc
}

func assertNaiveAlive(t *testing.T, nc *naiveClient, what string) {
	t.Helper()
	select {
	case <-nc.done:
		t.Fatalf("%s: process exited unexpectedly (waitErr=%v)", what, nc.waitErr)
	case <-time.After(250 * time.Millisecond):
	}
	if _, err := os.Stat(nc.cfgPath); err != nil {
		t.Fatalf("%s: config file was removed: %v", what, err)
	}
}

func assertNaiveDead(t *testing.T, nc *naiveClient, what string) {
	t.Helper()
	select {
	case <-nc.done:
	case <-time.After(3 * time.Second):
		t.Fatalf("%s: process still running", what)
	}
	if _, err := os.Stat(nc.cfgPath); !os.IsNotExist(err) {
		t.Fatalf("%s: config file still exists (err=%v)", what, err)
	}
}

// The restart race from the field: the old session's teardown
// runs after the new session has registered its clients. The old
// teardown must be a no-op for the new generation.
func TestStopNaiveClientsGenerationScoped(t *testing.T) {
	m := &Manager{workDir: t.TempDir()}
	m.gen = 2
	old := fakeNaiveClient(t, m, 1, "n1")
	cur1 := fakeNaiveClient(t, m, 2, "n1") // same node, new generation
	cur2 := fakeNaiveClient(t, m, 2, "n2")

	m.stopNaiveClients(1)
	assertNaiveDead(t, old, "old generation client")
	assertNaiveAlive(t, cur1, "current generation client (same node)")
	assertNaiveAlive(t, cur2, "current generation client")

	m.nvMu.Lock()
	remaining := len(m.nvClients)
	m.nvMu.Unlock()
	if remaining != 2 {
		t.Fatalf("registry holds %d clients, want 2", remaining)
	}

	m.stopNaiveClients(2)
	assertNaiveDead(t, cur1, "current client after its own teardown")
	assertNaiveDead(t, cur2, "current client after its own teardown")
}

func TestDropNaiveClientGenerationScoped(t *testing.T) {
	m := &Manager{workDir: t.TempDir()}
	m.gen = 2
	old := fakeNaiveClient(t, m, 1, "n1")
	cur := fakeNaiveClient(t, m, 2, "n1")

	m.dropNaiveClient(1, "n1")
	assertNaiveDead(t, old, "dropped old generation client")
	assertNaiveAlive(t, cur, "current generation client with same node ID")

	m.stopNaiveClients(2)
}

func TestStopPrivprotoClientsGenerationScoped(t *testing.T) {
	m := &Manager{workDir: t.TempDir()}
	m.gen = 2
	old := fakePrivprotoClient(t, m, 1, "p1")
	cur := fakePrivprotoClient(t, m, 2, "p1")

	m.stopPrivprotoClients(1)
	select {
	case <-old.done:
	case <-time.After(3 * time.Second):
		t.Fatal("old generation privproto client still running")
	}
	if _, err := os.Stat(old.cfgPath); !os.IsNotExist(err) {
		t.Fatalf("old generation privproto config still exists (err=%v)", err)
	}
	select {
	case <-cur.done:
		t.Fatalf("current generation privproto client exited (waitErr=%v)", cur.waitErr)
	case <-time.After(250 * time.Millisecond):
	}
	if _, err := os.Stat(cur.cfgPath); err != nil {
		t.Fatalf("current generation privproto config was removed: %v", err)
	}
	m.stopPrivprotoClients(2)
}

// A client that dies on its own must surface in the log with its
// exit status verbatim and as a node error.
func TestWatchNaiveExitReportsUnexpectedExit(t *testing.T) {
	m := &Manager{workDir: t.TempDir()}
	m.gen = 3
	cmd := exec.Command("sh", "-c", "exit 42")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	nc := &naiveClient{
		spec: naiveSpec{nodeID: "nX", tag: "tag-nX"},
		cmd:  cmd,
		done: make(chan struct{}),
		gen:  3,
	}
	go func() {
		nc.waitErr = cmd.Wait()
		close(nc.done)
	}()
	m.watchNaiveExit(nc) // returns once the exit has been reported

	found := false
	for _, l := range m.Logs() {
		if strings.Contains(l, "naive: node tag-nX exited: exit status 42") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing exit log line; logs=%v", m.Logs())
	}
	errs := m.NodeErrors()
	if !strings.Contains(errs["nX"], "exit status 42") {
		t.Fatalf("missing node error with exit status; errs=%v", errs)
	}
}

// A client killed by teardown is expected to exit: no log line,
// no node error.
func TestWatchNaiveExitSilentForTeardownKill(t *testing.T) {
	m := &Manager{workDir: t.TempDir()}
	m.gen = 1
	nc := fakeNaiveClient(t, m, 1, "nT")
	go m.watchNaiveExit(nc)
	m.stopNaiveClients(1)
	time.Sleep(300 * time.Millisecond)
	for _, l := range m.Logs() {
		if strings.Contains(l, "exited:") {
			t.Fatalf("teardown kill was reported as a failure: %q", l)
		}
	}
	if errs := m.NodeErrors(); len(errs) != 0 {
		t.Fatalf("teardown kill recorded node errors: %v", errs)
	}
}
