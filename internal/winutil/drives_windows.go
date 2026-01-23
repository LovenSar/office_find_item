//go:build windows

package winutil

import (
	"syscall"
	"unsafe"
)

var (
	modKernel32          = syscall.NewLazyDLL("kernel32.dll")
	procGetLogicalDrives = modKernel32.NewProc("GetLogicalDrives")
	procGetDriveTypeW    = modKernel32.NewProc("GetDriveTypeW")
)

const (
	driveUnknown   = 0
	driveNoRootDir = 1
	driveRemovable = 2
	driveFixed     = 3
	driveRemote    = 4
	driveCDROM     = 5
	driveRAMDisk   = 6
)

// ListSearchableDrives 返回可搜索的盘符根路径（如 C:\）。
// 默认包含 Fixed/Removable；排除 CD-ROM/网络盘（可按需扩展）。
func ListSearchableDrives() []string {
	r1, _, _ := procGetLogicalDrives.Call()
	mask := uint32(r1)
	out := make([]string, 0, 8)
	for i := 0; i < 26; i++ {
		if (mask & (1 << uint(i))) == 0 {
			continue
		}
		root := []uint16{uint16('A' + i), ':', '\\', 0}
		t := getDriveType(&root[0])
		switch t {
		case driveFixed, driveRemovable:
			out = append(out, string([]byte{byte('A' + i), ':', '\\'}))
		}
	}
	return out
}

func getDriveType(root *uint16) uint32 {
	r1, _, _ := procGetDriveTypeW.Call(uintptr(unsafe.Pointer(root)))
	return uint32(r1)
}
