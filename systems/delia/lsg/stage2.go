package lsg

import (
	"sync"

	"delia/globalstate"
	"delia/hash"
	"delia/logcache"
	"delia/merkle"
)

// Broadcaster sends stage 2 messages to the other servers in the domain
// (LSG-007, LSG-009). Its concrete implementation (real TLS/TCP transport vs
// an in-process test harness, GAP-54) is outside this package.
type Broadcaster interface {
	BroadcastRootState(root hash.Digest, g globalstate.GlobalState) error
	BroadcastFailure(sender, proposer uint32, nu uint64, reason string) error
}

// Node is one server's LSG stage 2 state: its provisional latest GlobalState
// (LSG-010, DEV-07), and its retained history (GAP-13(a)/PERSIST-003).
type Node struct {
	self         logcache.ServerID
	index        int                 // this server's position in the P order frozen at Init (GAP-08)
	participants []logcache.ServerID // the full P order frozen at Init, position i = participants[i]
	cache        *logcache.Store
	bcast        Broadcaster

	mu      sync.Mutex
	latest  globalstate.GlobalState
	history []globalstate.GlobalState
}

// NewNode creates a stage 2 node starting from an initial GlobalState
// (ordinarily globalstate.Zero(n), G_0, GAP-06). participants is the P order
// frozen at Init (DATA-013): participants[i] is the ServerID whose
// LogCacheQueue lives at position i of every domain-wide GlobalState.
func NewNode(self logcache.ServerID, index int, participants []logcache.ServerID, cache *logcache.Store, bcast Broadcaster, initial globalstate.GlobalState) *Node {
	ps := make([]logcache.ServerID, len(participants))
	copy(ps, participants)
	return &Node{
		self:         self,
		index:        index,
		participants: ps,
		cache:        cache,
		bcast:        bcast,
		latest:       initial.Clone(),
		history:      []globalstate.GlobalState{initial.Clone()},
	}
}

// Latest returns a copy of the node's current provisional GlobalState.
func (n *Node) Latest() globalstate.GlobalState {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.latest.Clone()
}

// History returns a copy of the retained GlobalState history (GAP-13(a):
// all LocalState/GlobalState history is retained; "replaced" means the
// latest pointer changes, not deletion).
func (n *Node) History() []globalstate.GlobalState {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]globalstate.GlobalState, len(n.history))
	for i, g := range n.history {
		out[i] = g.Clone()
	}
	return out
}

// adopt records g as this node's new provisional latest state and appends
// it to the retained history. Callers must hold n.mu.
func (n *Node) adopt(g globalstate.GlobalState) {
	n.latest = g
	n.history = append(n.history, g.Clone())
}

// Propose implements LSG-006/007: called when this node's own batch li
// fills (batch is exactly theta record-hash digests; no GlobalState is
// generated for a partial batch — the caller only invokes Propose once a
// batch has actually completed). It computes root <- MHTGen(psi),
// G_nu <- Alg1.GlobalGen(root, G_{nu-1}, index), provisionally adopts G_nu as
// its own latest state (DEV-07: provisional until HMSC confirmation), and
// broadcasts (root, G_nu) to the other servers.
func (n *Node) Propose(psi []hash.Digest) (globalstate.GlobalState, error) {
	root := merkle.MHTGen(psi)

	n.mu.Lock()
	prev := n.latest
	g, err := globalstate.GlobalGen(root, prev, n.index)
	if err != nil {
		n.mu.Unlock()
		return globalstate.GlobalState{}, err
	}
	n.adopt(g)
	n.mu.Unlock()

	if err := n.bcast.BroadcastRootState(root, g); err != nil {
		return globalstate.GlobalState{}, err
	}
	return g, nil
}

// Receive implements LSG-008..011: called when a (root, G_nu) broadcast
// arrives from the server at position fromIndex. It calls Alg2.StateVerify
// against this node's OWN locally maintained psi for fromIndex (never a psi
// supplied by the proposer, LSG-008) and this node's own G_{nu-1}.
//
// Invalid: broadcasts a failure notification (sender=this node,
// proposer=fromIndex, nu, reason) and aborts this attempt; this node's
// latest state is unchanged; no dispute is invoked (LSG-009, GAP-12(a)).
//
// Valid: replaces this node's local G_{nu-1} with G_nu, provisionally
// (LSG-010, DEV-07).
func (n *Node) Receive(fromIndex int, root hash.Digest, g globalstate.GlobalState) bool {
	n.mu.Lock()
	psi := n.cache.Current(n.proposerID(fromIndex))
	prev := n.latest
	ok := globalstate.StateVerify(psi, root, prev, g, fromIndex)
	if !ok {
		n.mu.Unlock()
		_ = n.bcast.BroadcastFailure(uint32(n.index), uint32(fromIndex), g.Nu, "state verify failed")
		return false
	}
	n.adopt(g)
	n.mu.Unlock()
	return true
}

// Reconcile implements the losing-proposer side of concurrent batch
// completion (GAP-11(b), DEV-07): "the losing proposer re-runs Alg. 1
// against the newly accepted state and proposes again; no merge." myRoot is
// the root this node itself already proposed (from its own completed
// batch); newlyAccepted is the G_nu that the network actually settled on
// instead (a different proposer's state for the same nu). The caller
// supplies newlyAccepted — at the LSG layer alone there is no rule for how a
// node learns which proposal "won" a concurrent conflict; that ordering is
// HMSC Confirm's job (leader election, currently blocked by GAP-05/GAP-81).
// This method builds only the re-proposal *logic* TRACKER calls for,
// ready to be wired to real conflict notification once that lands.
//
// Reconcile adopts newlyAccepted as this node's latest state, then
// regenerates a fresh proposal from the SAME root (same underlying batch
// content — no merge) chained onto newlyAccepted, adopts that as the new
// latest, and broadcasts it.
func (n *Node) Reconcile(myRoot hash.Digest, newlyAccepted globalstate.GlobalState) (globalstate.GlobalState, error) {
	n.mu.Lock()
	n.adopt(newlyAccepted)
	newG, err := globalstate.GlobalGen(myRoot, n.latest, n.index)
	if err != nil {
		n.mu.Unlock()
		return globalstate.GlobalState{}, err
	}
	n.adopt(newG)
	n.mu.Unlock()

	if err := n.bcast.BroadcastRootState(myRoot, newG); err != nil {
		return globalstate.GlobalState{}, err
	}
	return newG, nil
}

// proposerID maps a P-order index to the logcache.ServerID this node's
// LogCacheQueue store keys the proposer's psi by, using the P order frozen
// at Init (DATA-013/GAP-08).
func (n *Node) proposerID(index int) logcache.ServerID {
	if index < 0 || index >= len(n.participants) {
		return ""
	}
	return n.participants[index]
}
