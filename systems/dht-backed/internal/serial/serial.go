// Package serial implements the deterministic binary serialization rules of
// Q12: uint64 as 8-byte big-endian; hashes as raw 32 bytes; strings and
// variable-length byte arrays as a 4-byte big-endian length followed by the
// bytes; fixed field order; a one-byte record-type/version prefix.
//
// Go maps are never serialized by this package or by any of its callers
// (Q12, R-EVAL-08).
package serial

import (
	"encoding/binary"
	"errors"

	"dhtbacked/internal/hashing"
)

// Record-type/version prefixes. Exactly one byte each (Q12).
const (
	TypeParentNode  byte = 0x01
	TypeAncestorCer byte = 0x02
	TypeRootRecord  byte = 0x03
	TypeMicroRoot   byte = 0x04
	TypeTokenBody   byte = 0x05
)

// Writer accumulates deterministically encoded bytes in a fixed field order.
type Writer struct {
	buf []byte
}

// NewWriter starts a record with its one-byte record-type/version prefix.
func NewWriter(recordType byte) *Writer {
	return &Writer{buf: []byte{recordType}}
}

// NewRawWriter starts a byte string with no record-type prefix. Used for
// values the SRS defines without a prefix, such as AD (Q7).
func NewRawWriter() *Writer { return &Writer{} }

// Uint64 appends v as an 8-byte big-endian unsigned integer.
func (w *Writer) Uint64(v uint64) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	w.buf = append(w.buf, b[:]...)
}

// Digest appends a hash as raw 32 bytes.
func (w *Writer) Digest(d hashing.Digest) { w.buf = append(w.buf, d[:]...) }

// Bytes appends a variable-length byte array as a 4-byte big-endian length
// followed by the bytes.
func (w *Writer) Bytes(b []byte) {
	var l [4]byte
	binary.BigEndian.PutUint32(l[:], uint32(len(b)))
	w.buf = append(w.buf, l[:]...)
	w.buf = append(w.buf, b...)
}

// String appends a UTF-8 string as a 4-byte big-endian length followed by the
// bytes.
func (w *Writer) String(s string) { w.Bytes([]byte(s)) }

// Raw appends bytes with no framing. Only used where the surrounding record
// already fixes the field width.
func (w *Writer) Raw(b []byte) { w.buf = append(w.buf, b...) }

// Result returns the encoded bytes.
func (w *Writer) Result() []byte { return w.buf }

// ErrMalformed reports a byte string that does not decode under these rules.
var ErrMalformed = errors.New("serial: malformed encoding")

// Reader decodes bytes produced by Writer, in the same fixed field order.
type Reader struct {
	buf []byte
	pos int
}

// NewReader checks and consumes the one-byte record-type prefix.
func NewReader(b []byte, recordType byte) (*Reader, error) {
	if len(b) < 1 || b[0] != recordType {
		return nil, ErrMalformed
	}
	return &Reader{buf: b, pos: 1}, nil
}

// NewRawReader reads a byte string that carries no record-type prefix.
func NewRawReader(b []byte) *Reader { return &Reader{buf: b} }

// Uint64 reads an 8-byte big-endian unsigned integer.
func (r *Reader) Uint64() (uint64, error) {
	if r.pos+8 > len(r.buf) {
		return 0, ErrMalformed
	}
	v := binary.BigEndian.Uint64(r.buf[r.pos : r.pos+8])
	r.pos += 8
	return v, nil
}

// Digest reads a raw 32-byte hash.
func (r *Reader) Digest() (hashing.Digest, error) {
	var d hashing.Digest
	if r.pos+hashing.Size > len(r.buf) {
		return d, ErrMalformed
	}
	copy(d[:], r.buf[r.pos:r.pos+hashing.Size])
	r.pos += hashing.Size
	return d, nil
}

// Bytes reads a 4-byte big-endian length followed by that many bytes.
func (r *Reader) Bytes() ([]byte, error) {
	if r.pos+4 > len(r.buf) {
		return nil, ErrMalformed
	}
	n := int(binary.BigEndian.Uint32(r.buf[r.pos : r.pos+4]))
	r.pos += 4
	if n < 0 || r.pos+n > len(r.buf) {
		return nil, ErrMalformed
	}
	b := make([]byte, n)
	copy(b, r.buf[r.pos:r.pos+n])
	r.pos += n
	return b, nil
}

// String reads a length-prefixed UTF-8 string.
func (r *Reader) String() (string, error) {
	b, err := r.Bytes()
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// End reports ErrMalformed if any bytes remain unconsumed.
func (r *Reader) End() error {
	if r.pos != len(r.buf) {
		return ErrMalformed
	}
	return nil
}
