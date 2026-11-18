package cassandradht

import (
	"context"
	"testing"

	"dhtbacked/internal/hashing"
)

// queueRows returns a Seeder whose channel already holds rows and is closed,
// so drain runs to completion without a Cassandra session.
func queueRows(payloadSizes []int) *Seeder {
	s := &Seeder{ctx: context.Background(), rows: make(chan seedRow, len(payloadSizes))}
	for index, size := range payloadSizes {
		var key hashing.Digest
		key[0], key[1] = byte(index), byte(index>>8)
		s.rows <- seedRow{key: key, payload: make([]byte, size)}
	}
	close(s.rows)
	return s
}

func TestSeederDrainBoundsEveryBatchAndKeepsEveryRow(t *testing.T) {
	sizes := make([]int, 0, 200)
	for index := 0; index < 200; index++ {
		sizes = append(sizes, 100+index*13%5000) // some batches hit the byte bound
	}
	sizes = append(sizes, 40<<10, 10) // a record above the batch byte bound
	s := queueRows(sizes)
	total := 0
	s.drain(func(batch []seedRow) {
		bytes := 0
		for _, row := range batch {
			bytes += len(row.key) + len(row.payload)
		}
		if len(batch) == 0 || len(batch) > seedBatchRows {
			t.Fatalf("batch of %d rows, want 1..%d", len(batch), seedBatchRows)
		}
		if len(batch) > 1 && bytes > seedBatchBytes {
			t.Fatalf("batch of %d rows is %d bytes, over the %d bound", len(batch), bytes, seedBatchBytes)
		}
		for _, row := range batch {
			want := byte(total)
			if row.key[0] != want {
				t.Fatalf("row %d out of order", total)
			}
			total++
		}
	})
	if total != len(sizes) {
		t.Fatalf("drain wrote %d rows, queued %d", total, len(sizes))
	}
}

func TestSeederDrainBatchesQueuedRows(t *testing.T) {
	s := queueRows(make([]int, 100))
	batches := 0
	s.drain(func([]seedRow) { batches++ })
	// 100 small rows already queued must go out in about 100/seedBatchRows
	// round trips, not one per row.
	if want := (100 + seedBatchRows - 1) / seedBatchRows; batches != want {
		t.Fatalf("100 queued rows took %d batches, want %d", batches, want)
	}
}
