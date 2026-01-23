//go:build windows

package winutil

import (
	"os"
	"syscall"
)

// EnsureConsole tries to attach to the parent process console (when the binary
// is built as windowsgui subsystem). If there is no parent console, it does nothing.
//
// This allows a single EXE to behave like:
// - Double-click: GUI (no console window)
// - Run from cmd/powershell: console output works
func EnsureConsole() {
	const ATTACH_PARENT_PROCESS = uint32(0xFFFFFFFF)

	dll, err := syscall.LoadDLL("kernel32.dll")
	if err != nil {
		return
	}
	attach, err := dll.FindProc("AttachConsole")
	if err != nil {
		return
	}
	r1, _, _ := attach.Call(uintptr(ATTACH_PARENT_PROCESS))
	if r1 == 0 {
		// no parent console
		return
	}

	// refresh std handles
	if h, err := syscall.GetStdHandle(syscall.STD_OUTPUT_HANDLE); err == nil && h != 0 && h != syscall.InvalidHandle {
		os.Stdout = os.NewFile(uintptr(h), "stdout")
	}
	if h, err := syscall.GetStdHandle(syscall.STD_ERROR_HANDLE); err == nil && h != 0 && h != syscall.InvalidHandle {
		os.Stderr = os.NewFile(uintptr(h), "stderr")
	}
}
