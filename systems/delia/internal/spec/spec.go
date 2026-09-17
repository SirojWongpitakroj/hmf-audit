// Package spec is the normative test-vector / constants file called for by
// M0 of TRACKER.md, implementing the GAP-73 delegation.
//
// Authority note: every value in this package is an IMPLEMENTER-FROZEN
// choice under GAP-73 ("some values the user required to be fixed without
// enumerating them are treated as delegated to the implementer and frozen in
// one normative test-vector file"), not something the DELIA paper states.
// GAP-73 itself is DECIDED with a residual (GAP-86: the complete signer-role
// set, the purposes used by offline/cancel/online/no-signed signatures, and
// the VOTE payload remain OPEN). This file freezes only the values a Tier-A
// requirement actually needs today; it is extended, never silently
// redefined, as later milestones need more of the enumeration.
//
// Frozen here:
//   - Purpose: the one-byte purpose codes for the signed-bytes layout
//     (GAP-72/GAP-73): STATE, CLOSE, RECOVERY, VOTE.
//   - MessageType: the one-byte off-chain frame message_type codes
//     (GAP-47/GAP-73). Only codes an implemented Tier-A message needs are
//     assigned; the rest of the NET-003..NET-032 catalogue is intentionally
//     left unassigned rather than guessed.
//   - The frame codec: uint32 frame_length (big-endian, bytes following) ||
//     uint8 message_type || message-specific fields (GAP-47/GAP-73). See
//     Frame/WriteFrame/ReadFrame.
//   - The signed-bytes layout: purpose || signer_role || canonical-encoded
//     object (GAP-72/GAP-73). See SignedBytes. The signer_role enumeration
//     itself is left open (GAP-86 residual) — SignedBytes accepts a raw
//     SignerRole byte so callers are not blocked, but no concrete role
//     values are frozen here because no Tier-A call site needs one yet.
//
// The canonical secp256k1 public-key encoding (used both for signing context
// and the CONFIRM-003 big-endian key comparison) is frozen in the sig
// package (sig.PublicKeySize, sig.KeyPair.PublicKey): the compressed SEC1
// point encoding, 33 bytes. Because that encoding is already a big-endian
// byte sequence, "compare keys as fixed-width unsigned big-endian values"
// means ordinary lexicographic byte comparison (bytes.Compare) of the
// encoded keys.
//
// The GlobalState wire/disk layout is frozen in the globalstate package
// (globalstate.GlobalState.Encode/Decode): uint64 nu (BE) || uint32
// entry_count (BE) || entry_count x 32-byte LocalState, in the P order
// frozen at Init.
//
// The record hash rule is frozen in the logcache package (logcache.RecordHash):
// SHA-256 over the UTF-8 log line, excluding line-ending bytes.
package spec

import "delia/hash"

// Purpose is the one-byte purpose tag bound into a signed object's bytes
// (GAP-72/GAP-73), distinguishing what a signature attests to.
type Purpose byte

// Frozen purpose codes (GAP-73 delegation). Values are otherwise arbitrary
// (the paper does not name them); they only need to be distinct and stable.
const (
	PurposeSTATE    Purpose = 0x01 // (G_nu | p) signed round state (§4.3.2)
	PurposeCLOSE    Purpose = 0x02 // (G_nu | Close) (§4.3.6)
	PurposeRECOVERY Purpose = 0x03 // Sign(G_{nu+1} | NewLeader_x) (§4.3.4)
	PurposeVOTE     Purpose = 0x04 // signed voting message (§4.3.4)
)

// SignerRole is the one-byte signer-role tag bound into a signed object's
// bytes (GAP-72/GAP-73). The complete enumeration is OPEN (GAP-86 residual);
// concrete values are frozen here only as a Tier-A call site actually needs
// one. Future milestones add named constants here as they need them, never
// redefine an already-frozen value.
type SignerRole byte

// RoleSigner is the one-byte signer-role tag used by every M6/M7 Tier-A
// signature this reproduction verifies so far: a participant's signature on
// a signed round state submitted to Update (purpose STATE; UPDATE-007,
// TEST-23/24/26/29/36), and a participant's signature on a Close/Disassociate
// state (purpose CLOSE; CLOSE-005/007, TEST-38/39/40/44). It deliberately
// does NOT distinguish leader from participant — that distinction
// (UPDATE-008's leader-presence sub-check, CONFIRM-008/009's leader-signs-
// then-participants-countersign flow) is Tier B/blocked (GAP-86 residual)
// and is not built by any Tier-A item. Do not reuse this constant for a
// signing point that DOES need a leader/participant distinction; freeze a
// new, separately-justified role for that when it actually unblocks.
const RoleSigner SignerRole = 0x01

// MessageType is the one-byte off-chain frame message-type code (GAP-47,
// GAP-73). Only codes needed by an implemented Tier-A message are assigned.
type MessageType byte

const (
	// MsgRootState carries NET-004: (root_{pi,li}, G_nu), the LSG stage 2
	// root-and-state broadcast (LSG-007).
	MsgRootState MessageType = 0x01
	// MsgFailureNotification carries NET-005: (sender, pi, nu, reason), the
	// LSG stage 2 failure notification (LSG-009, GAP-12(a)).
	MsgFailureNotification MessageType = 0x02
)

// SignedBytes builds the digest input for a signature under the GAP-72/73
// layout: purpose || signer_role || encoded_object. sid is deliberately not
// included (GAP-09; cross-instance/cross-purpose replay is a documented
// limitation, LIM-02, not something this layout is redesigned to prevent).
// The result is hashed by the caller with hash.H before signing/verifying
// (CRYPTO-002: SHA-256 is the sole hash, used for the signing digest too).
func SignedBytes(purpose Purpose, role SignerRole, encodedObject []byte) []byte {
	return hash.Concat([]byte{byte(purpose)}, []byte{byte(role)}, encodedObject)
}

// SignedDigest is SignedBytes followed by H(.), the actual bytes handed to
// sig.KeyPair.Sign / sig.Vrfy.
func SignedDigest(purpose Purpose, role SignerRole, encodedObject []byte) hash.Digest {
	return hash.H(SignedBytes(purpose, role, encodedObject))
}
