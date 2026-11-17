// Package bitmap implements the uncompressed bitmap representation used by
// every IIMT/RIMBT leaf and by Boolean/range query evaluation (R-IOMF-009,
// R-IOMF-024).
//
// Governing decisions (DECISIONS.md §A, item A4):
//   - 0-based bit positions.
//   - LSB-first within each byte.
//   - Uncompressed.
//   - Exactly ceil(N/8) bytes; unused high bits of the final byte are
//     zeroed.
//   - N is derived from the authenticated object population, never from an
//     unauthenticated length field carried in a message (C5, R-GO-016).
package bitmap

import (
	"encoding/binary"
	"fmt"
	"math/bits"
)

// Bitmap is a fixed-length (N bits), 0-based, LSB-first, uncompressed
// bitmap. N is stored explicitly and is never inferred from len(bytes)*8
// (R-GO-016), because padding to a byte boundary would otherwise make the
// serialized bytes ambiguous.
type Bitmap struct {
	n     uint64
	bytes []byte // exactly ceil(n/8) bytes
}

// New returns an all-zero bitmap of length n bits.
func New(n uint64) Bitmap {
	return Bitmap{n: n, bytes: make([]byte, byteLen(n))}
}

func byteLen(n uint64) uint64 {
	return (n + 7) / 8
}

// Len returns N, the number of objects this bitmap is defined over.
func (b Bitmap) Len() uint64 { return b.n }

func (b Bitmap) Count() uint64 {
	var n uint64
	for _, v := range b.bytes {
		n += uint64(bits.OnesCount8(v))
	}
	return n
}

// Set sets bit i (0-based) to 1. Panics if i >= N: this is a programmer
// error (constructing a bitmap), not adversarial input; adversarial bitmap
// handling goes through FromBytes, which never panics.
func (b Bitmap) Set(i uint64) {
	if i >= b.n {
		panic(fmt.Sprintf("bitmap: Set(%d) out of range for N=%d", i, b.n))
	}
	b.bytes[i/8] |= 1 << (i % 8)
}

// Get returns bit i (0-based). Panics if i >= N.
func (b Bitmap) Get(i uint64) bool {
	if i >= b.n {
		panic(fmt.Sprintf("bitmap: Get(%d) out of range for N=%d", i, b.n))
	}
	return b.bytes[i/8]&(1<<(i%8)) != 0
}

// Bytes returns the canonical serialized form: exactly ceil(N/8) bytes,
// LSB-first within each byte, unused high bits of the final byte zero. This
// is exactly what feeds H(bm) (R-CRYPTO-003, R-CRYPTO-005). The returned
// slice is a copy; mutating it does not affect the bitmap.
func (b Bitmap) Bytes() []byte {
	out := make([]byte, len(b.bytes))
	copy(out, b.bytes)
	return out
}

// GobEncode gives persistence adapters an explicit representation even though
// Bitmap keeps its invariants in unexported fields. The first eight bytes are
// N in big-endian order, followed by the canonical bitmap bytes.
func (b Bitmap) GobEncode() ([]byte, error) {
	out := make([]byte, 8+len(b.bytes))
	binary.BigEndian.PutUint64(out[:8], b.n)
	copy(out[8:], b.bytes)
	return out, nil
}

// GobDecode restores a bitmap while enforcing the same canonical checks as
// FromBytes, so malformed persisted evidence cannot bypass bitmap validation.
func (b *Bitmap) GobDecode(data []byte) error {
	if len(data) < 8 {
		return fmt.Errorf("bitmap: gob payload too short: %d bytes", len(data))
	}
	decoded, err := FromBytes(binary.BigEndian.Uint64(data[:8]), data[8:])
	if err != nil {
		return err
	}
	*b = decoded
	return nil
}

