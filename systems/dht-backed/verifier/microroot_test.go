package verifier_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"

	"dhtbacked/dht"
	"dhtbacked/internal/clock"
	"dhtbacked/internal/hashing"
	"dhtbacked/mktree"
	"dhtbacked/verifier"
)

// Q49: windowID is a zero-based uint64 counter local to each (epoch, log_type)
// tree, windowID = floor(leaf_index / W). Windows are count-based and
// non-overlapping; overlapping sliding windows are not implemented.
func TestTumblingWindowMembership(t *testing.T) {
	for _, w := range []uint64{256, 512, 1024, 2048} {
		for i := uint64(0); i < 4*w; i++ {
			got := mktree.WindowID(i, w)
			want := i / w
			if got != want {
				t.Fatalf("W = %d: leaf %d is in window %d, want %d", w, i, got, want)
			}
		}
		// Boundaries: [0, W-1] is window 0, [W, 2W-1] is window 1.
		if mktree.WindowID(w-1, w) != 0 || mktree.WindowID(w, w) != 1 {
			t.Fatalf("W = %d: window boundary is wrong", w)
		}
		if mktree.WindowStart(3, w) != 3*w {
			t.Fatalf("W = %d: i_min is wrong", w)
		}
	}
	// Every leaf belongs to exactly one window: membership never overlaps.
	const w = 256
	seen := map[uint64]uint64{}
	for i := uint64(0); i < 1024; i++ {
		id := mktree.WindowID(i, w)
		if prev, dup := seen[i]; dup && prev != id {
			t.Fatal("a leaf belongs to two windows")
		}
		seen[i] = id
	}
}

// R-RTV-01 / Eq. (28): R^(delta)_tau = H(d_i1 || ... || d_ik) with no
// domain-separation tag.
func TestMicroRootHasNoDomainSeparationTag(t *testing.T) {
	a := sha256.Sum256([]byte("a"))
	b := sha256.Sum256([]byte("b"))
	pre := append(append([]byte{}, a[:]...), b[:]...)
	if mktree.MicroRoot([]hashing.Digest{a, b}) != sha256.Sum256(pre) {
		t.Fatal("micro-root is not the untagged concatenation of the digests")
	}
	// Order matters and no separator is inserted.
	if mktree.MicroRoot([]hashing.Digest{a, b}) == mktree.MicroRoot([]hashing.Digest{b, a}) {
		t.Fatal("micro-root ignores digest order")
	}
}

// Q53b: dummy padding leaves never participate in a micro-root.
func TestMicroRootCoversRealDigestsOnly(t *testing.T) {
	cfg := defaultConfig()
	cfg.Window = 256
	f := build(t, 400, cfg, true)
	ctx := context.Background()

	rec, _ := f.store.Get(ctx, f.records[0].RecordID)
	treeID := dht.TreeID(rec.Epoch, rec.LogType)
	raw, err := f.sys.DHT.GetRaw(dht.KeyM(treeID, 0))
	if err != nil {
		t.Fatal(err)
	}
	m, err := dht.ParseMicroRootRecord(raw)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := f.sys.Pipeline.Partitioner().Tree(rec.LogType)
	if err != nil {
		t.Fatal(err)
	}
	k := m.IMax - m.IMin + 1
	if k > tree.LeafCount() {
		t.Fatalf("micro-root claims %d records but only %d were ingested", k, tree.LeafCount())
	}
	// Recompute over the real digests only, and confirm the dummy leaf is not
	// among them.
	digests := make([]hashing.Digest, 0, k)
	for i := m.IMin; i <= m.IMax; i++ {
		n, err := tree.Node(tree.Levels(), i)
		if err != nil {
			t.Fatal(err)
		}
		if n == mktree.DummyLeaf() {
			t.Fatalf("leaf %d inside the micro-root range is a dummy", i)
		}
		digests = append(digests, n)
	}
	if mktree.MicroRoot(digests) != m.MicroRoot {
		t.Fatal("the published micro-root does not cover exactly the real digests in its range")
	}
	// Padding a window out to W would change the value; it must not have been
	// padded.
	padded := append(append([]hashing.Digest(nil), digests...), mktree.DummyLeaf())
	if mktree.MicroRoot(padded) == m.MicroRoot {
		t.Fatal("the micro-root includes padding")
	}
}

