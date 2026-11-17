//go:build windows

package canonicalingestion

import (
	"syscall"
	"unsafe"
)

var processMemoryInfo = syscall.NewLazyDLL("psapi.dll").NewProc("GetProcessMemoryInfo")

type memoryCounters struct {
	CB, Faults                                                                               uint32
	Peak, Working, PeakPaged, Paged, PeakNonpaged, Nonpaged, Pagefile, PeakPagefile, Private uintptr
}

func residentBytes() (uint64, bool) {
	var counters memoryCounters
	counters.CB = uint32(unsafe.Sizeof(counters))
	ok, _, _ := processMemoryInfo.Call(^uintptr(0), uintptr(unsafe.Pointer(&counters)), unsafe.Sizeof(counters))
	return uint64(counters.Working), ok != 0
}
