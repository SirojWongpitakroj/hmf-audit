package mktree_test

// Independent verification of sliding-window micro-roots (M9).
//
// Every published micro-root is read back out of the DHT and recomputed here
// from the raw real-leaf digests with crypto/sha256, never by calling
// mktree.MicroRoot. The digests fed to the partitioner are held by the test, so
// the recomputation depends on nothing the package produced.
//
//   Eq. (28):  R^(delta)_tau = H(d_i1 || d_i2 || ... || d_ik)   — no tag
//   Q21:       count-based, non-overlapping tumbling windows
//   Q49:       windowID = floor(leaf_index / W)
//   Q53a:      a partial window is republished at the same Key_M as it grows
//   Q53b:      real digests only; H(0x00 || 0x00) padding never participates

import (
	"crypto/sha256"
	"testing"

	"dhtbacked/dht"
	"dhtbacked/internal/hashing"
	"dhtbacked/mktree"
)

func refMicroRoot(ds []hashing.Digest) [32]byte {
	h := sha256.New()
	for _, d := range ds {
		h.Write(d[:])
	}
	var out [32]byte
	h.Sum(out[:0])
	return out
}

func readMicroRoot(t *testing.T, store *dht.DHT, treeID hashing.Digest, w uint64) (dht.MicroRootRecord, bool) {
	t.Helper()
	raw, err := store.GetRaw(dht.KeyM(treeID, w))
	if err != nil {
		return dht.MicroRootRecord{}, false
	}
	m, err := dht.ParseMicroRootRecord(raw)
	if err != nil {
		t.Fatalf("window %d: malformed micro-root record: %v", w, err)
	}
	return m, true
}

// Eq. (28), Q21, Q49, Q53b: with W = 4 and 10 real leaves in a 16-leaf tree,
// windows 0 and 1 are full, window 2 is partial (k = 2), and window 3 — all
// padding — is never published. Each published value is the tagless hash of
// exactly the real digests in its range.
func TestIndependentMicroRootsOverRealDigestsOnly(t *testing.T) {
	const W = 4
	store := newStore()
	part, err := mktree.NewPartitioner(smallConfig(16, 2, W), store)
	if err != nil {
		t.Fatal(err)
	}
	real := make([]hashing.Digest, 10)
	for i := range real {
		real[i] = digest(100 + i)
		if _, err := part.Insert("access", real[i]); err != nil {
			t.Fatal(err)
		}
	}
	tree, _ := part.Tree("access")
	treeID := tree.TreeID()

	want := []struct{ w, iMin, iMax uint64 }{{0, 0, 3}, {1, 4, 7}, {2, 8, 9}}
	for _, c := range want {
		m, ok := readMicroRoot(t, store, treeID, c.w)
		if !ok {
			t.Fatalf("window %d was not published", c.w)
		}
		if m.WindowID != c.w || m.IMin != c.iMin || m.IMax != c.iMax {
			t.Fatalf("window %d: range [%d,%d], want [%d,%d]", c.w, m.IMin, m.IMax, c.iMin, c.iMax)
		}
		if mktree.WindowID(c.iMin, W) != c.w || mktree.WindowID(c.iMax, W) != c.w {
			t.Fatalf("window %d: floor(leaf_index / W) disagrees with the published window", c.w)
		}
		if got, want := [32]byte(m.MicroRoot), refMicroRoot(real[c.iMin:c.iMax+1]); got != want {
			t.Fatalf("window %d: published micro-root is not H(d_%d || ... || d_%d) over real digests", c.w, c.iMin, c.iMax)
		}
	}
	// Window 3 covers leaf positions 12..15, all padding. It must not exist.
	if _, ok := readMicroRoot(t, store, treeID, 3); ok {
		t.Fatal("a window containing only padding was published; Q53b says padding never participates")
	}
}

// Q53b sharpened: the partial window's value must equal the hash over the k
// real digests and must differ from the value that would result if the
// H(0x00 || 0x00) dummies at positions 10 and 11 had been included.
func TestIndependentPaddingNeverEntersAMicroRoot(t *testing.T) {
	const W = 4
	store := newStore()
	part, err := mktree.NewPartitioner(smallConfig(16, 2, W), store)
	if err != nil {
		t.Fatal(err)
	}
	real := make([]hashing.Digest, 10)
	for i := range real {
		real[i] = digest(200 + i)
		if _, err := part.Insert("access", real[i]); err != nil {
			t.Fatal(err)
		}
	}
	tree, _ := part.Tree("access")
	m, ok := readMicroRoot(t, store, tree.TreeID(), 2)
	if !ok {
		t.Fatal("partial window 2 was not published")
	}
	dummy := hashing.Digest(sha256.Sum256([]byte{0x00, 0x00}))
	withPadding := refMicroRoot([]hashing.Digest{real[8], real[9], dummy, dummy})
	if [32]byte(m.MicroRoot) == withPadding {
		t.Fatal("the micro-root includes the two padding leaves")
	}
	if [32]byte(m.MicroRoot) != refMicroRoot(real[8:10]) {
		t.Fatal("the micro-root is not the hash of the two real digests")
	}
	if k := m.IMax - m.IMin + 1; k != 2 {
		t.Fatalf("window 2 covers k = %d records, want 2 (k <= W)", k)
	}
}

