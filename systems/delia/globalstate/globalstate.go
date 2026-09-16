// Package globalstate implements GlobalState (Eq. (3), DATA-013..018),
// Algorithm 1 GlobalGen (ALG1-001..006) and Algorithm 2 StateVerify
// (ALG2-001..007).
//
// GlobalState is domain-wide (GAP-10): one G_nu array covers every server in
// the domain, at a fixed position per server frozen by the P order at Init
// (GAP-08). G_0 is n all-zero 32-byte sentinel entries with nu = 0 and is
// never submitted on-chain (GAP-06); a server's slot stays the zero sentinel
// until its first completed batch (GAP-75).
package globalstate

import (
	"encoding/binary"
	"errors"

	"delia/hash"
)

// GlobalState is G_nu: the serial number nu and one LocalState entry per
// server, in the P order frozen at Init.
type GlobalState struct {
	Nu      uint64
	Entries []hash.Digest
}

// Zero returns G_0 for a domain of n servers: n all-zero sentinel entries,
// nu = 0 (GAP-06). G_0 is never submitted on-chain.
func Zero(n int) GlobalState {
	return GlobalState{Nu: 0, Entries: make([]hash.Digest, n)}
}

// Encode returns the canonical wire/disk encoding of g, frozen by GAP-73:
//
//	uint64 nu (big-endian) || uint32 entry_count (big-endian) || entry_count x 32-byte LocalState
//
// Raw bytes, never hex. This is the encoding compared for Alg. 2 equality
// (GAP-09: nu is part of the comparison; sid is not) and the encoded object
// wrapped by the signed-bytes layout (purpose || signer_role || encoding).
func (g GlobalState) Encode() []byte {
	out := make([]byte, 8+4+len(g.Entries)*hash.Size)
	binary.BigEndian.PutUint64(out[0:8], g.Nu)
	binary.BigEndian.PutUint32(out[8:12], uint32(len(g.Entries)))
	off := 12
	for _, e := range g.Entries {
		copy(out[off:off+hash.Size], e[:])
		off += hash.Size
	}
	return out
}

// Decode parses the GAP-73 GlobalState layout produced by Encode.
func Decode(b []byte) (GlobalState, error) {
	if len(b) < 12 {
		return GlobalState{}, errors.New("globalstate: buffer too short for header")
	}
	nu := binary.BigEndian.Uint64(b[0:8])
	count := binary.BigEndian.Uint32(b[8:12])
	want := 12 + int(count)*hash.Size
	if len(b) != want {
		return GlobalState{}, errors.New("globalstate: buffer length does not match entry_count")
	}
	entries := make([]hash.Digest, count)
	off := 12
	for i := range entries {
		copy(entries[i][:], b[off:off+hash.Size])
		off += hash.Size
	}
	return GlobalState{Nu: nu, Entries: entries}, nil
}

// Equal compares the canonical encodings of g and other (GAP-09(a)): nu is
// part of the comparison, sid is not (sid is never part of a GlobalState).
func (g GlobalState) Equal(other GlobalState) bool {
	ge, oe := g.Encode(), other.Encode()
	if len(ge) != len(oe) {
		return false
	}
	for i := range ge {
		if ge[i] != oe[i] {
			return false
		}
	}
	return true
}

// Clone returns a deep copy of g, so callers can mutate the result without
// aliasing the original entries slice.
func (g GlobalState) Clone() GlobalState {
	entries := make([]hash.Digest, len(g.Entries))
	copy(entries, g.Entries)
	return GlobalState{Nu: g.Nu, Entries: entries}
}
