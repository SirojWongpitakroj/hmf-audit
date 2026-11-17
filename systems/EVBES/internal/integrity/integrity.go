// Package integrity is EVSEB's result verification: the off-chain DHT
// (Eq. 28), the quaternary Merkle tree whose root R is anchored on-chain
// (Theorem 2, Fig. 3), identifyTamperedFiles (Alg. 4.1) and TraceTamperedNode
// (Alg. 4.2).
//
// Paper-resolution decisions (QUESTIONS.md Q9, Q10; these resolve
// inconsistencies in the paper, they are not optimizations or extensions):
//
//   - Q9. Alg. 4.1 line 3 recomputes a root over only the candidate LIDs and
//     compares it with R. R is the root over every hLID (Fig. 3), so the two
//     can't be equal for a partial result set. Following Theorem 2 ("retrieves
//     the Merkle proof path associated with hLID and recomputes the root"),
//     each candidate's live hLID is folded up its quaternary proof path and
//     compared with the on-chain R. The proof path comes from the stored tree
//     (stored_tree of Alg. 4.2): the quaternary tree built from the DHT's hLID
//     values when R was anchored, kept off-chain. RecomputeMerkleRoot
//     recomputes exactly the candidates' paths (with every other node taken
//     from the stored tree); its root equals R iff every candidate verifies.
//   - Q10. Alg. 4.2 lines 17-21 loop over every leaf at Level 0, which
//     conflicts with Fig. 9 ("only drills down into the relevant branches").
//     At Level 0 only the leaves under the affected level-1 branches are
//     inspected, each by its proof path against R.
package integrity

import (
	"bytes"
	"encoding/hex"
	"errors"
	"slices"

	"evseb/internal/blockchain"
	"evseb/internal/crypto"
	"evseb/internal/logstore"
)

// Arity is the Merkle tree fan-out: quaternary (Theorem 2).
const Arity = 4

// DHT is the off-chain DHT, in memory (B20; Q3 ruling): DHT[H(LID)] = hLID
// (Eq. 28).
type DHT struct {
	Entries map[string][]byte // key: hex(H(LID)), value: hLID
}

// NewDHT returns an empty DHT.
func NewDHT() *DHT { return &DHT{Entries: map[string][]byte{}} }

// Key is hex(H(LID)).
func Key(lid string) string {
	h := crypto.H([]byte(lid))
	return hex.EncodeToString(h[:])
}

// Put stores DHT[H(LID)] = hLID.
func (d *DHT) Put(lid string, hLID []byte) { d.Entries[Key(lid)] = bytes.Clone(hLID) }

// Get returns hLID for lid.
func (d *DHT) Get(lid string) ([]byte, bool) {
	v, ok := d.Entries[Key(lid)]
	return bytes.Clone(v), ok
}

// HashLog is hLID = H(EncAES(Ln, Kenc)) (Theorem 2 proof): SHA-256 of the
// stored log ciphertext CTLog. CTMetadata is not covered; the paper hashes
// only the encrypted log entry.
func HashLog(ctLog []byte) []byte {
	h := crypto.H(ctLog)
	return h[:]
}

// MerkleTree is a quaternary Merkle tree. Levels[0] are the leaves (hLID
// values, in LIDs order); every node at level l >= 1 is
// SHA-256(c0 || c1 || c2 || c3) over its four children at level l-1, children
// concatenated as raw 32-byte digests. A group with fewer than four children
// is padded by repeating its last child (B14). A tree of one leaf has that
// leaf as root; an empty tree has a nil root.
type MerkleTree struct {
	LIDs   []string // LIDs[i] owns Leaves[i]; the Alg. 4.2 Level-0 keys
	Leaves [][]byte
	Levels [][][]byte
	Root   []byte
	pos    map[string]int

	// A partial stored tree read from storage holds only the nodes a request
	// needs: widths gives each level's node count, nodes and lids the loaded
	// entries. Levels, Leaves and LIDs are then nil.
	widths []int
	nodes  map[[2]int][]byte
	lids   map[int]string
}

// NewPartialTree returns a stored tree of the given level widths holding only
// the supplied nodes (keyed {level, index}) and leaf owners. Nodes that were
// not loaded read as nil.
func NewPartialTree(widths []int, nodes map[[2]int][]byte, lids map[int]string,
	positions map[string]int, root []byte) *MerkleTree {
	return &MerkleTree{Root: root, pos: positions, widths: widths, nodes: nodes, lids: lids}
}

