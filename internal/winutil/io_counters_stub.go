//go:build !windows

package winutil

type ProcessIOCounters struct {
	ReadOps    uint64
	WriteOps   uint64
	OtherOps   uint64
	ReadBytes  uint64
	WriteBytes uint64
	OtherBytes uint64
}

func GetProcessIOCounters() (ProcessIOCounters, error) {
	return ProcessIOCounters{}, nil
}
