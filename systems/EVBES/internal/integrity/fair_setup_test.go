package integrity

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"testing"
)

const (
	// fairEVBESSeedFlushRows is how many records are written to PostgreSQL per
	// COPY while seeding. A larger batch means fewer network round trips; the
	// stored rows are identical.
	fairEVBESSeedFlushRows = 4096

	// fairEVBESDHTBatchRows caps one unlogged Cassandra batch. It stays at the
	// earlier size because the entries in a batch live in different partitions
	// and Cassandra rejects batches above batch_size_fail_threshold (50 KiB).
	fairEVBESDHTBatchRows = 256

	// fairEVBESSeedWriters is the number of concurrent Cassandra writers while
	// seeding the DHT. It is deliberately independent of
	// FAIR_EVSEB_DHT_CONCURRENCY, which bounds lookups in timed requests.
	fairEVBESSeedWriters = 32
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

func TestFairEVBESSeedSubmitSplitsIntoBoundedBatches(t *testing.T) {
	entries := make([]fairEVBESDHTEntry, fairEVBESSeedFlushRows+7)
	for index := range entries {
		entries[index].key = strconv.Itoa(index)
	}
	seeder := &fairEVBESDHTSeeder{
		ctx:     context.Background(),
		batches: make(chan []fairEVBESDHTEntry, len(entries)/fairEVBESDHTBatchRows+2),
	}
	if err := seeder.Submit(entries); err != nil {
		t.Fatal(err)
	}
	close(seeder.batches)
	next := 0
	for batch := range seeder.batches {
		if len(batch) == 0 || len(batch) > fairEVBESDHTBatchRows {
			t.Fatalf("batch of %d entries, want 1..%d", len(batch), fairEVBESDHTBatchRows)
		}
		for _, entry := range batch {
			if entry.key != strconv.Itoa(next) {
				t.Fatalf("entry %d out of order: got key %q", next, entry.key)
			}
			next++
		}
	}
	if next != len(entries) {
		t.Fatalf("submitted %d entries, batches carried %d", len(entries), next)
	}
}

func TestRaiseFairSetupProcsRestoresPrevious(t *testing.T) {
	before := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(before)
	t.Setenv("FAIR_SETUP_GOMAXPROCS", "3")
	restore := raiseFairSetupProcs()
	if got := runtime.GOMAXPROCS(0); got != 3 {
		t.Fatalf("setup GOMAXPROCS = %d, want 3", got)
	}
	restore()
	if got := runtime.GOMAXPROCS(0); got != 1 {
		t.Fatalf("GOMAXPROCS after restore = %d, want 1", got)
	}
}
