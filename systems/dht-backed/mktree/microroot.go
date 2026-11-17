package mktree

import "dhtbacked/internal/hashing"

// DefaultWindow is the default tumbling aggregation window W = 1,024 records
// (Q21, Q54).
const DefaultWindow = uint64(1024)

// WindowID returns windowID = floor(leafIndex / W): a zero-based uint64
// counter local to each (epoch, log_type) tree, giving count-based,
// non-overlapping tumbling windows (Q21, Q49). Overlapping sliding windows are
// not implemented.
func WindowID(leafIndex, window uint64) uint64 { return leafIndex / window }

// WindowStart returns i_min = windowID * W for a window (Q49).
func WindowStart(windowID, window uint64) uint64 { return windowID * window }

// MicroRoot computes R^(delta)_tau = H(d_i1 || d_i2 || ... || d_ik) with no
// domain-separation tag, exactly as Eq. (28) is written (R-RTV-01, R-CRY-02).
//
// Only digests of real records participate: the H(0x00 || 0x00) dummy padding
// leaves never enter a micro-root (Q53b). The value therefore covers k real
// records with k <= W and does not depend on tree padding.
func MicroRoot(digests []hashing.Digest) hashing.Digest {
	parts := make([][]byte, 0, len(digests))
	for i := range digests {
		parts = append(parts, digests[i][:])
	}
	return hashing.H(parts...)
}
