// Package rimbt implements the Range Index Merkle B-Tree (IV-C, Fig. 2): one
// leaf per distinct numerical attribute value, holding a cumulative "value
// <= v" bitmap, in a fixed-fanout Merkle B-tree (R-IOMF-016..021).
//
// Governing rulings (DECISIONS.md):
//   - B2 (GAP-8): the Algorithm 2 box is a publication error; the IV-C prose
//     governs insertion. A non-existent predecessor (inserting below the
//     current minimum) is a virtual all-zero bitmap.
//   - B5 (GAP-11): at a domain edge, omit the non-existent neighbour and
//     authenticate the leftmost/rightmost real leaf; reject alpha > beta;
//     boundary v values are returned in the VO.
//   - B6 (GAP-12): the rightmost leaf is the authenticated upper-universe
//     bitmap used for complement operations at a domain edge.
//   - A7: maximum fanout F; overflow "split" is realized here as a full,
//     canonical, order-independent rebuild of the level structure by
//     grouping the sorted leaf array into chunks of F, left to right, with
//     one incomplete rightmost group permitted. This is adopted, rather
//     than a classic incremental-split B-tree, because (a) R-GO-021
//     explicitly permits any implementation whose committed node contents
//     and hashes match R-IOMF-018, and (b) the system-wide determinism
//     requirement (R-GO-006/007, the T-056 "build determinism" regression)
//     requires that the same final leaf set produce the same root
//     regardless of insertion order; a canonical grouping is
//     order-independent by construction, while a classic incremental split
//     generally is not. Recorded here as an explicit, reasoned
//     interpretation, not a silently invented default.
//
// Hash formulas:
//   - leaf:     H(v ‖ H(bm))                                  (R-CRYPTO-005)
//   - internal: H(V ‖ h_c1 ‖ ... ‖ h_ck), V = {min(V_c2),     (R-CRYPTO-006)
//     ..., min(V_ck)} for the k <= F children actually present.
package rimbt

import (
	"fmt"
	"sort"

	"blas/bhash"
	"blas/bitmap"
)

type leafRec struct {
	v    int64
	bm   bitmap.Bitmap
	hash bhash.Digest
}

func leafHash(v int64, bm bitmap.Bitmap) bhash.Digest {
	inner := bhash.H(bm.Bytes())
	return bhash.HashConcat(bhash.Int64BE(v), bhash.DigestBytes(inner))
}

func encodeV(vs []int64) []byte {
	out := make([]byte, 0, 8*len(vs))
	for _, v := range vs {
		out = append(out, bhash.Int64BE(v)...)
	}
	return out
}

type nodeInfo struct {
	hash     bhash.Digest
	v        []int64 // this node's own V field (empty for leaves)
	minValue int64   // smallest value in this node's subtree (used only during construction)
}

// Tree is a mutable RIMBT over one numerical attribute.
type Tree struct {
	fanout int
	n      uint64
	leaves []leafRec // sorted ascending by v, unique v
	levels [][]nodeInfo
}

// NewEmpty returns a RIMBT with fanout F and N=0.
func NewEmpty(fanout int) (*Tree, error) {
	if fanout < 2 {
		return nil, fmt.Errorf("rimbt: fanout must be >= 2, got %d", fanout)
	}
	return &Tree{fanout: fanout}, nil
}

func (t *Tree) Fanout() int    { return t.fanout }
func (t *Tree) N() uint64      { return t.n }
func (t *Tree) NumLeaves() int { return len(t.leaves) }

