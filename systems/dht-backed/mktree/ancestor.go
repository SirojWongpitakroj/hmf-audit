package mktree

import (
	"fmt"

	"dhtbacked/dht"
	"dhtbacked/internal/hashing"
)

// AncestorCount returns 2^s, the number of level-s ancestor checkpoints
// A_s,k for k in {0, ..., 2^s - 1} (Eq. (4), R-ANC-01).
//
// Because the root is level j = 0 and the leaves are level j = L, a larger s
// places the checkpoints closer to the leaves and increases their number
// (Q25). Sec. V.C.5's contrary causal explanation is not reproduced.
func (t *Tree) AncestorCount() uint64 { return uint64(1) << t.stride }

// Ancestor returns A_s,k = N_s,k (Eq. (4)).
func (t *Tree) Ancestor(k uint64) (hashing.Digest, error) {
	if k >= t.AncestorCount() {
		return hashing.Digest{}, fmt.Errorf("mktree: ancestor index %d is outside {0, ..., %d}", k, t.AncestorCount()-1)
	}
	return t.nodes[t.stride][k], nil
}

// CoveringAncestor returns the index k of the level-s ancestor whose subtree
// contains the given leaf index.
func (t *Tree) CoveringAncestor(leafIndex uint64) uint64 {
	return CoveringAncestorIndex(leafIndex, t.levels, t.stride)
}

// CoveringAncestorIndex is CoveringAncestor as a free function, for verifiers
// that know only (leafIndex, L, s).
func CoveringAncestorIndex(leafIndex, levels, stride uint64) uint64 {
	return leafIndex >> (levels - stride)
}

// AncestorRange returns the leaf-index range [i_min, i_max] covered by the
// level-s ancestor k (Q17, R-ANC-02). Timestamps are never substituted here.
func AncestorRange(k, levels, stride uint64) (iMin, iMax uint64) {
	span := uint64(1) << (levels - stride)
	iMin = k * span
	return iMin, iMin + span - 1
}

// UpProof returns upProof_s,k: the minimal set of sibling hashes required to
// recompute the path A_s,k => R^(e)_tau (R-ANC-03).
//
// The result holds exactly s sibling hashes, ordered from level s upward.
func (t *Tree) UpProof(k uint64) ([]hashing.Digest, error) {
	if k >= t.AncestorCount() {
		return nil, fmt.Errorf("mktree: ancestor index %d is outside the stride level", k)
	}
	out := make([]hashing.Digest, 0, t.stride)
	idx := k
	for j := t.stride; j >= 1; j-- {
		out = append(out, t.nodes[j][idx^1])
		idx >>= 1
	}
	return out, nil
}

// SiblingPath returns the sibling hashes on the path from a leaf up to its
// level-s ancestor, ordered from the leaf level upward. It holds exactly
// L - s entries.
//
// The auditor-side verifier does not use this: it retrieves each sibling from
// the DHT as a P_j,k record and recomputes every parent itself (Q18). This
// method exists so the partitioner and tests can address the same nodes.
func (t *Tree) SiblingPath(leafIndex uint64) ([]hashing.Digest, error) {
	if leafIndex >= t.width {
		return nil, fmt.Errorf("mktree: leaf index %d is outside the tree", leafIndex)
	}
	out := make([]hashing.Digest, 0, t.levels-t.stride)
	idx := leafIndex
	for j := t.levels; j > t.stride; j-- {
		out = append(out, t.nodes[j][idx^1])
		idx >>= 1
	}
	return out, nil
}

// FoldUp recomputes a node value upward from start, consuming siblings in the
// order produced by UpProof or SiblingPath. index is the position of start at
// its own level; levels are walked one at a time toward the root.
//
// Each parent is recomputed with the left/right ordering of Eq. (3): a node at
// an even index is the left child, at an odd index the right child.
//
// If count is non-nil it is incremented once per parent recomputation, which
// is the quantity Theorem G.1 bounds.
func FoldUp(start hashing.Digest, index uint64, siblings []hashing.Digest, count *uint64) hashing.Digest {
	cur := start
	idx := index
	for _, sib := range siblings {
		if idx%2 == 0 {
			cur = Internal(cur, sib)
		} else {
			cur = Internal(sib, cur)
		}
		if count != nil {
			*count++
		}
		idx >>= 1
	}
	return cur
}

// Certificate builds the ancestor certificate C_s,k of Eq. (5)/(6)/(23) for
// ancestor index k at the given commit sequence.
//
// Certificates are not cryptographically signed (Q4, R-ANC-05); their
// authenticity rests on recomputation against the on-chain anchor.
func (t *Tree) Certificate(k, commitSeq uint64) (dht.AncestorCert, error) {
	a, err := t.Ancestor(k)
	if err != nil {
		return dht.AncestorCert{}, err
	}
	up, err := t.UpProof(k)
	if err != nil {
		return dht.AncestorCert{}, err
	}
	iMin, iMax := AncestorRange(k, t.levels, t.stride)
	return dht.AncestorCert{
		AncestorID:   dht.AncestorID(t.treeID, t.stride, k),
		TreeID:       t.treeID,
		Level:        t.stride,
		Index:        k,
		AncestorHash: a,
		UpProof:      up,
		IMin:         iMin,
		IMax:         iMax,
		CommitSeq:    commitSeq,
	}, nil
}
