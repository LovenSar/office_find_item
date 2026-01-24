//go:build windows

package winutil

import (
	"errors"
	"syscall"
	"unsafe"
)

type ProcessIOCounters struct {
	ReadOps    uint64
	WriteOps   uint64
	OtherOps   uint64
	ReadBytes  uint64
	WriteBytes uint64
	OtherBytes uint64
}

type windowsIOCounters struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

var (
	modKernel32IO            = syscall.NewLazyDLL("kernel32.dll")
	procGetCurrentProcess    = modKernel32IO.NewProc("GetCurrentProcess")
	procGetProcessIoCounters = modKernel32IO.NewProc("GetProcessIoCounters")
)

func GetProcessIOCounters() (ProcessIOCounters, error) {
	h, _, _ := procGetCurrentProcess.Call()
	var raw windowsIOCounters
	ok, _, e := procGetProcessIoCounters.Call(h, uintptr(unsafe.Pointer(&raw)))
	if ok == 0 {
		if e != syscall.Errno(0) {
			return ProcessIOCounters{}, e
		}
		return ProcessIOCounters{}, errors.New("GetProcessIoCounters failed")
	}
	return ProcessIOCounters{
		ReadOps:    raw.ReadOperationCount,
		WriteOps:   raw.WriteOperationCount,
		OtherOps:   raw.OtherOperationCount,
		ReadBytes:  raw.ReadTransferCount,
		WriteBytes: raw.WriteTransferCount,
		OtherBytes: raw.OtherTransferCount,
	}, nil
}
