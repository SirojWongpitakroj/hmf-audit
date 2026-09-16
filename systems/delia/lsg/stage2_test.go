package lsg

import (
	"testing"

	"delia/globalstate"
	"delia/hash"
	"delia/logcache"
	"delia/merkle"
)

// hub is an in-process multi-node broadcast harness (GAP-54: "an in-process
// multi-node harness is used for tests only"). Each Node's Broadcaster
// delivers directly (synchronously, no network) to every other node's
// Receive.
type hub struct {
	nodes []*Node
}

type hubBroadcaster struct {
	h        *hub
	fromIdx  int
	failures []failureRecord
}

type failureRecord struct {
	sender, proposer int
	nu               uint64
	reason           string
}

func (b *hubBroadcaster) BroadcastRootState(root hash.Digest, g globalstate.GlobalState) error {
	for i, n := range b.h.nodes {
		if i == b.fromIdx {
			continue
		}
		n.Receive(b.fromIdx, root, g)
	}
	return nil
}

func (b *hubBroadcaster) BroadcastFailure(sender, proposer uint32, nu uint64, reason string) error {
	b.failures = append(b.failures, failureRecord{int(sender), int(proposer), nu, reason})
	return nil
}

func newHub(n int) (*hub, []*hubBroadcaster) {
	h := &hub{}
	bcasts := make([]*hubBroadcaster, n)
	for i := 0; i < n; i++ {
		bcasts[i] = &hubBroadcaster{h: h, fromIdx: i}
	}
	return h, bcasts
}

func participantIDs(n int) []logcache.ServerID {
	names := []string{"p1", "p2", "p3", "p4", "p5"}
	ids := make([]logcache.ServerID, n)
	for i := 0; i < n; i++ {
		ids[i] = logcache.ServerID(names[i])
	}
	return ids
}

func setupNodes(t *testing.T, n, theta int) (*hub, []*Node, []*logcache.Store, []*hubBroadcaster) {
	t.Helper()
	participants := participantIDs(n)
	h, bcasts := newHub(n)
	nodes := make([]*Node, n)
	stores := make([]*logcache.Store, n)
	for i := 0; i < n; i++ {
		store, err := logcache.New(theta)
		if err != nil {
			t.Fatalf("logcache.New: %v", err)
		}
		stores[i] = store
		nodes[i] = NewNode(participants[i], i, participants, store, bcasts[i], globalstate.Zero(n))
	}
	h.nodes = nodes
	return h, nodes, stores, bcasts
}

func fillBatch(t *testing.T, store *logcache.Store, server logcache.ServerID, theta int, seed byte) []hash.Digest {
	t.Helper()
	var batch []hash.Digest
	for i := 0; i < theta; i++ {
		full, b := store.Append(server, hash.H([]byte{seed, byte(i)}))
		if full {
			batch = b
		}
	}
	if batch == nil {
		t.Fatalf("batch did not complete")
	}
	return batch
}

// TEST-09 (honest path): every pj's latest GlobalState becomes G_nu after an
// honest proposal, including the proposer itself.
func TestStage2HonestProposalAdoptedByAll(t *testing.T) {
	n, theta := 3, 4
	_, nodes, stores, _ := setupNodes(t, n, theta)

	// Every node must have its own copy of p1's psi before p1 proposes
	// (simulating stage 1 having already delivered it — M3 scope note:
	// build stage 2 against an already-populated psi).
	batch := fillBatch(t, stores[0], "p1", theta, 1)
	for i := 1; i < n; i++ {
		stores[i].SetCurrent("p1", batch)
	}

	g, err := nodes[0].Propose(batch)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if g.Nu != 1 {
		t.Fatalf("g.Nu = %d, want 1", g.Nu)
	}
	for i, node := range nodes {
		if !node.Latest().Equal(g) {
			t.Fatalf("node %d latest state != proposed state", i)
		}
	}
	// No GlobalState is generated for a partial batch: a second, smaller
	// append to p1 must not trigger anything (LSG-006).
	full, _ := stores[0].Append("p1", hash.H([]byte("partial")))
	if full {
		t.Fatalf("a single record wrongly completed a new batch")
	}
}

