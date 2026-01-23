//go:build windows

package winutil

import (
	"errors"
	"syscall"
	"unsafe"
)

// DesktopDir returns the current user's desktop directory.
// Uses SHGetFolderPathW for Win7 compatibility.
func DesktopDir() (string, error) {
	const (
		CSIDL_DESKTOPDIRECTORY = 0x0010
		SHGFP_TYPE_CURRENT     = 0
		MAX_PATH               = 260
	)

	dll, err := syscall.LoadDLL("shell32.dll")
	if err != nil {
		return "", err
	}
	proc, err := dll.FindProc("SHGetFolderPathW")
	if err != nil {
		return "", err
	}

	buf := make([]uint16, MAX_PATH)
	r1, _, _ := proc.Call(
		0,
		uintptr(CSIDL_DESKTOPDIRECTORY),
		0,
		uintptr(SHGFP_TYPE_CURRENT),
		uintptr(unsafe.Pointer(&buf[0])),
	)
	// S_OK == 0
	if r1 != 0 {
		return "", errors.New("SHGetFolderPathW failed")
	}
	return syscall.UTF16ToString(buf), nil
}
