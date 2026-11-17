// Package omt implements the Object Merkle Tree (IV-A, Fig. 2): a modified,
// fixed-fanout Merkle tree whose leaves bind an object identifier (oid) to
// an object hash, sorted ascending by oid (R-IOMF-001..006).
//
// Hash formulas (DECISIONS.md A6, and the extension recorded in
// TRACKER.md "Concrete encoding decisions"):
//   - leaf:     H(oid ‖ H(o))
//   - internal: H(h_c1 ‖ ... ‖ h_ck) for the k <= F children actually
//     present (A7: one incomplete rightmost node permitted per level, no
//     padding).
//
// Fanout F is a construction parameter (R-IOMF-011, R-GO-018) — never
// hard-coded to 2 or to the experiment value 4.
package omt

import (
	"errors"
	"fmt"
	"sort"

	"blas/bhash"
)

// Leaf is one (oid, object) pair before insertion into the tree. Object is
// the caller-supplied, already-deterministically-encoded byte representation
// of the object (per the field-order rule in TRACKER.md, A5) — the tree
// itself is agnostic to what the object actually is (LogEntry, Transaction,
// ...).
type Leaf struct {
	OID    uint64
	Object []byte
}

type node struct {
	oid     uint64 // meaningful only at leaf level
	object  []byte // meaningful only at leaf level
	objHash bhash.Digest
	hash    bhash.Digest
}

// Tree is a built, immutable OMT over a complete object set (R-IOMF-006: the
// paper describes no incremental OMT insertion; entry-level OMTs are built
// per immutable file, transaction-level OMTs per immutable block).
type Tree struct {
	fanout int
	levels [][]node // levels[0] = leaves, ascending oid; last level has exactly one node (the root), unless there are zero leaves
}

// ErrEmpty is returned when a tree is built with no leaves and the caller
// asks for a root or a proof.
var ErrEmpty = errors.New("omt: tree has no leaves")

// ErrDuplicateOID is returned when two leaves share an oid.
var ErrDuplicateOID = errors.New("omt: duplicate oid")

// Build constructs the OMT with the given fanout F (F >= 2) over the given
// leaves. Leaves are sorted ascending by oid regardless of input order
// (R-IOMF-001, T-001): building from a shuffled input yields an identical
// root to building from sorted input.
func Build(fanout int, leaves []Leaf) (*Tree, error) {
	if fanout < 2 {
		return nil, fmt.Errorf("omt: fanout must be >= 2, got %d", fanout)
	}
	sorted := make([]Leaf, len(leaves))
	copy(sorted, leaves)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].OID < sorted[j].OID })
	for i := 1; i < len(sorted); i++ {
		if sorted[i].OID == sorted[i-1].OID {
			return nil, fmt.Errorf("%w: %d", ErrDuplicateOID, sorted[i].OID)
		}
	}

	leafNodes := make([]node, len(sorted))
	for i, l := range sorted {
		objHash := bhash.H(l.Object)
		leafHash := bhash.HashConcat(bhash.Uint64BE(l.OID), bhash.DigestBytes(objHash))
		leafNodes[i] = node{oid: l.OID, object: l.Object, objHash: objHash, hash: leafHash}
	}

	t := &Tree{fanout: fanout}
	if len(leafNodes) == 0 {
		return t, nil
	}
	t.levels = append(t.levels, leafNodes)

	cur := leafNodes
	for len(cur) > 1 {
		next := buildLevel(cur, fanout)
		t.levels = append(t.levels, next)
		cur = next
	}
	return t, nil
}

func buildLevel(cur []node, fanout int) []node {
	numGroups := (len(cur) + fanout - 1) / fanout
	next := make([]node, numGroups)
	for g := 0; g < numGroups; g++ {
		start := g * fanout
		end := start + fanout
		if end > len(cur) {
			end = len(cur)
		}
		parts := make([][]byte, 0, end-start)
		for i := start; i < end; i++ {
			parts = append(parts, bhash.DigestBytes(cur[i].hash))
		}
		next[g] = node{hash: bhash.HashConcat(parts...)}
	}
	return next
}

// Root returns the OMT root hash.
func (t *Tree) Root() (bhash.Digest, error) {
	if len(t.levels) == 0 {
		return bhash.Digest{}, ErrEmpty
	}
	top := t.levels[len(t.levels)-1]
	return top[0].hash, nil
}

// NumLeaves returns the number of leaves in the tree.
func (t *Tree) NumLeaves() int {
	if len(t.levels) == 0 {
		return 0
	}
	return len(t.levels[0])
}

// Fanout returns the tree's construction-time fanout F.
func (t *Tree) Fanout() int { return t.fanout }

// ProofStep is one level of an OMT membership proof: the hashes of every
// other child in the same internal-node group, in left-to-right order, plus
// the explicit position (0-based) of the path node among them (R-GO-019 —
// proofs are ordered sequences with explicit position, never a bare hash
// set).
type ProofStep struct {
	ChildIndex    int
	SiblingHashes []bhash.Digest // all children of this group except the path node, left-to-right, with a gap at ChildIndex
}

