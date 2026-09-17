package lsg

// Independent tester-agent verification tests for LSG stage 2, covering
// scenarios the developer's own stage2_test.go did not: repeated corrupted
// proposals in a row (no permanent lockup / no stale-failure accumulation
// bleeding into a later honest proposal), and that Receive recomputes
// StateVerify fresh each call rather than short-circuiting after the first
// rejection.

import (
	"testing"

	"delia/globalstate"
	"delia/hash"
	"delia/merkle"
)

// Multiple corrupted proposals in a row must each be independently
// rejected (not just the first), the receiver's state must stay at G_0
// throughout, and a subsequent HONEST proposal for the same nu must still
// be accepted afterward (no permanent lockup from the earlier failures).
func TestTesterRepeatedCorruptedProposalsThenHonestRecovers(t *testing.T) {
	n, theta := 3, 4
	_, nodes, stores, bcasts := setupNodes(t, n, theta)

	batch := fillBatch(t, stores[0], "p1", theta, 55)
	for i := 1; i < n; i++ {
		stores[i].SetCurrent("p1", batch)
	}
	root := merkle.MHTGen(batch)
	before := nodes[0].Latest()

	// Three independently-corrupted proposals in a row.
	for attempt := 0; attempt < 3; attempt++ {
		corrupted, err := globalstate.GlobalGen(root, before, 0)
		if err != nil {
			t.Fatalf("GlobalGen: %v", err)
		}
		corrupted.Entries[0] = hash.H([]byte{byte(attempt)}) // different corruption each time
		for i := 1; i < n; i++ {
			if nodes[i].Receive(0, root, corrupted) {
				t.Fatalf("attempt %d: node %d accepted a corrupted proposal", attempt, i)
			}
		}
	}
	for i := 1; i < n; i++ {
		if nodes[i].Latest().Nu != 0 {
			t.Fatalf("node %d advanced past G_0 after only corrupted proposals (nu=%d)", i, nodes[i].Latest().Nu)
		}
		if len(bcasts[i].failures) != 3 {
			t.Fatalf("node %d recorded %d failure notifications, want exactly 3 (one per corrupted attempt)", i, len(bcasts[i].failures))
		}
	}

	// Now an honest proposal for the SAME batch/root must still succeed:
	// prior rejections must not have poisoned this node's ability to accept
	// a later valid proposal.
	honest, err := globalstate.GlobalGen(root, before, 0)
	if err != nil {
		t.Fatalf("GlobalGen: %v", err)
	}
	for i := 1; i < n; i++ {
		if !nodes[i].Receive(0, root, honest) {
			t.Fatalf("node %d rejected a genuinely honest proposal after prior corrupted attempts", i)
		}
	}
	for i := 1; i < n; i++ {
		if !nodes[i].Latest().Equal(honest) {
			t.Fatalf("node %d did not adopt the honest state after recovering from corrupted attempts", i)
		}
		if len(bcasts[i].failures) != 3 {
			t.Fatalf("node %d recorded %d failure notifications after the honest recovery, want still exactly 3 (no notification for a valid proposal)", i, len(bcasts[i].failures))
		}
	}
}

// Receive must recompute StateVerify fresh from the receiver's OWN psi on
// every call -- not reuse a decision cached from an earlier Receive call for
// the same (fromIndex, nu). Verified by having the receiver's local psi for
// the proposer change BETWEEN two Receive calls for two different nu values,
// and confirming each call's accept/reject decision tracks its own inputs.
func TestTesterReceiveRecomputesFreshEachCall(t *testing.T) {
	n, theta := 2, 2
	_, nodes, stores, _ := setupNodes(t, n, theta)

	// nu=1: p1 completes a batch, p2's local copy of p1's psi matches -> valid.
	batch1 := fillBatch(t, stores[0], "p1", theta, 1)
	stores[1].SetCurrent("p1", batch1)
	root1 := merkle.MHTGen(batch1)
	g0 := nodes[0].Latest()
	g1, err := globalstate.GlobalGen(root1, g0, 0)
	if err != nil {
		t.Fatalf("GlobalGen g1: %v", err)
	}
	if !nodes[1].Receive(0, root1, g1) {
		t.Fatalf("nu=1: honest proposal rejected")
	}

	// nu=2: p1 completes a second batch, but p2's local copy is deliberately
	// stale/wrong (simulating a still-blocked stage-1 delivery mismatch) ->
	// must be rejected, proving the SAME node doesn't just keep accepting
	// after having accepted once.
	batch2 := fillBatch(t, stores[0], "p1", theta, 2)
	stores[1].SetCurrent("p1", []hash.Digest{hash.H([]byte("wrong")), hash.H([]byte("wrong2"))}) // wrong psi
	root2 := merkle.MHTGen(batch2)
	g2, err := globalstate.GlobalGen(root2, g1, 0)
	if err != nil {
		t.Fatalf("GlobalGen g2: %v", err)
	}
	if nodes[1].Receive(0, root2, g2) {
		t.Fatalf("nu=2: proposal accepted despite the receiver's own local psi not matching root2")
	}
	if nodes[1].Latest().Nu != 1 {
		t.Fatalf("node 1 latest.Nu = %d, want 1 (must stay at the last validly-accepted state)", nodes[1].Latest().Nu)
	}
}
