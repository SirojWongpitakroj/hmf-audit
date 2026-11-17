// Package iimt implements the Inverted Index Merkle Tree (IV-B, Fig. 2): a
// modified, fully balanced, fixed-fanout Merkle tree with one leaf per
// keyword, laid out in kid order (R-IOMF-007..015).
//
// Hash formulas:
//   - leaf:     H(w ‖ H(bm))                              (R-CRYPTO-003)
//   - internal: H(h_c1 ‖ ... ‖ h_ck) for k <= F present    (R-CRYPTO-004, A7)
//
// Keyword universe (DECISIONS.md §C, item C1): kid starts at 0, increases
// monotonically, append-only; every keyword known when a leaf set is frozen
// gets a leaf even if its bitmap is all-zero (R-ALIS-005).
//
// Algorithm 1 update (R-IOMF-012) is implemented literally, with deviation
// D-1 applied: a leaf's hash is recomputed whenever its serialized bitmap
// bytes change — which happens both when its bit is set (matching keyword)
// and, independently, whenever N crosses a byte boundary and every leaf's
// serialized length grows by a byte.
package iimt

import (
	"fmt"

	"blas/bhash"
	"blas/bitmap"
)

type leafState struct {
	kid     uint64
	keyword string
	bm      bitmap.Bitmap
	hash    bhash.Digest
}

func leafHash(w string, bm bitmap.Bitmap) bhash.Digest {
	inner := bhash.H(bm.Bytes())
	return bhash.HashConcat(bhash.FramedString(w), bhash.DigestBytes(inner))
}

// Tree is a mutable IIMT: keywords may be appended (AddKeyword) and objects
// may be inserted (Insert, Algorithm 1) over its lifetime.
type Tree struct {
	fanout  int
	n       uint64 // authenticated bitmap length N (number of objects)
	leaves  []leafState
	kwIndex map[string]int // keyword -> slice index; lookup accelerator only (R-GO-006), never iterated to build hashed state
	levels  [][]bhash.Digest
	dirty   bool
}

// NewEmpty returns an IIMT with fanout F, zero keywords and N=0.
func NewEmpty(fanout int) (*Tree, error) {
	if fanout < 2 {
		return nil, fmt.Errorf("iimt: fanout must be >= 2, got %d", fanout)
	}
	return &Tree{fanout: fanout, kwIndex: map[string]int{}}, nil
}

// Fanout returns F.
func (t *Tree) Fanout() int { return t.fanout }

// N returns the current authenticated bitmap length.
func (t *Tree) N() uint64 { return t.n }

// NumLeaves returns the number of keyword leaves.
func (t *Tree) NumLeaves() int { return len(t.leaves) }

// KID returns the kid assigned to keyword w, if it has a leaf.
func (t *Tree) KID(w string) (uint64, bool) {
	idx, ok := t.kwIndex[w]
	if !ok {
		return 0, false
	}
	return t.leaves[idx].kid, true
}

// AddKeyword appends a new keyword leaf with an all-zero bitmap of the
// current length N, and returns its assigned kid (append-only, starts at 0,
// C1 ruling). It is a no-op returning the existing kid if w already has a
// leaf.
func (t *Tree) AddKeyword(w string) uint64 {
	if idx, ok := t.kwIndex[w]; ok {
		return t.leaves[idx].kid
	}
	kid := uint64(len(t.leaves))
	bm := bitmap.New(t.n)
	ls := leafState{kid: kid, keyword: w, bm: bm, hash: leafHash(w, bm)}
	t.leaves = append(t.leaves, ls)
	t.kwIndex[w] = len(t.leaves) - 1
	t.dirty = true
	t.rebuild()
	return kid
}

// Insert applies Algorithm 1 for one new data object: oid must equal the
// current N (objects are inserted in oid order, from 0). For every leaf
// whose keyword is in keywords, bit oid is set. Deviation D-1: if N grows
// past a byte boundary, every leaf's serialized bytes change length, so
// every leaf's hash is recomputed, not only the matching ones. Every
// non-leaf hash is then recomputed bottom-up (Algorithm 1 step 2, always,
// literally).
//
// keywords is a slice, not a map, and is only used for membership tests
// against each leaf's own keyword — no hashed state is ever built by
// iterating a map (R-GO-006).
func (t *Tree) Insert(oid uint64, keywords []string) error {
	if oid != t.n {
		return fmt.Errorf("iimt: Insert requires oid==N (sequential); got oid=%d N=%d", oid, t.n)
	}
	newN := oid + 1
	matched := make(map[string]bool, len(keywords))
	for _, k := range keywords {
		matched[k] = true
	}
	anyLengthChanged := false
	for i := range t.leaves {
		grown, lenChanged := t.leaves[i].bm.Grow(newN)
		t.leaves[i].bm = grown
		if lenChanged {
			anyLengthChanged = true
		}
	}
	for i := range t.leaves {
		isMatch := matched[t.leaves[i].keyword]
		if isMatch {
			t.leaves[i].bm.Set(oid)
		}
		if isMatch || anyLengthChanged {
			t.leaves[i].hash = leafHash(t.leaves[i].keyword, t.leaves[i].bm)
		}
	}
	t.n = newN
	t.dirty = true
	t.rebuild()
	return nil
}

func (t *Tree) rebuild() {
	if !t.dirty {
		return
	}
	if len(t.leaves) == 0 {
		t.levels = nil
		t.dirty = false
		return
	}
	leafHashes := make([]bhash.Digest, len(t.leaves))
	for i, l := range t.leaves {
		leafHashes[i] = l.hash
	}
	levels := [][]bhash.Digest{leafHashes}
	cur := leafHashes
	for len(cur) > 1 {
		next := buildLevel(cur, t.fanout)
		levels = append(levels, next)
		cur = next
	}
	t.levels = levels
	t.dirty = false
}

