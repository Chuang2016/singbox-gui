package core

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Leftover sing-box cleanup.
//
// sing-box runs as a child process. If sbgui is force-quit
// (SIGKILL, Activity Monitor "Force Quit", a crash), the child
// is orphaned to init and keeps holding the configured local
// ports, so the next launch's sing-box cannot bind them and the
// start fails. The Manager therefore sweeps for leftovers when
// it is constructed (app start) and again on every Start.
//
// A process counts as OUR leftover only if all of these hold:
//
//   - its command line is a sing-box "run" invocation naming
//     this workDir's generated config file — exactly the command
//     Start issues: <sing-box> run -c <workDir>/config.json;
//   - its parent process is gone (or it has been reparented to
//     init, ppid <= 1): a sing-box whose parent is still alive
//     belongs to that live owner — another sbgui instance, or
//     this same process — and is left alone;
//   - it is not this process and not the child this Manager is
//     currently running.
//
// Anything else — a sing-box the user runs themselves with
// their own config, an editor with our config file open, a
// process merely named sing-box — is never touched, and if the
// port conflict comes from such a process the existing start
// error stands. A failed sweep is silent: it must never block
// startup.

// staleProc is one enumerated process. argv is the exact
// argument vector where the platform provides it (Linux /proc);
// otherwise cmd is the joined command-line string (ps, CIM).
type staleProc struct {
	pid  int
	ppid int
	argv []string
	cmd  string
}

// cleanupStaleSingbox finds and terminates leftover sing-box
// processes matching the signature above, logging what it did
// through the Manager log.
func (m *Manager) cleanupStaleSingbox() {
	cfgPath := filepath.Join(m.workDir, "config.json")
	binPath := singboxBin(m.workDir)
	procs, err := listProcesses()
	if err != nil || len(procs) == 0 {
		return
	}
	alive := make(map[int]bool, len(procs))
	for _, p := range procs {
		alive[p.pid] = true
	}
	self := os.Getpid()
	m.mu.Lock()
	managed := 0
	if m.cmd != nil && m.cmd.Process != nil {
		managed = m.cmd.Process.Pid
	}
	m.mu.Unlock()
	for _, p := range procs {
		if p.pid == self || p.pid == managed {
			continue
		}
		if !matchSingboxProc(p, cfgPath, binPath) {
			continue
		}
		if p.ppid > 1 && alive[p.ppid] {
			m.pushLog(fmt.Sprintf("leftover check: sing-box pid %d left alone (parent pid %d still running)", p.pid, p.ppid))
			continue
		}
		if terminateProcess(p.pid) {
			m.pushLog(fmt.Sprintf("cleaned up leftover sing-box (pid %d) from a previous sbgui run", p.pid))
		} else {
			m.pushLog(fmt.Sprintf("leftover check: could not stop sing-box pid %d", p.pid))
		}
	}
}

// matchSingboxProc reports whether p is a sing-box "run"
// invocation for cfgPath, in the exact shape Manager.Start
// issues it: <bin> run -c <cfgPath>.
func matchSingboxProc(p staleProc, cfgPath, binPath string) bool {
	if len(p.argv) > 0 {
		return matchSingboxArgv(p.argv, cfgPath, binPath)
	}
	return matchSingboxCmdline(p.cmd, cfgPath, binPath)
}

func isSingboxBin(s, binPath string) bool {
	if eqPath(s, binPath) {
		return true
	}
	switch filepath.Base(s) {
	case "sing-box", "sing-box.exe":
		return true
	}
	return false
}

func matchSingboxArgv(argv []string, cfgPath, binPath string) bool {
	if len(argv) < 4 || !isSingboxBin(argv[0], binPath) {
		return false
	}
	for i := 1; i+2 < len(argv); i++ {
		if argv[i] == "run" && argv[i+1] == "-c" && eqPath(argv[i+2], cfgPath) {
			return true
		}
	}
	return false
}

// matchSingboxCmdline is the joined-string form of
// matchSingboxArgv, for platforms that only expose a command
// line string (macOS ps, Windows CIM). It anchors on the exact
// " run -c <cfgPath>" substring our launch produces: the config
// path must end the string or be followed by a space, and the
// head before the anchor must be the sing-box binary itself
// (the bundled path, or a bare sing-box name) — so an editor
// holding the config open, or a command line merely mentioning
// the path, never matches.
func matchSingboxCmdline(s, cfgPath, binPath string) bool {
	anchor := " run -c " + cfgPath
	i := strings.Index(s, anchor)
	if i < 0 {
		return false
	}
	if tail := s[i+len(anchor):]; tail != "" && !strings.HasPrefix(tail, " ") {
		return false
	}
	head := s[:i]
	if strings.Contains(head, " ") && !eqPath(head, binPath) {
		return false
	}
	return isSingboxBin(head, binPath)
}

// eqPath compares two path strings, cleaned; case-insensitive
// on Windows.
func eqPath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// waitProcessGone polls until pid no longer exists or the
// timeout elapses; it reports the final state.
func waitProcessGone(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !processAlive(pid) {
			return true
		}
		if time.Now().After(deadline) {
			return !processAlive(pid)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
