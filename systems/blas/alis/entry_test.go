package alis

import (
	"reflect"
	"testing"

	"blas/iomf"
)

func buildEntryFile(t *testing.T) (*LogGenerator, *EntryFile, FileMetadata) {
	t.Helper()
	gen := NewLogGenerator(1)
	b, err := NewEntryFileBuilder(gen, 100, 4)
	if err != nil {
		t.Fatal(err)
	}
	data := []struct {
		ts int64
		ew []string
	}{
		{1000, []string{"login"}},
		{1010, []string{"login", "error"}},
		{1020, []string{"logout"}},
		{1030, []string{"error"}},
		{1040, []string{"login"}},
	}
	for i, d := range data {
		esn, err := b.AddEntry(d.ts, d.ew, []byte{byte(i)})
		if err != nil {
			t.Fatal(err)
		}
		if esn != uint64(i) {
			t.Fatalf("expected esn=%d, got %d", i, esn)
		}
	}
	file, meta, err := b.Close()
	if err != nil {
		t.Fatal(err)
	}
	return gen, file, meta
}

func TestEntryFileMetadata(t *testing.T) {
	_, _, meta := buildEntryFile(t)
	if meta.TsMin != 1000 || meta.TsMax != 1040 {
		t.Fatalf("ts range = [%d,%d], want [1000,1040]", meta.TsMin, meta.TsMax)
	}
	wantFW := []string{"login", "error", "logout"}
	// FW is emitted in generator keyword-universe order, not insertion
	// order of the literal test data; just check set equality.
	if len(meta.FW) != len(wantFW) {
		t.Fatalf("FW = %v, want set %v", meta.FW, wantFW)
	}
	for _, w := range wantFW {
		found := false
		for _, got := range meta.FW {
			if got == w {
				found = true
			}
		}
		if !found {
			t.Fatalf("FW missing %q: got %v", w, meta.FW)
		}
	}
}

// TestAbsentGeneratorKeywordAllZeroLeaf: T-014/T-015-style — a keyword the
// generator later learns about, applied to a NEW file, keeps an all-zero
// leaf in a file that predates it, and does not rebuild that earlier file.
func TestAbsentGeneratorKeywordAllZeroLeaf(t *testing.T) {
	gen := NewLogGenerator(1)
	b1, _ := NewEntryFileBuilder(gen, 1, 4)
	if _, err := b1.AddEntry(100, []string{"login"}, nil); err != nil {
		t.Fatal(err)
	}
	file1, _, err := b1.Close()
	if err != nil {
		t.Fatal(err)
	}
	root1Before, _ := file1.IIMT.Root()

	b2, _ := NewEntryFileBuilder(gen, 2, 4)
	if _, err := b2.AddEntry(200, []string{"newkeyword"}, nil); err != nil {
		t.Fatal(err)
	}
	file2, _, err := b2.Close()
	if err != nil {
		t.Fatal(err)
	}

	// file1 is unaffected by the new keyword discovered in file2.
	root1After, _ := file1.IIMT.Root()
	if root1Before != root1After {
		t.Fatal("file1's committed IIMT root must not change when a later file discovers a new keyword")
	}
	// file2's IIMT does have a leaf for "newkeyword".
	if _, _, ok := file2.IIMT.Query("newkeyword"); !ok {
		t.Fatal("file2 should have a leaf for the keyword it discovered")
	}
	// file1's IIMT has no leaf for "newkeyword" (it didn't exist yet).
	if _, _, ok := file1.IIMT.Query("newkeyword"); ok {
		t.Fatal("file1 should not have a leaf for a keyword discovered later")
	}
}

// TestEntryQueryKeywordAndRange: end-to-end entry-level query + verify
// (R-QUERY-021/022, R-VER-010).
func TestEntryQueryKeywordAndRange(t *testing.T) {
	_, file, meta := buildEntryFile(t)
	q := EntryQuery{EK: iomf.Keyword("login"), HasRange: true, TS: 1005, TE: 1035}
	vo, err := file.ProcessQuery(q)
	if err != nil {
		t.Fatal(err)
	}
	// login entries: esn 0, 1(has login+error), 4. Range [1005,1035]: esn
	// 1(1010),2(1020),3(1030). Intersection: esn 1.
	if len(vo.ResultProofs) != 1 || vo.ResultProofs[0].OID != 1 {
		t.Fatalf("expected exactly esn=1, got %+v", vo.ResultProofs)
	}
	if !VerifyEntryQuery(meta.RHIIMT, meta.RHOMT, q, vo) {
		t.Fatal("honest entry-level query should verify")
	}
}

