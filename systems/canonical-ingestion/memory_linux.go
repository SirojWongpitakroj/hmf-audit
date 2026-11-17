//go:build linux

package canonicalingestion

import (
	"os"
	"strconv"
	"strings"
)

func residentBytes() (uint64, bool) {
	b, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(b))
	if len(fields) < 2 {
		return 0, false
	}
	pages, err := strconv.ParseUint(fields[1], 10, 64)
	return pages * uint64(os.Getpagesize()), err == nil
}
