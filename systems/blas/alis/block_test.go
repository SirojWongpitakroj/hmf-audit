package alis

import (
	"reflect"
	"testing"

	"blas/iomf"
)

func mkBlockSummaries() []BlockSummary {
	return []BlockSummary{
		{BlockHeight: 0, LIDs: []uint64{1, 2}, FileKeywords: []string{"login"}, FileTsMin: 100, FileTsMax: 200},
		{BlockHeight: 1, LIDs: []uint64{2}, FileKeywords: []string{"error"}, FileTsMin: 300, FileTsMax: 400},
		{BlockHeight: 2, LIDs: []uint64{1, 3}, FileKeywords: []string{"login", "error"}, FileTsMin: 500, FileTsMax: 600},
	}
}

func TestBuildBlockLevelSequentialCheck(t *testing.T) {
	bad := []BlockSummary{{BlockHeight: 1}}
	if _, err := BuildBlockLevel(2, bad); err == nil {
		t.Fatal("expected error for non-sequential BlockHeight")
	}
}

// TestBlockLevelLGQuery: candidate blocks for lg=1 should be {0,2}.
func TestBlockLevelLGQuery(t *testing.T) {
	store, err := BuildBlockLevel(2, mkBlockSummaries())
	if err != nil {
		t.Fatal(err)
	}
	q := BlockLevelQuery{LG: 1}
	vo, err := ProcessBlockLevelQuery(store, 3, q)
	if err != nil {
		t.Fatal(err)
	}
	want := []uint64{0, 2}
	if !reflect.DeepEqual(vo.CandidateHeights, want) {
		t.Fatalf("candidates = %v, want %v", vo.CandidateHeights, want)
	}

	rh := BlockLevelRoots{}
	rh.LGRoot, _ = store.LG.Root()
	rh.FKRoot, _ = store.FK.Root()
	rh.FileTsMinRoot, _ = store.FileTsMin.Root()
	rh.FileTsMaxRoot, _ = store.FileTsMax.Root()
	if !VerifyBlockLevelQuery(rh, 3, q, vo) {
		t.Fatal("honest block-level query should verify")
	}
}

// TestBlockLevelCombinedQuery: lg=1 (blocks {0,2}) AND keyword "login"
// (blocks {0,2}) AND range covering block 2 only (ts in [450,650]).
func TestBlockLevelCombinedQuery(t *testing.T) {
	store, err := BuildBlockLevel(2, mkBlockSummaries())
	if err != nil {
		t.Fatal(err)
	}
	q := BlockLevelQuery{LG: 1, FK: iomf.Keyword("login"), HasRange: true, TS: 450, TE: 650}
	vo, err := ProcessBlockLevelQuery(store, 3, q)
	if err != nil {
		t.Fatal(err)
	}
	want := []uint64{2}
	if !reflect.DeepEqual(vo.CandidateHeights, want) {
		t.Fatalf("candidates = %v, want %v", vo.CandidateHeights, want)
	}

	rh := BlockLevelRoots{}
	rh.LGRoot, _ = store.LG.Root()
	rh.FKRoot, _ = store.FK.Root()
	rh.FileTsMinRoot, _ = store.FileTsMin.Root()
	rh.FileTsMaxRoot, _ = store.FileTsMax.Root()
	if !VerifyBlockLevelQuery(rh, 3, q, vo) {
		t.Fatal("honest combined block-level query should verify")
	}

	// Tamper: claim block 1 also matches by forging CandidateHeights.
	tampered := vo
	tampered.CandidateHeights = []uint64{1, 2}
	if VerifyBlockLevelQuery(rh, 3, q, tampered) {
		t.Fatal("forged candidate set should be rejected")
	}
}