// Proof is an OMT membership proof for one oid (R-IOMF-003).
type Proof struct {
	OID    uint64
	Object []byte
	Steps  []ProofStep
}

// IsRightmost reports whether the proved leaf is the rightmost (largest
// oid) leaf in the whole tree, purely from the proof's own shape: at every
// level, the leaf/node lies at the last position among the children
// actually present in its group (ChildIndex == len(SiblingHashes), i.e. no
// sibling occupies a position to its right). This gives a self-contained,
// cryptographically checked "extent" proof — used to authenticate a
// file's entry count (C5 in DECISIONS.md: N must come from the
// authenticated object population, e.g. OMT extent evidence) — without a
// separate non-existence lookup.
func (p Proof) IsRightmost() bool {
	for _, step := range p.Steps {
		if step.ChildIndex != len(step.SiblingHashes) {
			return false
		}
	}
	return true
}

// Prove returns the membership proof for oid, or (Proof{}, false) if oid is
// not present in the tree.
func (t *Tree) Prove(oid uint64) (Proof, bool) {
	idx, ok := t.leafIndex(oid)
	if !ok {
		return Proof{}, false
	}
	return t.proveAt(idx), true
}

func (t *Tree) leafIndex(oid uint64) (int, bool) {
	if len(t.levels) == 0 {
		return 0, false
	}
	leaves := t.levels[0]
	idx := sort.Search(len(leaves), func(i int) bool { return leaves[i].oid >= oid })
	if idx < len(leaves) && leaves[idx].oid == oid {
		return idx, true
	}
	return 0, false
}

func (t *Tree) proveAt(leafIdx int) Proof {
	leaf := t.levels[0][leafIdx]
	p := Proof{OID: leaf.oid, Object: leaf.object}
	idx := leafIdx
	for lvl := 0; lvl < len(t.levels)-1; lvl++ {
		cur := t.levels[lvl]
		groupStart := (idx / t.fanout) * t.fanout
		groupEnd := groupStart + t.fanout
		if groupEnd > len(cur) {
			groupEnd = len(cur)
		}
		step := ProofStep{ChildIndex: idx - groupStart}
		for i := groupStart; i < groupEnd; i++ {
			if i == idx {
				continue
			}
			step.SiblingHashes = append(step.SiblingHashes, cur[i].hash)
		}
		p.Steps = append(p.Steps, step)
		idx = idx / t.fanout
	}
	return p
}

// Verify reconstructs the root from a proof and reports whether it equals
// the given committed root (R-IOMF-004): accept iff equal.
func Verify(root bhash.Digest, p Proof) bool {
	objHash := bhash.H(p.Object)
	cur := bhash.HashConcat(bhash.Uint64BE(p.OID), bhash.DigestBytes(objHash))
	for _, step := range p.Steps {
		total := len(step.SiblingHashes) + 1
		if step.ChildIndex < 0 || step.ChildIndex >= total {
			return false
		}
		ordered := make([]bhash.Digest, total)
		si := 0
		for i := 0; i < total; i++ {
			if i == step.ChildIndex {
				ordered[i] = cur
			} else {
				ordered[i] = step.SiblingHashes[si]
				si++
			}
		}
		parts := make([][]byte, total)
		for i, h := range ordered {
			parts[i] = bhash.DigestBytes(h)
		}
		cur = bhash.HashConcat(parts...)
	}
	return cur == root
}

// LeafOIDs returns the ascending list of oids present in the tree (useful
// for adjacency/boundary evidence at query time).
func (t *Tree) LeafOIDs() []uint64 {
	if len(t.levels) == 0 {
		return nil
	}
	out := make([]uint64, len(t.levels[0]))
	for i, n := range t.levels[0] {
		out[i] = n.oid
	}
	return out
}

// Neighbors returns boundary evidence for a non-existent oid (R-IOMF-005):
// the proof of the nearest existing leaf with oid strictly less than the
// queried one (leftProof, ok) and the nearest existing leaf with oid
// strictly greater (rightProof, ok). At a domain edge the non-existent side
// is reported via ok=false and no proof is fabricated for it (B5 ruling in
// DECISIONS.md).
func (t *Tree) Neighbors(oid uint64) (left Proof, leftOK bool, right Proof, rightOK bool) {
	if len(t.levels) == 0 {
		return Proof{}, false, Proof{}, false
	}
	leaves := t.levels[0]
	idx := sort.Search(len(leaves), func(i int) bool { return leaves[i].oid >= oid })
	if idx-1 >= 0 {
		left = t.proveAt(idx - 1)
		leftOK = true
	}
	// idx now points at the first leaf with oid >= queried oid. If that
	// leaf's oid equals the queried oid, it's not a "neighbor" of a
	// non-existent value — but callers use Neighbors only when Prove
	// already reported non-existence, so idx's oid, if present, is > oid.
	if idx < len(leaves) {
		right = t.proveAt(idx)
		rightOK = true
	}
	return left, leftOK, right, rightOK
}