// height is the number of levels.
func (m *MerkleTree) height() int {
	if m.widths != nil {
		return len(m.widths)
	}
	return len(m.Levels)
}

// width is the number of nodes at level.
func (m *MerkleTree) width(level int) int {
	if m.widths != nil {
		return m.widths[level]
	}
	return len(m.Levels[level])
}

// nodeAt is the stored node at (level, i).
func (m *MerkleTree) nodeAt(level, i int) []byte {
	if m.widths != nil {
		return m.nodes[[2]int{level, i}]
	}
	return m.Levels[level][i]
}

// lidAt is the LID owning leaf i.
func (m *MerkleTree) lidAt(i int) string {
	if m.widths != nil {
		return m.lids[i]
	}
	return m.LIDs[i]
}

// storedGroup is group over the stored level `level`.
func (m *MerkleTree) storedGroup(level, j int) [][]byte {
	n := m.width(level)
	g := make([][]byte, Arity)
	for c := range Arity {
		g[c] = m.nodeAt(level, min(j*Arity+c, n-1))
	}
	return g
}

// Build builds the tree over leaves (one per LID, same order).
func Build(lids []string, leaves [][]byte) *MerkleTree {
	m := &MerkleTree{LIDs: slices.Clone(lids), Leaves: leaves, pos: make(map[string]int, len(lids))}
	for i, lid := range lids {
		m.pos[lid] = i
	}
	if len(leaves) == 0 {
		return m
	}
	m.Levels = [][][]byte{leaves}
	for cur := leaves; len(cur) > 1; {
		next := make([][]byte, (len(cur)+Arity-1)/Arity)
		for j := range next {
			next[j] = hashConcatGroup(group(cur, j))
		}
		m.Levels = append(m.Levels, next)
		cur = next
	}
	m.Root = m.Levels[len(m.Levels)-1][0]
	return m
}

// Index returns the leaf position of lid.
func (m *MerkleTree) Index(lid string) (int, bool) {
	i, ok := m.pos[lid]
	return i, ok
}

// group returns the four children of parent j over level `below`, padded by
// repeating the last child (B14).
func group(below [][]byte, j int) [][]byte {
	g := make([][]byte, Arity)
	for c := range Arity {
		g[c] = below[min(j*Arity+c, len(below)-1)]
	}
	return g
}

// childIndices are the distinct indices of node j's children in a level of
// width n.
func childIndices(n, j int) []int {
	var out []int
	for c := j * Arity; c < min((j+1)*Arity, n); c++ {
		out = append(out, c)
	}
	return out
}

// hashConcatGroup is hash_concat_group of Alg. 4.2: H(c0 || c1 || c2 || c3).
func hashConcatGroup(g [][]byte) []byte {
	h := crypto.H(bytes.Join(g, nil))
	return h[:]
}

// VerifyProof folds leaf (the live hLID at leaf position idx) up its
// quaternary proof path, whose sibling groups are read from the stored tree,
// and reports whether the result equals root (Theorem 2; Q9). A padded
// duplicate of idx itself takes the live value.
func VerifyProof(stored *MerkleTree, idx int, leaf, root []byte) bool {
	if idx < 0 || stored.height() == 0 || idx >= stored.width(0) {
		return false
	}
	cur, i := leaf, idx
	for level := 0; level < stored.height()-1; level++ {
		width := stored.width(level)
		p := i / Arity
		g := make([][]byte, Arity)
		for c := range Arity {
			j := min(p*Arity+c, width-1)
			if j == i {
				g[c] = cur
			} else {
				g[c] = stored.nodeAt(level, j)
			}
		}
		cur, i = hashConcatGroup(g), p
	}
	return bytes.Equal(cur, root)
}

// ComputedTree is ComputedTree of Alg. 4.1: the stored tree with the
// candidates' leaves replaced by their live hLIDs and exactly their ancestor
// nodes recomputed. Over[l] holds the recomputed nodes of level l.
type ComputedTree struct {
	Stored *MerkleTree
	Over   []map[int][]byte
	Root   []byte
}

type preparedLeaf struct {
	index int
	ctLog []byte
	found bool
}

// PreparedVerification contains ciphertexts acquired from storage. The
// persistence-inclusive fair benchmark times this acquisition as well as
// hashing, Merkle recomputation and localization.
type PreparedVerification struct {
	candidates []string
	leaves     []preparedLeaf
	unknown    []string
}

