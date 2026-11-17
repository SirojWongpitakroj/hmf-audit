package encryptor_test

// Independent verification of Phase 1 (M2).
//
// Every check in this file recomputes the package's output from first
// principles using only the Go standard library. It never calls the package's
// own helper for the quantity under test — LeafDigest, AttestationPreImage,
// VerifyAttestation, ParseAD — so a defect shared by an implementation and the
// tests written alongside it cannot hide here.
//
// Decryption appears below only to prove the encryption is AES-256-GCM with the
// stated framing. It is a test of Phase 1, not an audit path; R-VER-09 (audit
// does not decrypt) is verified separately in the verifier package.

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"dhtbacked/encryptor"
)

func independentFixture(t *testing.T) (encryptor.Record, []byte, ed25519.PrivateKey, encryptor.Emitted) {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	_, sk, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rec := encryptor.Record{
		RecordID:       encryptor.FormatRecordID(7),
		ProducerID:     "producer-access",
		LogType:        "access",
		TimestampNanos: time.Date(2026, 9, 11, 4, 30, 0, 500_000_000, time.UTC).UnixNano(),
		Message:        "access event",
		Attributes: encryptor.Attributes{
			Host: "h1", SourceIP: "10.0.0.1", Status: "200", RequestID: "r1", Padding: "",
		},
	}
	enc, err := encryptor.New(key)
	if err != nil {
		t.Fatal(err)
	}
	em, err := enc.ProcessRecord(rec, sk)
	if err != nil {
		t.Fatal(err)
	}
	return rec, key, sk, em
}

// Q1: AES-256-GCM, 12-byte nonce, 16-byte tag, and c = nonce || ciphertext.
// Proven by decrypting with the standard library alone: if any of the four
// facts were false, Open would fail or return the wrong plaintext.
func TestIndependentAES256GCMFraming(t *testing.T) {
	rec, key, _, em := independentFixture(t)

	if len(key) != 32 {
		t.Fatalf("key is %d bytes; AES-256 needs 32", len(key))
	}
	if len(em.T) != 16 {
		t.Fatalf("tag t is %d bytes, want 16 (Q1)", len(em.T))
	}
	const nonceLen = 12
	if len(em.C) <= nonceLen {
		t.Fatalf("c is %d bytes; too short to hold a 12-byte nonce prefix", len(em.C))
	}
	nonce, ciphertext := em.C[:nonceLen], em.C[nonceLen:]

	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, nonceLen)
	if err != nil {
		t.Fatal(err)
	}
	if gcm.Overhead() != 16 {
		t.Fatalf("GCM overhead %d, want 16", gcm.Overhead())
	}
	// Go's GCM expects ciphertext || tag; the system stores them separately.
	sealed := append(append([]byte(nil), ciphertext...), em.T...)
	plaintext, err := gcm.Open(nil, nonce, sealed, em.AD)
	if err != nil {
		t.Fatalf("stdlib AES-256-GCM could not open (c, t) under AD: %v", err)
	}
	if want := encryptor.CanonicalBytes(rec); !bytes.Equal(plaintext, want) {
		t.Fatalf("decrypted plaintext is not the canonical record M:\n got  %s\n want %s", plaintext, want)
	}
}

// Q7: the identical AD bytes are the AEAD associated data. Proven by
// tampering: opening under any other AD must fail the GCM tag check.
func TestIndependentADIsTheAEADAssociatedData(t *testing.T) {
	_, key, _, em := independentFixture(t)
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCMWithNonceSize(block, 12)
	sealed := append(append([]byte(nil), em.C[12:]...), em.T...)

	tampered := append([]byte(nil), em.AD...)
	tampered[len(tampered)-1] ^= 0x01
	if _, err := gcm.Open(nil, em.C[:12], sealed, tampered); err == nil {
		t.Fatal("GCM opened under a different AD; AD is not bound as associated data")
	}
}

// AD layout, parsed independently of serial/ParseAD: producerID, logType,
// timestamp, then the authenticated record ID.
//
// The length prefix is a 4-byte unsigned big-endian integer, fixed by the
// user's GAP-3 decision (2026-09-11): Q7 says "length-prefixed" but names no
// width. This test pins that decision.
func TestIndependentADLayout(t *testing.T) {
	rec, _, _, em := independentFixture(t)
	ad := em.AD

	readStr := func(name string) string {
		if len(ad) < 4 {
			t.Fatalf("AD truncated before %s length", name)
		}
		n := binary.BigEndian.Uint32(ad[:4])
		ad = ad[4:]
		if uint32(len(ad)) < n {
			t.Fatalf("AD truncated inside %s", name)
		}
		s := string(ad[:n])
		ad = ad[n:]
		return s
	}
	if got := readStr("producerID"); got != rec.ProducerID {
		t.Fatalf("AD field 1 = %q, want producerID %q", got, rec.ProducerID)
	}
	if got := readStr("logType"); got != rec.LogType {
		t.Fatalf("AD field 2 = %q, want logType %q", got, rec.LogType)
	}
	if len(ad) < 8 {
		t.Fatalf("AD tail is %d bytes, want an 8-byte timestamp and record ID", len(ad))
	}
	if got := binary.BigEndian.Uint64(ad[:8]); got != uint64(rec.TimestampNanos) {
		t.Fatalf("AD timestamp = %d, want Unix ns %d", got, rec.TimestampNanos)
	}
	ad = ad[8:]
	if got := readStr("recordID"); got != rec.RecordID || len(ad) != 0 {
		t.Fatalf("AD record ID = %q, want %q, trailing bytes=%d", got, rec.RecordID, len(ad))
	}
}

