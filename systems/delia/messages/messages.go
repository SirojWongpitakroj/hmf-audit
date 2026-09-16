// Package messages defines the Go structs for the off-chain message
// catalogue (NET-003..NET-032) that Tier-A milestones need, and their wire
// encode/decode using the frame format frozen in internal/spec (GAP-47,
// GAP-73). Fields not stated by the paper are marked IMPL with their GAP
// reference; this package adds no field the paper does not need and no gap
// does not license (SRS Section 21 package table: "fields not in the paper
// unless marked IMPL with GAP ref").
package messages

import (
	"encoding/binary"
	"errors"

	"delia/globalstate"
	"delia/hash"
	"delia/internal/spec"
)

// RootState is NET-004, the LSG stage 2 "root and state broadcast"
// (LSG-007): pi -> other servers in the domain, (root_{pi,li}, G_nu).
type RootState struct {
	Root  hash.Digest
	State globalstate.GlobalState
}

// Encode serializes RootState as root (32 bytes) || GlobalState.Encode().
func (m RootState) Encode() []byte {
	return hash.Concat(m.Root[:], m.State.Encode())
}

// DecodeRootState parses the RootState payload produced by Encode.
func DecodeRootState(b []byte) (RootState, error) {
	if len(b) < hash.Size {
		return RootState{}, errors.New("messages: RootState payload too short")
	}
	var root hash.Digest
	copy(root[:], b[:hash.Size])
	state, err := globalstate.Decode(b[hash.Size:])
	if err != nil {
		return RootState{}, err
	}
	return RootState{Root: root, State: state}, nil
}

// FailureNotification is NET-005, LSG stage 2's failure notification
// (LSG-009, GAP-12(a)): pj -> servers in the domain, (sender, pi, nu,
// reason), broadcast when pj's Alg. 2 call returns invalid. sender and
// proposer are positions in the P order frozen at Init (the same indexing
// GAP-08 uses elsewhere); reason is a short free-form UTF-8 string.
type FailureNotification struct {
	Sender   uint32
	Proposer uint32
	Nu       uint64
	Reason   string
}

// Encode serializes FailureNotification as sender(4,BE) || proposer(4,BE) ||
// nu(8,BE) || reason_len(2,BE) || reason bytes.
func (m FailureNotification) Encode() []byte {
	reason := []byte(m.Reason)
	out := make([]byte, 4+4+8+2+len(reason))
	binary.BigEndian.PutUint32(out[0:4], m.Sender)
	binary.BigEndian.PutUint32(out[4:8], m.Proposer)
	binary.BigEndian.PutUint64(out[8:16], m.Nu)
	binary.BigEndian.PutUint16(out[16:18], uint16(len(reason)))
	copy(out[18:], reason)
	return out
}

// DecodeFailureNotification parses the FailureNotification payload produced
// by Encode.
func DecodeFailureNotification(b []byte) (FailureNotification, error) {
	if len(b) < 18 {
		return FailureNotification{}, errors.New("messages: FailureNotification payload too short")
	}
	sender := binary.BigEndian.Uint32(b[0:4])
	proposer := binary.BigEndian.Uint32(b[4:8])
	nu := binary.BigEndian.Uint64(b[8:16])
	rl := binary.BigEndian.Uint16(b[16:18])
	if len(b) != 18+int(rl) {
		return FailureNotification{}, errors.New("messages: FailureNotification reason length mismatch")
	}
	return FailureNotification{
		Sender:   sender,
		Proposer: proposer,
		Nu:       nu,
		Reason:   string(b[18 : 18+int(rl)]),
	}, nil
}

// Type re-exports the frozen frame message_type enumeration (GAP-47/73).
type Type = spec.MessageType

// Frozen message_type codes used by the Tier-A messages in this package.
const (
	TypeRootState           = spec.MsgRootState
	TypeFailureNotification = spec.MsgFailureNotification
)
