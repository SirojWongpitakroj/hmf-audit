// Package dht is the deterministic in-process simulated private DHT (Q10),
// its record formats and key derivation (Q12, Q45), and the Phase 5(a) access
// gate (Q13, Q41).
//
// It is not a real networked Kademlia deployment: 16 logical nodes,
// replication factor 1, synchronous lookups, and no routing-delay simulation.
package dht

import (
	"crypto/sha256"
	"encoding/binary"

	"dhtbacked/internal/hashing"
)

// Domain prefixes for identifiers and lookup keys. Each is the literal ASCII
// string the SRS writes inside H(.) (Q45, §0.9).
const (
	prefixTree      = "TREE"
	prefixAncestor  = "ANC"
	prefixParent    = "P"
	prefixCert      = "C"
	prefixRoot      = "R"
	prefixMicroRoot = "M"
)

// NodeCount is the number of logical DHT nodes (Q10).
const NodeCount = 16

// ReplicationFactor is 1 (Q10).
const ReplicationFactor = 1

// u64 encodes a numeric value as an unsigned 64-bit big-endian integer — the
// only numeric encoding used before hashing (Q45).
func u64(v uint64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	return b[:]
}

// TreeID computes treeID = H("TREE" || epoch || logType) (Q45, §0.9).
//
// logType is appended as raw UTF-8 with no length prefix, matching the literal
// concatenation the SRS writes; it is the final field, so the pre-image
// remains injective.
func TreeID(epoch uint64, logType string) hashing.Digest {
	return hashing.H([]byte(prefixTree), u64(epoch), []byte(logType))
}

// AncestorID computes ancestorID = H("ANC" || treeID || s || k) (Q45, §0.9).
func AncestorID(treeID hashing.Digest, s, k uint64) hashing.Digest {
	return hashing.H([]byte(prefixAncestor), treeID[:], u64(s), u64(k))
}

// KeyP computes Key_P = H("P" || treeID || j || k) (Q45).
// seq appears in no lookup key.
func KeyP(treeID hashing.Digest, j, k uint64) hashing.Digest {
	return hashing.H([]byte(prefixParent), treeID[:], u64(j), u64(k))
}

// KeyC computes Key_C = H("C" || treeID || s || k) (Q45).
func KeyC(treeID hashing.Digest, s, k uint64) hashing.Digest {
	return hashing.H([]byte(prefixCert), treeID[:], u64(s), u64(k))
}

// KeyR computes Key_R = H("R" || treeID) (Q45).
func KeyR(treeID hashing.Digest) hashing.Digest {
	return hashing.H([]byte(prefixRoot), treeID[:])
}

// KeyM computes Key_M = H("M" || treeID || windowID) (Q45, Q49).
func KeyM(treeID hashing.Digest, windowID uint64) hashing.Digest {
	return hashing.H([]byte(prefixMicroRoot), treeID[:], u64(windowID))
}

// NodeIndex assigns a key to one of the 16 logical nodes: the first eight
// bytes of SHA256(key), read as an unsigned big-endian integer, reduced modulo
// 16 (Q10).
func NodeIndex(key hashing.Digest) int {
	h := sha256.Sum256(key[:])
	return int(binary.BigEndian.Uint64(h[:8]) % NodeCount)
}