// Q53a: a partial window emits a provisional micro-root that is republished at
// the same Key_M each time the window grows.
func TestPartialWindowsEmitAndUpdate(t *testing.T) {
	cfg := defaultConfig()
	store := dht.New(nil, f0clock())
	part, err := mktree.NewPartitioner(cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	treeID := dht.TreeID(1, "access")
	key := dht.KeyM(treeID, 0)

	var previous hashing.Digest
	var previousIMax uint64
	for i := 0; i < 12; i++ {
		d := hashing.H([]byte("record"), []byte{byte(i)})
		if _, err := part.Insert("access", d); err != nil {
			t.Fatal(err)
		}
		raw, err := store.GetRaw(key)
		if err != nil {
			t.Fatalf("insert %d: no micro-root published: %v", i, err)
		}
		m, err := dht.ParseMicroRootRecord(raw)
		if err != nil {
			t.Fatal(err)
		}
		if m.IMin != 0 {
			t.Fatalf("insert %d: i_min = %d, want 0", i, m.IMin)
		}
		if m.IMax != uint64(i) {
			t.Fatalf("insert %d: i_max = %d, want %d", i, m.IMax, i)
		}
		if i > 0 {
			if m.MicroRoot == previous {
				t.Fatalf("insert %d: the provisional micro-root did not change", i)
			}
			if m.IMax <= previousIMax {
				t.Fatalf("insert %d: i_max did not advance", i)
			}
		}
		previous, previousIMax = m.MicroRoot, m.IMax
	}
	// All republications went to the same key: the DHT holds one micro-root
	// record for the window, not twelve.
	count := 0
	for w := uint64(0); w < 4; w++ {
		if _, err := store.GetRaw(dht.KeyM(treeID, w)); err == nil {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("%d micro-root records exist for the tree, want 1", count)
	}
}

// Q54 / §0.4.1: at the default configuration (epoch 1,000, W = 1,024) there is
// exactly one window, windowID = 0, which never fills. This is the specified
// outcome, not a defect.
func TestDefaultConfigurationProducesOneNeverFullWindow(t *testing.T) {
	cfg := defaultConfig()
	if cfg.Window != 1024 || cfg.EpochCapacity != 1000 {
		t.Fatalf("default configuration is W = %d, epoch = %d", cfg.Window, cfg.EpochCapacity)
	}
	f := build(t, 1000, cfg, true)
	for _, lt := range cfg.LogTypes {
		treeID := dht.TreeID(1, lt)
		tree, err := f.sys.Pipeline.Partitioner().Tree(lt)
		if err != nil {
			t.Fatal(err)
		}
		if tree.LeafCount() == 0 {
			continue
		}
		if _, err := f.sys.DHT.GetRaw(dht.KeyM(treeID, 0)); err != nil {
			t.Fatalf("%s: window 0 has no micro-root: %v", lt, err)
		}
		if _, err := f.sys.DHT.GetRaw(dht.KeyM(treeID, 1)); err == nil {
			t.Fatalf("%s: a second window exists at the default configuration", lt)
		}
		raw, _ := f.sys.DHT.GetRaw(dht.KeyM(treeID, 0))
		m, _ := dht.ParseMicroRootRecord(raw)
		if m.WindowID != 0 {
			t.Fatalf("%s: windowID = %d", lt, m.WindowID)
		}
		if k := m.IMax - m.IMin + 1; k >= cfg.Window {
			t.Fatalf("%s: the window filled (k = %d)", lt, k)
		}
	}
}

// Q54: multi-window behaviour is exercised at W = 256 and W = 512.
func TestMultipleWindowsAtSmallerW(t *testing.T) {
	for _, w := range []uint64{256, 512} {
		cfg := defaultConfig()
		cfg.Window = w
		store := dht.New(nil, f0clock())
		part, err := mktree.NewPartitioner(cfg, store)
		if err != nil {
			t.Fatal(err)
		}
		const n = 1000
		for i := 0; i < n; i++ {
			if _, err := part.Insert("access", hashing.H([]byte{byte(i), byte(i >> 8)})); err != nil {
				t.Fatal(err)
			}
		}
		if _, _, err := part.Finalize(); err != nil {
			t.Fatal(err)
		}
		treeID := dht.TreeID(1, "access")
		wantWindows := uint64((n + int(w) - 1) / int(w))
		found := uint64(0)
		for id := uint64(0); id < 8; id++ {
			if _, err := store.GetRaw(dht.KeyM(treeID, id)); err == nil {
				found++
			}
		}
		if found != wantWindows {
			t.Fatalf("W = %d: %d windows, want %d", w, found, wantWindows)
		}
	}
}

// Q16/R-RTV-03: Eq. (29) is not implemented as the per-type root. The
// authoritative per-type root remains N_0,0.
func TestPerTypeRootIsNotTheMicroRootAggregate(t *testing.T) {
	cfg := defaultConfig()
	cfg.Window = 256
	store := dht.New(nil, f0clock())
	part, _ := mktree.NewPartitioner(cfg, store)
	for i := 0; i < 700; i++ {
		if _, err := part.Insert("access", hashing.H([]byte{byte(i), byte(i >> 8)})); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := part.Finalize(); err != nil {
		t.Fatal(err)
	}
	tree, _ := part.Tree("access")
	treeID := dht.TreeID(1, "access")

	// Eq. (29)'s flat consolidation of the micro-roots.
	var micro []hashing.Digest
	for id := uint64(0); ; id++ {
		raw, err := store.GetRaw(dht.KeyM(treeID, id))
		if err != nil {
			break
		}
		m, _ := dht.ParseMicroRootRecord(raw)
		micro = append(micro, m.MicroRoot)
	}
	if len(micro) < 2 {
		t.Fatal("fixture produced fewer than two windows")
	}
	eq29 := mktree.MicroRoot(micro)
	if tree.Root() == eq29 {
		t.Fatal("the per-type root equals Eq. (29)'s aggregate; N_0,0 must be authoritative")
	}
	// The published Root Record carries N_0,0.
	raw, err := store.GetRaw(dht.KeyR(treeID))
	if err != nil {
		t.Fatal(err)
	}
	rr, _ := dht.ParseRootRecord(raw)
	if rr.Root != tree.Root() {
		t.Fatal("the Root Record does not carry N_0,0")
	}
}

// R-RTV-04 / Q52: a micro-root check is available while the epoch is open, and
// is explicitly provisional.
func TestProvisionalMicroRootCheckDuringAnOpenEpoch(t *testing.T) {
	cfg := defaultConfig()
	cfg.Window = 256
	f := build(t, 300, cfg, false)
	ctx := context.Background()

	// Full Phase 4 verification is refused while the epoch is open.
	if _, err := f.sys.Verifier.VerifyInclusion(ctx, &f.token, f.records[0].RecordID); !errors.Is(err, verifier.ErrEpochNotAnchored) {
		t.Fatalf("open epoch inclusion returned %v", err)
	}
	// The micro-root check succeeds and is marked provisional.
	res, err := f.sys.Verifier.VerifyMicroRoot(ctx, &f.token, f.records[0].RecordID)
	if err != nil {
		t.Fatalf("micro-root check: %v", err)
	}
	if !res.Accepted || !res.Provisional {
		t.Fatalf("micro-root result = %+v", res)
	}
	if res.Covered == 0 || res.IMax < res.IMin {
		t.Fatalf("micro-root range = [%d, %d]", res.IMin, res.IMax)
	}

	// Tampering with a covered record breaks the provisional check too.
	rec, _ := f.store.Get(ctx, f.records[0].RecordID)
	tampered := rec
	tampered.C = append([]byte(nil), rec.C...)
	tampered.C[0] ^= 0xFF
	f.store.Replace(tampered)
	if _, err := f.sys.Verifier.VerifyMicroRoot(ctx, &f.token, rec.RecordID); err == nil {
		t.Fatal("a tampered record passed the micro-root check")
	}
	f.store.Replace(rec)

	// A corrupted micro-root record is rejected.
	treeID := dht.TreeID(rec.Epoch, rec.LogType)
	key := dht.KeyM(treeID, mktree.WindowID(rec.LeafIndex, cfg.Window))
	original, _ := f.sys.DHT.GetRaw(key)
	m, _ := dht.ParseMicroRootRecord(original)
	m.MicroRoot[0] ^= 0xFF
	f.sys.DHT.PutRaw(key, m.Serialize())
	if _, err := f.sys.Verifier.VerifyMicroRoot(ctx, &f.token, rec.RecordID); err == nil {
		t.Fatal("a corrupted micro-root was accepted")
	}
	f.sys.DHT.PutRaw(key, original)

	// The token gate applies to micro-root requests as well.
	if _, err := f.sys.Verifier.VerifyMicroRoot(ctx, nil, rec.RecordID); !errors.Is(err, verifier.ErrProofUnavailable) {
		t.Fatal("a micro-root was served without a token")
	}
}

func f0clock() *clock.Manual { return &clock.Manual{} }
