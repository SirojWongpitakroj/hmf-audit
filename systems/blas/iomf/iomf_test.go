package iomf

import (
	"reflect"
	"testing"

	"blas/bhash"
	"blas/iimt"
	"blas/omt"
	"blas/rimbt"
)

// buildFixture builds the Fig. 2 8-object fixture across all three trees:
// IIMT (Error/Warning/PUT/GET), RIMBT (attribute "attr"), and an OMT holding
// trivial 1-byte objects. F=2 throughout.
func buildFixture(t *testing.T) (*Store, [][]byte) {
	t.Helper()
	iimtTree, err := iimt.NewEmpty(2)
	if err != nil {
		t.Fatal(err)
	}
	iimtTree.AddKeyword("Error")
	iimtTree.AddKeyword("Warning")
	iimtTree.AddKeyword("PUT")
	iimtTree.AddKeyword("GET")

	kwFixture := map[string]string{
		"Error":   "11001001",
		"Warning": "00100110",
		"PUT":     "10011000",
		"GET":     "01000001",
	}
	rimbtTree, err := rimbt.NewEmpty(2)
	if err != nil {
		t.Fatal(err)
	}
	rangeFixture := []struct {
		v    int64
		bits string
	}{
		{12, "00110010"},
		{15, "00111010"},
		{97, "10111110"},
		{99, "11111111"},
	}
	attrValues := make([]int64, 8)
	for obj := 0; obj < 8; obj++ {
		for _, f := range rangeFixture {
			if f.bits[obj] == '1' {
				attrValues[obj] = f.v
				break
			}
		}
	}

	objects := make([][]byte, 8)
	var omtLeaves []omt.Leaf
	for i := 0; i < 8; i++ {
		var kws []string
		for w, bits := range kwFixture {
			if bits[i] == '1' {
				kws = append(kws, w)
			}
		}
		if err := iimtTree.Insert(uint64(i), kws); err != nil {
			t.Fatal(err)
		}
		if err := rimbtTree.Insert(uint64(i), attrValues[i]); err != nil {
			t.Fatal(err)
		}
		objects[i] = []byte{byte(i + 1)}
		omtLeaves = append(omtLeaves, omt.Leaf{OID: uint64(i), Object: objects[i]})
	}
	omtTree, err := omt.Build(2, omtLeaves)
	if err != nil {
		t.Fatal(err)
	}

	s := &Store{
		IIMT:   iimtTree,
		OMT:    omtTree,
		RIMBTs: map[string]*rimbt.Tree{"attr": rimbtTree},
	}
	return s, objects
}

func rootHashes(t *testing.T, s *Store) RootHashes {
	t.Helper()
	iimtRoot, err := s.IIMT.Root()
	if err != nil {
		t.Fatal(err)
	}
	omtRoot, err := s.OMT.Root()
	if err != nil {
		t.Fatal(err)
	}
	rimbtRoot, err := s.RIMBTs["attr"].Root()
	if err != nil {
		t.Fatal(err)
	}
	return RootHashes{IIMTRoot: iimtRoot, OMTRoot: omtRoot, RIMBTRoots: map[string]bhash.Digest{"attr": rimbtRoot}}
}

// TestAlgorithm3EndToEnd: T-026 — Q = <(¬Error ∧ PUT ∨ GET), ([13,98])>.
func TestAlgorithm3EndToEnd(t *testing.T) {
	s, _ := buildFixture(t)
	q := Query{
		K: Or(And(Not(Keyword("Error")), Keyword("PUT")), Keyword("GET")),
		R: []RangeCond{{Attr: "attr", Alpha: 13, Beta: 98}},
	}
	vo, err := ProcessQuery(s, 8, q)
	if err != nil {
		t.Fatal(err)
	}
	// QR_K = ((¬Error) ∧ PUT) ∨ GET = 01010001 -> oids {1,3,7} (0-based)
	wantQK := []uint64{1, 3, 7}
	// QR_R (range [13,98]) = {0,4,5} (from the RIMBT test fixture)
	wantQR := []uint64{0, 4, 5}
	_ = wantQK
	_ = wantQR
	// QR_O = QR_K ∩ QR_R = {} for this fixture (disjoint sets) — assert the
	// computed intersection, not a guessed value (per T-026's own caveat).
	if len(vo.QRO) != 0 {
		t.Fatalf("expected empty QR_O for this fixture, got %v", vo.QRO)
	}
	if len(vo.KeywordEvidence) != 3 {
		t.Fatalf("expected 3 keyword evidences (Error, PUT, GET), got %d", len(vo.KeywordEvidence))
	}
	if len(vo.RangeEvidence) != 1 {
		t.Fatalf("expected 1 range evidence, got %d", len(vo.RangeEvidence))
	}

	rh := rootHashes(t, s)
	if !Verify(rh, 8, q, vo) {
		t.Fatal("honest VO should verify")
	}
}

