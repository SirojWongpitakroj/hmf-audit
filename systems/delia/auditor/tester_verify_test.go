package auditor

// Independent tester-agent verification tests for Algorithm 3 (LogVerify),
// covering scenarios the developer's own logverify_test.go did not: a
// zero-sentinel skip with NON-empty leftover records supplied (to rule out
// the empty-records case being what actually made TestLogVerifySkipsZeroSentinel
// pass), a mixed multi-server history where the audited server's slot starts
// at zero and only becomes non-zero partway through, and a single-linear-pass
// check built independently of the developer's own instrumentation test.

import (
	"testing"

	"delia/globalstate"
	"delia/hash"
	"delia/merkle"
)

func trec(bs ...byte) []Record {
	out := make([]Record, len(bs))
	for i, b := range bs {
		out[i] = Record([]byte{b})
	}
	return out
}

// GAP-75 with non-empty leftover records: the audited server never
// completes a batch across the whole state history, but records ARE
// supplied (as if a partial batch had accumulated locally). ErrList must
// still be empty, and no reconstruction must be attempted (no records are
// "consumed"), because every G_k entry for this server is still the zero
// sentinel.
func TestTesterGapSeventyFiveSkipWithLeftoverRecords(t *testing.T) {
	theta, n, auditedIndex := 4, 2, 1
	otherRecords := trec(1, 2, 3, 4, 5, 6, 7, 8) // other server: 2 batches

	g := globalstate.Zero(n)
	var states []State
	for l := 0; l < 2; l++ {
		leaves := make([]hash.Digest, theta)
		for x := 0; x < theta; x++ {
			leaves[x] = hash.H(otherRecords[l*theta+x])
		}
		root := merkle.MHTGen(leaves)
		next, err := globalstate.GlobalGen(root, g, 0)
		if err != nil {
			t.Fatal(err)
		}
		g = next
		states = append(states, g)
	}

	// The audited server (index 1) has some leftover partial-batch records
	// sitting around locally, but no completed batch appears anywhere in the
	// state history -- its slot is the zero sentinel in every state.
	leftover := trec(9, 9, 9)
	errList := LogVerify(leftover, theta, auditedIndex, states)
	if len(errList) != 0 {
		t.Fatalf("ErrList = %v, want empty while the audited server's slot is still the zero sentinel", errList)
	}
}

// A mixed history: server B (audited) is zero for the first two states (A
// completes batches instead), then completes its own first two batches, then
// a third with a corrupted record. Only B's own batch numbering (li) must
// appear in ErrList, counted from B's own first real batch (li=0), not from
// the global k index.
func TestTesterMixedHistoryZeroThenRealThenTamperedLiNumbering(t *testing.T) {
	theta, n := 3, 2
	indexA, indexB := 0, 1

	aRecords := [][]byte{{1}, {2}, {3}, {4}, {5}, {6}} // A's two batches
	bRecords := [][]byte{{10}, {11}, {12}, {13}, {14}, {15}, {16}, {17}, {18}}

	g := globalstate.Zero(n)
	var states []State

	// k=1,2: A completes two batches; B stays at the zero sentinel.
	for l := 0; l < 2; l++ {
		leaves := make([]hash.Digest, theta)
		for x := 0; x < theta; x++ {
			leaves[x] = hash.H(Record(aRecords[l*theta+x]))
		}
		root := merkle.MHTGen(leaves)
		next, err := globalstate.GlobalGen(root, g, indexA)
		if err != nil {
			t.Fatal(err)
		}
		g = next
		states = append(states, g)
	}

	// k=3,4,5: B completes three batches of its own (li=0,1,2), honestly.
	for l := 0; l < 3; l++ {
		leaves := make([]hash.Digest, theta)
		for x := 0; x < theta; x++ {
			leaves[x] = hash.H(Record(bRecords[l*theta+x]))
		}
		root := merkle.MHTGen(leaves)
		next, err := globalstate.GlobalGen(root, g, indexB)
		if err != nil {
			t.Fatal(err)
		}
		g = next
		states = append(states, g)
	}

	// Build B's record slice, but tamper a record inside B's SECOND batch
	// (li=1, bRecords[3:6]).
	var bAsRecords []Record
	for _, r := range bRecords {
		bAsRecords = append(bAsRecords, Record(r))
	}
	bAsRecords[4] = Record([]byte("TAMPERED"))

	errList := LogVerify(bAsRecords, theta, indexB, states)
	if len(errList) != 1 || errList[0] != 1 {
		t.Fatalf("ErrList = %v, want exactly [1] (B's own li=1, not a k-index or A's batch numbers)", errList)
	}
}

// Independent single-linear-pass check: every rebuilt batch must be visited
// exactly once across the ENTIRE run, verified by counting MHTGen calls
// against the number of states supplied (not just the number of records),
// using a spy written independently of the developer's TestLogVerifyInstrumentation.
func TestTesterSingleLinearPassAcrossManyStates(t *testing.T) {
	theta, n, index := 2, 1, 0
	numBatches := 6
	var records []Record
	for i := 0; i < numBatches*theta; i++ {
		records = append(records, Record([]byte{byte(i + 1)}))
	}

	g := globalstate.Zero(n)
	var states []State
	for l := 0; l < numBatches; l++ {
		leaves := make([]hash.Digest, theta)
		for x := 0; x < theta; x++ {
			leaves[x] = hash.H(records[l*theta+x])
		}
		root := merkle.MHTGen(leaves)
		next, err := globalstate.GlobalGen(root, g, index)
		if err != nil {
			t.Fatal(err)
		}
		g = next
		states = append(states, g)
	}

	mhtCalls := 0
	origMHT := mhtGen
	mhtGen = func(leaves []hash.Digest) hash.Digest {
		mhtCalls++
		return origMHT(leaves)
	}
	defer func() { mhtGen = origMHT }()

	errList := LogVerify(records, theta, index, states)
	if len(errList) != 0 {
		t.Fatalf("unexpected ErrList: %v", errList)
	}
	if mhtCalls != numBatches {
		t.Fatalf("MHTGen called %d times across %d states, want exactly %d (each batch rebuilt exactly once)", mhtCalls, len(states), numBatches)
	}
}

// A tampered batch must not cascade: once li is flagged, the chain resumes
// from the ledger value, so a batch AFTER the tampered one, if genuinely
// honest, must NOT also appear in ErrList.
func TestTesterTamperDoesNotCascadeToLaterBatches(t *testing.T) {
	theta, n, index := 3, 1, 0
	numBatches := 4
	var records []Record
	for i := 0; i < numBatches*theta; i++ {
		records = append(records, Record([]byte{byte(i + 1)}))
	}

	g := globalstate.Zero(n)
	var states []State
	for l := 0; l < numBatches; l++ {
		leaves := make([]hash.Digest, theta)
		for x := 0; x < theta; x++ {
			leaves[x] = hash.H(records[l*theta+x])
		}
		root := merkle.MHTGen(leaves)
		next, err := globalstate.GlobalGen(root, g, index)
		if err != nil {
			t.Fatal(err)
		}
		g = next
		states = append(states, g)
	}

	tampered := make([]Record, len(records))
	copy(tampered, records)
	tampered[1] = Record([]byte("TAMPERED")) // inside batch 0 (li=0)

	errList := LogVerify(tampered, theta, index, states)
	if len(errList) != 1 || errList[0] != 0 {
		t.Fatalf("ErrList = %v, want exactly [0]; batches 1-3 must not cascade-fail from batch 0's tamper", errList)
	}
}
