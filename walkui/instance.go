//go:build windows

package main

import (
	"golang.org/x/sys/windows"
)

// Single-instance guard for the Walk front-end: the first process owns
// a named mutex; a second launch finds it, pokes a named auto-reset
// event and exits, and the owner brings its window to the front. This
// keeps the "one exe, one sing-box owner" rule the store format
// assumes (the web back-end in the same process would otherwise fight
// over port 20123 and the sing-box child).

const (
	instanceMutexName = `Local\sbgui-frontend-instance`
	instanceEventName = `Local\sbgui-frontend-show`
)

// acquireInstance returns (showEvent, true) for the first instance.
// For a later instance it returns (0, false) after signalling the
// owner to show its window.
func acquireInstance() (windows.Handle, bool) {
	namePtr, _ := windows.UTF16PtrFromString(instanceMutexName)
	mutex, err := windows.CreateMutex(nil, false, namePtr)
	if err == windows.ERROR_ALREADY_EXISTS {
		if mutex != 0 {
			windows.CloseHandle(mutex)
		}
		signalShowInstance()
		return 0, false
	}
	// Any other error (or none): proceed as the owner; the port-bind
	// check in main is the backstop if the mutex itself failed.
	_ = mutex // held for the process lifetime

	evtPtr, _ := windows.UTF16PtrFromString(instanceEventName)
	evt, err := windows.CreateEvent(nil, 0, 0, evtPtr)
	if err != nil {
		return 0, true
	}
	return evt, true
}

// signalShowInstance tells the running instance to surface its window.
func signalShowInstance() {
	evtPtr, _ := windows.UTF16PtrFromString(instanceEventName)
	evt, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, evtPtr)
	if err != nil {
		return
	}
	defer windows.CloseHandle(evt)
	_ = windows.SetEvent(evt)
}

// waitShow blocks until a later launch asks us to show the window.
func waitShow(evt windows.Handle) bool {
	if evt == 0 {
		return false
	}
	s, err := windows.WaitForSingleObject(evt, windows.INFINITE)
	return err == nil && s == windows.WAIT_OBJECT_0
}
