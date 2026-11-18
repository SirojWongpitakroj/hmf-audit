package benchmark

import (
	"os"
	"runtime"
	"strconv"
)

// raiseFairSetupProcs lets setup use every CPU and returns a function that
// restores the previous GOMAXPROCS. Call it as `defer raiseFairSetupProcs()()`
// at the start of fixture seeding: seeding is outside every timer, while the
// timed requests keep the benchmark's GOMAXPROCS. FAIR_SETUP_GOMAXPROCS
// overrides the CPU count (a positive integer); an invalid value is ignored.
func raiseFairSetupProcs() func() {
	procs := runtime.NumCPU()
	if value := os.Getenv("FAIR_SETUP_GOMAXPROCS"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			procs = parsed
		}
	}
	previous := runtime.GOMAXPROCS(procs)
	return func() { runtime.GOMAXPROCS(previous) }
}
