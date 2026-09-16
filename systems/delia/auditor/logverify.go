// Package auditor implements Algorithm 3 (LogVerify), the integrity audit
// (AUDIT-002..010; §3.4 stage 2 p. 3290, Alg. 3 p. 3289).
//
// Listing as printed (Alg. 3, p. 3289):
//
//	procedure LogVerify({delta_{pi,1}, ..., delta_{pi,xi}}, {G_1, ..., G_nu})
//	  ErrList <- {}
//	  k <- 1, l <- 0;
//	  for k <= nu do
//	    Parse G_k as (omega_{p1,l1}, ..., omega_{pi,li}, ..., omega_{pn,ln});
//	    for l <= li do
//	      psi <- {H(delta_{pi,l*theta+1}), ..., H(delta_{pi,(l+1)*theta})};
//	      root <- MHTGen(psi);
//	      if l == 0 then omega <- H(root);
//	      else omega <- H(root || omega);
//	    if omega != omega_{pi,li} then
//	      ErrList <- ErrList U {li};
//	    omega <- omega_{pi,li}
//	  return ErrList;
//
// This package implements the DEV-01-ruled counter semantics: l is
// initialized once before the outer loop and never reset per k; k++ after
// each G_k; l++ per reconstructed batch; each batch is rebuilt exactly once
// across the whole run (a single linear pass, AUDIT-004/006). The comparison
// follows the printed indentation (after the inner loop, per-G_k); on
// mismatch li is added to ErrList, and omega is then reset unconditionally
// to the ledger value so later batches are not cascade-flagged (AUDIT-007).
//
// Per GAP-75, while a server's slot in G_k is still the GAP-06 zero
// sentinel (no completed batch yet), no comparison is made and nothing is
// reconstructed for that k.
//
// li per G_k is not carried in GlobalState (GAP-08); it is derived by
// replaying the supplied state sequence and counting how many times the
// audited server's slot has changed (AUDIT-011/GAP-61): li_at_Gk = (number
// of slot changes in G_1..G_k) - 1.
//
// End-to-end retrieval of {delta..} and {G_1..G_nu} (AUDIT-001/005/011/012)
// is out of scope here (blocked by GAP-81/GAP-83, Tier C) — LogVerify is a
// pure function over in-memory slices supplied by the caller/test harness.
// This package must contain no Merkle inclusion proofs, multiproofs, binary
// search, caching or goroutines (AUDIT-010, GO-003; SRS Section 26, item 7).
package auditor

import (
	"delia/hash"
	"delia/localstate"
	"delia/merkle"
)

// Record is one audit log entry (delta_{pi,xi}): the raw record bytes, per
// GAP-03(a) the UTF-8 line excluding line-ending bytes.
type Record []byte

// LogVerify implements Algorithm 3. records is the audited server's
// complete record sequence in order (delta_{pi,1}..delta_{pi,xi}); theta is
// the batch size (DATA-007, required, no default); index is the audited
// server's position in the P order frozen at Init (GAP-08); states is
// {G_1, ..., G_nu} in ascending serial order. It returns ErrList: the set of
// batch numbers (li) whose reconstruction did not match the ledger value.
// Empty ⇔ audit passes (AUDIT-008, AUDIT-009).
func LogVerify(records []Record, theta int, index int, states []State) []uint64 {
	var errList []uint64

	l := uint64(0)
	var omega hash.Digest
	changeCount := uint64(0)
	prevEntry := hash.Zero

	for k := 0; k < len(states); k++ {
		entry := stateEntryAt(states[k], index)
		if entry != prevEntry {
			changeCount++
		}
		prevEntry = entry

		if entry == hash.Zero {
			// GAP-75: no completed batch yet for this server; skip.
			continue
		}
		li := changeCount - 1

		for l <= li {
			start := l * uint64(theta)
			end := start + uint64(theta)
			if end > uint64(len(records)) {
				// GAP-16(a): not enough records to rebuild this batch
				// (deleted records, or a genuine trailing partial that was
				// never actually completed). Stop reconstructing; the
				// comparison below will catch a real mismatch.
				break
			}
			leaves := make([]hash.Digest, theta)
			for x := uint64(0); x < uint64(theta); x++ {
				leaves[x] = hashRecord(records[start+x])
			}
			root := mhtGen(leaves)
			if l == 0 {
				omega = localstate.Compute(root, nil)
			} else {
				prev := omega
				omega = localstate.Compute(root, &prev)
			}
			l++
		}

		if omega != entry {
			errList = append(errList, li)
		}
		// AUDIT-007: resume the chain from the ledger value unconditionally,
		// so a flagged batch does not cascade into later batches.
		omega = entry
	}

	return errList
}

// hashRecord and mhtGen are package-level indirections to hash.H and
// merkle.MHTGen (TEST-14: "H called once per record per rebuilt batch;
// MHTGen once per inner iteration"). They add no caching or behavior change;
// tests that override them must restore the originals.
var hashRecord = func(r Record) hash.Digest { return hash.H(r) }
var mhtGen = merkle.MHTGen
