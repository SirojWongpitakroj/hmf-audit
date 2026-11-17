package ingestverifier_test

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"testing"

	"dhtbacked/encryptor"
	"dhtbacked/ingestverifier"
)

func signer(seed byte) ed25519.PrivateKey {
	s := make([]byte, ed25519.SeedSize)
	for i := range s {
		s[i] = seed
	}
	return ed25519.NewKeyFromSeed(s)
}

func testEncryptor(t *testing.T) *encryptor.Encryptor {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	e, err := encryptor.New(key)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func record() encryptor.Record {
	return encryptor.Record{
		RecordID:       "000000000000",
		ProducerID:     "producer-access",
		LogType:        "access",
		TimestampNanos: 1789101000500000000,
		Message:        "access event 1",
		Attributes: encryptor.Attributes{
			Host: "host-1", Padding: "", RequestID: "req-1",
			SourceIP: "10.0.0.1", Status: "200",
		},
	}
}

func fixture(t *testing.T) (*ingestverifier.Verifier, encryptor.Emitted, ed25519.PrivateKey) {
	t.Helper()
	sk := signer(1)
	reg := ingestverifier.NewRegistry(map[string]ed25519.PublicKey{
		"producer-access": sk.Public().(ed25519.PublicKey),
	})
	e := testEncryptor(t)
	em, err := e.ProcessRecord(record(), sk)
	if err != nil {
		t.Fatal(err)
	}
	return ingestverifier.New(reg), em, sk
}

func TestValidTupleIsAccepted(t *testing.T) {
	v, em, _ := fixture(t)
	if err := v.Validate(em); err != nil {
		t.Fatalf("valid tuple rejected: %v", err)
	}
	if !v.Accept(em) {
		t.Fatal("Accept rejected a valid tuple")
	}
}

// R-ING-02 acceptance: altered (c, t, AD) fails.
func TestAlteredCiphertextTagOrADFails(t *testing.T) {
	for name, mutate := range map[string]func(*encryptor.Emitted){
		"c":  func(e *encryptor.Emitted) { e.C[len(e.C)-1] ^= 0x01 },
		"t":  func(e *encryptor.Emitted) { e.T[0] ^= 0x01 },
		"ad": func(e *encryptor.Emitted) { e.AD[len(e.AD)-1] ^= 0x01 },
	} {
		v, em, _ := fixture(t)
		mutate(&em)
		if err := v.Validate(em); err == nil {
			t.Errorf("altered %s was accepted", name)
		}
	}
}

func TestAlteredSignatureFails(t *testing.T) {
	v, em, _ := fixture(t)
	em.Sigma[0] ^= 0x01
	if err := v.Validate(em); err != ingestverifier.ErrSignatureInvalid {
		t.Fatalf("altered signature returned %v", err)
	}
}

func TestCarriedDigestMustMatchRecomputation(t *testing.T) {
	v, em, sk := fixture(t)
	// A self-consistent tuple over a different digest: d and sigma agree with
	// each other but not with (c, t, AD).
	em.D[0] ^= 0xFF
	em.Sigma = encryptor.Attest(sk, em.D)
	if err := v.Validate(em); err != ingestverifier.ErrDigestMismatch {
		t.Fatalf("digest mismatch returned %v", err)
	}
}

// Q5: an arbitrary in-band public key is not sufficient identity binding.
func TestInBandPublicKeyIsRejected(t *testing.T) {
	v, _, _ := fixture(t)
	rogue := signer(9)
	e := testEncryptor(t)
	em, err := e.ProcessRecord(record(), rogue)
	if err != nil {
		t.Fatal(err)
	}
	// The tuple is internally perfectly consistent under the rogue key.
	if !encryptor.VerifyAttestation(em.ProducerPublicKey, em.D, em.Sigma) {
		t.Fatal("test fixture is not self-consistent")
	}
	if err := v.Validate(em); err != ingestverifier.ErrPublicKeyMismatch {
		t.Fatalf("in-band public key returned %v, want ErrPublicKeyMismatch", err)
	}
}

// R-ING-03: a record from an unregistered producer is rejected.
func TestUnregisteredProducerIsRejected(t *testing.T) {
	v, _, _ := fixture(t)
	sk := signer(1)
	r := record()
	r.ProducerID = "producer-unknown"
	e := testEncryptor(t)
	em, err := e.ProcessRecord(r, sk)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Validate(em); err != ingestverifier.ErrUnregisteredProducer {
		t.Fatalf("unregistered producer returned %v", err)
	}
}

func TestMalformedTupleIsRejected(t *testing.T) {
	v, em, _ := fixture(t)
	short := em
	short.AD = []byte{1, 2}
	if err := v.Validate(short); err != ingestverifier.ErrMalformedTuple {
		t.Errorf("malformed AD returned %v", err)
	}
	badTag := em
	badTag.T = []byte{1}
	if err := v.Validate(badTag); err != ingestverifier.ErrMalformedTuple {
		t.Errorf("wrong tag length returned %v", err)
	}
}

// Q5/Q46/Q47: the registry is loaded from a deterministic JSON file.
func TestRegistryFileRoundTripIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")
	entries := map[string]ed25519.PublicKey{
		"producer-security": signer(5).Public().(ed25519.PublicKey),
		"producer-access":   signer(1).Public().(ed25519.PublicKey),
		"producer-api":      signer(2).Public().(ed25519.PublicKey),
	}
	if err := ingestverifier.WriteRegistry(path, entries); err != nil {
		t.Fatal(err)
	}
	first := readFile(t, path)
	for i := 0; i < 20; i++ {
		if err := ingestverifier.WriteRegistry(path, entries); err != nil {
			t.Fatal(err)
		}
		if readFile(t, path) != first {
			t.Fatal("registry serialization is not deterministic")
		}
	}
	// Entries are ordered by producer_id, not by map iteration order.
	if idx1, idx2 := indexOf(first, "producer-access"), indexOf(first, "producer-api"); idx1 > idx2 {
		t.Fatal("registry entries are not sorted by producer_id")
	}
	reg, err := ingestverifier.LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	for id, pk := range entries {
		got, ok := reg.Lookup(id)
		if !ok || string(got) != string(pk) {
			t.Fatalf("registry lost %s", id)
		}
	}
	if _, ok := reg.Lookup("producer-absent"); ok {
		t.Fatal("registry returned an unregistered producer")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func indexOf(hay, needle string) int {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