// Eq. (15): d = H(0x00 || c || t || AD), recomputed with crypto/sha256.
func TestIndependentLeafDigest(t *testing.T) {
	_, _, _, em := independentFixture(t)
	h := sha256.New()
	h.Write([]byte{0x00})
	h.Write(em.C)
	h.Write(em.T)
	h.Write(em.AD)
	if want := h.Sum(nil); !bytes.Equal(em.D[:], want) {
		t.Fatalf("leaf digest mismatch:\n got  %x\n want %x", em.D, want)
	}
}

// Eq. (16): m_P = SHA256(0xA3 || d), then sigma = Ed25519.Sign(sk_P, m_P).
// Verified with crypto/ed25519 over an independently computed m_P.
func TestIndependentAttestation(t *testing.T) {
	_, _, sk, em := independentFixture(t)
	pre := sha256.Sum256(append([]byte{0xA3}, em.D[:]...))

	pk := sk.Public().(ed25519.PublicKey)
	if !bytes.Equal(em.ProducerPublicKey, pk) {
		t.Fatal("emitted public key is not the signer's key")
	}
	if len(em.Sigma) != ed25519.SignatureSize {
		t.Fatalf("sigma is %d bytes, want 64", len(em.Sigma))
	}
	if !ed25519.Verify(pk, pre[:], em.Sigma) {
		t.Fatal("sigma does not verify over SHA256(0xA3 || d)")
	}
	// It must be a signature over the pre-hash, not over d directly.
	if ed25519.Verify(pk, em.D[:], em.Sigma) {
		t.Fatal("sigma verifies over raw d; Eq. (16) requires the 0xA3 pre-image")
	}
}

// Q6/Q48/Q50/Q51: the canonical plaintext M has exactly the six fields, the
// five flat attribute keys, a 12-digit record_id and a 30-character timestamp.
// Parsed with encoding/json, independently of the package's canonicalizer.
func TestIndependentCanonicalSchema(t *testing.T) {
	rec, _, _, _ := independentFixture(t)
	m := encryptor.CanonicalBytes(rec)

	var top map[string]json.RawMessage
	if err := json.Unmarshal(m, &top); err != nil {
		t.Fatalf("M is not JSON: %v", err)
	}
	wantTop := []string{"attributes", "log_type", "message", "producer_id", "record_id", "timestamp"}
	if len(top) != len(wantTop) {
		t.Fatalf("M has %d top-level fields, want exactly 6: %s", len(top), m)
	}
	for _, k := range wantTop {
		if _, ok := top[k]; !ok {
			t.Fatalf("M is missing field %q", k)
		}
	}
	var attrs map[string]string
	if err := json.Unmarshal(top["attributes"], &attrs); err != nil {
		t.Fatalf("attributes is not a flat string object: %v", err)
	}
	for _, k := range []string{"host", "padding", "request_id", "source_ip", "status"} {
		if _, ok := attrs[k]; !ok {
			t.Fatalf("attributes is missing %q", k)
		}
	}
	if len(attrs) != 5 {
		t.Fatalf("attributes has %d keys, want exactly 5", len(attrs))
	}

	var id, ts string
	_ = json.Unmarshal(top["record_id"], &id)
	_ = json.Unmarshal(top["timestamp"], &ts)
	if id != "000000000007" || len(id) != 12 {
		t.Fatalf("record_id = %q, want 12-digit zero-padded %q", id, "000000000007")
	}
	if ts != "2026-09-11T04:30:00.500000000Z" || len(ts) != 30 {
		t.Fatalf("timestamp = %q (len %d), want %q with trailing zeros preserved",
			ts, len(ts), "2026-09-11T04:30:00.500000000Z")
	}

	// RFC 8785-style: no insignificant whitespace, keys in sorted order.
	if bytes.ContainsAny(m, "\n\t") || strings.Contains(string(m), ": ") || strings.Contains(string(m), ", ") {
		t.Fatalf("M contains insignificant whitespace: %s", m)
	}
	order := []string{`"attributes"`, `"log_type"`, `"message"`, `"producer_id"`, `"record_id"`, `"timestamp"`}
	last := -1
	for _, k := range order {
		i := bytes.Index(m, []byte(k))
		if i <= last {
			t.Fatalf("top-level keys are not in sorted order in %s", m)
		}
		last = i
	}
}
