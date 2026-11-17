package canonicalingestion

import "testing"

func TestSegmentPositionBandsAndStartOverlap(t *testing.T) {
	for _, tc := range []struct {
		position uint64
		band     int
	}{{1, 0}, {128, 0}, {129, 1}, {256, 1}, {257, 2}, {512, 2}, {513, 3}, {768, 3}, {769, 4}, {1024, 4}, {1025, 4}} {
		if got := SegmentPositionBand(tc.position); got != tc.band {
			t.Fatal(tc, got)
		}
	}
	var m AccessMetrics
	m.ObserveSegmentPosition(1, 10, 1, 2e9, 1e9, 7)
	m.ObserveSegmentPosition(129, 5, 1, 1e9, 5e8, 4)
	s := m.Snapshot().SegmentPositions
	if s[0].Records != 10 || s[0].NativeRecordsPerSecond != 5 || s[5].Records != 10 || s[5].BlockBuilds != 1 || s[1].Records != 5 || s[2].Records != 0 {
		t.Fatal(s)
	}
}