// TestEntryQueryCompletenessRejectsHiddenEntry: T-040-style — the SP hides a
// matching entry and forges boundary evidence to cover the gap; the boundary
// proof's own decoded ts must contradict the claim.
func TestEntryQueryCompletenessRejectsHiddenEntry(t *testing.T) {
	_, file, meta := buildEntryFile(t)
	q := EntryQuery{HasRange: true, TS: 1000, TE: 1040}
	vo, err := file.ProcessQuery(q)
	if err != nil {
		t.Fatal(err)
	}
	if len(vo.ResultProofs) != 5 {
		t.Fatalf("expected all 5 entries in range, got %d", len(vo.ResultProofs))
	}
	if !VerifyEntryQuery(meta.RHIIMT, meta.RHOMT, q, vo) {
		t.Fatal("honest full-range query should verify")
	}

	// Hide the last entry (esn=4) without adjusting the (nil) upper
	// boundary — since esn=4 is the last entry, UpperBoundary is nil
	// (range runs to the end), so simply dropping it changes the count.
	tampered := vo
	tampered.ResultProofs = vo.ResultProofs[:len(vo.ResultProofs)-1]
	if VerifyEntryQuery(meta.RHIIMT, meta.RHOMT, q, tampered) {
		t.Fatal("hiding a matching entry should be rejected")
	}
}

// TestEntryQueryForgedBoundaryRejected exercises a forged boundary whose
// own ts contradicts the claimed exclusion.
func TestEntryQueryForgedBoundaryRejected(t *testing.T) {
	_, file, meta := buildEntryFile(t)
	// Range covering only esn=2 (ts=1020): [1015,1025].
	q := EntryQuery{HasRange: true, TS: 1015, TE: 1025}
	vo, err := file.ProcessQuery(q)
	if err != nil {
		t.Fatal(err)
	}
	if len(vo.ResultProofs) != 1 || vo.ResultProofs[0].OID != 2 {
		t.Fatalf("expected exactly esn=2, got %+v", vo.ResultProofs)
	}
	if !VerifyEntryQuery(meta.RHIIMT, meta.RHOMT, q, vo) {
		t.Fatal("honest query should verify")
	}

	// Attack: claim there is no upper boundary (pretend the range runs to
	// the file's end) while actually still only returning esn=2 — this
	// would hide esn=3 and esn=4 if the verifier didn't cross-check count.
	forged := vo
	forged.UpperBoundary = nil
	if VerifyEntryQuery(meta.RHIIMT, meta.RHOMT, q, forged) {
		t.Fatal("dropping the upper boundary to hide entries should be rejected")
	}
}

func TestDecodeLogEntryRoundTrip(t *testing.T) {
	e := LogEntry{ESN: 7, TS: 12345, EW: []string{"b", "a"}, V: []byte("payload")}
	encoded := EncodeLogEntry(e)
	decoded, err := DecodeLogEntry(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ESN != e.ESN || decoded.TS != e.TS || string(decoded.V) != string(e.V) {
		t.Fatalf("decode mismatch: %+v vs original %+v", decoded, e)
	}
	// EW is sorted on encode ("a" < "b").
	if !reflect.DeepEqual(decoded.EW, []string{"a", "b"}) {
		t.Fatalf("decoded EW = %v, want sorted [a b]", decoded.EW)
	}
}

// TestEntryQueryUnderstatedExtentRejected: an SP that supplies a
// non-rightmost esn as the "extent" proof (understating N to hide later
// entries) must be rejected.
func TestEntryQueryUnderstatedExtentRejected(t *testing.T) {
	_, file, meta := buildEntryFile(t)
	q := EntryQuery{HasRange: false}
	vo, err := file.ProcessQuery(q)
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyEntryQuery(meta.RHIIMT, meta.RHOMT, q, vo) {
		t.Fatal("honest query should verify")
	}

	understated, ok := file.OMT.Prove(2) // esn=2, not the true rightmost (esn=4)
	if !ok {
		t.Fatal("expected esn=2 to exist")
	}
	forged := vo
	forged.ExtentProof = understated
	if VerifyEntryQuery(meta.RHIIMT, meta.RHOMT, q, forged) {
		t.Fatal("an understated (non-rightmost) extent proof should be rejected")
	}
}
