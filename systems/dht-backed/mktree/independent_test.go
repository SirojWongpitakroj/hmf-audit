package mktree_test

// Independent verification of MK-Tree construction (M4).
//
// refTree below rebuilds the tree from scratch, straight from the paper's
// equations, using only crypto/sha256. It shares no code with package mktree:
// it does not call DummyLeaf, Internal, or any tree method to produce a value.
// The package's tree is then compared against it node for node.
//
//   Eq. (2):  leaf ell_i = H(0x00 || c_i || t_i || AD_i); dummy = H(0x00 || 0x00) (Q9)
//   Eq. (3):  N_j,k = H(0x01 || N_{j+1,2k} || N_{j+1,2k+1}), root j = 0, leaves j = L
//   Eq. (4):  A_s,k = N_s,k for k in {0, ..., 2^s - 1}

import (
	"crypto/sha256"
	"testing"

	"dhtbacked/internal/hashing"
	"dhtbacked/mktree"
)

type refTree struct {
	width, levels uint64
	nodes         [][][32]byte // nodes[j][k]
}

func refDummy() [32]byte { return sha256.Sum256([]byte{0x00, 0x00}) }

func refInternal(l, r [32]byte) [32]byte {
	b := make([]byte, 0, 65)
	b = append(b, 0x01)
	b = append(b, l[:]...)
	b = append(b, r[:]...)
	return sha256.Sum256(b)
}

// newRefTree pads capacity to the next power of two (Q24) and fills the first
// len(real) leaves with real digests, the rest with the Q9 dummy.
func newRefTree(capacity uint64, real [][32]byte) refTree {
	width, levels := uint64(1), uint64(0)
	for width < capacity {
		width <<= 1
		levels++
	}
	nodes := make([][][32]byte, levels+1)
	nodes[levels] = make([][32]byte, width)
	for i := range nodes[levels] {
		if i < len(real) {
			nodes[levels][i] = real[i]
		} else {
			nodes[levels][i] = refDummy()
		}
	}
	for j := int(levels) - 1; j >= 0; j-- {
		n := uint64(1) << uint(j)
		nodes[j] = make([][32]byte, n)
		for k := uint64(0); k < n; k++ {
			nodes[j][k] = refInternal(nodes[j+1][2*k], nodes[j+1][2*k+1])
		}
	}
	return refTree{width: width, levels: levels, nodes: nodes}
}

func refDigests(n int) [][32]byte {
	out := make([][32]byte, n)
	for i := range out {
		out[i] = sha256.Sum256([]byte{0xEE, byte(i), byte(i >> 8)})
	}
	return out
}

func buildPkgTree(t *testing.T, capacity, stride uint64, real [][32]byte) *mktree.Tree {
	t.Helper()
	tr, err := mktree.NewTree(1, "access", capacity, stride)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range real {
		if _, _, err := tr.Insert(hashing.Digest(d)); err != nil {
			t.Fatal(err)
		}
	}
	return tr
}

// Q9, Eq. (2)/(3), R-MKT-02/03/04: every node of the package's tree equals the
// reference, across several capacities and fill levels, including the default
// 1,000-record epoch padded to 1,024.
func TestIndependentTreeMatchesReferenceNodeForNode(t *testing.T) {
	cases := []struct {
		capacity uint64
		filled   int
	}{
		{1000, 0}, {1000, 1}, {1000, 537}, {1000, 1000},
		{8, 3}, {8, 8}, {5, 5},
	}
	// A capacity-1 tree has width 1, so L = 0 and no stride s in {1, ..., L}
	// exists (R-ANC-01); the package rightly refuses to build one.
	for _, c := range cases {
		real := refDigests(c.filled)
		ref := newRefTree(c.capacity, real)
		got := buildPkgTree(t, c.capacity, 1, real) // stride fixes which nodes are ancestors, not their values

		if got.Width() != ref.width || got.Levels() != ref.levels {
			t.Fatalf("capacity %d: width/L = %d/%d, want %d/%d",
				c.capacity, got.Width(), got.Levels(), ref.width, ref.levels)
		}
		for j := uint64(0); j <= ref.levels; j++ {
			for k := uint64(0); k < uint64(len(ref.nodes[j])); k++ {
				n, err := got.Node(j, k)
				if err != nil {
					t.Fatal(err)
				}
				if [32]byte(n) != ref.nodes[j][k] {
					t.Fatalf("capacity %d, filled %d: N_%d,%d differs from the reference", c.capacity, c.filled, j, k)
				}
			}
		}
		if [32]byte(got.Root()) != ref.nodes[0][0] {
			t.Fatalf("capacity %d: root is not N_0,0", c.capacity)
		}
	}
}

