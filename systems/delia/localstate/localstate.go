// Package localstate implements Eq. (2), the LocalState hash chain
// (DATA-010, DATA-011, DATA-012; §3.2(2) p. 3288).
//
//	li == 0:  omega_{pi,0}  = H(root_{pi,0})
//	li  > 0:  omega_{pi,li} = H(root_{pi,li} || omega_{pi,li-1})
//
// Per the frozen GAP-02 ruling: H's input is the raw 32-byte root (li == 0),
// or the raw 64-byte concatenation root || omega_prev, root first (li > 0).
// This governs over the Fig. 2 operand order (INC-01).
package localstate

import "delia/hash"

// Compute returns the LocalState for a batch whose Merkle root is root. prev
// is the previous batch's LocalState (omega_{pi,li-1}); pass nil for the
// li == 0 branch. This package caches nothing beyond what Eq. (2) itself
// carries forward (the single prev value).
func Compute(root hash.Digest, prev *hash.Digest) hash.Digest {
	if prev == nil {
		return hash.H(root[:])
	}
	return hash.H(hash.Concat(root[:], prev[:]))
}
