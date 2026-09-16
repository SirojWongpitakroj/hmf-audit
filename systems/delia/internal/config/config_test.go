package config

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"delia/sig"
)

func genParticipant(t *testing.T, id, endpoint string) Participant {
	t.Helper()
	kp, err := sig.Gen()
	if err != nil {
		t.Fatalf("sig.Gen: %v", err)
	}
	return Participant{
		ID:           id,
		Endpoint:     endpoint,
		PublicKeyHex: hex.EncodeToString(kp.PublicKey()),
	}
}

func writeConfig(t *testing.T, d Domain) string {
	t.Helper()
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "domain.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestLoadValidConfig(t *testing.T) {
	d := Domain{
		Theta: 100,
		Participants: []Participant{
			genParticipant(t, "p1", "127.0.0.1:9001"),
			genParticipant(t, "p2", "127.0.0.1:9002"),
		},
	}
	path := writeConfig(t, d)

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Theta != 100 {
		t.Fatalf("Theta = %d, want 100", got.Theta)
	}
	if len(got.Participants) != 2 {
		t.Fatalf("got %d participants, want 2", len(got.Participants))
	}
	if len(got.Participants[0].PublicKey) != sig.PublicKeySize {
		t.Fatalf("decoded public key length = %d, want %d", len(got.Participants[0].PublicKey), sig.PublicKeySize)
	}
	if got.Index("p2") != 1 {
		t.Fatalf("Index(p2) = %d, want 1", got.Index("p2"))
	}
	if got.Index("nope") != -1 {
		t.Fatalf("Index(nope) = %d, want -1", got.Index("nope"))
	}
}

// DATA-007/GAP-55: theta is required, no global default.
func TestValidateRejectsMissingTheta(t *testing.T) {
	d := Domain{
		Theta:        0,
		Participants: []Participant{genParticipant(t, "p1", "x:1")},
	}
	if err := d.Validate(); err == nil {
		t.Fatalf("expected error for theta = 0")
	}
}

func TestValidateRejectsNoParticipants(t *testing.T) {
	d := Domain{Theta: 10}
	if err := d.Validate(); err == nil {
		t.Fatalf("expected error for no participants")
	}
}

func TestValidateRejectsBadPublicKey(t *testing.T) {
	d := Domain{
		Theta: 10,
		Participants: []Participant{
			{ID: "p1", Endpoint: "x:1", PublicKeyHex: "not-hex"},
		},
	}
	if err := d.Validate(); err == nil {
		t.Fatalf("expected error for invalid public key hex")
	}
}

func TestValidateRejectsWrongLengthPublicKey(t *testing.T) {
	d := Domain{
		Theta: 10,
		Participants: []Participant{
			{ID: "p1", Endpoint: "x:1", PublicKeyHex: hex.EncodeToString([]byte{0x01, 0x02})},
		},
	}
	if err := d.Validate(); err == nil {
		t.Fatalf("expected error for wrong-length public key")
	}
}

func TestValidateRejectsMissingID(t *testing.T) {
	d := Domain{
		Theta: 10,
		Participants: []Participant{
			{ID: "", Endpoint: "x:1", PublicKeyHex: hex.EncodeToString(mustPubKey(t))},
		},
	}
	if err := d.Validate(); err == nil {
		t.Fatalf("expected error for missing participant id")
	}
}

func mustPubKey(t *testing.T) []byte {
	kp, err := sig.Gen()
	if err != nil {
		t.Fatal(err)
	}
	return kp.PublicKey()
}