// Q24: the default 1,000-record epoch allocates exactly 1,024 leaf positions,
// so L = 10.
func TestIndependentDefaultEpochPadsTo1024(t *testing.T) {
	tr, err := mktree.NewTree(1, "access", mktree.DefaultEpochCapacity, mktree.DefaultStride)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Width() != 1024 || tr.Levels() != 10 {
		t.Fatalf("default epoch: width %d, L %d; want 1024, 10", tr.Width(), tr.Levels())
	}
	if mktree.DefaultStride != 8 {
		t.Fatalf("default stride = %d, want 8 (Q25)", mktree.DefaultStride)
	}
}

// R-MKT-07, Q24: inserting a leaf recomputes exactly one leaf-to-root path —
// L+1 coordinates, each the parent of the one before — and nothing else.
func TestIndependentInsertTouchesExactlyOnePath(t *testing.T) {
	tr, err := mktree.NewTree(1, "access", 1000, 8)
	if err != nil {
		t.Fatal(err)
	}
	L := tr.Levels()
	for n := 0; n < 300; n++ {
		idx, affected, err := tr.Insert(hashing.Digest(refDigests(n + 1)[n]))
		if err != nil {
			t.Fatal(err)
		}
		if idx != uint64(n) {
			t.Fatalf("insert %d landed at leaf %d; leaves must fill in ingestion order (R-MKT-06)", n, idx)
		}
		if uint64(len(affected)) != L+1 {
			t.Fatalf("insert %d touched %d nodes, want exactly L+1 = %d", n, len(affected), L+1)
		}
		k := idx
		for step, c := range affected {
			wantJ := L - uint64(step)
			if c.J != wantJ || c.K != k {
				t.Fatalf("insert %d, step %d: touched N_%d,%d, want N_%d,%d", n, step, c.J, c.K, wantJ, k)
			}
			k >>= 1
		}
	}
}

// Eq. (4), R-ANC-01/03, Q25: ancestors are the level-s nodes, there are 2^s of
// them, and each up-proof holds exactly s siblings that fold back to the root.
func TestIndependentAncestorsAndUpProofs(t *testing.T) {
	real := refDigests(1000)
	ref := newRefTree(1000, real)
	for _, s := range []uint64{1, 2, 4, 8} {
		tr := buildPkgTree(t, 1000, s, real)
		if tr.AncestorCount() != uint64(1)<<s {
			t.Fatalf("s=%d: %d ancestors, want 2^s = %d", s, tr.AncestorCount(), uint64(1)<<s)
		}
		for k := uint64(0); k < tr.AncestorCount(); k++ {
			a, err := tr.Ancestor(k)
			if err != nil {
				t.Fatal(err)
			}
			if [32]byte(a) != ref.nodes[s][k] {
				t.Fatalf("s=%d: A_%d,%d is not N_%d,%d", s, s, k, s, k)
			}
			up, err := tr.UpProof(k)
			if err != nil {
				t.Fatal(err)
			}
			if uint64(len(up)) != s {
				t.Fatalf("s=%d: |upProof| = %d, want exactly s (R-ANC-03)", s, len(up))
			}
			// Fold independently from A_s,k to the root.
			cur, idx := ref.nodes[s][k], k
			for i, j := 0, s; j >= 1; i, j = i+1, j-1 {
				sib := ref.nodes[j][idx^1]
				if [32]byte(up[i]) != sib {
					t.Fatalf("s=%d, k=%d: up-proof element %d is not the sibling N_%d,%d", s, k, i, j, idx^1)
				}
				if idx%2 == 0 {
					cur = refInternal(cur, sib)
				} else {
					cur = refInternal(sib, cur)
				}
				idx >>= 1
			}
			if cur != ref.nodes[0][0] {
				t.Fatalf("s=%d, k=%d: up-proof does not fold to the root", s, k)
			}
		}
	}
}

