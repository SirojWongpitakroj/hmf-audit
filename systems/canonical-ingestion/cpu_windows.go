//go:build windows

package canonicalingestion

import (
	"syscall"
	"unsafe"
)

var getProcessTimes = syscall.NewLazyDLL("kernel32.dll").NewProc("GetProcessTimes")

func processCPUSeconds() (float64, bool) {
	var created, exited, kernel, user syscall.Filetime
	ok, _, _ := getProcessTimes.Call(^uintptr(0), uintptr(unsafe.Pointer(&created)), uintptr(unsafe.Pointer(&exited)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	k := uint64(kernel.HighDateTime)<<32 | uint64(kernel.LowDateTime)
	u := uint64(user.HighDateTime)<<32 | uint64(user.LowDateTime)
	return float64(k+u) / 1e7, ok != 0
}
