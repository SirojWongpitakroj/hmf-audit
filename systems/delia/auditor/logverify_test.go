package auditor

import (
	"delia/globalstate"
	"delia/hash"
	"delia/merkle"
	"testing"
)

// buildHonestHistory constructs a fully honest {G_1..G_nu} history for a
// single audited server (index) alone in an n-server domain, from records
// split into batches of theta, mirroring exactly what LSG/Alg.1 would have
// produced.
func buildHonestHistory(t *testing.T, records []Record, theta int, n, index int) []State {
	t.Helper()
	if len(records)%theta != 0 {
		t.Fatalf("test records not a multiple of theta")
	}
	numBatches := len(records) / theta
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
			t.Fatalf("GlobalGen: %v", err)
		}
		g = next
		states = append(states, g)
	}
	return states
}

func makeRecords(n int) []Record {
	recs := make([]Record, n)
	for i := 0; i < n; i++ {
		recs[i] = Record([]byte{byte('a' + i%26), byte(i)})
	}
	return recs
}

// TEST-11: untampered logs -> ErrList empty.
func TestLogVerifyValidLogs(t *testing.T) {
	theta, n, index := 4, 3, 1
	records := makeRecords(12) // 3 batches
	states := buildHonestHistory(t, records, theta, n, index)

	errList := LogVerify(records, theta, index, states)
	if len(errList) != 0 {
		t.Fatalf("ErrList = %v, want empty", errList)
	}
}

// TEST-12: modify one record in batch b -> b is in ErrList, added exactly
// once (single linear pass, DEV-01).
func TestLogVerifyTamperedBatch(t *testing.T) {
	theta, n, index := 4, 3, 1
	records := makeRecords(12) // batches 0, 1, 2
	states := buildHonestHistory(t, records, theta, n, index)

	tampered := make([]Record, len(records))
	copy(tampered, records)
	// Tamper a record in batch 1 (records[4:8]).
	tampered[5] = Record([]byte("TAMPERED"))

	errList := LogVerify(tampered, theta, index, states)
	if len(errList) != 1 || errList[0] != 1 {
		t.Fatalf("ErrList = %v, want [1]", errList)
	}
}

// TEST-13: delete one record (indices shift) -> the batch containing the
// change is reported.
func TestLogVerifyDeletedRecord(t *testing.T) {
	theta, n, index := 4, 3, 1
	records := makeRecords(12)
	states := buildHonestHistory(t, records, theta, n, index)

	// Delete the record at absolute index 5 (inside batch 1); everything
	// after shifts down by one, and the resulting slice is one short of a
	// full 3rd batch, so it becomes a trailing partial batch (ignored per
	// GAP-16(a)).
	edited := make([]Record, 0, len(records)-1)
	edited = append(edited, records[:5]...)
	edited = append(edited, records[6:]...)

	errList := LogVerify(edited, theta, index, states)
	found := false
	for _, b := range errList {
		if b == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("ErrList = %v, want batch 1 (the batch containing the deletion) present", errList)
	}
}

// TEST-13 (insertion variant): inserting one record also shifts indices and
// is reported.
func TestLogVerifyInsertedRecord(t *testing.T) {
	theta, n, index := 4, 3, 1
	records := makeRecords(12)
	states := buildHonestHistory(t, records, theta, n, index)

	edited := make([]Record, 0, len(records)+1)
	edited = append(edited, records[:5]...)
	edited = append(edited, Record([]byte("INSERTED")))
	edited = append(edited, records[5:]...)
	// edited now has 13 records: batches 0,1,2 rebuildable (12 records),
	// with 1 leftover trailing partial (ignored).

	errList := LogVerify(edited, theta, index, states)
	found := false
	for _, b := range errList {
		if b == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("ErrList = %v, want batch 1 (the batch containing the insertion point) present", errList)
	}
}

// TEST-13 (trailing partial ignored): extra records after the last
// complete, ledger-confirmed batch must not be flagged or crash the audit.
func TestLogVerifyTrailingPartialIgnored(t *testing.T) {
	theta, n, index := 4, 3, 1
	records := makeRecords(12)
	states := buildHonestHistory(t, records, theta, n, index)

	withExtra := append(append([]Record{}, records...), Record([]byte("partial-next-batch")))
	errList := LogVerify(withExtra, theta, index, states)
	if len(errList) != 0 {
		t.Fatalf("ErrList = %v, want empty (trailing partial batch ignored)", errList)
	}
}

// GAP-75: while the server's slot is still the zero sentinel, no comparison
// is made. Model this with a 2-server domain where the OTHER server (index
// 0) produces several states before the audited server (index 1) ever
// completes a batch.
func TestLogVerifySkipsZeroSentinel(t *testing.T) {
	theta, n, index := 4, 2, 1
	otherRecords := makeRecords(8) // other server: 2 batches, index 0

	g := globalstate.Zero(n)
	var states []State
	for l := 0; l < 2; l++ {
		leaves := make([]hash.Digest, theta)
		for x := 0; x < theta; x++ {
			leaves[x] = hash.H(otherRecords[l*theta+x])
		}
		root := merkle.MHTGen(leaves)
		next, err := globalstate.GlobalGen(root, g, 0) // other server's index
		if err != nil {
			t.Fatal(err)
		}
		g = next
		states = append(states, g)
	}

	// Audited server (index 1) has no records and no completed batches yet;
	// its slot in every state above is still the zero sentinel.
	errList := LogVerify(nil, theta, index, states)
	if len(errList) != 0 {
		t.Fatalf("ErrList = %v, want empty while slot is the zero sentinel", errList)
	}
}

// TEST-14: instrumentation — H called once per record per rebuilt batch;
// MHTGen called once per inner iteration; l is never reset (DEV-01); the
// audit runs on a single goroutine (verified structurally: LogVerify itself
// spawns no goroutines, checked by code review / absence of `go` statements
// — see logverify.go).
func TestLogVerifyInstrumentation(t *testing.T) {
	theta, n, index := 4, 3, 1
	records := makeRecords(12) // 3 batches
	states := buildHonestHistory(t, records, theta, n, index)

	hCalls := 0
	origHash := hashRecord
	hashRecord = func(r Record) hash.Digest {
		hCalls++
		return origHash(r)
	}
	defer func() { hashRecord = origHash }()

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
	wantH := len(records) // exactly one H per record, once each, total
	if hCalls != wantH {
		t.Fatalf("H called %d times, want %d (once per record)", hCalls, wantH)
	}
	wantMHT := len(records) / theta // one MHTGen per rebuilt batch
	if mhtCalls != wantMHT {
		t.Fatalf("MHTGen called %d times, want %d (once per inner iteration)", mhtCalls, wantMHT)
	}
}

func TestLogVerifyEmptyStates(t *testing.T) {
	errList := LogVerify(nil, 4, 0, nil)
	if len(errList) != 0 {
		t.Fatalf("ErrList = %v, want empty for no states", errList)
	}
}
