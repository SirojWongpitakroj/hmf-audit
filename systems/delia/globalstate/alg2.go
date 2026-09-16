// Algorithm 2 (StateVerify), p. 3289.
//
//	procedure StateVerify(psi_{pi}, root_{pi,li}, G_{nu-1}, G_nu)
//	  root'_{pi,li} <- MHTGen(psi_{pi});
//	  if root'_{pi,li} != root_{pi,li} then return 0;        > invalid root
//	  G'_nu <- GlobalGen(root_{pi,li}, G_{nu-1})
//	  if G'_nu != G_nu then return 0;                        > invalid global state
//	  return 1;                                              > valid global state
//
// The verifier's root is recomputed fresh on every call from its own locally
// maintained psi (LSG-008: never a psi supplied by the proposer). i is the
// proposing server's position in the frozen P order (same GAP-08 ruling as
// GlobalGen), supplied by the caller from the message's sender identity.
package globalstate

import "delia/hash"

// StateVerify implements Algorithm 2. It returns true (1) iff root
// recomputes from psi and GlobalGen(root, prev, i) equals next by full
// canonical encoding (GAP-09(a): nu is part of the comparison).
func StateVerify(psi []hash.Digest, root hash.Digest, prev GlobalState, next GlobalState, i int) bool {
	rootPrime := mhtGen(psi)
	if rootPrime != root {
		return false
	}
	gPrime, err := globalGenFn(root, prev, i)
	if err != nil {
		return false
	}
	return gPrime.Equal(next)
}

// globalGenFn is a package-level indirection to GlobalGen (ALG2-004: "using
// Alg. 1 as the subroutine"). It exists only so tests can spy on whether
// GlobalGen was invoked (TEST-07: an invalid root must return 0 before
// GlobalGen is called); it does not add caching or change behavior. Tests
// that override it must restore GlobalGen afterward.
var globalGenFn = GlobalGen
