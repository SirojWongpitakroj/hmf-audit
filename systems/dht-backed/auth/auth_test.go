package auth_test

import (
	"bytes"
	"crypto/ed25519"
	"os"
	"path/filepath"
	"testing"
	"time"

	"dhtbacked/auth"
	"dhtbacked/internal/clock"
)

func fixture(t *testing.T) (*auth.CA, ed25519.PublicKey, *clock.Manual) {
	t.Helper()
	c := &clock.Manual{T: time.Date(2026, 9, 11, 4, 30, 0, 0, time.UTC)}
	dir := t.TempDir()
	priv, pub, err := auth.LoadOrCreateCAKeys(filepath.Join(dir, "ca.sk"), filepath.Join(dir, "ca.pk"))
	if err != nil {
		t.Fatal(err)
	}
	ca, err := auth.NewCA(priv, c)
	if err != nil {
		t.Fatal(err)
	}
	return ca, pub, c
}

// Q4: CA keys are Ed25519 and encoded as deterministic binary — not X.509,
// not JWT.
func TestCAKeyFilesAreRawEd25519(t *testing.T) {
	dir := t.TempDir()
	skPath, pkPath := filepath.Join(dir, "ca.sk"), filepath.Join(dir, "ca.pk")
	priv, pub, err := auth.LoadOrCreateCAKeys(skPath, pkPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(priv) != ed25519.PrivateKeySize || len(pub) != ed25519.PublicKeySize {
		t.Fatalf("key sizes = %d/%d", len(priv), len(pub))
	}
	// Raw binary is exactly 32 bytes holding the key itself. PEM (which would
	// carry "BEGIN") and JWT (base64url text with "." separators) are both
	// longer than 32 bytes, so the length and equality checks below rule them
	// out. Do not test for a "." byte: a raw key legitimately contains 0x2E
	// about 11.8% of the time (1-(255/256)^32), which made this test flaky.
	raw := readFile(t, pkPath)
	if len(raw) != 32 {
		t.Fatalf("CA public key file is %d bytes, want raw 32", len(raw))
	}
	if !bytes.Equal(raw, pub) {
		t.Fatal("CA public key file does not hold the raw public key bytes")
	}
	// Reloading returns the same key: it is never silently regenerated.
	priv2, pub2, err := auth.LoadOrCreateCAKeys(skPath, pkPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(priv, priv2) || !bytes.Equal(pub, pub2) {
		t.Fatal("CA key material changed on reload")
	}
	loaded, err := auth.LoadCAPublicKey(pkPath)
	if err != nil || !bytes.Equal(loaded, pub) {
		t.Fatalf("LoadCAPublicKey: %v", err)
	}
}

// Q4: the enrollment credential is distinct from the authorization token.
func TestEnrollmentCredentialIsDistinctFromToken(t *testing.T) {
	ca, pub, _ := fixture(t)
	cred := ca.Enroll("auditor-1")
	if !auth.VerifyCredential(pub, cred) {
		t.Fatal("credential does not verify")
	}
	tok := ca.Issue("auditor-1", 1, []string{"access"})
	if bytes.Equal(cred.Signature, tok.Signature) {
		t.Fatal("credential and token share a signature")
	}
	// A credential signature must not pass as a token signature.
	forged := tok
	forged.Signature = cred.Signature
	if err := auth.Verify(pub, &forged, time.Now(), 1, "access"); err == nil {
		t.Fatal("a credential signature was accepted as a token")
	}
	cred.AuditorID = "auditor-2"
	if auth.VerifyCredential(pub, cred) {
		t.Fatal("a tampered credential verified")
	}
}

// Q13: token body is (version, auditorID, epoch, allowedTypes, expiresAt),
// deterministically serialized; tau_A = Ed25519.Sign(sk_CA, SHA256(body)).
func TestTokenBodySerializationIsDeterministic(t *testing.T) {
	b := auth.TokenBody{
		Version: 1, AuditorID: "auditor-1", Epoch: 3,
		AllowedTypes: []string{"access", "api"}, ExpiresAtUnix: 1789104600,
	}
	first := b.Serialize()
	for i := 0; i < 100; i++ {
		if !bytes.Equal(first, b.Serialize()) {
			t.Fatal("token body serialization is not deterministic")
		}
	}
	// The five fields are all bound: changing any changes the bytes.
	for _, alt := range []auth.TokenBody{
		{Version: 2, AuditorID: "auditor-1", Epoch: 3, AllowedTypes: []string{"access", "api"}, ExpiresAtUnix: 1789104600},
		{Version: 1, AuditorID: "auditor-2", Epoch: 3, AllowedTypes: []string{"access", "api"}, ExpiresAtUnix: 1789104600},
		{Version: 1, AuditorID: "auditor-1", Epoch: 4, AllowedTypes: []string{"access", "api"}, ExpiresAtUnix: 1789104600},
		{Version: 1, AuditorID: "auditor-1", Epoch: 3, AllowedTypes: []string{"access"}, ExpiresAtUnix: 1789104600},
		{Version: 1, AuditorID: "auditor-1", Epoch: 3, AllowedTypes: []string{"access", "api"}, ExpiresAtUnix: 1789104601},
	} {
		if bytes.Equal(first, alt.Serialize()) {
			t.Fatal("a token body field is not bound into the signed bytes")
		}
	}
}

// Q40: validity is exactly one hour, expiresAt is Unix seconds, and there is
// no clock-skew grace period. Expiry is tested through the injectable Clock.
func TestTokenValidityIsExactlyOneHourWithNoGrace(t *testing.T) {
	ca, pub, c := fixture(t)
	issued := c.Now()
	tok := ca.Issue("auditor-1", 1, []string{"access"})
	if want := issued.Add(time.Hour).Unix(); tok.Body.ExpiresAtUnix != want {
		t.Fatalf("expiresAt = %d, want %d", tok.Body.ExpiresAtUnix, want)
	}
	if err := auth.Verify(pub, &tok, issued, 1, "access"); err != nil {
		t.Fatalf("token invalid at issuance: %v", err)
	}
	justBefore := time.Unix(tok.Body.ExpiresAtUnix, 0).Add(-time.Nanosecond)
	if err := auth.Verify(pub, &tok, justBefore, 1, "access"); err != nil {
		t.Fatalf("token rejected one nanosecond before expiry: %v", err)
	}
	atExpiry := time.Unix(tok.Body.ExpiresAtUnix, 0)
	if err := auth.Verify(pub, &tok, atExpiry, 1, "access"); err != auth.ErrTokenExpired {
		t.Fatalf("token at expiresAt returned %v, want ErrTokenExpired", err)
	}
	c.Advance(time.Hour + time.Second)
	if err := auth.Verify(pub, &tok, c.Now(), 1, "access"); err != auth.ErrTokenExpired {
		t.Fatalf("expired token returned %v", err)
	}
}

// Q13: all four conditions are checked.
func TestVerifyChecksAllFourConditions(t *testing.T) {
	ca, pub, c := fixture(t)
	tok := ca.Issue("auditor-1", 5, []string{"access", "auth"})
	now := c.Now()

	if err := auth.Verify(pub, nil, now, 5, "access"); err != auth.ErrNoToken {
		t.Errorf("absent token returned %v", err)
	}
	malformed := tok
	malformed.Signature = []byte{1, 2, 3}
	if err := auth.Verify(pub, &malformed, now, 5, "access"); err != auth.ErrMalformedToken {
		t.Errorf("malformed token returned %v", err)
	}
	wrongSig := tok
	wrongSig.Signature = append([]byte(nil), tok.Signature...)
	wrongSig.Signature[0] ^= 0xFF
	if err := auth.Verify(pub, &wrongSig, now, 5, "access"); err != auth.ErrBadSignature {
		t.Errorf("wrong signature returned %v", err)
	}
	// A body edited after signing fails the signature check.
	edited := tok
	edited.Body.Epoch = 6
	if err := auth.Verify(pub, &edited, now, 6, "access"); err != auth.ErrBadSignature {
		t.Errorf("edited body returned %v", err)
	}
	if err := auth.Verify(pub, &tok, now, 6, "access"); err != auth.ErrEpochMismatch {
		t.Errorf("wrong epoch returned %v", err)
	}
	if err := auth.Verify(pub, &tok, now, 5, "security"); err != auth.ErrTypeNotPermitted {
		t.Errorf("disallowed type returned %v", err)
	}
	// A different CA's key must not validate the token.
	other, _, _ := fixture(t)
	if err := auth.Verify(other.PublicKey(), &tok, now, 5, "access"); err != auth.ErrBadSignature {
		t.Errorf("another CA's key returned %v", err)
	}
	if err := auth.Verify(pub, &tok, now, 5, "auth"); err != nil {
		t.Errorf("valid request rejected: %v", err)
	}
}

// Q13: policy is the set of permitted log types, and issuance orders it.
func TestAllowedTypesIsThePolicy(t *testing.T) {
	ca, pub, c := fixture(t)
	tok := ca.Issue("auditor-1", 1, []string{"security", "access", "api"})
	want := []string{"access", "api", "security"}
	if len(tok.Body.AllowedTypes) != len(want) {
		t.Fatalf("allowedTypes = %v", tok.Body.AllowedTypes)
	}
	for i := range want {
		if tok.Body.AllowedTypes[i] != want[i] {
			t.Fatalf("allowedTypes = %v, want %v", tok.Body.AllowedTypes, want)
		}
	}
	for _, lt := range want {
		if err := auth.Verify(pub, &tok, c.Now(), 1, lt); err != nil {
			t.Errorf("permitted type %s rejected: %v", lt, err)
		}
	}
	for _, lt := range []string{"auth", "error", ""} {
		if err := auth.Verify(pub, &tok, c.Now(), 1, lt); err != auth.ErrTypeNotPermitted {
			t.Errorf("type %q returned %v", lt, err)
		}
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
