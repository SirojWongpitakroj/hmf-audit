// Package merkle implements MHTGen, the Merkle hash tree construction over a
// LogCacheQueue (DATA-008, DATA-009; §3.2(2) p. 3288; §3.3 stage 2 p. 3289).
//
// Construction, per the frozen GAP-01/GAP-74 ruling (implementation decision,
// not paper-mandated — the paper only shows a 4-leaf example in Fig. 2):
//   - leaves = the psi digests as-is, in record order (NOT re-hashed)
//   - internal node = SHA-256(left || right), over raw 32-byte digests
//   - at any level with an odd node count, the last node is duplicated
//   - no domain separation between leaf and internal-node hashing
//   - a single-leaf tree's root is the leaf itself; H(leaf||leaf) is computed
//     only when a node genuinely has two children (GAP-74)
//
// This package must not contain incremental or cached trees, inclusion
// proofs, multiproofs, or any alternative tree layout (SRS Section 26, item 2).
package merkle

import "delia/hash"

// MHTGen builds the Merkle hash tree over leaves (in the given order) and
// returns its root. leaves must be non-empty; MHTGen panics on an empty
// slice, since Eq. (1) never produces an empty LogCacheQueue (theta >= 1 is
// enforced by DATA-007/GAP-55 configuration validation upstream).
//
// MHTGen is deterministic and is recomputed fresh on every call (no caching).
func MHTGen(leaves []hash.Digest) hash.Digest {
	if len(leaves) == 0 {
		panic("merkle: MHTGen requires at least one leaf")
	}
	level := make([]hash.Digest, len(leaves))
	copy(level, leaves)

	for len(level) > 1 {
		next := make([]hash.Digest, 0, (len(level)+1)/2)
		for i := 0; i < len(level); i += 2 {
			left := level[i]
			right := left
			if i+1 < len(level) {
				right = level[i+1]
			}
			next = append(next, hash.H(hash.Concat(left[:], right[:])))
		}
		level = next
	}
	return level[0]
}