// Q25: under the paper's own level convention (root j = 0, leaves j = L), a
// larger s places checkpoints closer to the leaves and yields more of them.
// Sec. V.C.5's contrary explanation is deliberately not reproduced.
func TestIndependentLargerStrideMeansMoreCheckpointsNearerLeaves(t *testing.T) {
	var prevCount, prevSpan uint64 = 0, 1 << 62
	for _, s := range []uint64{1, 2, 4, 8} {
		tr := buildPkgTree(t, 1000, s, nil)
		span := uint64(1) << (tr.Levels() - s) // leaves under each checkpoint
		if tr.AncestorCount() <= prevCount {
			t.Fatalf("s=%d: checkpoint count %d did not grow with s", s, tr.AncestorCount())
		}
		if span >= prevSpan {
			t.Fatalf("s=%d: leaf-to-checkpoint span %d did not shrink with s", s, span)
		}
		prevCount, prevSpan = tr.AncestorCount(), span
	}
}

// Q17, R-ANC-02: a certificate's range is the structural leaf-index range of
// its subtree, [k*2^(L-s), (k+1)*2^(L-s) - 1] — never a timestamp. Q4/R-ANC-05:
// it carries no signature field.
func TestIndependentCertificateRangeIsStructural(t *testing.T) {
	tr := buildPkgTree(t, 1000, 8, refDigests(1000))
	span := uint64(1) << (tr.Levels() - 8)
	for _, k := range []uint64{0, 1, 17, 255} {
		c, err := tr.Certificate(k, 1)
		if err != nil {
			t.Fatal(err)
		}
		if c.IMin != k*span || c.IMax != (k+1)*span-1 {
			t.Fatalf("k=%d: range [%d, %d], want [%d, %d]", k, c.IMin, c.IMax, k*span, (k+1)*span-1)
		}
		if c.Level != 8 || c.Index != k || c.TreeID != tr.TreeID() {
			t.Fatalf("k=%d: certificate names the wrong node", k)
		}
	}
}

// Q45, §0.9: treeID = H("TREE" || epoch || logType) and
// ancestorID = H("ANC" || treeID || s || k), numerics as big-endian uint64.
func TestIndependentTreeIDAndAncestorID(t *testing.T) {
	be := func(v uint64) []byte {
		b := make([]byte, 8)
		for i := 7; i >= 0; i-- {
			b[i] = byte(v)
			v >>= 8
		}
		return b
	}
	h := func(parts ...[]byte) [32]byte {
		d := sha256.New()
		for _, p := range parts {
			d.Write(p)
		}
		var out [32]byte
		d.Sum(out[:0])
		return out
	}
	tr, err := mktree.NewTree(42, "security", 1000, 8)
	if err != nil {
		t.Fatal(err)
	}
	wantTree := h([]byte("TREE"), be(42), []byte("security"))
	if [32]byte(tr.TreeID()) != wantTree {
		t.Fatal("treeID != H(\"TREE\" || epoch || logType)")
	}
	c, err := tr.Certificate(3, 1)
	if err != nil {
		t.Fatal(err)
	}
	if want := h([]byte("ANC"), wantTree[:], be(8), be(3)); [32]byte(c.AncestorID) != want {
		t.Fatal("ancestorID != H(\"ANC\" || treeID || s || k)")
	}
}