// PrepareVerification performs only candidate lookup and ciphertext
// acquisition. The returned evidence is still untrusted.
func PrepareVerification(candidates []string, stored *MerkleTree,
	storage *logstore.Storage) *PreparedVerification {
	prepared := &PreparedVerification{candidates: slices.Clone(candidates)}
	for _, lid := range candidates {
		index, ok := stored.Index(lid)
		if !ok {
			prepared.unknown = append(prepared.unknown, lid)
			continue
		}
		entry, found := storage.Get(lid)
		leaf := preparedLeaf{index: index, found: found}
		if found {
			leaf.ctLog = bytes.Clone(entry.CTLog)
		}
		prepared.leaves = append(prepared.leaves, leaf)
	}
	return prepared
}

// node returns the recomputed node if there is one, else the stored node.
func (c *ComputedTree) node(level, i int) []byte {
	if v, ok := c.Over[level][i]; ok {
		return v
	}
	return c.Stored.nodeAt(level, i)
}

func (c *ComputedTree) group(level, j int) [][]byte {
	n := c.Stored.width(level)
	g := make([][]byte, Arity)
	for k := range Arity {
		g[k] = c.node(level, min(j*Arity+k, n-1))
	}
	return g
}

// RecomputeMerkleRoot is Alg. 4.1 line 3 under the Q9 resolution: recompute
// the candidates' proof paths from their current ciphertexts (a missing
// ciphertext gets an empty leaf, which never matches). unknown lists
// candidates that are not leaves of the anchored tree; they can't be verified.
func RecomputeMerkleRoot(candidates []string, stored *MerkleTree, storage *logstore.Storage) (root []byte, computed *ComputedTree, unknown []string) {
	return RecomputeMerkleRootPrepared(PrepareVerification(candidates, stored, storage), stored)
}

// RecomputeMerkleRootPrepared is RecomputeMerkleRoot with storage access
// removed from the timed path.
func RecomputeMerkleRootPrepared(prepared *PreparedVerification,
	stored *MerkleTree) (root []byte, computed *ComputedTree, unknown []string) {
	c := &ComputedTree{Stored: stored, Over: make([]map[int][]byte, stored.height())}
	for l := range c.Over {
		c.Over[l] = map[int][]byte{}
	}
	for _, item := range prepared.leaves {
		leaf := []byte{}
		if item.found {
			leaf = HashLog(item.ctLog)
		}
		c.Over[0][item.index] = leaf
	}
	for l := 1; l < stored.height(); l++ {
		for _, i := range sortedKeys(c.Over[l-1]) {
			if p := i / Arity; c.Over[l][p] == nil {
				c.Over[l][p] = hashConcatGroup(c.group(l-1, p))
			}
		}
	}
	if stored.height() > 0 {
		c.Root = c.node(stored.height()-1, 0)
	}
	return c.Root, c, slices.Clone(prepared.unknown)
}