// Insert applies the IV-C insertion procedure for one new object (oid must
// equal the current N; objects are inserted in oid order from 0):
//   - if a leaf with v==attr exists at position x, set bit oid in every
//     bitmap from leaf_nodes[x] to the last leaf;
//   - otherwise create a new leaf at its ascending-order position x,
//     initialize its bitmap as a copy of the predecessor's bitmap (or a
//     virtual all-zero bitmap if there is no predecessor, B2), then apply
//     the same suffix update from the new leaf onward.
//
// Deviation D-1 (extended by the same reasoning as iimt.Insert): whenever N
// crosses a byte boundary, every leaf's serialized bitmap bytes change
// length, so every leaf's hash is recomputed, not only the ones whose bit
// changed.
func (t *Tree) Insert(oid uint64, attr int64) error {
	if oid != t.n {
		return fmt.Errorf("rimbt: Insert requires oid==N (sequential); got oid=%d N=%d", oid, t.n)
	}
	newN := oid + 1

	anyLengthChanged := false
	for i := range t.leaves {
		grown, lenChanged := t.leaves[i].bm.Grow(newN)
		t.leaves[i].bm = grown
		if lenChanged {
			anyLengthChanged = true
		}
	}

	x := sort.Search(len(t.leaves), func(i int) bool { return t.leaves[i].v >= attr })
	exact := x < len(t.leaves) && t.leaves[x].v == attr

	if !exact {
		var newBM bitmap.Bitmap
		if x-1 >= 0 {
			newBM = t.leaves[x-1].bm.Clone()
		} else {
			newBM = bitmap.New(newN) // virtual all-zero predecessor (B2)
		}
		newLeaf := leafRec{v: attr, bm: newBM}
		t.leaves = append(t.leaves, leafRec{})
		copy(t.leaves[x+1:], t.leaves[x:])
		t.leaves[x] = newLeaf
	}

	// Suffix update: set bit oid from position x to the last leaf.
	for i := x; i < len(t.leaves); i++ {
		t.leaves[i].bm.Set(oid)
	}

	// Recompute hashes: every leaf in the updated suffix (bit changed) plus,
	// under D-1, every leaf if the byte length changed.
	for i := range t.leaves {
		bitChanged := i >= x
		if bitChanged || anyLengthChanged {
			t.leaves[i].hash = leafHash(t.leaves[i].v, t.leaves[i].bm)
		}
	}

	t.n = newN
	t.rebuild()
	return nil
}

func (t *Tree) rebuild() {
	if len(t.leaves) == 0 {
		t.levels = nil
		return
	}
	level0 := make([]nodeInfo, len(t.leaves))
	for i, l := range t.leaves {
		level0[i] = nodeInfo{hash: l.hash, minValue: l.v}
	}
	levels := [][]nodeInfo{level0}
	cur := level0
	for len(cur) > 1 {
		next := buildLevel(cur, t.fanout)
		levels = append(levels, next)
		cur = next
	}
	t.levels = levels
}

func buildLevel(cur []nodeInfo, fanout int) []nodeInfo {
	numGroups := (len(cur) + fanout - 1) / fanout
	next := make([]nodeInfo, numGroups)
	for g := 0; g < numGroups; g++ {
		start := g * fanout
		end := start + fanout
		if end > len(cur) {
			end = len(cur)
		}
		children := cur[start:end]
		var vArr []int64
		for i := 1; i < len(children); i++ {
			vArr = append(vArr, children[i].minValue)
		}
		parts := [][]byte{encodeV(vArr)}
		for _, c := range children {
			parts = append(parts, bhash.DigestBytes(c.hash))
		}
		next[g] = nodeInfo{hash: bhash.HashConcat(parts...), v: vArr, minValue: children[0].minValue}
	}
	return next
}

// Root returns the RIMBT root hash.
func (t *Tree) Root() (bhash.Digest, error) {
	if len(t.levels) == 0 {
		return bhash.Digest{}, fmt.Errorf("rimbt: tree has no leaves")
	}
	top := t.levels[len(t.levels)-1]
	return top[0].hash, nil
}

// ProofStep is one level of a RIMBT membership proof. SiblingHashes are the
// other children's node hashes in this group, in order, excluding the path
// node (R-GO-019). V is this level's own node content needed to recompute
// its hash: H(V ‖ all children hashes in order) — required at every
// non-leaf level, including the level that yields the root (R-CRYPTO-006).
type ProofStep struct {
	ChildIndex    int
	SiblingHashes []bhash.Digest
	V             []int64
}

// Proof is a RIMBT membership proof for one leaf value v.
type Proof struct {
	V      int64
	Bitmap bitmap.Bitmap
	Steps  []ProofStep
}

func (t *Tree) proveIndex(idx int) Proof {
	leaf := t.leaves[idx]
	p := Proof{V: leaf.v, Bitmap: leaf.bm}
	pos := idx
	for lvl := 0; lvl < len(t.levels)-1; lvl++ {
		cur := t.levels[lvl]
		groupStart := (pos / t.fanout) * t.fanout
		groupEnd := groupStart + t.fanout
		if groupEnd > len(cur) {
			groupEnd = len(cur)
		}
		parentIdx := pos / t.fanout
		// step.V is the V of the PARENT being reconstructed by this step
		// (one level up from `cur`), i.e. the node whose hash formula is
		// H(V ‖ children-in-order). It was computed and stored on the
		// parent node when that level was built.
		parentV := t.levels[lvl+1][parentIdx].v
		step := ProofStep{ChildIndex: pos - groupStart, V: parentV}
		for i := groupStart; i < groupEnd; i++ {
			if i == pos {
				continue
			}
			step.SiblingHashes = append(step.SiblingHashes, cur[i].hash)
		}
		p.Steps = append(p.Steps, step)
		pos = parentIdx
	}
	return p
}

