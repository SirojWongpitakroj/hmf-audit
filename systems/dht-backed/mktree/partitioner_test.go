package mktree_test

import (
	"crypto/sha256"
	"testing"

	"dhtbacked/dht"
	"dhtbacked/internal/clock"
	"dhtbacked/internal/hashing"
	"dhtbacked/mktree"
)

func newStore() *dht.DHT { return dht.New(nil, &clock.Manual{}) }

func smallConfig(capacity, stride, window uint64) mktree.Config {
	return mktree.Config{
		EpochCapacity: capacity,
		Stride:        stride,
		Window:        window,
		LogTypes:      []string{"access", "api", "auth", "error", "security"},
	}
}

// Q23: a global monotonically increasing seq beginning at 1, incremented once
// per accepted log-state update; all objects emitted by that update share it.
func TestSeqIsGlobalAndStartsAtOne(t *testing.T) {
	store := newStore()
	part, err := mktree.NewPartitioner(smallConfig(64, 2, 16), store)
	if err != nil {
		t.Fatal(err)
	}
	if part.Seq() != 0 {
		t.Fatalf("seq before any update = %d, want 0", part.Seq())
	}
	types := []string{"access", "api", "access", "security", "api"}
	for i, lt := range types {
		if _, err := part.Insert(lt, digest(i)); err != nil {
			t.Fatal(err)
		}
		if part.Seq() != uint64(i+1) {
			t.Fatalf("after update %d seq = %d, want %d", i, part.Seq(), i+1)
		}
	}
	// Every object emitted by the last update carries the same seq, and seq is
	// global rather than per tree.
	last := part.Seq()
	tree, _ := part.Tree("api")
	treeID := tree.TreeID()
	raw, err := store.GetRaw(dht.KeyR(treeID))
	if err != nil {
		t.Fatal(err)
	}
	rr, _ := dht.ParseRootRecord(raw)
	if rr.Seq != last {
		t.Fatalf("root record seq = %d, want %d", rr.Seq, last)
	}
	raw, err = store.GetRaw(dht.KeyM(treeID, 0))
	if err != nil {
		t.Fatal(err)
	}
	m, _ := dht.ParseMicroRootRecord(raw)
	if m.Seq != last {
		t.Fatalf("micro-root seq = %d, want %d", m.Seq, last)
	}
	if rr.LeafCnt != 2 {
		t.Fatalf("api leafCnt = %d, want 2", rr.LeafCnt)
	}
}