func TestVerifierBindsKeywordAndRangeToQuery(t *testing.T) {
	s, _ := buildFixture(t)
	rh := rootHashes(t, s)
	q := Query{K: Keyword("PUT"), R: []RangeCond{{Attr: "attr", Alpha: 13, Beta: 98}}}
	vo, err := ProcessQuery(s, 8, q)
	if err != nil {
		t.Fatal(err)
	}
	if !Verify(rh, 8, q, vo) {
		t.Fatal("honest result rejected")
	}
	wrongKeyword := q
	wrongKeyword.K = Keyword("Error")
	if Verify(rh, 8, wrongKeyword, vo) {
		t.Fatal("accepted a valid proof for a different keyword")
	}
	wrongRange := q
	wrongRange.R = []RangeCond{{Attr: "attr", Alpha: 98, Beta: 99}}
	if Verify(rh, 8, wrongRange, vo) {
		t.Fatal("accepted boundary proofs for a different interval")
	}
	aliased := vo
	aliased.KeywordEvidence = append([]KeywordEvidence(nil), vo.KeywordEvidence...)
	aliased.KeywordEvidence[0].Keyword = "Error"
	if Verify(rh, 8, q, aliased) {
		t.Fatal("accepted a keyword label different from its committed leaf")
	}
}

// TestAlgorithm3NonEmptyIntersection picks a query whose K and R overlap,
// so QR_O is non-empty and the OMT retrieval path is exercised.
func TestAlgorithm3NonEmptyIntersection(t *testing.T) {
	s, objects := buildFixture(t)
	// PUT bits: 10011000 -> {0,3,4}; range [13,98] -> {0,4,5}. Intersection: {0,4}.
	q := Query{
		K: Keyword("PUT"),
		R: []RangeCond{{Attr: "attr", Alpha: 13, Beta: 98}},
	}
	vo, err := ProcessQuery(s, 8, q)
	if err != nil {
		t.Fatal(err)
	}
	want := []uint64{0, 4}
	if !reflect.DeepEqual(vo.QRO, want) {
		t.Fatalf("QR_O = %v, want %v", vo.QRO, want)
	}
	if len(vo.ObjectProofs) != 2 {
		t.Fatalf("expected 2 object proofs, got %d", len(vo.ObjectProofs))
	}
	for _, op := range vo.ObjectProofs {
		found := false
		for _, o := range objects {
			if reflect.DeepEqual(op.Object, o) {
				found = true
			}
		}
		if !found {
			t.Fatalf("returned object %v not among fixture objects", op.Object)
		}
	}

	rh := rootHashes(t, s)
	if !Verify(rh, 8, q, vo) {
		t.Fatal("honest VO should verify")
	}
}

// TestVerifierRejectsForgedQRO: T-028, R-VER-005 — a QR_O not following from
// the authenticated bitmaps must be rejected even with otherwise-valid
// proofs.
func TestVerifierRejectsForgedQRO(t *testing.T) {
	s, _ := buildFixture(t)
	q := Query{K: Keyword("PUT"), R: []RangeCond{{Attr: "attr", Alpha: 13, Beta: 98}}}
	vo, err := ProcessQuery(s, 8, q)
	if err != nil {
		t.Fatal(err)
	}
	rh := rootHashes(t, s)
	if !Verify(rh, 8, q, vo) {
		t.Fatal("honest VO should verify")
	}

	forged := vo
	forged.QRO = append([]uint64{}, vo.QRO...)
	forged.QRO = append(forged.QRO, 6) // oid=6 does not satisfy Q
	if Verify(rh, 8, q, forged) {
		t.Fatal("forged QR_O (mismatching recomputation) should be rejected")
	}
}

// TestOmissionDetected: T-029 — removing an oid from QR_O (bitmaps
// untouched) must be caught by recomputation mismatch.
func TestOmissionDetected(t *testing.T) {
	s, _ := buildFixture(t)
	q := Query{K: Keyword("PUT"), R: []RangeCond{{Attr: "attr", Alpha: 13, Beta: 98}}}
	vo, err := ProcessQuery(s, 8, q)
	if err != nil {
		t.Fatal(err)
	}
	rh := rootHashes(t, s)

	omitted := vo
	omitted.QRO = vo.QRO[:len(vo.QRO)-1] // drop the last oid
	omitted.ObjectProofs = vo.ObjectProofs[:len(vo.ObjectProofs)-1]
	if Verify(rh, 8, q, omitted) {
		t.Fatal("omission (QR_O shrunk without a matching cause) should be rejected")
	}
}

// TestNonSatisfyingEntryInjected: T-030.
func TestNonSatisfyingEntryInjected(t *testing.T) {
	s, _ := buildFixture(t)
	q := Query{K: Keyword("PUT"), R: []RangeCond{{Attr: "attr", Alpha: 13, Beta: 98}}}
	vo, err := ProcessQuery(s, 8, q)
	if err != nil {
		t.Fatal(err)
	}
	rh := rootHashes(t, s)

	// (a) add oid=6 to ObjectProofs without adding it to QR_O: OMT proof
	// verifies (6 is a real object) but QR_O/oids-from-objects mismatch.
	proof6, ok := s.OMT.Prove(6)
	if !ok {
		t.Fatal("expected oid=6 to exist")
	}
	injected := vo
	injected.ObjectProofs = append(append([]omt.Proof{}, vo.ObjectProofs...), proof6)
	if Verify(rh, 8, q, injected) {
		t.Fatal("non-satisfying entry injected without QR_O update should be rejected")
	}
}

