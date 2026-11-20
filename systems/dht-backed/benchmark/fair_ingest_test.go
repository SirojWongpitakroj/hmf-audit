package benchmark

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"dhtbacked/storage"
)

type fakeRecordWriter struct {
	mu      sync.Mutex
	records []storage.Record
}

func (w *fakeRecordWriter) CopyRecords(_ context.Context, records []storage.Record) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.records = append(w.records, records...)
	return nil
}

func writeFairDHTTestDataset(t *testing.T, path string, regions []string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	writer := bufio.NewWriter(file)
	for index, region := range regions {
		line, _ := json.Marshal(fairDHTDatasetRecord{
			SourceID: fmt.Sprintf("app_%d/container_%d.log:%d", index%17, index%5, index),
			RawLog:   fmt.Sprintf("17/06/09 20:10:%02d INFO record %d", index%60, index),
			RegionID: region, TenantID: fmt.Sprintf("T%d", index%9),
		})
		writer.Write(append(line, '\n'))
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
}

// The parallel ingest must store exactly what a one-record-at-a-time loop
// stores: the same records in dataset order with the same log types and
// per-type leaf indices (ciphertexts differ only by their random nonces).
func TestFairDHTParallelIngestKeepsDatasetOrder(t *testing.T) {
	const total = 3*fairDHTIngestChunkLines + 77 // several chunks, a partial last one
	dir := t.TempDir()
	regions := make([]string, total)
	for index := range regions {
		regions[index] = fmt.Sprintf("R%d", index*4/total)
	}
	dataset := filepath.Join(dir, "dataset.jsonl")
	writeFairDHTTestDataset(t, dataset, regions)
	authorities, err := newFairDHTAuthorities(filepath.Join(dir, "work"))
	if err != nil {
		t.Fatal(err)
	}

	// Reference: the sequential rule.
	type want struct {
		id, logType string
		leaf        uint64
		producer    string
		timestamp   int64
	}
	var reference []want
	refCounts := map[string]uint64{}
	file, _ := os.Open(dataset)
	scanner := bufio.NewScanner(file)
	for index := uint64(0); scanner.Scan(); index++ {
		record, _, err := prepareFairDHTRecord(scanner.Bytes(), index, authorities)
		if err != nil {
			t.Fatal(err)
		}
		reference = append(reference, want{record.RecordID, record.LogType, refCounts[record.LogType],
			record.ProducerID, record.TimestampNanos})
		refCounts[record.LogType]++
	}
	file.Close()

	for _, procs := range []int{1, 4} {
		previous := runtime.GOMAXPROCS(procs)
		spool, err := newFairDigestSpool(filepath.Join(dir, fmt.Sprintf("spool-%d", procs)), []string{"access", "api", "auth", "error", "security"})
		if err != nil {
			t.Fatal(err)
		}
		writer := &fakeRecordWriter{}
		counts, err := ingestFairDHTDataset(context.Background(), dataset, total, authorities, writer, spool)
		runtime.GOMAXPROCS(previous)
		if err != nil {
			t.Fatal(err)
		}
		if err := spool.Close(); err != nil {
			t.Fatal(err)
		}
		if len(writer.records) != total {
			t.Fatalf("procs=%d: stored %d records, want %d", procs, len(writer.records), total)
		}
		for index, record := range writer.records {
			w := reference[index]
			if record.RecordID != w.id || record.LogType != w.logType || record.LeafIndex != w.leaf ||
				record.ProducerID != w.producer || record.TimestampNanos != w.timestamp {
				t.Fatalf("procs=%d: record %d = %s/%s/leaf %d, want %s/%s/leaf %d", procs, index,
					record.RecordID, record.LogType, record.LeafIndex, w.id, w.logType, w.leaf)
			}
		}
		for logType, count := range refCounts {
			if counts[logType] != count {
				t.Fatalf("procs=%d: %s has %d leaves, want %d", procs, logType, counts[logType], count)
			}
			info, err := os.Stat(spool.paths[logType])
			if err != nil || info.Size() != int64(count)*32 {
				t.Fatalf("procs=%d: %s spool holds %d bytes, want %d", procs, logType, info.Size(), count*32)
			}
		}
	}

	// Regions out of order are still rejected.
	regions[total/2] = "R0"
	writeFairDHTTestDataset(t, dataset, regions)
	spool, err := newFairDigestSpool(filepath.Join(dir, "spool-bad"), []string{"access", "api", "auth", "error", "security"})
	if err != nil {
		t.Fatal(err)
	}
	defer spool.Close()
	if _, err := ingestFairDHTDataset(context.Background(), dataset, total, authorities, &fakeRecordWriter{}, spool); err == nil ||
		!strings.Contains(err.Error(), "grouped R0, R1") {
		t.Fatalf("out-of-order regions: err = %v", err)
	}
}