func sortedKeys(m map[int][]byte) []int {
	ks := make([]int, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return ks
}

// TraceTamperedNode is Alg. 4.2. recomputed_tree is computed; stored_tree is
// computed.Stored (built from the DHT's hLIDs when root was anchored). Levels
// are walked from the root down. At each internal level, every affected
// node's hash_concat_group is compared between the two trees, and on mismatch
// its children become affected. At Level 0 only the candidate leaves under
// the affected level-1 branches are inspected (Q10), each by its proof path
// against root (Q9); failing LIDs are returned.
func TraceTamperedNode(computed *ComputedTree, root []byte) []string {
	tampered, _ := trace(computed, root)
	return tampered
}

// trace is TraceTamperedNode, also returning the Level-0 positions it
// inspected (for tests of the Q10 behaviour).
func trace(computed *ComputedTree, root []byte) (tampered []string, inspected []int) {
	tampered = []string{}
	stored := computed.Stored
	if stored.height() == 0 {
		return tampered, nil
	}
	affected := []int{0} // first_key_in(recomputed_tree[levels[0]])
	for level := stored.height() - 1; level >= 1; level-- {
		var next []int
		for _, node := range affected {
			rec := hashConcatGroup(computed.group(level-1, node))
			sto := hashConcatGroup(stored.storedGroup(level-1, node))
			if !bytes.Equal(rec, sto) {
				next = append(next, childIndices(stored.width(level-1), node)...)
			}
		}
		affected = next
	}
	return append(tampered, failingLeaves(computed, affected, root)...), affected
}

// failingLeaves checks the candidate leaves among idxs by proof path.
func failingLeaves(computed *ComputedTree, idxs []int, root []byte) []string {
	var out []string
	for _, idx := range idxs {
		leaf, isCandidate := computed.Over[0][idx]
		if isCandidate && !VerifyProof(computed.Stored, idx, leaf, root) {
			out = append(out, computed.Stored.lidAt(idx))
		}
	}
	return out
}

// ErrStoredTreeMismatch means the off-chain stored tree does not have the
// on-chain root, so no proof path from it can be trusted.
var ErrStoredTreeMismatch = errors.New("integrity: stored Merkle tree root differs from on-chain root")

// ErrPreparedVerificationInvalid means the caller did not supply a complete
// prepared core-verification state.
var ErrPreparedVerificationInvalid = errors.New("integrity: invalid prepared verification")

// IdentifyTamperedFiles is Alg. 4.1. It returns the verified result set and
// the Integrity flag; tampered_lids starts empty (B12). Every returned LID has
// been verified against the on-chain root.
//
// If the recomputed root differs from R but the trace finds no failing
// candidate, the stored tree is inconsistent along the candidates' paths. In
// that case every candidate is checked by its own proof path, failures are
// excluded, and Integrity is FALSE.
func IdentifyTamperedFiles(candidates []string, stored *MerkleTree, storage *logstore.Storage,
	chain blockchain.BlockchainContracts) (verified []string, integrityOK bool, err error) {
	storedProof, err := chain.VerifyIntegrity()
	if err != nil {
		return nil, false, err
	}
	if stored == nil || !bytes.Equal(stored.Root, storedProof) {
		return nil, false, ErrStoredTreeMismatch
	}
	prepared := PrepareVerification(candidates, stored, storage)
	return IdentifyTamperedFilesPrepared(prepared, stored, storedProof)
}

// IdentifyTamperedFilesPrepared is the fair core boundary: chain and storage
// access have completed, while ciphertext hashing, root recomputation and
// tamper localization remain timed.
func IdentifyTamperedFilesPrepared(prepared *PreparedVerification, stored *MerkleTree,
	storedProof []byte) (verified []string, integrityOK bool, err error) {
	if prepared == nil {
		return nil, false, ErrPreparedVerificationInvalid
	}
	if stored == nil || !bytes.Equal(stored.Root, storedProof) {
		return nil, false, ErrStoredTreeMismatch
	}
	_, computed, _ := RecomputeMerkleRootPrepared(prepared, stored)
	return LocalizeComputed(prepared, computed, storedProof)
}

// LocalizeComputed times localization after a root mismatch has already been
// detected and the candidate paths have already been recomputed.
func LocalizeComputed(prepared *PreparedVerification, computed *ComputedTree,
	storedProof []byte) (verified []string, integrityOK bool, err error) {
	if prepared == nil || computed == nil || computed.Stored == nil {
		return nil, false, ErrPreparedVerificationInvalid
	}
	tampered := slices.Clone(prepared.unknown)
	integrityOK = len(prepared.unknown) == 0
	if !bytes.Equal(computed.Root, storedProof) {
		integrityOK = false
		traced := TraceTamperedNode(computed, storedProof)
		if len(traced) == 0 {
			traced = failingLeaves(computed, sortedKeys(computed.Over[0]), storedProof)
		}
		tampered = append(tampered, traced...)
	}
	if len(tampered) == 0 {
		return slices.Clone(prepared.candidates), integrityOK, nil
	}
	for _, lid := range prepared.candidates {
		if !slices.Contains(tampered, lid) {
			verified = append(verified, lid)
		}
	}
	return verified, false, nil
}

// AnchorRoot is the Data Owner's anchoring step. It builds the quaternary tree
// over the DHT's hLID values for lids (in the given order), stores its root R
// on-chain with UpdateMerkleRoot, and returns the tree. The caller keeps the
// tree off-chain as stored_tree.
func AnchorRoot(lids []string, dht *DHT, chain blockchain.BlockchainContracts) (*MerkleTree, error) {
	leaves := make([][]byte, len(lids))
	for i, lid := range lids {
		if v, ok := dht.Get(lid); ok {
			leaves[i] = v
		} else {
			leaves[i] = []byte{}
		}
	}
	t := Build(lids, leaves)
	return t, chain.UpdateMerkleRoot(t.Root)
}
