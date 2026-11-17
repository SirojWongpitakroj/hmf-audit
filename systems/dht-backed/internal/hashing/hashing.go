// Package hashing provides the single hash function H and the only three
// domain-separation tags the system is permitted to use.
//
// SRS: R-CRY-01 (H = SHA-256, Q36), R-CRY-02 (exactly three tags: 0x00 leaf,
// 0x01 internal, 0xA3 attestation pre-image), Q8 (raw byte concatenation).
package hashing

import "crypto/sha256"

// Size is the digest length in bytes. Every digest the system produces is 32
// bytes (R-CRY-01 acceptance).
const Size = sha256.Size

// The three domain-separation tags. No fourth tag exists anywhere in this
// system (R-CRY-02).
const (
	// TagLeaf is tag_L, prefixed to leaf digests (Sec. III.B, Eq. (2)/(15)/(20)).
	TagLeaf byte = 0x00
	// TagInternal is tag_I, prefixed to internal nodes (Eq. (3)/(21)).
	TagInternal byte = 0x01
	// TagAttestation is the producer attestation pre-image tag (Eq. (16)).
	TagAttestation byte = 0xA3
)

// Digest is a SHA-256 output.
type Digest [Size]byte

// H is the collision-resistant hash function used throughout (SHA-256).
// Arguments are concatenated as raw bytes, with no separator (Q8).
func H(parts ...[]byte) Digest {
	h := sha256.New()
	for _, p := range parts {
		h.Write(p)
	}
	var d Digest
	h.Sum(d[:0])
	return d
}

// Tag returns a one-byte slice holding t. Tags are exactly one byte each (Q8).
func Tag(t byte) []byte { return []byte{t} }