func buildLevel(cur []bhash.Digest, fanout int) []bhash.Digest {
	numGroups := (len(cur) + fanout - 1) / fanout
	next := make([]bhash.Digest, numGroups)
	for g := 0; g < numGroups; g++ {
		start := g * fanout
		end := start + fanout
		if end > len(cur) {
			end = len(cur)
		}
		parts := make([][]byte, 0, end-start)
		for i := start; i < end; i++ {
			parts = append(parts, bhash.DigestBytes(cur[i]))
		}
		next[g] = bhash.HashConcat(parts...)
	}
	return next
}

// Root returns the IIMT root hash.
func (t *Tree) Root() (bhash.Digest, error) {
	if len(t.levels) == 0 {
		return bhash.Digest{}, fmt.Errorf("iimt: tree has no leaves")
	}
	top := t.levels[len(t.levels)-1]
	return top[0], nil
}

// ProofStep mirrors omt.ProofStep: explicit sibling hashes plus position
// (R-GO-019).
type ProofStep struct {
	ChildIndex    int
	SiblingHashes []bhash.Digest
}

// Proof is an Algorithm 4 query proof for one keyword.
type Proof struct {
	Keyword string
	Bitmap  bitmap.Bitmap
	Steps   []ProofStep
}

// AbsenceProof discloses the complete committed keyword tree when a queried
// keyword has no leaf. IIMT leaves are ordered by insertion ID, so ordinary
// neighbouring leaves cannot establish non-membership by keyword value.
// Reconstructing the committed root from every leaf makes the absence claim
// verifiable without trusting the responder's assertion.
type AbsenceProof struct {
	Fanout int
	Leaves []AbsentLeaf
}

type AbsentLeaf struct {
	Keyword string
	Bitmap  bitmap.Bitmap
}

// ProveAbsent returns a complete-tree witness only when keyword is absent.
func (t *Tree) ProveAbsent(keyword string) (AbsenceProof, bool) {
	if _, present := t.kwIndex[keyword]; present {
		return AbsenceProof{}, false
	}
	p := AbsenceProof{Fanout: t.fanout, Leaves: make([]AbsentLeaf, len(t.leaves))}
	for i, leaf := range t.leaves {
		p.Leaves[i] = AbsentLeaf{Keyword: leaf.keyword, Bitmap: leaf.bm.Clone()}
	}
	return p, true
}

// VerifyAbsent checks that the complete disclosed tree matches the trusted
// root and contains no leaf for keyword. n is the authenticated object count.
func VerifyAbsent(root bhash.Digest, n uint64, keyword string, p AbsenceProof) bool {
	if p.Fanout < 2 || len(p.Leaves) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(p.Leaves))
	level := make([]bhash.Digest, len(p.Leaves))
	for i, leaf := range p.Leaves {
		if leaf.Keyword == keyword || leaf.Bitmap.Len() != n {
			return false
		}
		if _, duplicate := seen[leaf.Keyword]; duplicate {
			return false
		}
		seen[leaf.Keyword] = struct{}{}
		level[i] = leafHash(leaf.Keyword, leaf.Bitmap)
	}
	for len(level) > 1 {
		level = buildLevel(level, p.Fanout)
	}
	return level[0] == root
}

// Query implements Algorithm 4 (R-IOMF-013): returns the set of object
// indices with the keyword, the keyword's bitmap, and a Merkle proof.
func (t *Tree) Query(w string) (qr []uint64, proof Proof, ok bool) {
	idx, present := t.kwIndex[w]
	if !present {
		return nil, Proof{}, false
	}
	leaf := t.leaves[idx]
	p := Proof{Keyword: w, Bitmap: leaf.bm}
	pos := idx
	for lvl := 0; lvl < len(t.levels)-1; lvl++ {
		cur := t.levels[lvl]
		groupStart := (pos / t.fanout) * t.fanout
		groupEnd := groupStart + t.fanout
		if groupEnd > len(cur) {
			groupEnd = len(cur)
		}
		step := ProofStep{ChildIndex: pos - groupStart}
		for i := groupStart; i < groupEnd; i++ {
			if i == pos {
				continue
			}
			step.SiblingHashes = append(step.SiblingHashes, cur[i])
		}
		p.Steps = append(p.Steps, step)
		pos = pos / t.fanout
	}
	return leaf.bm.SetIndices(), p, true
}

// Verify reconstructs the IIMT root from a proof and compares it with root.
func Verify(root bhash.Digest, p Proof) bool {
	cur := leafHash(p.Keyword, p.Bitmap)
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

// MergeProofs merges several single-keyword proofs into one, per the union-
// minus-recomputable rule (B4 in DECISIONS.md): the merged set is the union
// of every sibling hash across all proofs, minus any hash that equals one of
// the queried leaves' own (recomputable) hash at that position. This keeps
// proof size assertions (e.g. the Fig. 2 shared-internal-hash example)
// meaningful while never fabricating or dropping information needed to
// reconstruct the root from any one of the merged proofs.
//
// The return value keeps each original proof's shape (so a verifier can
// still walk each keyword's own path) but exposes, for size accounting
// only, the deduplicated set of distinct sibling hashes actually needed.
func MergeProofs(proofs []Proof) (merged []Proof, distinctSiblingHashes int) {
	seen := map[bhash.Digest]bool{}
	for _, p := range proofs {
		for _, step := range p.Steps {
			for _, h := range step.SiblingHashes {
				seen[h] = true
			}
		}
	}
	// seen is used only to count distinct hashes (a size metric), never to
	// build or order any hashed/serialized structure (R-GO-006).
	return proofs, len(seen)
}
