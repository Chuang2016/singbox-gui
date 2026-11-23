//go:build windows

package core

import (
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

// listProcesses enumerates all processes through CIM, the only
// stock source of full command lines on Windows.
func listProcesses() ([]staleProc, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// Same console suppression as proc_windows.go's
	// quietCommand, plus the context timeout that helper cannot
	// express.
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command",
		"Get-CimInstance Win32_Process | Select-Object ProcessId,ParentProcessId,CommandLine | ConvertTo-Json -Compress")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	type cimProc struct {
		ProcessId       int
		ParentProcessId int
		CommandLine     string
	}
	// ConvertTo-Json emits a bare object when exactly one row
	// comes back, an array otherwise.
	var rows []cimProc
	if err := json.Unmarshal(out, &rows); err != nil {
		var one cimProc
		if err := json.Unmarshal(out, &one); err != nil {
			return nil, err
		}
		rows = []cimProc{one}
	}
	procs := make([]staleProc, 0, len(rows))
	for _, r := range rows {
		if r.ProcessId <= 0 {
			continue
		}
		procs = append(procs, staleProc{pid: r.ProcessId, ppid: r.ParentProcessId, cmd: r.CommandLine})
	}
	return procs, nil
}

// processAlive reports whether pid currently names a process.
// OpenProcess fails for a pid that no longer exists.
func processAlive(pid int) bool {
	h, err := syscall.OpenProcess(0x1000 /* PROCESS_QUERY_LIMITED_INFORMATION */, false, uint32(pid))
	if err != nil {
		return false
	}
	_ = syscall.CloseHandle(h)
	return true
}

// terminateProcess asks pid to exit (taskkill), waits briefly,
// then forces it (taskkill /F). It reports whether the process
// is gone at the end.
func terminateProcess(pid int) bool {
	if !processAlive(pid) {
		return true
	}
	_ = quietCommand("taskkill", "/PID", strconv.Itoa(pid)).Run()
	if waitProcessGone(pid, 2*time.Second) {
		return true
	}
	_ = quietCommand("taskkill", "/F", "/PID", strconv.Itoa(pid)).Run()
	return waitProcessGone(pid, time.Second)
}
