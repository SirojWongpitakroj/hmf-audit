package mktree_test

import (
	"crypto/sha256"
	"testing"

	"dhtbacked/dht"
	"dhtbacked/internal/hashing"
	"dhtbacked/mktree"
)

func digest(i int) hashing.Digest {
	return sha256.Sum256([]byte{byte(i), byte(i >> 8), byte(i >> 16)})
}

// Q9: the dummy leaf is SHA256(0x00 || 0x00) — a two-byte pre-image.
func TestDummyLeafIsTwoBytePreImage(t *testing.T) {
	want := sha256.Sum256([]byte{0x00, 0x00})
	if mktree.DummyLeaf() != want {
		t.Fatal("dummy leaf is not SHA256(0x00 || 0x00)")
	}
}

// Q24: default epoch 1,000 allocates 1,024 leaf positions, initially dummy.
func TestPreSizedPaddingFromTheBeginning(t *testing.T) {
	tr, err := mktree.NewTree(1, "access", mktree.DefaultEpochCapacity, mktree.DefaultStride)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Width() != 1024 {
		t.Fatalf("padded width = %d, want 1024", tr.Width())
	}
	if tr.Levels() != 10 {
		t.Fatalf("L = %d, want 10", tr.Levels())
	}
	if tr.LeafCount() != 0 {
		t.Fatalf("leaf count = %d, want 0", tr.LeafCount())
	}
	dummy := mktree.DummyLeaf()
	for k := uint64(0); k < tr.Width(); k++ {
		n, err := tr.Node(tr.Levels(), k)
		if err != nil {
			t.Fatal(err)
		}
		if n != dummy {
			t.Fatalf("leaf position %d does not hold the dummy leaf", k)
		}
	}
	for _, c := range []struct{ cap, width, levels uint64 }{
		{1, 1, 0}, {2, 2, 1}, {3, 4, 2}, {1000, 1024, 10}, {10000, 16384, 14}, {100000, 131072, 17},
	} {
		if got := mktree.PaddedWidth(c.cap); got != c.width {
			t.Errorf("PaddedWidth(%d) = %d, want %d", c.cap, got, c.width)
		}
	}
}

// R-MKT-03 / Eq. (3): root at j = 0, leaves at j = L, left child N_{j+1,2k}.
func TestInternalNodeConstructionAndLevelIndexing(t *testing.T) {
	tr, err := mktree.NewTree(1, "access", 4, 1)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if _, _, err := tr.Insert(digest(i)); err != nil {
			t.Fatal(err)
		}
	}
	l := func(k uint64) hashing.Digest {
		n, err := tr.Node(2, k)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	want10 := mktree.Internal(l(0), l(1))
	want11 := mktree.Internal(l(2), l(3))
	got10, _ := tr.Node(1, 0)
	got11, _ := tr.Node(1, 1)
	if got10 != want10 || got11 != want11 {
		t.Fatal("internal node is not H(0x01 || left || right) with left = N_{j+1,2k}")
	}
	if tr.Root() != mktree.Internal(want10, want11) {
		t.Fatal("root is not N_0,0")
	}
	// Swapping children must change the value: ordering is significant.
	if mktree.Internal(want10, want11) == mktree.Internal(want11, want10) {
		t.Fatal("child ordering does not affect the parent hash")
	}
}

// Q24 / R-MKT-07: each arriving digest replaces the next dummy position and
// only the affected path is recomputed.
func TestInsertReplacesNextDummyAndTouchesOnlyOnePath(t *testing.T) {
	tr, _ := mktree.NewTree(1, "access", 8, 2)
	L := tr.Levels()

	// Snapshot every node before the insert.
	before := map[mktree.Coord]hashing.Digest{}
	for j := uint64(0); j <= L; j++ {
		for k := uint64(0); k < uint64(1)<<j; k++ {
			n, _ := tr.Node(j, k)
			before[mktree.Coord{J: j, K: k}] = n
		}
	}

	idx, affected, err := tr.Insert(digest(7))
	if err != nil {
		t.Fatal(err)
	}
	if idx != 0 {
		t.Fatalf("first insert took leaf index %d, want 0", idx)
	}
	if uint64(len(affected)) != L+1 {
		t.Fatalf("affected path has %d nodes, want L+1 = %d", len(affected), L+1)
	}
	touched := map[mktree.Coord]bool{}
	for _, c := range affected {
		touched[c] = true
	}
	changed := 0
	for j := uint64(0); j <= L; j++ {
		for k := uint64(0); k < uint64(1)<<j; k++ {
			c := mktree.Coord{J: j, K: k}
			n, _ := tr.Node(j, k)
			if n != before[c] {
				changed++
				if !touched[c] {
					t.Errorf("node (%d,%d) changed but is not on the affected path", j, k)
				}
			}
		}
	}
	if changed == 0 {
		t.Fatal("insert changed nothing")
	}
	// Tree geometry is stable within the epoch.
	if tr.Levels() != L || tr.Width() != 8 {
		t.Fatal("tree geometry changed after an insert")
	}
	if got, _ := tr.Node(L, 0); got != digest(7) {
		t.Fatal("leaf 0 does not hold the inserted digest")
	}
}