// Verify reconstructs the RIMBT root from a proof and compares it with root.
//
// Each ProofStep.V is the value-set of the node ONE LEVEL ABOVE the current
// reconstructed hash (i.e. the parent being reconstructed at this step),
// mirroring proveIndex's walk from leaf upward.
func Verify(root bhash.Digest, p Proof) bool {
	cur := leafHash(p.V, p.Bitmap)
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
		parts := [][]byte{encodeV(step.V)}
		for _, h := range ordered {
			parts = append(parts, bhash.DigestBytes(h))
		}
		cur = bhash.HashConcat(parts...)
	}
	return cur == root
}

// leftmost and rightmost follow the authenticated child positions from a
// proved leaf to the root.
func leftmost(p Proof) bool {
	for _, step := range p.Steps {
		if step.ChildIndex != 0 {
			return false
		}
	}
	return true
}

func rightmost(p Proof) bool {
	for _, step := range p.Steps {
		if step.ChildIndex != len(step.SiblingHashes) {
			return false
		}
	}
	return true
}

// adjacent establishes that no committed leaf lies between two proofs.
// Below their first divergent parent, the left path must be the rightmost
// branch and the right path the leftmost branch.
func adjacent(left, right Proof) bool {
	if left.V >= right.V || len(left.Steps) == 0 || len(left.Steps) != len(right.Steps) {
		return false
	}
	divergence := -1
	for i := len(left.Steps) - 1; i >= 0; i-- {
		a, b := left.Steps[i], right.Steps[i]
		if len(a.SiblingHashes) != len(b.SiblingHashes) || len(a.V) != len(b.V) {
			return false
		}
		for j := range a.V {
			if a.V[j] != b.V[j] {
				return false
			}
		}
		if a.ChildIndex != b.ChildIndex {
			divergence = i
			if b.ChildIndex != a.ChildIndex+1 {
				return false
			}
			break
		}
	}
	if divergence < 0 {
		return false
	}
	for i := 0; i < divergence; i++ {
		if left.Steps[i].ChildIndex != len(left.Steps[i].SiblingHashes) || right.Steps[i].ChildIndex != 0 {
			return false
		}
	}
	return true
}

// VerifyRangeResult binds authenticated boundary leaves to the actual query
// interval, including the empty and domain-edge cases of Algorithm 5.
func VerifyRangeResult(root bhash.Digest, n uint64, alpha, beta int64, res RangeResult) bool {
	if alpha > beta {
		return false
	}
	check := func(p Proof) bool { return p.Bitmap.Len() == n && Verify(root, p) }
	if res.LowerReal {
		if !check(res.LowerProof) || res.LowerProof.V >= alpha {
			return false
		}
	}
	if res.AlphaNeighborReal && !check(res.AlphaNeighborProof) {
		return false
	}
	if !res.LowerReal {
		if !res.AlphaNeighborReal || !leftmost(res.AlphaNeighborProof) || res.AlphaNeighborProof.V < alpha {
			return false
		}
	} else if res.AlphaNeighborReal {
		if !adjacent(res.LowerProof, res.AlphaNeighborProof) || res.AlphaNeighborProof.V < alpha {
			return false
		}
	} else if !rightmost(res.LowerProof) {
		return false
	}

	if res.UpperReal && !check(res.UpperProof) {
		return false
	}
	if res.BetaNeighborReal && !check(res.BetaNeighborProof) {
		return false
	}
	if !res.UpperReal {
		return !res.UpperIsExact && !res.BetaNeighborReal && !res.LowerReal &&
			res.AlphaNeighborReal && leftmost(res.AlphaNeighborProof) && beta < res.AlphaNeighborProof.V
	}
	if res.UpperIsExact {
		return !res.BetaNeighborReal && res.UpperProof.V == beta
	}
	if res.UpperProof.V >= beta {
		return false
	}
	if res.BetaNeighborReal {
		return adjacent(res.UpperProof, res.BetaNeighborProof) && beta < res.BetaNeighborProof.V
	}
	return rightmost(res.UpperProof)
}

