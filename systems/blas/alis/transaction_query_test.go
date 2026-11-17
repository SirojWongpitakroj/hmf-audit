package alis

import (
	"reflect"
	"testing"

	"blas/bhash"
	"blas/bitmap"
	"blas/iomf"
	"blas/omt"
)

func makeTestBitmap(n uint64, setBits []uint64) bitmap.Bitmap {
	bm := bitmap.New(n)
	for _, i := range setBits {
		bm.Set(i)
	}
	return bm
}

func buildTxLevelFixture(t *testing.T) (*TransactionLevelBuilder, *iomf.Store, []Transaction) {
	t.Helper()
	metas := []FileMetadata{
		mkMeta(1, 1, 100, 150, []string{"login"}),
		mkMeta(1, 2, 200, 250, []string{"login", "error"}),
		mkMeta(2, 1, 300, 350, []string{"error"}),
		mkMeta(3, 1, 400, 450, []string{"login"}),
	}
	b, store, err := BuildTransactionLevel(2, metas)
	if err != nil {
		t.Fatal(err)
	}
	return b, store, b.Transactions()
}

func txRootHashes(t *testing.T, store *iomf.Store) TxLevelRoots {
	t.Helper()
	omtRoot, err := store.OMT.Root()
	if err != nil {
		t.Fatal(err)
	}
	fkRoot, err := store.IIMT.Root()
	if err != nil {
		t.Fatal(err)
	}
	tsMinRoot, err := store.RIMBTs["ts_min"].Root()
	if err != nil {
		t.Fatal(err)
	}
	tsMaxRoot, err := store.RIMBTs["ts_max"].Root()
	if err != nil {
		t.Fatal(err)
	}
	return TxLevelRoots{OMTRoot: omtRoot, FKRoot: fkRoot, TsMinRoot: tsMinRoot, TsMaxRoot: tsMaxRoot}
}