// TestBlockLevelAbsentKeywordTreatedAsZero: a keyword never seen anywhere
// in this segment contributes an all-zero bitmap rather than erroring
// (disclosed non-membership gap, see ProcessBlockLevelQuery's doc comment).
func TestBlockLevelAbsentKeywordTreatedAsZero(t *testing.T) {
	store, err := BuildBlockLevel(2, mkBlockSummaries())
	if err != nil {
		t.Fatal(err)
	}
	q := BlockLevelQuery{LG: 1, FK: iomf.Keyword("nonexistent")}
	vo, err := ProcessBlockLevelQuery(store, 3, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(vo.CandidateHeights) != 0 {
		t.Fatalf("expected no candidates for an absent keyword, got %v", vo.CandidateHeights)
	}
	if len(vo.FKAbsentKeywords) != 1 || vo.FKAbsentKeywords[0] != "nonexistent" {
		t.Fatalf("expected FKAbsentKeywords=[nonexistent], got %v", vo.FKAbsentKeywords)
	}

	rh := BlockLevelRoots{}
	rh.LGRoot, _ = store.LG.Root()
	rh.FKRoot, _ = store.FK.Root()
	rh.FileTsMinRoot, _ = store.FileTsMin.Root()
	rh.FileTsMaxRoot, _ = store.FileTsMax.Root()
	if !VerifyBlockLevelQuery(rh, 3, q, vo) {
		t.Fatal("honest absent-keyword result should still verify")
	}
}

func TestBlockLevelRejectsForgedAbsenceAndWrongQuery(t *testing.T) {
	store, err := BuildBlockLevel(2, mkBlockSummaries())
	if err != nil {
		t.Fatal(err)
	}
	rh := BlockLevelRoots{}
	rh.LGRoot, _ = store.LG.Root()
	rh.FKRoot, _ = store.FK.Root()
	rh.FileTsMinRoot, _ = store.FileTsMin.Root()
	rh.FileTsMaxRoot, _ = store.FileTsMax.Root()
	q := BlockLevelQuery{LG: 1, FK: iomf.Keyword("nonexistent")}
	vo, err := ProcessBlockLevelQuery(store, 3, q)
	if err != nil || !VerifyBlockLevelQuery(rh, 3, q, vo) {
		t.Fatalf("honest absent keyword proof failed: %v", err)
	}
	forged := vo
	forged.FKAbsentKeywords = []string{"login"}
	if VerifyBlockLevelQuery(rh, 3, BlockLevelQuery{LG: 1, FK: iomf.Keyword("login")}, forged) {
		t.Fatal("accepted false claim that committed keyword is absent")
	}
	wrongLG := q
	wrongLG.LG = 2
	if VerifyBlockLevelQuery(rh, 3, wrongLG, vo) {
		t.Fatal("accepted a valid LG proof for a different generator")
	}
	wrongRange := BlockLevelQuery{LG: 1, HasRange: true, TS: 450, TE: 650}
	rangeVO, err := ProcessBlockLevelQuery(store, 3, wrongRange)
	if err != nil {
		t.Fatal(err)
	}
	wrongRange.TS = 650
	if VerifyBlockLevelQuery(rh, 3, wrongRange, rangeVO) {
		t.Fatal("accepted a boundary proof for a different time range")
	}
}

// TestBlockLevelAbsentLGTreatedAsZero: an lg that never appears in this
// segment contributes zero candidates.
func TestBlockLevelAbsentLGTreatedAsZero(t *testing.T) {
	store, err := BuildBlockLevel(2, mkBlockSummaries())
	if err != nil {
		t.Fatal(err)
	}
	q := BlockLevelQuery{LG: 999}
	vo, err := ProcessBlockLevelQuery(store, 3, q)
	if err != nil {
		t.Fatal(err)
	}
	if vo.LGPresent {
		t.Fatal("lg=999 should not be present")
	}
	if len(vo.CandidateHeights) != 0 {
		t.Fatalf("expected no candidates, got %v", vo.CandidateHeights)
	}
	rh := BlockLevelRoots{}
	rh.LGRoot, _ = store.LG.Root()
	rh.FKRoot, _ = store.FK.Root()
	rh.FileTsMinRoot, _ = store.FileTsMin.Root()
	rh.FileTsMaxRoot, _ = store.FileTsMax.Root()
	if !VerifyBlockLevelQuery(rh, 3, q, vo) {
		t.Fatal("honest absent-lg result should still verify")
	}
}

// TestVerifyBlockLevelRejectsForgedRangeQR: security regression (found by
// independent tester verification) — a malicious full node that injects an
// out-of-range block height into the time-range sub-query's claimed result
// (vo.TsMaxRange.QR / vo.TsMinRange.QR) must be rejected, even when it
// leaves every Merkle boundary proof untouched (so those proofs still
// verify fine against the root — they just don't relate to the injected
// height). Before the fix, VerifyBlockLevelQuery recomputed the range
// bitmap directly from vo.TsMaxRange.QR/vo.TsMinRange.QR (server-supplied,
// unauthenticated on its own) instead of from the VERIFIED cumulative
// bitmaps in LowerProof/UpperProof (the pattern iomf.Verify already uses
// correctly at the generic IOMF level), so this forgery was silently
// accepted whenever the forged height also happened to satisfy the LG/FK
// predicates — exactly the case here: block 0 is one of lg=1's genuine
// blocks (LIDs {1,2}), but its [100,200] interval does not intersect the
// queried [450,650] range.
func TestVerifyBlockLevelRejectsForgedRangeQR(t *testing.T) {
	store, err := BuildBlockLevel(2, mkBlockSummaries())
	if err != nil {
		t.Fatal(err)
	}
	q := BlockLevelQuery{LG: 1, HasRange: true, TS: 450, TE: 650}
	vo, err := ProcessBlockLevelQuery(store, 3, q)
	if err != nil {
		t.Fatal(err)
	}
	want := []uint64{2}
	if !reflect.DeepEqual(vo.CandidateHeights, want) {
		t.Fatalf("candidates = %v, want %v", vo.CandidateHeights, want)
	}

	rh := BlockLevelRoots{}
	rh.LGRoot, _ = store.LG.Root()
	rh.FKRoot, _ = store.FK.Root()
	rh.FileTsMinRoot, _ = store.FileTsMin.Root()
	rh.FileTsMaxRoot, _ = store.FileTsMax.Root()
	if !VerifyBlockLevelQuery(rh, 3, q, vo) {
		t.Fatal("honest range query should verify")
	}

	// Forge: claim block 0 (a genuine lg=1 block, but out of range) is also
	// a candidate, by editing CandidateHeights AND the range evidence's QR
	// fields consistently -- WITHOUT touching any of the actual Merkle
	// boundary proofs.
	forged := vo
	forged.CandidateHeights = []uint64{0, 2}
	forgedMax := vo.TsMaxRange
	forgedMax.QR = append([]uint64{0}, vo.TsMaxRange.QR...)
	forged.TsMaxRange = forgedMax
	forgedMin := vo.TsMinRange
	forgedMin.QR = append([]uint64{0}, vo.TsMinRange.QR...)
	forged.TsMinRange = forgedMin

	if VerifyBlockLevelQuery(rh, 3, q, forged) {
		t.Fatal("forged out-of-range candidate (via the range sub-query's QR field) should be rejected")
	}
}
