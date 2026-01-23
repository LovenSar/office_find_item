//go:build windows

package winutil

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	modKernel32fs           = syscall.NewLazyDLL("kernel32.dll")
	procGetDiskFreeSpaceExW = modKernel32fs.NewProc("GetDiskFreeSpaceExW")
)

func driveFreeBytes(root string) (uint64, error) {
	p, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return 0, err
	}
	var freeBytesAvailable uint64
	var totalNumberOfBytes uint64
	var totalNumberOfFreeBytes uint64
	r1, _, e := procGetDiskFreeSpaceExW.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalNumberOfBytes)),
		uintptr(unsafe.Pointer(&totalNumberOfFreeBytes)),
	)
	if r1 == 0 {
		if e != nil {
			return 0, e
		}
		return 0, errors.New("GetDiskFreeSpaceExW failed")
	}
	return freeBytesAvailable, nil
}

// BestCacheDir chooses a cache directory located on the drive with the most free space.
// It creates the directory if needed.
func BestCacheDir() (string, error) {
	drives := ListSearchableDrives()
	var best string
	var bestFree uint64
	for _, d := range drives {
		free, err := driveFreeBytes(d)
		if err != nil {
			continue
		}
		if best == "" || free > bestFree {
			best = d
			bestFree = free
		}
	}
	if best == "" {
		return "", errors.New("无法获取可用盘符剩余空间")
	}
	root := filepath.Join(best, "OfficeFindItemCache", "v1")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	return root, nil
}
