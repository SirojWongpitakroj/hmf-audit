package canonicalingestion

import "sync/atomic"

// Native adapter work, not committed progress or elapsed wall-clock ingestion.
// Six fixed counters: the first five partition position; segment_start overlaps.
type SegmentPositionCounters struct {
	Records, BlockBuilds, NativeNanos, IndexBuildNanos, SQLCalls atomic.Uint64
}
type SegmentPositionSnapshot struct {
	Band                                                         string
	Records, BlockBuilds, NativeNanos, IndexBuildNanos, SQLCalls uint64
	NativeRecordsPerSecond                                       float64
}

func SegmentPositionBand(position uint64) int {
	switch {
	case position <= 128:
		return 0
	case position <= 256:
		return 1
	case position <= 512:
		return 2
	case position <= 768:
		return 3
	default:
		return 4
	}
}
func (m *AccessMetrics) ObserveSegmentPosition(position, records, blocks, nativeNanos, indexNanos, sqlCalls uint64) {
	add := func(i int) {
		p := &m.SegmentPositions[i]
		p.Records.Add(records)
		p.BlockBuilds.Add(blocks)
		p.NativeNanos.Add(nativeNanos)
		p.IndexBuildNanos.Add(indexNanos)
		p.SQLCalls.Add(sqlCalls)
	}
	add(SegmentPositionBand(position))
	if position == 1 {
		add(5)
	}
}
func (m *AccessMetrics) segmentPositions() (out [6]SegmentPositionSnapshot) {
	names := [6]string{"1-128", "129-256", "257-512", "513-768", "769+", "segment_start"}
	for i := range out {
		p := &m.SegmentPositions[i]
		out[i] = SegmentPositionSnapshot{Band: names[i], Records: p.Records.Load(), BlockBuilds: p.BlockBuilds.Load(), NativeNanos: p.NativeNanos.Load(), IndexBuildNanos: p.IndexBuildNanos.Load(), SQLCalls: p.SQLCalls.Load()}
		if out[i].NativeNanos > 0 {
			out[i].NativeRecordsPerSecond = float64(out[i].Records) * 1e9 / float64(out[i].NativeNanos)
		}
	}
	return
}

func (m *AccessMetrics) ObserveSegmentHashCacheBytes(bytes int64) {
	m.SegmentHashCacheBytes.Store(bytes)
	peak(&m.PeakSegmentHashCacheBytes, bytes)
}
