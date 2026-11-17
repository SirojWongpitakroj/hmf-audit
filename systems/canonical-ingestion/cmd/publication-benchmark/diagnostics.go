package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

type walSample struct {
	Records, FullPageImages, Bytes, Writes, Syncs int64
	WriteMilliseconds, SyncMilliseconds           float64
	Error                                         string `json:"error,omitempty"`
}

func readWAL(ctx context.Context, conn *pgx.Conn) walSample {
	var s walSample
	err := conn.QueryRow(ctx, `SELECT wal_records::bigint,wal_fpi::bigint,wal_bytes::bigint,wal_write::bigint,wal_sync::bigint,wal_write_time,wal_sync_time FROM pg_stat_wal`).Scan(&s.Records, &s.FullPageImages, &s.Bytes, &s.Writes, &s.Syncs, &s.WriteMilliseconds, &s.SyncMilliseconds)
	if err != nil {
		s.Error = err.Error()
	}
	return s
}
func walDelta(before, after walSample) walSample {
	if before.Error != "" {
		return before
	}
	if after.Error != "" {
		return after
	}
	d := walSample{Records: after.Records - before.Records, FullPageImages: after.FullPageImages - before.FullPageImages, Bytes: after.Bytes - before.Bytes, Writes: after.Writes - before.Writes, Syncs: after.Syncs - before.Syncs, WriteMilliseconds: after.WriteMilliseconds - before.WriteMilliseconds, SyncMilliseconds: after.SyncMilliseconds - before.SyncMilliseconds}
	if d.Records < 0 || d.Bytes < 0 || d.Writes < 0 || d.Syncs < 0 {
		d.Error = "WAL statistics reset during trial; delta invalid"
	}
	return d
}

type resources struct {
	SampledPeakRSSBytes, SampledPeakHeapBytes, TotalAllocatedBytes uint64
	GCCycles                                                       uint32
	GCPauseMilliseconds                                            float64
	WaitSamples                                                    map[string]int
	WaitErrors                                                     int
	RSSAvailable                                                   bool
}

func currentRSS() (uint64, bool) {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			f := strings.Fields(line)
			if len(f) >= 2 {
				n, err := strconv.ParseUint(f[1], 10, 64)
				return n * 1024, err == nil
			}
		}
	}
	return 0, false
}

// Wait sampling is approximate and observational. It uses its own read-only
// connection and never resets database statistics or infrastructure caches.
func startResources(ctx context.Context, observer *pgx.Conn, pid uint32, interval time.Duration) func() resources {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	var mu sync.Mutex
	r := resources{WaitSamples: map[string]int{}}
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	sampleMemory := func() {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		rss, available := currentRSS()
		mu.Lock()
		defer mu.Unlock()
		r.SampledPeakHeapBytes = max(r.SampledPeakHeapBytes, m.HeapAlloc)
		r.SampledPeakRSSBytes = max(r.SampledPeakRSSBytes, rss)
		r.RSSAvailable = r.RSSAvailable || available
	}
	sampleMemory()
	go func() {
		defer close(done)
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				sampleMemory()
				qctx, stop := context.WithTimeout(ctx, min(interval, time.Second))
				var state string
				var kind, event *string
				err := observer.QueryRow(qctx, `SELECT state,wait_event_type,wait_event FROM pg_stat_activity WHERE pid=$1`, int64(pid)).Scan(&state, &kind, &event)
				stop()
				mu.Lock()
				if err != nil {
					r.WaitErrors++
				} else {
					key := state
					if kind != nil {
						key += "/" + *kind
					}
					if event != nil {
						key += "/" + *event
					}
					r.WaitSamples[key]++
				}
				mu.Unlock()
			}
		}
	}()
	return func() resources {
		cancel()
		<-done
		sampleMemory()
		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		mu.Lock()
		defer mu.Unlock()
		r.TotalAllocatedBytes = after.TotalAlloc - before.TotalAlloc
		r.GCCycles = after.NumGC - before.NumGC
		r.GCPauseMilliseconds = float64(after.PauseTotalNs-before.PauseTotalNs) / 1e6
		return r
	}
}

func explainInsert(ctx context.Context, admin *pgx.Conn, schema string, entries [][]any) (map[string]any, error) {
	if err := resetScratch(ctx, admin, schema); err != nil {
		return nil, err
	}
	tx, err := admin.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var sql strings.Builder
	sql.WriteString("EXPLAIN (ANALYZE,BUFFERS,WAL,FORMAT JSON) INSERT INTO " + pgx.Identifier{schema, "entries"}.Sanitize() + "(stream_id,ordinal,source_id,payload,payload_hash,prefix_hash) VALUES ")
	var args []any
	for i, row := range entries {
		if i > 0 {
			sql.WriteByte(',')
		}
		sql.WriteByte('(')
		for j, v := range row {
			if j > 0 {
				sql.WriteByte(',')
			}
			fmt.Fprintf(&sql, "$%d", len(args)+1)
			args = append(args, v)
		}
		sql.WriteByte(')')
	}
	var raw []byte
	if err = tx.QueryRow(ctx, sql.String(), args...).Scan(&raw); err != nil {
		return nil, err
	}
	if err = tx.Rollback(ctx); err != nil {
		return nil, err
	}
	if err = verifyRows(ctx, admin, schema, nil); err != nil {
		return nil, err
	}
	return map[string]any{"rows": len(entries), "plan": json.RawMessage(raw), "rolled_back": true}, nil
}
