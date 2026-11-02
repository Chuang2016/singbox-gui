//go:build windows

package core

import (
	"os/exec"
	"syscall"
)

// quietCommand is exec.Command plus, on Windows, "do not give the child
// a console window". sing-box.exe is a console-subsystem binary: started
// from the GUI-subsystem sbgui process with a plain exec.Command,
// Windows allocates it a brand-new, empty console window that then sits
// on the desktop for as long as sing-box runs (the black cmd window
// users reported). CREATE_NO_WINDOW prevents the console from being
// created at all; HideWindow is belt-and-braces for helpers that do
// show one. Short-lived helpers (reg, sing-box check) benefit the same
// way: no console flash.
func quietCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	return cmd
}