// RangeResult is the outcome of Algorithm 5 (R-IOMF-020). Alongside the two
// bitmap-carrying boundary leaves used to compute QR, it also carries
// adjacency evidence (the "other side" neighbour of each boundary, ln_x+1
// and, when the upper boundary is not exact, its own successor) so a
// verifier can confirm the shipped boundary leaves are truly the tightest
// adjacent pair and not a looser substitute that would silently change the
// result (R-VER-006, T-024). This is the full four-leaf evidence Algorithm
// 5 step 4 collects (ln_x, ln_{x+1}, ln_y, ln_{y+1}), restricted to what
// exists at domain edges (B5).
type RangeResult struct {
	QR []uint64

	LowerReal  bool // false iff alpha <= min(v): the lower bound is a virtual all-zero bitmap (B2)
	LowerProof Proof

	// AlphaNeighborReal/Proof: the immediate successor of LowerProof (the
	// first leaf with v >= alpha), proving LowerProof is truly the tightest
	// predecessor. False iff no leaf has v >= alpha.
	AlphaNeighborReal  bool
	AlphaNeighborProof Proof

	UpperReal    bool // always true unless the tree is empty
	UpperProof   Proof
	UpperIsExact bool // true iff the upper boundary leaf's v == beta (Alg. 5 "if ln_{y+1}.v = β" branch)

	// BetaNeighborReal/Proof: in the non-exact branch, the immediate
	// successor of UpperProof (first leaf with v > beta), proving
	// UpperProof is truly the tightest predecessor of beta. Not populated
	// in the exact or rightmost-fallback branches, where it is unnecessary
	// (the upper boundary leaf's own v already pins the position).
	BetaNeighborReal  bool
	BetaNeighborProof Proof
}

// ErrInvertedRange is returned when alpha > beta (B5: reject).
var ErrInvertedRange = fmt.Errorf("rimbt: alpha > beta")

// ErrEmptyTree is returned when a range query is issued against a tree with
// no leaves.
var ErrEmptyTree = fmt.Errorf("rimbt: tree has no leaves")

// RangeQuery implements Algorithm 5: locate the boundary leaves for
// [alpha, beta] and compute QR = {i | value(i) in [alpha, beta]} via the
// cumulative-bitmap complement rule ¬bm_lower ∧ bm_upper.
func (t *Tree) RangeQuery(alpha, beta int64) (RangeResult, error) {
	if alpha > beta {
		return RangeResult{}, ErrInvertedRange
	}
	if len(t.leaves) == 0 {
		return RangeResult{}, ErrEmptyTree
	}

	// Locate alpha: gA = first leaf index with v >= alpha.
	gA := sort.Search(len(t.leaves), func(i int) bool { return t.leaves[i].v >= alpha })
	lowerIdx := gA - 1 // leaf strictly less than alpha, if any

	// Locate beta: gB = first leaf index with v >= beta.
	gB := sort.Search(len(t.leaves), func(i int) bool { return t.leaves[i].v >= beta })

	var res RangeResult
	var lowerBM bitmap.Bitmap
	if lowerIdx >= 0 {
		res.LowerReal = true
		res.LowerProof = t.proveIndex(lowerIdx)
		lowerBM = t.leaves[lowerIdx].bm
	} else {
		res.LowerReal = false
		lowerBM = bitmap.New(t.n) // virtual all-zero (B2/B5)
	}
	if gA < len(t.leaves) {
		res.AlphaNeighborReal = true
		res.AlphaNeighborProof = t.proveIndex(gA)
	}

	var upperBM bitmap.Bitmap
	switch {
	case gB < len(t.leaves) && t.leaves[gB].v == beta:
		// Alg. 5 line 5: ln_{y+1}.v == beta.
		res.UpperReal = true
		res.UpperIsExact = true
		res.UpperProof = t.proveIndex(gB)
		upperBM = t.leaves[gB].bm
	case gB >= len(t.leaves):
		// beta exceeds every stored value: the rightmost leaf is the
		// authenticated upper-universe bitmap (B6).
		last := len(t.leaves) - 1
		res.UpperReal = true
		res.UpperIsExact = false
		res.UpperProof = t.proveIndex(last)
		upperBM = t.leaves[last].bm
	default:
		// Alg. 5 line 6: use ln_y, the largest leaf strictly less than beta.
		lnY := gB - 1
		if lnY < 0 {
			// beta is below the smallest stored value: no object qualifies.
			res.UpperReal = false
			upperBM = bitmap.New(t.n)
		} else {
			res.UpperReal = true
			res.UpperIsExact = false
			res.UpperProof = t.proveIndex(lnY)
			upperBM = t.leaves[lnY].bm
			// gB itself (the immediate successor of ln_y) proves ln_y is
			// the tightest predecessor of beta.
			if gB < len(t.leaves) {
				res.BetaNeighborReal = true
				res.BetaNeighborProof = t.proveIndex(gB)
			}
		}
	}

	qrBM := bitmap.And(bitmap.Not(lowerBM), upperBM)
	res.QR = qrBM.SetIndices()
	return res, nil
}
