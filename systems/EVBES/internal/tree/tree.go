// Package tree is the hierarchical log-type tree T (Def. 2, Fig. 2): treeSetup
// (Alg. 1), index insertion (Alg. 2), LocateTreeNode (Eq. 22),
// BloomFilterCheck (Eq. 23), RetrieveFromIndex (Eq. 24) and index encryption
// (Eq. 17, encrypt.go).
//
// Shape (B1): root -> primary node per LType -> sub-node per log source Si.
// Each sub-node carries a Bloom filter and an inverted index. Each primary
// node also carries the Alg. 1 inverted index INL, which Alg. 2 never
// populates and search never reads (B2).
package tree

import (
	"slices"

	"evseb/internal/bloom"
	"evseb/internal/index"
	"evseb/internal/logstore"
	"evseb/internal/trapdoor"
)

// SeedLTypes is the LType list Alg. 1 line 3 iterates.
var SeedLTypes = []string{"Firewall Logs", "IDPS Logs", "Application Server Logs"}

// Tree is T.
type Tree struct {
	PrimaryNodes map[string]*PrimaryNode // key: LType
	// BloomM, BloomK size every sub-node Bloom filter (B3b configuration).
	BloomM, BloomK int
}

// PrimaryNode is N_LType.
type PrimaryNode struct {
	LType         string
	InvertedIndex *index.InvertedIndex // INL, Eq. 9 / Alg. 1 SET_INDEX (not searched, B2)
	SubNodes      map[string]*SubNode  // key: sub-node key (log source Si, B1)
}

// SubNode is n_ij.
type SubNode struct {
	Key           string
	BloomFilter   *bloom.BloomFilter   // B_nij, Def. 3
	InvertedIndex *index.InvertedIndex // In_ij, Eq. 10
}

// TreeSetup is Alg. 1. It creates one primary node per seed LType with its
// inverted index. Alg. 1 lines 8-11 create a Bloom filter for each existing
// sub-node; at setup no sub-node exists yet (sub-node keys come from log
// sources at ingestion, B1), so sub-nodes and their filters are created by
// getSubNode when Alg. 2 first needs them.
func TreeSetup(bloomM, bloomK int) *Tree {
	t := &Tree{PrimaryNodes: map[string]*PrimaryNode{}, BloomM: bloomM, BloomK: bloomK}
	for _, lt := range SeedLTypes {
		n := createPrimaryNode(lt)
		t.PrimaryNodes[lt] = n
		for _, key := range n.sortedSubKeys() {
			n.SubNodes[key].BloomFilter = bloom.New(bloomM, bloomK)
		}
	}
	return t
}

func createPrimaryNode(lType string) *PrimaryNode {
	return &PrimaryNode{LType: lType, InvertedIndex: index.New(), SubNodes: map[string]*SubNode{}}
}

// getSubNode is get_sub_node(NL, Ln) (Alg. 2 line 6), routing on Ln.Si (B1).
// A missing sub-node is created with an empty Bloom filter and index.
func (t *Tree) getSubNode(n *PrimaryNode, lg logstore.Log) *SubNode {
	s, ok := n.SubNodes[lg.Si]
	if !ok {
		s = &SubNode{Key: lg.Si, BloomFilter: bloom.New(t.BloomM, t.BloomK), InvertedIndex: index.New()}
		n.SubNodes[lg.Si] = s
	}
	return s
}

// InsertIndices is Alg. 2: route lg to its primary node (creating it if
// absent, lines 1-5) and sub-node (line 6); for each keyword, compute
// PRF_K(w) = HMAC-SHA256(Ksearch, SHA-256(w)) via trapdoor.Generate (lines
// 8-9), add (PRF_K(w), LID) to the sub-node inverted index (lines 10-13) and
// insert PRF_K(w) into the sub-node Bloom filter (lines 14-15).
func InsertIndices(t *Tree, lType string, lg logstore.Log, keywords []string, ksearch [32]byte) {
	n, ok := t.PrimaryNodes[lType]
	if !ok {
		n = createPrimaryNode(lType)
		t.PrimaryNodes[lType] = n
	}
	s := t.getSubNode(n, lg)
	for _, w := range keywords {
		prf := trapdoor.Generate(w, ksearch).Tw
		if !s.InvertedIndex.Has(prf) {
			s.InvertedIndex.Init(prf)
		}
		s.InvertedIndex.Add(prf, lg.LID)
		s.BloomFilter.Insert(prf)
	}
}

// LocateTreeNode is Eq. 22 with LType as an explicit argument (B10). It
// directs the query to the primary node of LType ("LocateTreeNode is used to
// pinpoint the appropriate primary node", Phase 6 Step 3). Returns nil for an
// unknown LType.
//
// Paper-resolution decision (QUESTIONS.md Q8): the trapdoor is then applied
// to every sub-node under that primary node. BloomFilterCheck runs on each
// sub-node (SubNodesInOrder), branches with a negative filter are skipped
// (Eq. 16 prose), and RetrieveFromIndex results from every positive sub-node
// are unioned. This resolves Eq. 22-24's single targetNode against sub-nodes
// keyed by source (B1); it is not an optimization or extension.
func LocateTreeNode(t *Tree, lType string) *PrimaryNode {
	return t.PrimaryNodes[lType]
}

// SubNodesInOrder returns the primary node's sub-nodes in ascending key
// order.
func (n *PrimaryNode) SubNodesInOrder() []*SubNode {
	keys := n.sortedSubKeys()
	out := make([]*SubNode, len(keys))
	for i, k := range keys {
		out[i] = n.SubNodes[k]
	}
	return out
}

// AllLIDs is every LID indexed under the primary node: Lall of Eq. 27 under
// the Q8 ruling.
func (n *PrimaryNode) AllLIDs() []string {
	var all []string
	for _, s := range n.SubNodesInOrder() {
		all = append(all, s.AllLIDs()...)
	}
	slices.Sort(all)
	return slices.Compact(all)
}

// BloomFilterCheck is Eq. 23 on one sub-node. A nil node is a negative.
func BloomFilterCheck(target *SubNode, tw []byte) bool {
	return target != nil && target.BloomFilter.Check(tw)
}

// RetrieveFromIndex is Eq. 24: the LIDs stored under Tw in the sub-node index.
func RetrieveFromIndex(target *SubNode, tw []byte) []string {
	if target == nil {
		return nil
	}
	return target.InvertedIndex.Get(tw)
}

// AllLIDs is every LID indexed under the sub-node.
func (s *SubNode) AllLIDs() []string { return s.InvertedIndex.AllLIDs() }

func (n *PrimaryNode) sortedSubKeys() []string {
	keys := make([]string, 0, len(n.SubNodes))
	for k := range n.SubNodes {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func (t *Tree) sortedLTypes() []string {
	keys := make([]string, 0, len(t.PrimaryNodes))
	for k := range t.PrimaryNodes {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
