// Package bloom is the per-sub-node Bloom filter Bnij (Def. 3, Eq. 1, 8, 16).
//
// Inserted values are trapdoors PRF(Ksearch, h(w)) (B3a), never plaintext
// keywords. m and k are caller-supplied prototype configuration (B3b); the
// paper fixes neither.
package bloom

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
)

// BloomFilter is a bit array of length M with K hash functions.
type BloomFilter struct {
	Bits []bool
	K    int
	M    int
}

// New returns an empty filter with m bits and k hash functions.
func New(m, k int) *BloomFilter {
	if m < 1 || k < 1 {
		panic("bloom: m and k must be >= 1")
	}
	return &BloomFilter{Bits: make([]bool, m), K: k, M: m}
}

// position is H_i(x) mod m. The paper names H_1..H_k without defining them;
// here H_i(x) = first 8 bytes (big-endian) of SHA-256(uint32_be(i) || x).
func (b *BloomFilter) position(i int, x []byte) int {
	var pre [4]byte
	binary.BigEndian.PutUint32(pre[:], uint32(i))
	h := sha256.New()
	h.Write(pre[:])
	h.Write(x)
	var d [32]byte
	h.Sum(d[:0])
	return int(binary.BigEndian.Uint64(d[:8]) % uint64(b.M))
}

// Insert sets B[H_i(x)] = 1 for i = 1..k (Eq. 8, with x = PRF(h(w)) per Alg. 2
// line 15).
func (b *BloomFilter) Insert(x []byte) {
	for i := 1; i <= b.K; i++ {
		b.Bits[b.position(i, x)] = true
	}
}

// Check is Eq. 16: true iff B[H_i(x)] = 1 for all i.
func (b *BloomFilter) Check(x []byte) bool {
	for i := 1; i <= b.K; i++ {
		if !b.Bits[b.position(i, x)] {
			return false
		}
	}
	return true
}

// FalsePositiveRate is Eq. 1: Pfp = (1 - e^(-kn/m))^k.
func FalsePositiveRate(k, n, m int) float64 {
	return math.Pow(1-math.Exp(-float64(k)*float64(n)/float64(m)), float64(k))
}

// Params sizes a filter for n expected insertions at target false-positive
// rate p, using the standard optimum of Eq. 1: m = -n ln p / (ln 2)^2,
// k = (m/n) ln 2. This is configuration, not an EVSEB parameter (B3b).
func Params(n int, p float64) (m, k int) {
	if n < 1 {
		n = 1
	}
	m = int(math.Ceil(-float64(n) * math.Log(p) / (math.Ln2 * math.Ln2)))
	k = int(math.Round(float64(m) / float64(n) * math.Ln2))
	return max(m, 1), max(k, 1)
}
