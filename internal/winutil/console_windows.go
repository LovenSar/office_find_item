//go:build windows

package winutil

import "syscall"

// DetachConsole detaches the process from the current console, if any.
// This prevents a separately-created console window (when double-click launching
// a console-subsystem binary) from controlling the lifetime of the UI.
func DetachConsole() {
	dll, err := syscall.LoadDLL("kernel32.dll")
	if err != nil {
		return
	}
	proc, err := dll.FindProc("FreeConsole")
	if err != nil {
		return
	}
	_, _, _ = proc.Call()
}
