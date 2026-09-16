package provision

import (
	"bytes"
	"testing"

	"delia/internal/config"
	"delia/internal/pki"
	"delia/sig"
)

func specs() []ParticipantSpec {
	return []ParticipantSpec{
		{ID: "p1", Endpoint: "127.0.0.1:9001", SANs: []string{"127.0.0.1"}},
		{ID: "p2", Endpoint: "127.0.0.1:9002", SANs: []string{"127.0.0.1"}},
		{ID: "p3", Endpoint: "127.0.0.1:9003", SANs: []string{"127.0.0.1"}},
	}
}

func TestDomainProvisionsDistinctKeysAndValidCerts(t *testing.T) {
	caCert, caKey, results, err := Domain("DELIA test CA", specs())
	if err != nil {
		t.Fatalf("Domain: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}

	seen := map[string]bool{}
	for _, r := range results {
		kp, err := sig.Load(r.PrivateKey)
		if err != nil {
			t.Fatalf("sig.Load(%s): %v", r.ID, err)
		}
		if !bytes.Equal(kp.PublicKey(), r.PublicKey) {
			t.Fatalf("%s: private/public key mismatch", r.ID)
		}
		if seen[string(r.PublicKey)] {
			t.Fatalf("%s: duplicate public key across participants", r.ID)
		}
		seen[string(r.PublicKey)] = true

		// Each issued cert must actually build a valid mTLS server config
		// against the CA (SYS-002/GAP-48).
		if _, err := pki.ServerTLSConfig(r.TLSCertPEM, r.TLSKeyPEM, caCert); err != nil {
			t.Fatalf("%s: ServerTLSConfig: %v", r.ID, err)
		}
	}
	_ = caKey
}

func TestWriteToDirProducesLoadableConfig(t *testing.T) {
	dir := t.TempDir()
	if err := WriteToDir(dir, 100, specs()); err != nil {
		t.Fatalf("WriteToDir: %v", err)
	}

	d, err := config.Load(dir + "/domain.json")
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if d.Theta != 100 {
		t.Fatalf("Theta = %d, want 100", d.Theta)
	}
	if len(d.Participants) != 3 {
		t.Fatalf("got %d participants, want 3", len(d.Participants))
	}
	// P order frozen at Init must match the specs order.
	wantOrder := []string{"p1", "p2", "p3"}
	for i, id := range wantOrder {
		if d.Participants[i].ID != id {
			t.Fatalf("participant %d = %s, want %s (P order must match provisioning order)", i, d.Participants[i].ID, id)
		}
	}
}
