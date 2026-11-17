// Package bhash provides the single, deterministic hash function and the
// deterministic byte-encoding primitives used to build every hash input in
// BLAS: OMT/IIMT/RIMBT leaves and internal nodes, transactions, and the
// aggregated MHTs of Optimization 2.
//
// Governing decisions (DECISIONS.md):
//   - A1: use golang.org/x/crypto/blake2b, never hand-implement BLAKE2b.
//   - A2: unkeyed BLAKE2b, 32-byte output (blake2b.New256) — no other
//     parameterisation, no domain-separation tag, no salt, no key
//     (R-CRYPTO-011).
//   - A3: fixed-width big-endian integers; raw 32-byte digests; length-
//     prefixed variable-length string/byte fields.
//
// Nothing in this package may be replaced by crypto/sha256, hash/fnv,
// encoding/json, encoding/gob, hex encoding of digests, or platform-width
// int/uint. See R-GO-002, R-GO-006, R-GO-008..R-GO-014.
package bhash

import (
	"encoding/binary"
	"sync/atomic"

	"golang.org/x/crypto/blake2b"
)

// DigestSize is the fixed output width of H(). R-CRYPTO-014 forbids any
// truncation below 256 bits.
const DigestSize = 32

// Digest is a raw 32-byte BLAKE2b-256 output. It is never represented as a
// hex string or any other text form inside a hash input (R-GO-011).
type Digest [DigestSize]byte

// H is the single hash function used by every BLAS authenticated structure
// (R-CRYPTO-002). It is unkeyed BLAKE2b-256 (A2).
func H(data []byte) Digest {
	if counting.Load() {
		invocations.Add(1)
	}
	// blake2b.New256 never errors for a nil key.
	h, err := blake2b.New256(nil)
	if err != nil {
		// Unreachable with a nil key; blake2b.New256 only errors on bad
		// key length. Panicking here would be protocol-control-flow on
		// attacker input elsewhere (forbidden by R-GO-026), but this is
		// a pure library-misuse case with no external input, so a panic
		// is appropriate (mirrors sha256.New's own no-error contract).
		panic("bhash: blake2b.New256 failed: " + err.Error())
	}
	h.Write(data)
	var d Digest
	copy(d[:], h.Sum(nil))
	return d
}

// HashConcat hashes the concatenation of several already-framed byte slices.
// It performs no additional framing itself — callers are responsible for
// ensuring each argument is unambiguous on its own (fixed-width, or already
// length-prefixed via FramedBytes/FramedString).
func HashConcat(parts ...[]byte) Digest {
	total := 0
	for _, p := range parts {
		total += len(p)
	}
	buf := make([]byte, 0, total)
	for _, p := range parts {
		buf = append(buf, p...)
	}
	return H(buf)
}

// Uint64BE encodes a protocol integer identifier (oid, esn, tsn, kid,
// BlockHeight, lid, fid, TxNum, ...) as fixed 8-byte big-endian. Never use
// Go's platform-width int/uint, strconv, or fmt for a hashed integer
// (R-GO-008, R-GO-009).
func Uint64BE(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

// Int64BE encodes a signed protocol quantity (timestamps, RIMBT numerical
// values v) as fixed 8-byte big-endian two's complement (D7 in
// DECISIONS.md: signed 64-bit Unix milliseconds).
func Int64BE(v int64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(v))
	return b
}

// Uint32BE encodes a 4-byte big-endian length prefix.
func Uint32BE(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

// FramedBytes returns a 4-byte big-endian length prefix followed by the raw
// bytes — the "length-prefixed variable-length ... byte field" framing
// adopted under A3, needed so that variable-length fields (keywords, log
// payloads) are unambiguously recoverable inside a concatenated hash input
// (R-SEC-013, R-GO-010).
func FramedBytes(b []byte) []byte {
	out := make([]byte, 0, 4+len(b))
	out = append(out, Uint32BE(uint32(len(b)))...)
	out = append(out, b...)
	return out
}

// FramedString frames a UTF-8 string identically to FramedBytes. No
// normalization (case folding, NFC/NFD) is applied — keywords are hashed
// byte-for-byte as stored (R-GO-012).
func FramedString(s string) []byte {
	return FramedBytes([]byte(s))
}

// DigestBytes returns the raw 32 bytes of a digest, for embedding as a fixed
// -width field inside an outer hash input.
func DigestBytes(d Digest) []byte {
	b := make([]byte, DigestSize)
	copy(b, d[:])
	return b
}

// The counter exists for the authentication-cost experiment. It is off by
// default, so ingestion and ordinary audits pay one atomic load per call. It
// is process-wide: enable it only around verification that runs on its own.
var (
	counting    atomic.Bool
	invocations atomic.Uint64
)

// StartCount zeroes the counter and begins counting H invocations.
func StartCount() {
	invocations.Store(0)
	counting.Store(true)
}

// StopCount ends counting and returns the invocations since StartCount. Each
// call of H is one invocation over one protocol-defined input; HashConcat is
// one invocation because it hashes the concatenation once.
func StopCount() uint64 {
	counting.Store(false)
	return invocations.Load()
}
