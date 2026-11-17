// Package search is Phase 6: SecureQueryExecution with Boolean operations
// (Alg. 3, Eq. 20-27) followed by result verification (Alg. 4.1).
//
// Paper-resolution decisions (these resolve inconsistencies/underspecification
// in EVSEB; they are not optimizations or extensions):
//   - Q6/B9. Eq. 20's Q = (Tw, LType, Tokenu) carries Tw, but AND/OR (Eq.
//     25-26) need two or more operand sets. Tw is read as a list/set of
//     trapdoors, one LID set per trapdoor, combined by Q.boolean_operator.
//     NOT (Eq. 27) is unary and takes exactly one trapdoor (Q12 item 3); a
//     multi-trapdoor NOT is rejected as unsupported.
//   - Q8. Every sub-node under the LType's primary node gets the Bloom test.
//     Negative sub-nodes are skipped, and candidates of positive ones are
//     unioned. Lall for NOT is all LIDs under the primary node.
//   - B8. valid_pointers <- candidateEntries (Alg. 3 line 13 never wires it).
package search

import (
	"errors"
	"slices"

	"evseb/internal/access"
	"evseb/internal/blockchain"
	"evseb/internal/integrity"
	"evseb/internal/logstore"
	"evseb/internal/trapdoor"
	"evseb/internal/tree"
)

// Boolean operators of Alg. 3 lines 19-27.
const (
	OpAND = "AND"
	OpOR  = "OR"
	OpNOT = "NOT"
)

// Alg. 3 early returns (lines 6 and 10), with the paper's strings.
var (
	ErrAccessDenied = errors.New("Access Denied")
	ErrNoMatch      = errors.New("No matching records found")
)

// ErrNOTArity rejects a NOT query with other than one trapdoor. Eq. 27
// defines only NOT(L1) = Lall \ L1 (QUESTIONS.md Q12 item 3).
var ErrNOTArity = errors.New("search: NOT takes exactly one trapdoor (Eq. 27 is unary); multi-keyword NOT is unsupported")

// Query is Eq. 20 with Tw read as a list of trapdoors (B9) plus
// Q.boolean_operator (Alg. 3 line 14).
type Query struct {
	Tw              []trapdoor.Trapdoor
	LType           string
	Tokenu          []byte // encrypted token
	BooleanOperator string
}

// Result is the Step 7 output: the verified, still-encrypted log entries
// (FR-20) and the Alg. 4.1 Integrity flag.
type Result struct {
	Entries   []logstore.EncryptedLog
	Integrity bool
}

// Engine is the Search Engine with the state Alg. 3 reads: the tree T, the
// chain (VerifyAccess, VerifyIntegrity), the stored Merkle tree built from the
// DHT when R was anchored, and EncryptedStorage.
type Engine struct {
	Tree    *tree.Tree
	Chain   blockchain.BlockchainContracts
	Stored  *integrity.MerkleTree
	Storage *logstore.Storage
}

// SecureQueryExecution is Alg. 3.
//
// A trapdoor "has no candidate" when no sub-node of the LType yields an LID
// for it: every Bloom test is negative (line 9), or a positive test finds
// nothing in the inverted index. "No matching records found" (line 10)
// follows from the Boolean definitions (QUESTIONS.md Q12 item 4):
//   - AND: when any trapdoor has no candidate (the intersection is empty).
//   - OR, and no operator: when every trapdoor has no candidate.
//   - NOT: never. If its keyword has no candidate, the result is Lall.
//
// Otherwise the Boolean result is returned, even if it is empty.
func (e *Engine) SecureQueryExecution(q Query) (Result, error) {
	allowed, err := access.VerifyAccess(e.Chain, q.Tokenu, q.LType) // line 5, Eq. 21
	if err != nil {
		return Result{}, err
	}
	if !allowed {
		return Result{}, ErrAccessDenied // line 6
	}
	if q.BooleanOperator == OpNOT && len(q.Tw) != 1 {
		return Result{}, ErrNOTArity
	}
	target := tree.LocateTreeNode(e.Tree, q.LType) // line 8, Eq. 22 (B10, Q8)
	if target == nil || len(q.Tw) == 0 {
		return Result{}, ErrNoMatch
	}
	subs := target.SubNodesInOrder()
	validPointers := make([][]string, len(q.Tw)) // line 13 with B8
	for i, tw := range q.Tw {
		for _, s := range subs {
			if !tree.BloomFilterCheck(s, tw.Tw) { // line 9, Eq. 23: skip this branch
				continue
			}
			validPointers[i] = append(validPointers[i], tree.RetrieveFromIndex(s, tw.Tw)...) // line 12, Eq. 24
		}
	}
	if noMatch(validPointers, q.BooleanOperator) {
		return Result{}, ErrNoMatch // line 10
	}
	resultSet, err := PerformBooleanOperations(validPointers, q.BooleanOperator, target.AllLIDs())
	if err != nil {
		return Result{}, err
	}
	verified, ok, err := integrity.IdentifyTamperedFiles(resultSet, e.Stored, e.Storage, e.Chain) // line 15
	if err != nil {
		return Result{}, err
	}
	res := Result{Integrity: ok}
	for _, lid := range verified { // Step 7: return the encrypted entries
		if enc, found := e.Storage.Get(lid); found {
			res.Entries = append(res.Entries, enc)
		}
	}
	return res, nil
}

// noMatch is the operator-dependent "No matching records found" rule of
// SecureQueryExecution (Q12 item 4). sets holds one candidate LID list per
// trapdoor.
func noMatch(sets [][]string, op string) bool {
	switch op {
	case OpAND:
		return slices.ContainsFunc(sets, func(s []string) bool { return len(s) == 0 })
	case OpNOT:
		return false
	default:
		return !slices.ContainsFunc(sets, func(s []string) bool { return len(s) > 0 })
	}
}

// PerformBooleanOperations is Alg. 3 lines 18-29. sets holds one LID list
// per trapdoor. lall is Lall for NOT (all LIDs under the primary node, Q8).
// AND/OR fold left to right (B9). NOT is Eq. 27, Lall \ L1, over exactly one
// set (Q12 item 3). With no operator, the valid pointers are returned
// (line 27) as their union. The result is sorted and duplicate-free.
func PerformBooleanOperations(sets [][]string, op string, lall []string) ([]string, error) {
	if len(sets) == 0 {
		return nil, errors.New("search: no operand sets")
	}
	switch op {
	case OpAND:
		return Intersect(sets...), nil
	case OpOR:
		return Union(sets...), nil
	case OpNOT:
		if len(sets) != 1 {
			return nil, ErrNOTArity
		}
		return Difference(lall, sets[0]), nil
	default:
		return Union(sets...), nil
	}
}

// Intersect is Eq. 25, AND(L1, L2) = L1 ∩ L2, folded over all given sets.
func Intersect(sets ...[]string) []string {
	if len(sets) == 0 {
		return nil
	}
	out := Union(sets[0])
	for _, s := range sets[1:] {
		in := setOf(s)
		out = slices.DeleteFunc(out, func(x string) bool { return !in[x] })
	}
	return out
}

// Union is Eq. 26, OR(L1, L2) = L1 ∪ L2, over all given sets.
func Union(sets ...[]string) []string {
	var out []string
	for _, s := range sets {
		out = append(out, s...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// Difference is Eq. 27, NOT(L1) = Lall \ L1.
func Difference(lall, l1 []string) []string {
	in := setOf(l1)
	return slices.DeleteFunc(Union(lall), func(x string) bool { return in[x] })
}

func setOf(xs []string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}