// R-MKT-06: leaves are placed in ingestion order and never permuted.
func TestLeafOrderingFollowsIngestionOrder(t *testing.T) {
	tr, _ := mktree.NewTree(1, "access", 8, 2)
	for i := 0; i < 5; i++ {
		idx, _, err := tr.Insert(digest(100 + i))
		if err != nil {
			t.Fatal(err)
		}
		if idx != uint64(i) {
			t.Fatalf("insert %d got leaf index %d", i, idx)
		}
	}
	for i := 0; i < 5; i++ {
		n, _ := tr.Node(tr.Levels(), uint64(i))
		if n != digest(100+i) {
			t.Fatalf("leaf %d holds the wrong digest", i)
		}
	}
	if tr.LeafCount() != 5 {
		t.Fatalf("leaf count = %d, want 5", tr.LeafCount())
	}
	// The remaining positions still hold dummy leaves.
	for i := uint64(5); i < tr.Width(); i++ {
		n, _ := tr.Node(tr.Levels(), i)
		if n != mktree.DummyLeaf() {
			t.Fatalf("leaf %d is not a dummy", i)
		}
	}
}

// Incremental construction must agree with a full rebuild.
func TestIncrementalUpdateMatchesFullRecomputation(t *testing.T) {
	const n = 300
	tr, _ := mktree.NewTree(9, "auth", 1000, 8)
	for i := 0; i < n; i++ {
		if _, _, err := tr.Insert(digest(i)); err != nil {
			t.Fatal(err)
		}
	}
	// Independent full recomputation from the leaf array.
	width := tr.Width()
	level := make([]hashing.Digest, width)
	for i := uint64(0); i < width; i++ {
		if i < n {
			level[i] = digest(int(i))
		} else {
			level[i] = mktree.DummyLeaf()
		}
	}
	for len(level) > 1 {
		next := make([]hashing.Digest, len(level)/2)
		for k := range next {
			next[k] = mktree.Internal(level[2*k], level[2*k+1])
		}
		level = next
	}
	if tr.Root() != level[0] {
		t.Fatal("incremental root differs from a full recomputation")
	}
}