// FromBytes reconstructs a bitmap of length n from raw bytes exactly as
// serialized by Bytes. It rejects a byte slice of the wrong length and any
// byte slice whose final byte has stray high bits set (both are cases where
// the input cannot have come from a well-formed Set sequence, so a
// malicious/corrupt bitmap must be rejected rather than silently accepted
// (R-GO-026): callers verifying a proof should treat an error here as a
// verification failure, i.e. reject, not a panic.
func FromBytes(n uint64, raw []byte) (Bitmap, error) {
	want := byteLen(n)
	if uint64(len(raw)) != want {
		return Bitmap{}, fmt.Errorf("bitmap: length mismatch: got %d bytes, want %d for N=%d", len(raw), want, n)
	}
	if want > 0 {
		usedBitsInLast := n % 8
		if usedBitsInLast != 0 {
			mask := byte(0xFF << usedBitsInLast)
			if raw[want-1]&mask != 0 {
				return Bitmap{}, fmt.Errorf("bitmap: stray high bits set in final byte for N=%d", n)
			}
		}
	}
	b := Bitmap{n: n, bytes: make([]byte, want)}
	copy(b.bytes, raw)
	return b, nil
}

// Grow returns a new bitmap of length newN (>= the current N) with all
// existing bits preserved and every new bit initialized to 0. It reports,
// via the second return value, whether the *serialized byte length* changed
// (i.e. ceil(newN/8) != ceil(N/8)) — the trigger condition for deviation D-1
// in DECISIONS.md: crossing a byte boundary changes every leaf's serialized
// bytes, not just the leaf whose bit was set, so callers must recompute
// every leaf hash when this returns true, even for leaves whose bits did
// not change. Panics if newN < the current N.
func (b Bitmap) Grow(newN uint64) (Bitmap, bool) {
	if newN < b.n {
		panic(fmt.Sprintf("bitmap: Grow(%d) is smaller than current N=%d", newN, b.n))
	}
	oldByteLen := byteLen(b.n)
	newByteLen := byteLen(newN)
	out := Bitmap{n: newN, bytes: make([]byte, newByteLen)}
	copy(out.bytes, b.bytes)
	return out, newByteLen != oldByteLen
}

// Clone returns an independent copy.
func (b Bitmap) Clone() Bitmap {
	c := Bitmap{n: b.n, bytes: make([]byte, len(b.bytes))}
	copy(c.bytes, b.bytes)
	return c
}

// Equal reports whether two bitmaps have the same N and the same bits.
func (b Bitmap) Equal(o Bitmap) bool {
	if b.n != o.n || len(b.bytes) != len(o.bytes) {
		return false
	}
	for i := range b.bytes {
		if b.bytes[i] != o.bytes[i] {
			return false
		}
	}
	return true
}

func requireSameLen(a, b Bitmap) {
	if a.n != b.n {
		panic(fmt.Sprintf("bitmap: length mismatch in bitwise op: %d vs %d", a.n, b.n))
	}
}

// And returns the bitwise AND of a and b. Both must share the same N.
func And(a, b Bitmap) Bitmap {
	requireSameLen(a, b)
	out := New(a.n)
	for i := range out.bytes {
		out.bytes[i] = a.bytes[i] & b.bytes[i]
	}
	return out
}

// Or returns the bitwise OR of a and b. Both must share the same N.
func Or(a, b Bitmap) Bitmap {
	requireSameLen(a, b)
	out := New(a.n)
	for i := range out.bytes {
		out.bytes[i] = a.bytes[i] | b.bytes[i]
	}
	return out
}

// Not returns the bitwise complement of a over its authenticated length N
// (R-IOMF-024). N must already be known/authenticated by the caller before
// calling Not (R-GO-016, GAP-50) — this function does not itself authenticate
// anything, it only guarantees the unused high bits of the final byte stay
// zero after complementing, so the result remains a canonical Bitmap.
func Not(a Bitmap) Bitmap {
	out := New(a.n)
	for i := range out.bytes {
		out.bytes[i] = ^a.bytes[i]
	}
	out.clearUnusedTail()
	return out
}

func (b Bitmap) clearUnusedTail() {
	if len(b.bytes) == 0 {
		return
	}
	usedBitsInLast := b.n % 8
	if usedBitsInLast == 0 {
		return
	}
	mask := byte(0xFF >> (8 - usedBitsInLast))
	b.bytes[len(b.bytes)-1] &= mask
}

// SetIndices returns the sorted (ascending) list of set-bit indices — the
// deterministic form of a Boolean/range query result set {i | bm[i]=1}
// (R-QUERY-025: results are sets; ordering here is only for deterministic
// emission, never relied on as onbehalf of set semantics elsewhere).
func (b Bitmap) SetIndices() []uint64 {
	var out []uint64
	for i := uint64(0); i < b.n; i++ {
		if b.Get(i) {
			out = append(out, i)
		}
	}
	return out
}