// TestForgedEntryRejected: T-031 — a fabricated OMT proof must not verify.
func TestForgedEntryRejected(t *testing.T) {
	s, _ := buildFixture(t)
	q := Query{K: Keyword("PUT"), R: []RangeCond{{Attr: "attr", Alpha: 13, Beta: 98}}}
	vo, err := ProcessQuery(s, 8, q)
	if err != nil {
		t.Fatal(err)
	}
	rh := rootHashes(t, s)

	forged := vo
	if len(forged.ObjectProofs) == 0 {
		t.Fatal("expected at least one object proof")
	}
	forged.ObjectProofs = append([]omt.Proof{}, vo.ObjectProofs...)
	forged.ObjectProofs[0].Object = []byte{0xFF} // tamper the object bytes
	if Verify(rh, 8, q, forged) {
		t.Fatal("forged/tampered object proof should be rejected")
	}
}

// TestMaintenanceSecuritySmoke: T-032 — RH_real != RH_forged for
// single-field, single-bit, single-oid differences.
func TestMaintenanceSecuritySmoke(t *testing.T) {
	s1, _ := buildFixture(t)
	rh1 := rootHashes(t, s1)

	// Build an identical fixture, then tamper one object's byte content.
	s2, _ := buildFixture(t)
	// Re-derive OMT with one object's content changed.
	var leaves []omt.Leaf
	for i := 0; i < 8; i++ {
		val := byte(i + 1)
		if i == 3 {
			val = 0xEE // tamper object 3's content
		}
		leaves = append(leaves, omt.Leaf{OID: uint64(i), Object: []byte{val}})
	}
	tamperedOMT, err := omt.Build(2, leaves)
	if err != nil {
		t.Fatal(err)
	}
	s2.OMT = tamperedOMT
	rh2 := rootHashes(t, s2)
	if rh1.OMTRoot == rh2.OMTRoot {
		t.Fatal("tampering one object's field should change the OMT root")
	}

	// One bitmap bit different (IIMT): re-run Insert with a different
	// keyword set for one object, changing exactly one bit of one bitmap.
	s3, _ := buildFixture(t)
	iimtTree3, err := iimt.NewEmpty(2)
	if err != nil {
		t.Fatal(err)
	}
	iimtTree3.AddKeyword("Error")
	iimtTree3.AddKeyword("Warning")
	iimtTree3.AddKeyword("PUT")
	iimtTree3.AddKeyword("GET")
	kwFixture := map[string]string{
		"Error": "11001001", "Warning": "00100110", "PUT": "10011000", "GET": "01000011", // last bit of GET flipped: 01000001 -> 01000011 (bit 6 flipped)
	}
	for i := 0; i < 8; i++ {
		var kws []string
		for w, bits := range kwFixture {
			if bits[i] == '1' {
				kws = append(kws, w)
			}
		}
		if err := iimtTree3.Insert(uint64(i), kws); err != nil {
			t.Fatal(err)
		}
	}
	s3.IIMT = iimtTree3
	rh3 := rootHashes(t, s3)
	if rh1.IIMTRoot == rh3.IIMTRoot {
		t.Fatal("flipping one bitmap bit should change the IIMT root")
	}

	// One oid different (OMT): rebuild with object 3's oid moved to 8
	// (still a valid, distinct oid) instead of changing its content.
	s4, _ := buildFixture(t)
	var leaves4 []omt.Leaf
	for i := 0; i < 8; i++ {
		oid := uint64(i)
		if i == 3 {
			oid = 8 // moved
		}
		leaves4 = append(leaves4, omt.Leaf{OID: oid, Object: []byte{byte(i + 1)}})
	}
	tamperedOMT4, err := omt.Build(2, leaves4)
	if err != nil {
		t.Fatal(err)
	}
	s4.OMT = tamperedOMT4
	rh4 := rootHashes(t, s4)
	if rh1.OMTRoot == rh4.OMTRoot {
		t.Fatal("changing one object's oid should change the OMT root")
	}
}

// TestBitmapLengthAuthenticated: T-080-style — Evaluate/ProcessQuery use the
// caller-supplied authenticated n, not any length carried inside the VO.
func TestBitmapLengthAuthenticated(t *testing.T) {
	s, _ := buildFixture(t)
	q := Query{K: Keyword("PUT")}
	vo, err := ProcessQuery(s, 8, q)
	if err != nil {
		t.Fatal(err)
	}
	rh := rootHashes(t, s)
	if !Verify(rh, 8, q, vo) {
		t.Fatal("expected honest VO to verify with the correct authenticated n=8")
	}
	// A different (wrong) n changes ¬ semantics and must not silently
	// verify with an inconsistent QR_O.
	if Verify(rh, 16, q, vo) {
		t.Fatal("verification with the wrong authenticated n should not accept the same VO")
	}
}