// TestTxLevelLGRun: lg=1 owns tsn 0,1 (contiguous, since sorted by lid then
// fid); left neighbor absent (run starts at 0), right neighbor is tsn=2.
func TestTxLevelLGRun(t *testing.T) {
	_, store, txs := buildTxLevelFixture(t)
	q := TxQuery{LG: 1}
	vo, err := ProcessTxLevelQuery(store, txs, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(vo.LGProofs) != 2 {
		t.Fatalf("expected 2 transactions for lg=1, got %d", len(vo.LGProofs))
	}
	if vo.LGLeftNeighbor != nil {
		t.Fatal("expected no left neighbor (run starts at tsn=0)")
	}
	if vo.LGRightNeighbor == nil || vo.LGRightNeighbor.OID != 2 {
		t.Fatalf("expected right neighbor at tsn=2, got %+v", vo.LGRightNeighbor)
	}
	rh := txRootHashes(t, store)
	if !VerifyTxLevelQuery(rh, uint64(len(txs)), q, vo) {
		t.Fatal("honest transaction-level LG query should verify")
	}
}

// TestTxLevelCombinedQuery: lg=1 AND keyword "login" AND range covering
// only the first of lg's two transactions.
func TestTxLevelCombinedQuery(t *testing.T) {
	_, store, txs := buildTxLevelFixture(t)
	q := TxQuery{LG: 1, FK: iomf.Keyword("login"), HasRange: true, TS: 0, TE: 180}
	vo, err := ProcessTxLevelQuery(store, txs, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(vo.ResultProofs) != 1 || vo.ResultProofs[0].OID != 0 {
		t.Fatalf("expected exactly tsn=0, got %+v", vo.ResultProofs)
	}
	rh := txRootHashes(t, store)
	if !VerifyTxLevelQuery(rh, uint64(len(txs)), q, vo) {
		t.Fatal("honest combined transaction-level query should verify")
	}
}

// TestTxLevelForgedNeighborRejected: T-037 — a right "neighbour" that
// actually belongs to lg (hiding a real transaction) must be rejected.
func TestTxLevelForgedNeighborRejected(t *testing.T) {
	_, store, txs := buildTxLevelFixture(t)
	q := TxQuery{LG: 1}
	vo, err := ProcessTxLevelQuery(store, txs, q)
	if err != nil {
		t.Fatal(err)
	}
	rh := txRootHashes(t, store)
	if !VerifyTxLevelQuery(rh, uint64(len(txs)), q, vo) {
		t.Fatal("honest query should verify")
	}

	// Attack: hide tsn=1 (drop it from LGProofs) and claim tsn=1's own
	// leaf is the "right neighbor" of a now-shrunk run {tsn=0}.
	forged := vo
	forged.LGProofs = vo.LGProofs[:1] // only tsn=0
	badNeighbor, ok := store.OMT.Prove(1)
	if !ok {
		t.Fatal("expected tsn=1 to exist")
	}
	forged.LGRightNeighbor = &badNeighbor
	if VerifyTxLevelQuery(rh, uint64(len(txs)), q, forged) {
		t.Fatal("a right neighbour that itself belongs to lg should be rejected")
	}
}

// TestTxLevelNonContiguousRejected: a forged LGProofs set with a gap must
// be rejected (contiguity is an invariant of the (lid,fid) sort).
func TestTxLevelNonContiguousRejected(t *testing.T) {
	_, store, txs := buildTxLevelFixture(t)
	q := TxQuery{LG: 1}
	vo, err := ProcessTxLevelQuery(store, txs, q)
	if err != nil {
		t.Fatal(err)
	}
	rh := txRootHashes(t, store)

	// Splice in a proof for tsn=3 (belongs to lid=3) mislabeled as if it
	// were part of lg=1's run — DecodeTransaction will reveal LID=3!= 1.
	p3, ok := store.OMT.Prove(3)
	if !ok {
		t.Fatal("expected tsn=3 to exist")
	}
	tampered := vo
	tampered.LGProofs = append(append([]omt.Proof{}, vo.LGProofs...), p3)
	if VerifyTxLevelQuery(rh, uint64(len(txs)), q, tampered) {
		t.Fatal("a transaction with the wrong LID injected into lg's run should be rejected")
	}
}

func TestDecodeTransactionRoundTrip(t *testing.T) {
	m := FileMetadata{
		LID: 5, FID: 9, TsMin: 111, TsMax: 222, FW: []string{"b", "a"},
		BmEW:   makeTestBitmap(4, []uint64{1, 3}),
		RHIIMT: bhash.H([]byte("iimt")), RHOMT: bhash.H([]byte("omt")),
	}
	tx := NewTransaction(7, m)
	encoded := EncodeTransaction(tx)
	decoded, err := DecodeTransaction(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.TSN != 7 || decoded.LID != 5 || decoded.FID != 9 || decoded.TsMin != 111 || decoded.TsMax != 222 {
		t.Fatalf("decode mismatch: %+v", decoded)
	}
	if !reflect.DeepEqual(decoded.FW, []string{"a", "b"}) {
		t.Fatalf("decoded FW = %v, want sorted [a b]", decoded.FW)
	}
	if decoded.RHIIMT != tx.RHIIMT || decoded.RHOMT != tx.RHOMT {
		t.Fatal("decoded RH digests mismatch")
	}
	for i := uint64(0); i < 4; i++ {
		want := i == 1 || i == 3
		if decoded.BmEW.Get(i) != want {
			t.Fatalf("decoded BmEW bit %d = %v, want %v", i, decoded.BmEW.Get(i), want)
		}
	}
}

// TestProcessTxLevelQueryAggregatedRoundTrip: GAP-32 closure — the
// aggregated path produces the SAME matched-transaction set as the plain
// path, verifiable against the segment-wide aggregation root instead of
// this block's own transaction-level OMT root.
func TestProcessTxLevelQueryAggregatedRoundTrip(t *testing.T) {
	_, store, txs := buildTxLevelFixture(t)
	height := uint64(7) // arbitrary global height for this block within its segment

	trees, indexes := BuildGeneratorAggregationTrees(2, []BlockTxs{{Height: height, Txs: txs}})
	tree, ok := trees[1]
	if !ok {
		t.Fatal("expected an aggregation tree for lid=1")
	}
	idx := indexes[1]

	q := TxQuery{LG: 1, FK: nil}
	plainVO, err := ProcessTxLevelQuery(store, txs, q)
	if err != nil {
		t.Fatal(err)
	}
	aggVO, err := ProcessTxLevelQueryAggregated(store, txs, height, tree, idx, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(aggVO.ResultProofs) != 0 {
		t.Fatal("aggregated VO should not populate ResultProofs")
	}
	if len(aggVO.AggregatedResultTxs) != len(plainVO.ResultProofs) {
		t.Fatalf("expected %d aggregated result txs, got %d", len(plainVO.ResultProofs), len(aggVO.AggregatedResultTxs))
	}

	rh := txRootHashes(t, store)
	if !VerifyTxLevelQuery(rh, uint64(len(txs)), q, plainVO) {
		t.Fatal("plain VO should verify")
	}
	if !VerifyTxLevelQueryAggregated(rh, uint64(len(txs)), q, aggVO, tree.Root(), height) {
		t.Fatal("aggregated VO should verify")
	}

	// Wrong block height: a genuine proof attributed to a different block
	// height must be rejected.
	tampered := aggVO
	tampered.AggregatedResultTxs = append([]AggregatedTxProof{}, aggVO.AggregatedResultTxs...)
	if len(tampered.AggregatedResultTxs) > 0 {
		tampered.AggregatedResultTxs[0].BlockHeight = height + 1
		if VerifyTxLevelQueryAggregated(rh, uint64(len(txs)), q, tampered, tree.Root(), height) {
			t.Fatal("a proof claiming the wrong block height should be rejected")
		}
	}

	// Forged aggregation root: verifying against a DIFFERENT (wrong) root
	// must fail.
	forgedRoot := tree.Root()
	forgedRoot[0] ^= 0xFF
	if VerifyTxLevelQueryAggregated(rh, uint64(len(txs)), q, aggVO, forgedRoot, height) {
		t.Fatal("verifying against a forged aggregation root should fail")
	}
}

// TestVerifyTxLevelRejectsForgedRangeQR: security regression (found by
// independent tester verification) — a malicious full node that injects an
// out-of-range transaction into a time-range sub-query's claimed result
// (via a genuine OMT proof for that transaction plus a forged
// vo.TsMaxRange.QR/vo.TsMinRange.QR) must be rejected, even though every
// individual Merkle proof it supplies (the result proof itself, and the
// untouched range boundary proofs) verifies fine on its own. Before the
// fix, verifyTxLevelCommon recomputed the range bitmap directly from
// vo.TsMaxRange.QR/vo.TsMinRange.QR (server-supplied, unauthenticated on
// its own) instead of from the VERIFIED cumulative bitmaps in
// LowerProof/UpperProof, so this forgery was silently accepted whenever the
// forged transaction also belonged to the queried lg — exactly the case
// here: tsn=1 genuinely belongs to lg=1's run, but its [200,250] interval
// does not intersect the queried [0,180] range.
func TestVerifyTxLevelRejectsForgedRangeQR(t *testing.T) {
	_, store, txs := buildTxLevelFixture(t)
	q := TxQuery{LG: 1, HasRange: true, TS: 0, TE: 180}
	vo, err := ProcessTxLevelQuery(store, txs, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(vo.ResultProofs) != 1 || vo.ResultProofs[0].OID != 0 {
		t.Fatalf("expected exactly tsn=0, got %+v", vo.ResultProofs)
	}
	rh := txRootHashes(t, store)
	if !VerifyTxLevelQuery(rh, uint64(len(txs)), q, vo) {
		t.Fatal("honest range query should verify")
	}

	// Forge: splice in tsn=1's own genuine OMT proof (it really exists and
	// really belongs to lg=1, just outside the queried range) as if it
	// also matched, and correspondingly forge the range evidence's QR
	// fields -- WITHOUT touching any of the actual Merkle boundary proofs.
	tsn1Proof, ok := store.OMT.Prove(1)
	if !ok {
		t.Fatal("expected tsn=1 to exist")
	}
	forged := vo
	forged.ResultProofs = append(append([]omt.Proof{}, vo.ResultProofs...), tsn1Proof)
	forgedMax := vo.TsMaxRange
	forgedMax.QR = append([]uint64{1}, vo.TsMaxRange.QR...)
	forged.TsMaxRange = forgedMax
	forgedMin := vo.TsMinRange
	forgedMin.QR = append([]uint64{1}, vo.TsMinRange.QR...)
	forged.TsMinRange = forgedMin

	if VerifyTxLevelQuery(rh, uint64(len(txs)), q, forged) {
		t.Fatal("forged out-of-range result (via the range sub-query's QR field) should be rejected")
	}
}