// Q53a, Q45: a partial window is republished at the same Key_M each time it
// grows. Its value changes until the window closes, the key never does, and
// exactly one record lives at that key throughout.
func TestIndependentPartialWindowRepublishesAtTheSameKey(t *testing.T) {
	const W = 4
	store := newStore()
	part, err := mktree.NewPartitioner(smallConfig(16, 2, W), store)
	if err != nil {
		t.Fatal(err)
	}
	tree, _ := part.Tree("access")
	treeID := tree.TreeID()

	var real []hashing.Digest
	var prev [32]byte
	for i := 0; i < W; i++ {
		d := digest(300 + i)
		real = append(real, d)
		if _, err := part.Insert("access", d); err != nil {
			t.Fatal(err)
		}
		m, ok := readMicroRoot(t, store, treeID, 0)
		if !ok {
			t.Fatalf("after %d inserts window 0 is not published", i+1)
		}
		if m.IMin != 0 || m.IMax != uint64(i) {
			t.Fatalf("after %d inserts: range [%d,%d], want [0,%d]", i+1, m.IMin, m.IMax, i)
		}
		cur := [32]byte(m.MicroRoot)
		if cur != refMicroRoot(real) {
			t.Fatalf("after %d inserts: value is not H over the %d real digests so far", i+1, i+1)
		}
		if i > 0 && cur == prev {
			t.Fatalf("after %d inserts: the partial window's value did not change as it grew", i+1)
		}
		prev = cur
	}
	// One logical object, one key, one entry: the newer write overwrote.
	n := 0
	store.ForEach(func(k hashing.Digest, _ []byte) {
		if k == dht.KeyM(treeID, 0) {
			n++
		}
	})
	if n != 1 {
		t.Fatalf("%d entries at window 0's Key_M; Q45 overwrite semantics require exactly 1", n)
	}
}

// Q54, §0.4.1: at the default settings — epoch 1,000 and W = 1,024 — there is
// exactly one window, windowID 0, and it never fills. This is the specified
// outcome, not a defect.
func TestIndependentDefaultSettingsGiveOneNeverFullWindow(t *testing.T) {
	store := newStore()
	cfg := mktree.DefaultConfig()
	if cfg.Window != 1024 || cfg.EpochCapacity != 1000 {
		t.Fatalf("defaults are W=%d, epoch=%d; want 1024, 1000", cfg.Window, cfg.EpochCapacity)
	}
	part, err := mktree.NewPartitioner(cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	var real []hashing.Digest
	for i := 0; i < 1000; i++ {
		d := digest(5000 + i)
		real = append(real, d)
		if _, err := part.Insert("access", d); err != nil {
			t.Fatal(err)
		}
	}
	tree, _ := part.Tree("access")
	m, ok := readMicroRoot(t, store, tree.TreeID(), 0)
	if !ok {
		t.Fatal("window 0 was not published")
	}
	if m.IMin != 0 || m.IMax != 999 {
		t.Fatalf("window 0 range [%d,%d], want [0,999]: 1,000 records never fill a 1,024 window", m.IMin, m.IMax)
	}
	if [32]byte(m.MicroRoot) != refMicroRoot(real) {
		t.Fatal("window 0's value is not H over all 1,000 real digests")
	}
	if _, ok := readMicroRoot(t, store, tree.TreeID(), 1); ok {
		t.Fatal("a second window exists at default settings; Q54 expects exactly one")
	}
}

// Q16, R-RTV-03: the authoritative per-type root is the MK-Tree root N_0,0.
// Eq. (29)'s flat hash over the micro-roots is not substituted for it.
func TestIndependentPerTypeRootIsNotEq29(t *testing.T) {
	const W = 4
	store := newStore()
	part, err := mktree.NewPartitioner(smallConfig(16, 2, W), store)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		if _, err := part.Insert("access", digest(400+i)); err != nil {
			t.Fatal(err)
		}
	}
	tree, _ := part.Tree("access")
	var micros []hashing.Digest
	for w := uint64(0); w < 3; w++ {
		m, ok := readMicroRoot(t, store, tree.TreeID(), w)
		if !ok {
			t.Fatalf("window %d missing", w)
		}
		micros = append(micros, m.MicroRoot)
	}
	eq29 := refMicroRoot(micros) // H(R^(delta1) || R^(delta2) || ...)
	if [32]byte(tree.Root()) == eq29 {
		t.Fatal("the per-type root equals Eq. (29)'s micro-root aggregate; Q16 makes N_0,0 authoritative")
	}
	n00, err := tree.Node(0, 0)
	if err != nil || tree.Root() != n00 {
		t.Fatal("the per-type root is not N_0,0")
	}
}
