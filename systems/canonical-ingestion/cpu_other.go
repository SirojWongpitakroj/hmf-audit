//go:build !linux && !windows

package canonicalingestion

func processCPUSeconds() (float64, bool) { return 0, false }
