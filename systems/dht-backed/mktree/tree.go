package mktree

import (
	"errors"
	"fmt"

	"dhtbacked/dht"
	"dhtbacked/internal/hashing"
)

// DefaultStride is s = 8, the default for the default padded tree of 1,024
// leaves (L = 10) (Q25).
const DefaultStride = uint64(8)

// DefaultEpochCapacity is the default count-based epoch boundary: 1,000 logs
// (Q23).
const DefaultEpochCapacity = uint64(1000)

// DummyLeaf is the padding leaf: SHA256(0x00 || 0x00), a two-byte pre-image of
// the leaf tag followed by one zero byte (Q9). It is used everywhere padding
// is required.
func DummyLeaf() hashing.Digest {
	return hashing.H(hashing.Tag(hashing.TagLeaf), []byte{0x00})
}

// Internal computes N_j,k = H(0x01 || N_{j+1,2k} || N_{j+1,2k+1}) — Eq. (3)/(21).
// The left child is N_{j+1,2k} and the right child is N_{j+1,2k+1}; this
// ordering is never altered.
func Internal(left, right hashing.Digest) hashing.Digest {
	return hashing.H(hashing.Tag(hashing.TagInternal), left[:], right[:])
}

// Tree is one MK-Tree, scoped to exactly one (log type, epoch) pair (Q22/Q24).
//
// The tree is pre-sized to the configured epoch capacity and padded from the
// beginning (Q24): for capacity N_e it holds N'_e = 2^ceil(log2 N_e) leaf
// positions, initially all dummy leaves. Tree height, node coordinates, stride
// levels and ancestor identifiers are therefore stable for the whole epoch.
//
// Levels are indexed exactly as the paper defines them: j = 0 is the root and
// j = L is the leaf level; k runs left to right (R-MKT-03, Q25).
type Tree struct {
	epoch    uint64
	logType  string
	treeID   hashing.Digest
	capacity uint64 // N_e, the configured epoch capacity
	width    uint64 // N'_e, the padded number of leaf positions
	levels   uint64 // L
	stride   uint64 // s
	nodes    [][]hashing.Digest
	filled   uint64 // number of real digests placed so far
}

// ErrTreeFull reports an insert beyond the pre-sized leaf capacity.
var ErrTreeFull = errors.New("mktree: no dummy leaf position remains")

// NewTree pre-sizes and fully pads a tree for one (epoch, logType) pair.
func NewTree(epoch uint64, logType string, capacity, stride uint64) (*Tree, error) {
	if capacity == 0 {
		return nil, errors.New("mktree: capacity must be positive")
	}
	width, levels := padTo(capacity)
	if stride < 1 || stride > levels {
		return nil, fmt.Errorf("mktree: stride s must be in {1, ..., %d}, got %d", levels, stride)
	}
	t := &Tree{
		epoch:    epoch,
		logType:  logType,
		treeID:   dht.TreeID(epoch, logType),
		capacity: capacity,
		width:    width,
		levels:   levels,
		stride:   stride,
		nodes:    make([][]hashing.Digest, levels+1),
	}
	for j := uint64(0); j <= levels; j++ {
		t.nodes[j] = make([]hashing.Digest, uint64(1)<<j)
	}
	dummy := DummyLeaf()
	for k := uint64(0); k < width; k++ {
		t.nodes[levels][k] = dummy
	}
	for j := levels; j > 0; j-- {
		for k := uint64(0); k < uint64(1)<<(j-1); k++ {
			t.nodes[j-1][k] = Internal(t.nodes[j][2*k], t.nodes[j][2*k+1])
		}
	}
	return t, nil
}

// padTo returns N'_e = 2^ceil(log2 n) and L = ceil(log2 n) (R-MKT-02).
func padTo(n uint64) (width, levels uint64) {
	width, levels = 1, 0
	for width < n {
		width <<= 1
		levels++
	}
	return width, levels
}

// PaddedWidth returns N'_e for a capacity, exposed for configuration checks.
func PaddedWidth(capacity uint64) uint64 { w, _ := padTo(capacity); return w }

// Epoch returns the epoch this tree belongs to.
func (t *Tree) Epoch() uint64 { return t.epoch }

// LogType returns the log type this tree belongs to.
func (t *Tree) LogType() string { return t.logType }

// TreeID returns H("TREE" || epoch || logType).
func (t *Tree) TreeID() hashing.Digest { return t.treeID }

// Capacity returns the configured epoch capacity N_e.
func (t *Tree) Capacity() uint64 { return t.capacity }

// Width returns the padded number of leaf positions N'_e.
func (t *Tree) Width() uint64 { return t.width }

// Levels returns L, the leaf level index (the root is level 0).
func (t *Tree) Levels() uint64 { return t.levels }

// Stride returns the configured stride s.
func (t *Tree) Stride() uint64 { return t.stride }

// LeafCount returns the number of real records inserted so far.
func (t *Tree) LeafCount() uint64 { return t.filled }

// Node returns N_j,k.
func (t *Tree) Node(j, k uint64) (hashing.Digest, error) {
	if j > t.levels || k >= uint64(1)<<j {
		return hashing.Digest{}, fmt.Errorf("mktree: node (%d, %d) is outside the tree", j, k)
	}
	return t.nodes[j][k], nil
}

// Root returns the per-type root R^(e)_tau = N_0,0. The binary MK-Tree root is
// authoritative; Eq. (29)'s flat aggregation does not redefine it (Q16).
func (t *Tree) Root() hashing.Digest { return t.nodes[0][0] }

// Insert places the next real digest in the next dummy leaf position and
// recomputes only the affected path (Q24, R-MKT-07). Leaves are placed in
// ingestion order and are never permuted (R-MKT-06).
//
// It returns the assigned zero-based leaf index and the list of node
// coordinates whose values changed, root-inclusive, ordered from the leaf
// level upward.
func (t *Tree) Insert(d hashing.Digest) (leafIndex uint64, affected []Coord, err error) {
	if t.filled >= t.width {
		return 0, nil, ErrTreeFull
	}
	i := t.filled
	t.nodes[t.levels][i] = d
	affected = append(affected, Coord{J: t.levels, K: i})
	k := i
	for j := t.levels; j > 0; j-- {
		parent := k / 2
		t.nodes[j-1][parent] = Internal(t.nodes[j][2*parent], t.nodes[j][2*parent+1])
		affected = append(affected, Coord{J: j - 1, K: parent})
		k = parent
	}
	t.filled++
	return i, affected, nil
}

// Coord is a node coordinate (level j, index k).
type Coord struct {
	J uint64
	K uint64
}
