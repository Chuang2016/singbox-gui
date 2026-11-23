//go:build !windows

package core

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// listProcesses enumerates all processes. On Linux it reads
// /proc, which yields exact argv; elsewhere on Unix it falls
// back to ps and the joined command line.
func listProcesses() ([]staleProc, error) {
	if _, err := os.Stat("/proc/self/cmdline"); err == nil {
		return listProcessesProc()
	}
	return listProcessesPs()
}

func listProcessesProc() ([]staleProc, error) {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var out []staleProc
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil || len(raw) == 0 {
			continue
		}
		argv := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
		p := staleProc{pid: pid, argv: argv}
		if stat, err := os.ReadFile(filepath.Join("/proc", e.Name(), "stat")); err == nil {
			p.ppid = parsePpid(string(stat))
		}
		out = append(out, p)
	}
	return out, nil
}

// parsePpid extracts the ppid from a /proc/<pid>/stat line. The
// comm field may itself contain spaces and parentheses, so the
// remaining fields are counted from the last ')'.
func parsePpid(stat string) int {
	i := strings.LastIndex(stat, ")")
	if i < 0 {
		return 0
	}
	fields := strings.Fields(stat[i+1:])
	// fields[0] is the state, fields[1] the ppid.
	if len(fields) < 2 {
		return 0
	}
	ppid, _ := strconv.Atoi(fields[1])
	return ppid
}

func listProcessesPs() ([]staleProc, error) {
	out, err := quietCommand("ps", "-axww", "-o", "pid,ppid,args").Output()
	if err != nil {
		return nil, err
	}
	var procs []staleProc
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// pid and ppid are the first two whitespace-separated
		// fields; the rest is the command line. The header line
		// fails the numeric parse and is skipped.
		f1 := strings.IndexByte(line, ' ')
		if f1 < 0 {
			continue
		}
		pid, err := strconv.Atoi(line[:f1])
		if err != nil {
			continue
		}
		rest := strings.TrimSpace(line[f1+1:])
		f2 := strings.IndexByte(rest, ' ')
		if f2 < 0 {
			continue
		}
		ppid, err := strconv.Atoi(rest[:f2])
		if err != nil {
			continue
		}
		procs = append(procs, staleProc{pid: pid, ppid: ppid, cmd: strings.TrimSpace(rest[f2+1:])})
	}
	return procs, nil
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// terminateProcess asks pid to exit (SIGTERM), waits briefly,
// then forces it (SIGKILL). It reports whether the process is
// gone at the end.
func terminateProcess(pid int) bool {
	if !processAlive(pid) {
		return true
	}
	_ = syscall.Kill(pid, syscall.SIGTERM)
	if waitProcessGone(pid, 2*time.Second) {
		return true
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	return waitProcessGone(pid, time.Second)
}