// R-DHT-04 acceptance: only nodes at levels j > s are published as P_j,k.
func TestOnlyNodesBelowTheAncestorLevelArePublished(t *testing.T) {
	const stride = uint64(3)
	store := newStore()
	part, err := mktree.NewPartitioner(smallConfig(64, stride, 16), store)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		if _, err := part.Insert("access", digest(i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := part.Finalize(); err != nil {
		t.Fatal(err)
	}
	tree, _ := part.Tree("access")
	treeID := tree.TreeID()
	for j := uint64(0); j <= tree.Levels(); j++ {
		for k := uint64(0); k < uint64(1)<<j; k++ {
			_, err := store.GetRaw(dht.KeyP(treeID, j, k))
			published := err == nil
			if j > stride && !published {
				t.Fatalf("node (%d,%d) below the ancestor level is not published", j, k)
			}
			if j <= stride && published {
				t.Fatalf("node (%d,%d) at or above the ancestor level is published as P_j,k", j, k)
			}
		}
	}
	// Every level-s ancestor has a certificate instead.
	for k := uint64(0); k < tree.AncestorCount(); k++ {
		if _, err := store.GetRaw(dht.KeyC(treeID, stride, k)); err != nil {
			t.Fatalf("ancestor %d has no certificate", k)
		}
	}
}

// R-DHT-05 / Eq. (6): the root record carries (tree_id, seq, R, leafCnt, tau).
func TestRootRecordContents(t *testing.T) {
	store := newStore()
	part, _ := mktree.NewPartitioner(smallConfig(64, 2, 16), store)
	for i := 0; i < 17; i++ {
		if _, err := part.Insert("error", digest(i)); err != nil {
			t.Fatal(err)
		}
	}
	tree, _ := part.Tree("error")
	raw, err := store.GetRaw(dht.KeyR(tree.TreeID()))
	if err != nil {
		t.Fatal(err)
	}
	rr, err := dht.ParseRootRecord(raw)
	if err != nil {
		t.Fatal(err)
	}
	if rr.TreeID != tree.TreeID() || rr.Root != tree.Root() || rr.LeafCnt != 17 || rr.LogType != "error" {
		t.Fatalf("root record = %+v", rr)
	}
	if rr.Seq != part.Seq() {
		t.Fatalf("root record seq = %d, want %d", rr.Seq, part.Seq())
	}
}

// Q45: a newer write overwrites the previous value at the same key, so the DHT
// holds one record per logical object rather than one per update.
func TestDHTHoldsOneRecordPerLogicalObject(t *testing.T) {
	const stride = uint64(2)
	store := newStore()
	part, _ := mktree.NewPartitioner(smallConfig(16, stride, 8), store)
	for i := 0; i < 16; i++ {
		if _, err := part.Insert("access", digest(i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := part.Finalize(); err != nil {
		t.Fatal(err)
	}
	// Expected records for the five trees: P_j,k for j in (s, L], one
	// certificate per level-s ancestor, one root record, plus micro-roots for
	// the trees that received records.
	tree, _ := part.Tree("access")
	parents := uint64(0)
	for j := stride + 1; j <= tree.Levels(); j++ {
		parents += uint64(1) << j
	}
	certs := tree.AncestorCount()
	perTree := parents + certs + 1
	want := int(perTree)*5 + 2 // five trees; "access" holds two micro-root windows
	if store.Count() != want {
		t.Fatalf("DHT holds %d records, want %d", store.Count(), want)
	}
}

// Q24: within an epoch the tree geometry, node coordinates, stride levels and
// ancestor identifiers are stable.
func TestAddressingIsStableWithinAnEpoch(t *testing.T) {
	store := newStore()
	part, _ := mktree.NewPartitioner(smallConfig(64, 3, 16), store)
	tree, _ := part.Tree("access")
	treeID := tree.TreeID()
	levels := tree.Levels()
	ancestorIDs := make([]hashing.Digest, tree.AncestorCount())
	for k := range ancestorIDs {
		ancestorIDs[k] = dht.AncestorID(treeID, tree.Stride(), uint64(k))
	}
	for i := 0; i < 50; i++ {
		if _, err := part.Insert("access", digest(i)); err != nil {
			t.Fatal(err)
		}
		if tree.Levels() != levels || tree.TreeID() != treeID {
			t.Fatal("tree geometry changed mid-epoch")
		}
		for k := range ancestorIDs {
			if dht.AncestorID(treeID, tree.Stride(), uint64(k)) != ancestorIDs[k] {
				t.Fatal("an ancestor identifier changed mid-epoch")
			}
		}
	}
}

// R-EPO-01 / Eq. (7): R_agg = H(R_tau1 || ... || R_taum) with no tag, over log
// types sorted lexicographically.
func TestRootOfRootsIsUntaggedAndLexicographic(t *testing.T) {
	a := sha256.Sum256([]byte("a"))
	b := sha256.Sum256([]byte("b"))
	pre := append(append([]byte{}, a[:]...), b[:]...)
	if mktree.RootOfRoots([]hashing.Digest{a, b}) != sha256.Sum256(pre) {
		t.Fatal("R_agg is not the untagged concatenation of the per-type roots")
	}

	store := newStore()
	cfg := smallConfig(64, 2, 16)
	part, _ := mktree.NewPartitioner(cfg, store)
	for i := 0; i < 30; i++ {
		if _, err := part.Insert(cfg.LogTypes[i%len(cfg.LogTypes)], digest(i)); err != nil {
			t.Fatal(err)
		}
	}
	roots, agg, err := part.Finalize()
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 5 {
		t.Fatalf("%d per-type roots, want 5", len(roots))
	}
	// The returned order is the lexicographic type order.
	for i, lt := range []string{"access", "api", "auth", "error", "security"} {
		tree, _ := part.Tree(lt)
		if roots[i] != tree.Root() {
			t.Fatalf("root %d is not %s's root", i, lt)
		}
	}
	if agg != mktree.RootOfRoots(roots) {
		t.Fatal("the aggregate does not match the per-type roots")
	}
	// Any other ordering yields a different aggregate.
	shuffled := []hashing.Digest{roots[1], roots[0], roots[2], roots[3], roots[4]}
	if mktree.RootOfRoots(shuffled) == agg {
		t.Fatal("R_agg is insensitive to type ordering")
	}
}

// Q23: the epoch boundary is count-based.
func TestEpochBoundaryIsCountBased(t *testing.T) {
	store := newStore()
	cfg := smallConfig(10, 2, 8)
	part, _ := mktree.NewPartitioner(cfg, store)
	for i := 0; i < 10; i++ {
		if part.Full() {
			t.Fatalf("epoch reported full after %d records", i)
		}
		if _, err := part.Insert("access", digest(i)); err != nil {
			t.Fatal(err)
		}
	}
	if !part.Full() {
		t.Fatal("epoch is not full at its capacity")
	}
	if _, err := part.Insert("access", digest(99)); err != mktree.ErrEpochClosed {
		t.Fatalf("insert past the boundary returned %v", err)
	}
	if _, _, err := part.Finalize(); err != nil {
		t.Fatal(err)
	}
	if _, err := part.Insert("access", digest(99)); err != mktree.ErrEpochClosed {
		t.Fatalf("insert into a finalized epoch returned %v", err)
	}
	if err := part.NextEpoch(); err != nil {
		t.Fatal(err)
	}
	if part.Epoch() != 2 || part.Accepted() != 0 {
		t.Fatalf("epoch = %d, accepted = %d", part.Epoch(), part.Accepted())
	}
	// A fresh epoch has fresh tree identifiers.
	tree, _ := part.Tree("access")
	if tree.TreeID() != dht.TreeID(2, "access") {
		t.Fatal("the new epoch reuses the previous tree identifier")
	}
	if _, err := part.Insert("access", digest(0)); err != nil {
		t.Fatal(err)
	}
	if part.Seq() != 11 {
		t.Fatalf("seq = %d; it must keep increasing across epochs and advance only on accepted updates", part.Seq())
	}
}

func TestUnknownLogTypeIsRejected(t *testing.T) {
	store := newStore()
	part, _ := mktree.NewPartitioner(smallConfig(16, 2, 8), store)
	if _, err := part.Insert("unknown", digest(0)); err != mktree.ErrUnknownType {
		t.Fatalf("unknown log type returned %v", err)
	}
}

// Q26 / R-EPO-01: the per-type roots feeding R^(e)_agg are ordered
// lexicographically by canonical UTF-8 log_type. The commit path must derive
// that order itself rather than trusting the order the caller supplied, because
// the verifier's anchor reconciliation sorts independently (verifier.go). If
// the two disagree, a correctly committed anchor fails every verification.
func TestAggregateOrderIsLexicographicRegardlessOfConfigOrder(t *testing.T) {
	shuffled := []string{"security", "access", "error", "api", "auth"}
	sorted := []string{"access", "api", "auth", "error", "security"}

	build := func(types []string) hashing.Digest {
		cfg := smallConfig(64, 2, 16)
		cfg.LogTypes = append([]string(nil), types...)
		part, err := mktree.NewPartitioner(cfg, newStore())
		if err != nil {
			t.Fatal(err)
		}
		for i, lt := range []string{"access", "api", "auth", "error", "security"} {
			if _, err := part.Insert(lt, digest(i)); err != nil {
				t.Fatal(err)
			}
		}
		_, agg, err := part.Finalize()
		if err != nil {
			t.Fatal(err)
		}
		return agg
	}

	if got, want := build(shuffled), build(sorted); got != want {
		t.Fatalf("R_agg depends on the configured log-type order:\n shuffled = %x\n sorted   = %x", got, want)
	}
}
