package globalstate

// Independent tester-agent verification tests for GlobalState / Alg. 1 / Alg.
// 2, covering edge cases the developer's own globalstate_test.go did not:
// malformed Decode input, n=0, a multi-generation chain (serial 0 -> 1 -> 2
// -> 3 with correct untouched-position invariants at each step), a
// byte-level Equal difference, and StateVerify called twice in a row with
// different valid inputs to rule out any stale/cached result.

import (
	"testing"

	"delia/hash"
	"delia/merkle"
)

func td(b byte) hash.Digest { return hash.H([]byte{b}) }

// Decode must reject a buffer shorter than the 12-byte header.
func TestTesterDecodeRejectsShortHeader(t *testing.T) {
	if _, err := Decode([]byte{1, 2, 3}); err == nil {
		t.Fatalf("Decode accepted a 3-byte buffer (header is 12 bytes)")
	}
}

// Decode must reject a buffer whose length disagrees with entry_count.
func TestTesterDecodeRejectsLengthMismatch(t *testing.T) {
	g := GlobalState{Nu: 1, Entries: []hash.Digest{td(1), td(2)}}
	enc := g.Encode()
	// Claim entry_count=2 (bytes 8:12) but truncate one entry's worth of
	// trailing bytes.
	truncated := enc[:len(enc)-1]
	if _, err := Decode(truncated); err == nil {
		t.Fatalf("Decode accepted a truncated buffer (missing bytes from the last entry)")
	}
	// Also an over-long buffer must be rejected, not silently ignored.
	overlong := append(append([]byte{}, enc...), 0xFF)
	if _, err := Decode(overlong); err == nil {
		t.Fatalf("Decode accepted a buffer with trailing garbage bytes")
	}
}

// n=0 domain: Zero(0) must round-trip through Encode/Decode without
// panicking or misbehaving (defensive edge case, not a mainline scenario).
func TestTesterZeroEntriesRoundTrip(t *testing.T) {
	g := Zero(0)
	if len(g.Entries) != 0 {
		t.Fatalf("Zero(0) has %d entries, want 0", len(g.Entries))
	}
	enc := g.Encode()
	got, err := Decode(enc)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !got.Equal(g) {
		t.Fatalf("n=0 round trip mismatch")
	}
}

// GAP-09: a single differing byte anywhere in the entries must break Equal
// (not just gross differences already covered by the dev's own nu test).
func TestTesterEqualDetectsSingleByteDiff(t *testing.T) {
	a := GlobalState{Nu: 5, Entries: []hash.Digest{td(1), td(2), td(3)}}
	b := a.Clone()
	b.Entries[1][17] ^= 0x01 // flip one bit deep inside entry 1
	if a.Equal(b) {
		t.Fatalf("Equal did not detect a single flipped bit inside an entry")
	}
}

// A 4-generation chain (G_0 -> G_1 -> G_2 -> G_3), driven by three different
// servers completing batches in turn, independently re-verifying: serial
// increments by exactly 1 each time: only the generating server's slot
// changes at each step; every other slot is byte-identical to the previous
// generation.
func TestTesterMultiGenerationChain(t *testing.T) {
	n := 3
	g := Zero(n)
	if g.Nu != 0 {
		t.Fatalf("G_0.Nu = %d, want 0", g.Nu)
	}

	roots := []hash.Digest{merkle.MHTGen([]hash.Digest{td(10)}), merkle.MHTGen([]hash.Digest{td(20), td(21)}), merkle.MHTGen([]hash.Digest{td(30)})}
	generators := []int{0, 1, 2} // p1, p2, p3 each complete their first batch in turn

	prev := g
	for step, i := range generators {
		next, err := GlobalGen(roots[step], prev, i)
		if err != nil {
			t.Fatalf("GlobalGen step %d: %v", step, err)
		}
		if next.Nu != prev.Nu+1 {
			t.Fatalf("step %d: Nu = %d, want %d", step, next.Nu, prev.Nu+1)
		}
		for pos := 0; pos < n; pos++ {
			if pos == i {
				if next.Entries[pos] == prev.Entries[pos] {
					t.Fatalf("step %d: generating position %d did not change", step, pos)
				}
			} else if next.Entries[pos] != prev.Entries[pos] {
				t.Fatalf("step %d: non-generating position %d changed unexpectedly", step, pos)
			}
		}
		prev = next
	}
	if prev.Nu != 3 {
		t.Fatalf("final Nu = %d, want 3 (G_0 -> G_1 -> G_2 -> G_3)", prev.Nu)
	}
}

