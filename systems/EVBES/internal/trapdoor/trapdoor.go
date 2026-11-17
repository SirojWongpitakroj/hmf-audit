// Package trapdoor is Phase 5 (Eq. 18-19). The same Generate is used by the
// indexer (Alg. 2 lines 8-9) and by the querying Data User, so index-side and
// query-side values are equal for equal keywords.
package trapdoor

import "evseb/internal/crypto"

// Trapdoor is Tw = HMAC-SHA256(Ksearch, SHA-256(w)), 32 bytes.
type Trapdoor struct {
	Tw []byte
}

// Generate computes h(w) = SHA-256(w) (Eq. 18 / Alg. 2 line 8) and
// Tw = PRF(Ksearch, h(w)) = HMAC-SHA256(Ksearch, h(w)) (Eq. 19 / Alg. 2 line 9).
// w is used byte-for-byte; callers pass the keyword in the same normalized
// (lower-case, Eq. 11) form the indexer used.
func Generate(w string, ksearch [crypto.KeySize]byte) Trapdoor {
	h := crypto.H([]byte(w))
	return Trapdoor{Tw: crypto.PRF(ksearch, h[:])}
}
