package spec

import (
	"bytes"
	"testing"

	"delia/globalstate"
	"delia/hash"
	"delia/localstate"
	"delia/merkle"
)

// --- Frame codec vectors -----------------------------------------------

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	payload := []byte{0xAA, 0xBB, 0xCC}
	if err := WriteFrame(&buf, MsgRootState, payload); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	gotType, gotPayload, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if gotType != MsgRootState {
		t.Fatalf("type = %v, want %v", gotType, MsgRootState)
	}
	if !bytes.Equal(gotPayload, payload) {
		t.Fatalf("payload = %x, want %x", gotPayload, payload)
	}
}

func TestFrameLayoutBytes(t *testing.T) {
	// Explicit byte-level check of the GAP-47/73 layout: uint32 length (BE)
	// || uint8 type || payload. length = 1 (type byte) + len(payload).
	var buf bytes.Buffer
	payload := []byte{0x01, 0x02}
	if err := WriteFrame(&buf, MsgFailureNotification, payload); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	got := buf.Bytes()
	want := []byte{0x00, 0x00, 0x00, 0x03, byte(MsgFailureNotification), 0x01, 0x02}
	if !bytes.Equal(got, want) {
		t.Fatalf("frame bytes = %x, want %x", got, want)
	}
}

func TestFrameEmptyPayload(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, MsgRootState, nil); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	got := buf.Bytes()
	want := []byte{0x00, 0x00, 0x00, 0x01, byte(MsgRootState)}
	if !bytes.Equal(got, want) {
		t.Fatalf("frame bytes = %x, want %x", got, want)
	}
	gotType, gotPayload, err := ReadFrame(bytes.NewReader(got))
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if gotType != MsgRootState || len(gotPayload) != 0 {
		t.Fatalf("round trip mismatch: %v %x", gotType, gotPayload)
	}
}

func TestFrameTruncatedInput(t *testing.T) {
	if _, _, err := ReadFrame(bytes.NewReader([]byte{0x00, 0x00})); err == nil {
		t.Fatalf("expected error on truncated length prefix")
	}
	if _, _, err := ReadFrame(bytes.NewReader([]byte{0x00, 0x00, 0x00, 0x05, 0x01})); err == nil {
		t.Fatalf("expected error when declared length exceeds available bytes")
	}
}

// --- Signed-bytes layout -------------------------------------------------

func TestSignedBytesLayout(t *testing.T) {
	obj := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	got := SignedBytes(PurposeSTATE, SignerRole(0x00), obj)
	want := []byte{byte(PurposeSTATE), 0x00, 0xDE, 0xAD, 0xBE, 0xEF}
	if !bytes.Equal(got, want) {
		t.Fatalf("SignedBytes = %x, want %x", got, want)
	}
}

func TestSignedBytesDistinguishesPurpose(t *testing.T) {
	obj := []byte{0x01, 0x02}
	a := SignedDigest(PurposeSTATE, SignerRole(0), obj)
	b := SignedDigest(PurposeCLOSE, SignerRole(0), obj)
	if a == b {
		t.Fatalf("STATE- and CLOSE-purpose digests over the same object must differ (TEST-49 basis)")
	}
}

func TestPurposeCodesAreDistinct(t *testing.T) {
	codes := []Purpose{PurposeSTATE, PurposeCLOSE, PurposeRECOVERY, PurposeVOTE}
	seen := map[Purpose]bool{}
	for _, c := range codes {
		if seen[c] {
			t.Fatalf("duplicate purpose code %v", c)
		}
		seen[c] = true
	}
}

// --- Cross-package normative vectors (the "one place" test-vector file) --

// This is the M0-mandated normative vector: fix a small psi, walk it through
// MHTGen -> LocalState -> GlobalState encoding, and pin the exact bytes.
// Any future change to any of these packages that silently changes the wire
// format will break this test.
func TestNormativeVectorLocalStateAndGlobalStateEncoding(t *testing.T) {
	l1 := hash.H([]byte("record-1"))
	l2 := hash.H([]byte("record-2"))
	l3 := hash.H([]byte("record-3"))

	root := merkle.MHTGen([]hash.Digest{l1, l2, l3})
	h12 := hash.H(hash.Concat(l1[:], l2[:]))
	h33 := hash.H(hash.Concat(l3[:], l3[:]))
	wantRoot := hash.H(hash.Concat(h12[:], h33[:]))
	if root != wantRoot {
		t.Fatalf("MHTGen vector mismatch: %x != %x", root, wantRoot)
	}

	omega0 := localstate.Compute(root, nil)
	wantOmega0 := hash.H(root[:])
	if omega0 != wantOmega0 {
		t.Fatalf("LocalState li=0 vector mismatch")
	}

	g0 := globalstate.Zero(3)
	g1, err := globalstate.GlobalGen(root, g0, 0)
	if err != nil {
		t.Fatalf("GlobalGen: %v", err)
	}
	if g1.Nu != 1 {
		t.Fatalf("g1.Nu = %d, want 1", g1.Nu)
	}
	if g1.Entries[0] != omega0 {
		t.Fatalf("g1.Entries[0] != omega0")
	}

	enc := g1.Encode()
	if len(enc) != 8+4+3*hash.Size {
		t.Fatalf("encoded length = %d, want %d", len(enc), 8+4+3*hash.Size)
	}
	// nu = 1, entry_count = 3, big-endian header.
	wantHeader := []byte{0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 3}
	if !bytes.Equal(enc[:12], wantHeader) {
		t.Fatalf("encoded header = %x, want %x", enc[:12], wantHeader)
	}
}