// TEST-09 (corrupted path): a corrupted G_nu broadcast is rejected by every
// receiver with a failure notification, and receivers keep G_{nu-1}.
func TestStage2CorruptedProposalRejected(t *testing.T) {
	n, theta := 3, 4
	_, nodes, stores, bcasts := setupNodes(t, n, theta)

	batch := fillBatch(t, stores[0], "p1", theta, 7)
	for i := 1; i < n; i++ {
		stores[i].SetCurrent("p1", batch)
	}

	root := merkle.MHTGen(batch)
	before := nodes[0].Latest()
	corrupted, err := globalstate.GlobalGen(root, before, 0)
	if err != nil {
		t.Fatalf("GlobalGen: %v", err)
	}
	corrupted.Entries[0] = hash.H([]byte("corrupted"))

	for i := 1; i < n; i++ {
		if nodes[i].Receive(0, root, corrupted) {
			t.Fatalf("node %d accepted a corrupted proposal", i)
		}
	}
	for i := 1; i < n; i++ {
		if nodes[i].Latest().Nu != 0 {
			t.Fatalf("node %d adopted the corrupted state (nu=%d)", i, nodes[i].Latest().Nu)
		}
	}

	// GAP-12(a): the failure notification carries (sender, pi, nu, reason).
	for i := 1; i < n; i++ {
		if len(bcasts[i].failures) != 1 {
			t.Fatalf("node %d recorded %d failure notifications, want 1", i, len(bcasts[i].failures))
		}
		got := bcasts[i].failures[0]
		if got.sender != i || got.proposer != 0 || got.nu != 1 {
			t.Fatalf("node %d failure notification = %+v, want sender=%d proposer=0 nu=1", i, got, i)
		}
	}
}

// TEST-43: p1 and p2 both complete batches "from" the same G_{nu-1}=G_0;
// exactly one (p1's) is confirmed as G_1 across the network; p2, the losing
// proposer, re-runs Alg. 1 on the newly accepted G_1 and proposes G_2; no
// merged state results. Because true network-level concurrency needs an
// external arbiter (HMSC Confirm leader ordering, blocked by GAP-05/GAP-81),
// this test plays that arbiter's role directly, per TRACKER M3's license to
// build the re-proposal *logic* now and wire it up once GAP-05 clears.
func TestStage2ConcurrentProposalReconciles(t *testing.T) {
	n, theta := 3, 2
	_, nodes, stores, _ := setupNodes(t, n, theta)

	batchP1 := fillBatch(t, stores[0], "p1", theta, 11)
	batchP2 := fillBatch(t, stores[1], "p2", theta, 22)
	for i := 0; i < n; i++ {
		stores[i].SetCurrent("p1", batchP1)
		stores[i].SetCurrent("p2", batchP2)
	}

	// p1 proposes; the hub synchronously delivers it to p2 and p3, so it
	// becomes the network's accepted G_1 everywhere except p2's own
	// (independently computed, conflicting) view — p2 has not called
	// Propose here at all; instead we feed it directly as "my own computed
	// root" to Reconcile, exactly modeling "p2 had independently completed
	// its batch concurrently".
	g1, err := nodes[0].Propose(batchP1)
	if err != nil {
		t.Fatalf("p1 Propose: %v", err)
	}
	if g1.Nu != 1 {
		t.Fatalf("g1.Nu = %d, want 1", g1.Nu)
	}
	for i, node := range nodes {
		if i == 1 {
			continue // p2 has not reconciled yet
		}
		if !node.Latest().Equal(g1) {
			t.Fatalf("node %d did not adopt p1's G_1", i)
		}
	}

	rootP2 := merkle.MHTGen(batchP2)
	g2, err := nodes[1].Reconcile(rootP2, g1)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if g2.Nu != 2 {
		t.Fatalf("g2.Nu = %d, want 2", g2.Nu)
	}
	// The re-proposal must chain from g1 (the newly accepted state), not
	// merge with it: entries at every position except p2's must equal g1's.
	for i := range g2.Entries {
		if i == 1 {
			continue
		}
		if g2.Entries[i] != g1.Entries[i] {
			t.Fatalf("entry %d changed during reconciliation; expected only p2's entry to change", i)
		}
	}
	if g2.Entries[1] == g1.Entries[1] {
		t.Fatalf("p2's own entry did not change in the re-proposal")
	}

	// Reconcile's broadcast must reach p1 and p3, who adopt g2 as the new
	// latest — the network converges on one chain, no merge.
	for i, node := range nodes {
		if !node.Latest().Equal(g2) {
			t.Fatalf("node %d did not converge on the reconciled state g2 (nu=%d)", i, node.Latest().Nu)
		}
	}
}
