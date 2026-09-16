// Algorithm 1 (GlobalGen), p. 3289.
//
//	procedure GlobalGen(root_{pi,li}, G_{nu-1})
//	  Parse G_{nu-1} as (omega_{p1,l1}, ..., omega_{pi,li-1}, ..., omega_{pn,ln});
//	  omega_{pi,li} <- H(root_{pi,li} || omega_{pi,li-1});        > Eq. (2)
//	  G_nu <- (omega_{p1,l1}, ..., omega_{pi,li}, ..., omega_{pn,ln});   > Eq. (3)
//	  return G_nu;
//
// The listing always chains with omega_{pi,li-1} and never shows the Eq. (2)
// li = 0 branch. Per the frozen GAP-07(a)/DEV-13 ruling, GlobalGen applies
// Eq. (2) in full: when the generating server's current entry in G_{nu-1} is
// still the GAP-06 zero sentinel (i.e. this is that server's first batch,
// li = 0), omega = H(root) alone, not H(root || zero-digest) (TEST-05b). The
// zero sentinel is never treated as a genuine LocalState input to the hash
// (GAP-75).
//
// Per ALG1-006/GAP-08, Alg. 1's own inputs do not identify i (which server's
// position to replace); the caller supplies it (the generating server's
// index in the P order frozen at Init).
package globalstate

import (
	"errors"

	"delia/hash"
	"delia/localstate"
)

// GlobalGen implements Algorithm 1. root is root_{pi,li}, prev is G_{nu-1},
// and i is the generating server's position in the frozen P order. It
// returns G_nu with nu = prev.Nu + 1. GlobalGen is computed fresh on every
// call, with no cache.
func GlobalGen(root hash.Digest, prev GlobalState, i int) (GlobalState, error) {
	if i < 0 || i >= len(prev.Entries) {
		return GlobalState{}, errors.New("globalstate: index i out of range for G_{nu-1}")
	}
	entries := make([]hash.Digest, len(prev.Entries))
	copy(entries, prev.Entries)

	prevOmega := prev.Entries[i]
	var omega hash.Digest
	if prevOmega == hash.Zero {
		// li = 0 branch (Eq. (2)): no completed batch yet at this position.
		omega = localstate.Compute(root, nil)
	} else {
		omega = localstate.Compute(root, &prevOmega)
	}
	entries[i] = omega

	return GlobalState{Nu: prev.Nu + 1, Entries: entries}, nil
}
