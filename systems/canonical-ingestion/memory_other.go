//go:build !windows && !linux

package canonicalingestion

func residentBytes() (uint64, bool) { return 0, false }
