// Package hash provides the single hash function H(.) used throughout DELIA.
//
// DELIA-CRYPTO-001/002: H(.) is used at every cited call site (record hashes,
// Eq. (2) LocalState, Alg. 1-3, H(psi) in OfflineResolving). The reproduction
// uses SHA-256 via Go's standard crypto/sha256, and it is the *only* hash in
// the system (SRS Section 26 prohibition: no alternative hashes).
package hash

import "crypto/sha256"

// Size is the digest size in bytes of H(.).
const Size = sha256.Size

// Digest is a raw 32-byte SHA-256 digest. Digests are always raw bytes, never
// hex strings (GAP-02, DATA-018).
type Digest [Size]byte

// Zero is the all-zero sentinel digest used as the GlobalState G_0
// initialization value (GAP-06). It is never the output of H on any real
// input's probability-zero collision aside, and callers must not treat it as
// a genuine produced LocalState (GAP-75).
var Zero = Digest{}

// H computes the SHA-256 digest of b. This is the sole hash function in the
// system; it is recomputed fresh on every call site (no caching beyond what
// Eq. (2) and Alg. 1-3 literally carry forward).
func H(b []byte) Digest {
	return sha256.Sum256(b)
}

// Concat concatenates raw fixed-size byte slices as raw bytes (GAP-02, "fixed
// size values are concatenated as raw bytes"). It is a small helper so call
// sites do not hand-roll append chains differently from each other.
func Concat(parts ...[]byte) []byte {
	n := 0
	for _, p := range parts {
		n += len(p)
	}
	out := make([]byte, 0, n)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}
