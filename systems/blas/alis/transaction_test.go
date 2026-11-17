package alis

import (
	"reflect"
	"testing"

	"blas/bhash"
	"blas/iomf"
)

func mkMeta(lid, fid uint64, tsMin, tsMax int64, fw []string) FileMetadata {
	return FileMetadata{LID: lid, FID: fid, TsMin: tsMin, TsMax: tsMax, FW: fw}
}

// TestTransactionSortAndTsnAssignment: T-038 — transactions sorted by
// (lid, fid) ascending, tsn assigned 0,1,2,...; shuffled input yields an
// identical transaction-level OMT root (R-FUNC-008, R-ALIS-011, R-GO-007).
func TestTransactionSortAndTsnAssignment(t *testing.T) {
	metasInOrder := []FileMetadata{
		mkMeta(1, 1, 100, 200, []string{"a"}),
		mkMeta(1, 2, 200, 300, []string{"b"}),
		mkMeta(2, 1, 50, 60, []string{"c"}),
	}
	shuffled := []FileMetadata{metasInOrder[2], metasInOrder[0], metasInOrder[1]}

	b1, store1, err := BuildTransactionLevel(2, metasInOrder)
	if err != nil {
		t.Fatal(err)
	}
	b2, store2, err := BuildTransactionLevel(2, shuffled)
	if err != nil {
		t.Fatal(err)
	}

	txs1 := b1.Transactions()
	txs2 := b2.Transactions()
	if len(txs1) != 3 || len(txs2) != 3 {
		t.Fatalf("expected 3 transactions in each, got %d and %d", len(txs1), len(txs2))
	}
	for i := range txs1 {
		if txs1[i].TSN != uint64(i) {
			t.Fatalf("tsn[%d] = %d, want %d", i, txs1[i].TSN, i)
		}
		if txs1[i].LID != txs2[i].LID || txs1[i].FID != txs2[i].FID {
			t.Fatalf("sorted order differs between runs at position %d", i)
		}
	}
	// (lid,fid) ascending: (1,1),(1,2),(2,1)
	wantOrder := [][2]uint64{{1, 1}, {1, 2}, {2, 1}}
	for i, w := range wantOrder {
		if txs1[i].LID != w[0] || txs1[i].FID != w[1] {
			t.Fatalf("position %d = (lid=%d,fid=%d), want (%d,%d)", i, txs1[i].LID, txs1[i].FID, w[0], w[1])
		}
	}

	root1, err := store1.OMT.Root()
	if err != nil {
		t.Fatal(err)
	}
	root2, err := store2.OMT.Root()
	if err != nil {
		t.Fatal(err)
	}
	if root1 != root2 {
		t.Fatal("shuffled input should yield an identical transaction-level OMT root")
	}
}

func TestDuplicateLidFidRejected(t *testing.T) {
	metas := []FileMetadata{mkMeta(1, 1, 0, 0, nil), mkMeta(1, 1, 0, 0, nil)}
	if _, _, err := BuildTransactionLevel(2, metas); err == nil {
		t.Fatal("expected error for duplicate (lid,fid)")
	}
}

// TestTransactionLevelQueryEndToEnd exercises the reused iomf.ProcessQuery/
// Verify machinery over the transaction-level Store (LG's own presence is
// tested at block level; here we test FK+range via iomf directly since
// R-ALIS-010..015 give transaction-level FK IIMT + ts_min/ts_max RIMBTs the
// same IOMF shape used in M5, just with the OMT holding transactions).
func TestTransactionLevelQueryEndToEnd(t *testing.T) {
	metas := []FileMetadata{
		mkMeta(1, 1, 100, 150, []string{"login"}),
		mkMeta(1, 2, 200, 250, []string{"login", "error"}),
		mkMeta(2, 1, 300, 350, []string{"error"}),
	}
	_, store, err := BuildTransactionLevel(2, metas)
	if err != nil {
		t.Fatal(err)
	}
	q := iomf.Query{
		K: iomf.Keyword("login"),
		R: []iomf.RangeCond{{Attr: "ts_min", Alpha: 0, Beta: 250}, {Attr: "ts_max", Alpha: 0, Beta: 1000}},
	}
	vo, err := iomf.ProcessQuery(store, 3, q)
	if err != nil {
		t.Fatal(err)
	}
	// login: tsn 0,1. ts_min<=250: tsn0(100),tsn1(200) both qualify; ts_max<=1000: all qualify.
	want := []uint64{0, 1}
	if !reflect.DeepEqual(vo.QRO, want) {
		t.Fatalf("QR_O = %v, want %v", vo.QRO, want)
	}

	iimtRoot, _ := store.IIMT.Root()
	omtRoot, _ := store.OMT.Root()
	tsMinRoot, _ := store.RIMBTs["ts_min"].Root()
	tsMaxRoot, _ := store.RIMBTs["ts_max"].Root()
	rh := iomf.RootHashes{IIMTRoot: iimtRoot, OMTRoot: omtRoot, RIMBTRoots: map[string]bhash.Digest{"ts_min": tsMinRoot, "ts_max": tsMaxRoot}}
	if !iomf.Verify(rh, 3, q, vo) {
		t.Fatal("honest transaction-level query should verify")
	}
}

// TestSigAlwaysFixedZeroWidth: T-062 — every transaction's Sig field is
// always exactly SigWidth zero bytes; no other signature mechanism exists
// (R-2 residual, E3 in DECISIONS.md).
func TestSigAlwaysFixedZeroWidth(t *testing.T) {
	metas := []FileMetadata{
		mkMeta(1, 1, 100, 200, []string{"a"}),
		mkMeta(2, 1, 300, 400, []string{"b"}),
	}
	b, _, err := BuildTransactionLevel(2, metas)
	if err != nil {
		t.Fatal(err)
	}
	for _, tx := range b.Transactions() {
		if len(tx.Sig) != SigWidth {
			t.Fatalf("tsn=%d: Sig length = %d, want %d", tx.TSN, len(tx.Sig), SigWidth)
		}
		for i, byteVal := range tx.Sig {
			if byteVal != 0 {
				t.Fatalf("tsn=%d: Sig[%d] = %d, want 0 (fixed all-zero placeholder)", tx.TSN, i, byteVal)
			}
		}
	}
}