func TestTreeFillsExactlyOnce(t *testing.T) {
	tr, _ := mktree.NewTree(1, "access", 4, 1)
	for i := 0; i < 4; i++ {
		if _, _, err := tr.Insert(digest(i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := tr.Insert(digest(99)); err != mktree.ErrTreeFull {
		t.Fatalf("insert past capacity returned %v, want ErrTreeFull", err)
	}
}

// R-ANC-01 / Q25: A_s,k = N_s,k for k in {0, ..., 2^s - 1}; a larger s places
// checkpoints closer to the leaves and increases their number.
func TestAncestorCheckpoints(t *testing.T) {
	for _, s := range []uint64{1, 2, 4, 8} {
		tr, err := mktree.NewTree(1, "access", 1000, s)
		if err != nil {
			t.Fatal(err)
		}
		if tr.AncestorCount() != uint64(1)<<s {
			t.Fatalf("s = %d: %d checkpoints, want %d", s, tr.AncestorCount(), uint64(1)<<s)
		}
		for k := uint64(0); k < tr.AncestorCount(); k++ {
			a, err := tr.Ancestor(k)
			if err != nil {
				t.Fatal(err)
			}
			n, _ := tr.Node(s, k)
			if a != n {
				t.Fatalf("A_%d,%d != N_%d,%d", s, k, s, k)
			}
		}
		if _, err := tr.Ancestor(tr.AncestorCount()); err == nil {
			t.Fatal("out-of-range ancestor index accepted")
		}
	}
	// A larger s yields more checkpoints and a shorter leaf-to-checkpoint path.
	small, _ := mktree.NewTree(1, "access", 1000, 1)
	large, _ := mktree.NewTree(1, "access", 1000, 8)
	if !(large.AncestorCount() > small.AncestorCount()) {
		t.Fatal("larger s did not increase the number of checkpoints")
	}
	sp1, _ := small.SiblingPath(0)
	sp8, _ := large.SiblingPath(0)
	if !(len(sp8) < len(sp1)) {
		t.Fatal("larger s did not shorten the leaf-to-checkpoint path")
	}
}

// R-ANC-03: |upProof_s,k| = s, and it is the minimal sibling set recomputing
// A_s,k => R^(e)_tau.
func TestUpProofIsMinimalAndOfLengthS(t *testing.T) {
	for _, s := range []uint64{1, 2, 4, 8} {
		tr, _ := mktree.NewTree(3, "api", 1000, s)
		for i := 0; i < 700; i++ {
			tr.Insert(digest(i))
		}
		for k := uint64(0); k < tr.AncestorCount(); k++ {
			up, err := tr.UpProof(k)
			if err != nil {
				t.Fatal(err)
			}
			if uint64(len(up)) != s {
				t.Fatalf("s = %d: |upProof| = %d", s, len(up))
			}
			a, _ := tr.Ancestor(k)
			if mktree.FoldUp(a, k, up, nil) != tr.Root() {
				t.Fatalf("s = %d, k = %d: up-proof does not recompute the root", s, k)
			}
			// Dropping any sibling breaks it: the set is minimal.
			if len(up) > 1 {
				if mktree.FoldUp(a, k, up[:len(up)-1], nil) == tr.Root() {
					t.Fatalf("s = %d, k = %d: a shorter sibling set also reaches the root", s, k)
				}
			}
		}
	}
}

// Leaf -> ancestor -> root recomputation for every leaf.
func TestLeafToAncestorToRoot(t *testing.T) {
	tr, _ := mktree.NewTree(3, "api", 64, 3)
	for i := 0; i < 50; i++ {
		tr.Insert(digest(i))
	}
	for i := uint64(0); i < tr.Width(); i++ {
		leaf, _ := tr.Node(tr.Levels(), i)
		sp, err := tr.SiblingPath(i)
		if err != nil {
			t.Fatal(err)
		}
		if uint64(len(sp)) != tr.Levels()-tr.Stride() {
			t.Fatalf("sibling path length = %d, want L - s = %d", len(sp), tr.Levels()-tr.Stride())
		}
		k := tr.CoveringAncestor(i)
		a := mktree.FoldUp(leaf, i, sp, nil)
		expected, _ := tr.Ancestor(k)
		if a != expected {
			t.Fatalf("leaf %d did not fold to its covering ancestor", i)
		}
		up, _ := tr.UpProof(k)
		if mktree.FoldUp(a, k, up, nil) != tr.Root() {
			t.Fatalf("leaf %d did not reach the root", i)
		}
		iMin, iMax := mktree.AncestorRange(k, tr.Levels(), tr.Stride())
		if i < iMin || i > iMax {
			t.Fatalf("leaf %d is outside its ancestor range [%d, %d]", i, iMin, iMax)
		}
	}
}

// Q17 / R-ANC-02: the certificate range is the leaf-index range, and the
// certificate carries no signature field (Q4, R-ANC-05).
func TestCertificateContents(t *testing.T) {
	tr, _ := mktree.NewTree(5, "security", 1000, 8)
	for i := 0; i < 400; i++ {
		tr.Insert(digest(i))
	}
	c, err := tr.Certificate(3, 42)
	if err != nil {
		t.Fatal(err)
	}
	if c.TreeID != dht.TreeID(5, "security") {
		t.Fatal("certificate treeID is wrong")
	}
	if c.AncestorID != dht.AncestorID(c.TreeID, 8, 3) {
		t.Fatal("certificate ancestorID is wrong")
	}
	if c.Level != 8 || c.Index != 3 || c.CommitSeq != 42 {
		t.Fatal("certificate level/index/commit_seq is wrong")
	}
	wantMin, wantMax := mktree.AncestorRange(3, tr.Levels(), tr.Stride())
	if c.IMin != wantMin || c.IMax != wantMax {
		t.Fatalf("certificate range = [%d, %d], want [%d, %d]", c.IMin, c.IMax, wantMin, wantMax)
	}
	if uint64(len(c.UpProof)) != tr.Stride() {
		t.Fatalf("certificate up-proof length = %d", len(c.UpProof))
	}
	a, _ := tr.Ancestor(3)
	if c.AncestorHash != a {
		t.Fatal("certificate ancestor hash is wrong")
	}
	// A round trip through the DHT record format preserves every field, and
	// there is no signature field to lose.
	got, err := dht.ParseAncestorCert(c.Serialize())
	if err != nil {
		t.Fatal(err)
	}
	if got.AncestorID != c.AncestorID || got.AncestorHash != c.AncestorHash ||
		got.IMin != c.IMin || got.IMax != c.IMax || got.CommitSeq != c.CommitSeq ||
		len(got.UpProof) != len(c.UpProof) {
		t.Fatal("certificate did not survive serialization")
	}
}

func TestStrideMustBeWithinRange(t *testing.T) {
	if _, err := mktree.NewTree(1, "access", 1000, 0); err == nil {
		t.Error("stride 0 accepted")
	}
	if _, err := mktree.NewTree(1, "access", 1000, 11); err == nil {
		t.Error("stride greater than L accepted")
	}
	if _, err := mktree.NewTree(1, "access", 1000, 10); err != nil {
		t.Errorf("stride = L rejected: %v", err)
	}
}

// R-EVAL-08: the same inputs in the same order produce the same root.
func TestRootIsDeterministic(t *testing.T) {
	build := func() hashing.Digest {
		tr, _ := mktree.NewTree(7, "error", 1000, 8)
		for i := 0; i < 250; i++ {
			tr.Insert(digest(i))
		}
		return tr.Root()
	}
	first := build()
	for i := 0; i < 20; i++ {
		if build() != first {
			t.Fatal("root is not deterministic")
		}
	}
	// A different order produces a different root (Theorem E.1 reorder detection).
	tr, _ := mktree.NewTree(7, "error", 1000, 8)
	tr.Insert(digest(1))
	tr.Insert(digest(0))
	for i := 2; i < 250; i++ {
		tr.Insert(digest(i))
	}
	if tr.Root() == first {
		t.Fatal("reordering two leaves did not change the root")
	}
}