// StateVerify must not cache anything across calls: call it twice back to
// back with two DIFFERENT honest (psi, root, prev, next, i) tuples and
// confirm both return true, and then confirm a call with a tampered variant
// of the SECOND tuple still correctly fails immediately after a true result
// (ruling out "the previous true answer leaked into this call").
func TestTesterStateVerifyNoCrossCallCaching(t *testing.T) {
	prevA := Zero(2)
	psiA := []hash.Digest{td(1), td(2)}
	rootA := merkle.MHTGen(psiA)
	nextA, err := GlobalGen(rootA, prevA, 0)
	if err != nil {
		t.Fatalf("GlobalGen A: %v", err)
	}
	if !StateVerify(psiA, rootA, prevA, nextA, 0) {
		t.Fatalf("call 1 (honest A) returned false")
	}

	prevB := Zero(2)
	psiB := []hash.Digest{td(99), td(98), td(97)}
	rootB := merkle.MHTGen(psiB)
	nextB, err := GlobalGen(rootB, prevB, 1)
	if err != nil {
		t.Fatalf("GlobalGen B: %v", err)
	}
	if !StateVerify(psiB, rootB, prevB, nextB, 1) {
		t.Fatalf("call 2 (honest B, different psi/root/prev/i) returned false right after a true result")
	}

	// Immediately after two true results, a tampered call must still fail:
	// proves the true result isn't just being echoed back.
	tamperedB := nextB.Clone()
	tamperedB.Entries[1] = td(250)
	if StateVerify(psiB, rootB, prevB, tamperedB, 1) {
		t.Fatalf("call 3 (tampered) returned true right after two true calls -- looks cached/stale")
	}

	// And a subsequent honest call must still succeed (the false result
	// didn't poison state either).
	if !StateVerify(psiA, rootA, prevA, nextA, 0) {
		t.Fatalf("call 4 (re-verify honest A) returned false after an intervening false call")
	}
}

// Independent li=0 special-case check with a different index and a
// multi-element psi than the developer's own TEST-05b, to rule out the
// branch only being correct for the specific index/psi shape already tested.
func TestTesterGlobalGenLi0IndependentCase(t *testing.T) {
	prev := Zero(4)
	psi := []hash.Digest{td(7), td(8), td(9), td(10), td(11)}
	root := merkle.MHTGen(psi)
	got, err := GlobalGen(root, prev, 3)
	if err != nil {
		t.Fatalf("GlobalGen: %v", err)
	}
	want := hash.H(root[:]) // H(root) alone, li=0 branch
	if got.Entries[3] != want {
		t.Fatalf("entry[3] = %x, want H(root) = %x", got.Entries[3], want)
	}
	for pos := 0; pos < 3; pos++ {
		if got.Entries[pos] != hash.Zero {
			t.Fatalf("position %d changed from the zero sentinel unexpectedly", pos)
		}
	}
}

// Independent GlobalGen call-count spy: an invalid root must skip the
// GlobalGen call entirely (not just discard its result). Written separately
// from the developer's own TestStateVerifyInvalidRoot spy so this isn't
// just re-running the same instrumentation the developer wrote.
func TestTesterStateVerifyRootMismatchSkipsGlobalGenEntirely(t *testing.T) {
	prev := Zero(3)
	psi := []hash.Digest{td(1), td(2), td(3), td(4)}
	realRoot := merkle.MHTGen(psi)
	next, err := GlobalGen(realRoot, prev, 2)
	if err != nil {
		t.Fatalf("GlobalGen: %v", err)
	}

	calls := 0
	origGen := globalGenFn
	globalGenFn = func(root hash.Digest, p GlobalState, i int) (GlobalState, error) {
		calls++
		return origGen(root, p, i)
	}
	defer func() { globalGenFn = origGen }()

	badRoot := td(200) // deliberately does not match MHTGen(psi)
	if StateVerify(psi, badRoot, prev, next, 2) {
		t.Fatalf("StateVerify accepted an invalid root")
	}
	if calls != 0 {
		t.Fatalf("GlobalGen was called %d time(s) after a root mismatch, want 0 (must short-circuit)", calls)
	}

	// Sanity: the same spy, called with the correct root, DOES invoke
	// GlobalGen exactly once -- proves the spy itself is wired correctly and
	// the zero count above isn't just a broken spy.
	if !StateVerify(psi, realRoot, prev, next, 2) {
		t.Fatalf("StateVerify rejected a valid root/state pair")
	}
	if calls != 1 {
		t.Fatalf("GlobalGen was called %d time(s) for a valid root, want exactly 1", calls)
	}
}
