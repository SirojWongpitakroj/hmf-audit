package messages

import (
	"bytes"
	"testing"

	"delia/globalstate"
	"delia/hash"
	"delia/internal/spec"
)

func TestRootStateEncodeDecode(t *testing.T) {
	g := globalstate.GlobalState{Nu: 5, Entries: []hash.Digest{hash.H([]byte("a")), hash.H([]byte("b"))}}
	root := hash.H([]byte("root"))
	m := RootState{Root: root, State: g}

	enc := m.Encode()
	got, err := DecodeRootState(enc)
	if err != nil {
		t.Fatalf("DecodeRootState: %v", err)
	}
	if got.Root != root {
		t.Fatalf("root mismatch")
	}
	if !got.State.Equal(g) {
		t.Fatalf("state mismatch")
	}
}

func TestRootStateOverFrame(t *testing.T) {
	g := globalstate.GlobalState{Nu: 1, Entries: []hash.Digest{hash.H([]byte("x"))}}
	m := RootState{Root: hash.H([]byte("r")), State: g}

	var buf bytes.Buffer
	if err := spec.WriteFrame(&buf, TypeRootState, m.Encode()); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	typ, payload, err := spec.ReadFrame(&buf)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if typ != TypeRootState {
		t.Fatalf("type mismatch")
	}
	got, err := DecodeRootState(payload)
	if err != nil {
		t.Fatalf("DecodeRootState: %v", err)
	}
	if !got.State.Equal(g) {
		t.Fatalf("round trip through frame mismatched")
	}
}

func TestFailureNotificationEncodeDecode(t *testing.T) {
	m := FailureNotification{Sender: 2, Proposer: 0, Nu: 7, Reason: "root mismatch"}
	enc := m.Encode()
	got, err := DecodeFailureNotification(enc)
	if err != nil {
		t.Fatalf("DecodeFailureNotification: %v", err)
	}
	if got != m {
		t.Fatalf("round trip mismatch: got %+v, want %+v", got, m)
	}
}

func TestFailureNotificationEmptyReason(t *testing.T) {
	m := FailureNotification{Sender: 1, Proposer: 1, Nu: 0, Reason: ""}
	enc := m.Encode()
	got, err := DecodeFailureNotification(enc)
	if err != nil {
		t.Fatalf("DecodeFailureNotification: %v", err)
	}
	if got != m {
		t.Fatalf("round trip mismatch with empty reason")
	}
}

func TestDecodeRootStateRejectsShortInput(t *testing.T) {
	if _, err := DecodeRootState([]byte{0x01, 0x02}); err == nil {
		t.Fatalf("expected error for short RootState payload")
	}
}

func TestDecodeFailureNotificationRejectsShortInput(t *testing.T) {
	if _, err := DecodeFailureNotification([]byte{0x01}); err == nil {
		t.Fatalf("expected error for short FailureNotification payload")
	}
}

// NET-006/CONFIRM-007: StateToLeader is a plain, unsigned send of a
// participant's generated G_nu to its leader.
func TestStateToLeaderEncodeDecode(t *testing.T) {
	g := globalstate.GlobalState{Nu: 3, Entries: []hash.Digest{hash.H([]byte("p"))}}
	m := StateToLeader{State: g}

	enc := m.Encode()
	got, err := DecodeStateToLeader(enc)
	if err != nil {
		t.Fatalf("DecodeStateToLeader: %v", err)
	}
	if !got.State.Equal(g) {
		t.Fatalf("state mismatch")
	}
}

func TestStateToLeaderOverFrame(t *testing.T) {
	g := globalstate.GlobalState{Nu: 1, Entries: []hash.Digest{hash.H([]byte("q"))}}
	m := StateToLeader{State: g}

	var buf bytes.Buffer
	if err := spec.WriteFrame(&buf, TypeStateToLeader, m.Encode()); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	typ, payload, err := spec.ReadFrame(&buf)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if typ != TypeStateToLeader {
		t.Fatalf("type mismatch")
	}
	got, err := DecodeStateToLeader(payload)
	if err != nil {
		t.Fatalf("DecodeStateToLeader: %v", err)
	}
	if !got.State.Equal(g) {
		t.Fatalf("round trip through frame mismatched")
	}
}

func TestDecodeStateToLeaderRejectsShortInput(t *testing.T) {
	if _, err := DecodeStateToLeader([]byte{0x01, 0x02}); err == nil {
		t.Fatalf("expected error for short StateToLeader payload")
	}
}
