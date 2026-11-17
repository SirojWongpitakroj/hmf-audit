//go:build linux

package canonicalingestion

import (
	"syscall"
)

func processCPUSeconds() (float64, bool) {
	var r syscall.Rusage
	err := syscall.Getrusage(syscall.RUSAGE_SELF, &r)
	return float64(r.Utime.Sec+r.Stime.Sec) + float64(r.Utime.Usec+r.Stime.Usec)/1e6, err == nil
}
