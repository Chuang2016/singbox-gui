//go:build !windows

package core

import "os/exec"

// quietCommand is exec.Command; the console-window suppression only
// exists (and is only needed) on Windows. See proc_windows.go.
func quietCommand(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}
