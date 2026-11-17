package dht_test

// Independent verification of the simulated DHT (M5) and its Phase 5(a)
// access gate (M6).
//
// Node placement is recomputed from first principles with crypto/sha256 rather
// than by calling NodeIndex. The gate is exercised through all four Q41 typed
// envelopes: the existing gate tests drive every failure mode through
// GetParent only, which leaves GetCert, GetRoot and GetMicroRoot unproven.

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"dhtbacked/auth"
	"dhtbacked/dht"
	"dhtbacked/internal/hashing"
)

// Q10: a key maps to the node given by the first eight bytes of SHA256(key),
// read as an unsigned big-endian integer, reduced modulo 16. Recomputed here
// without calling NodeIndex, over enough keys to reach every node.
func TestIndependentNodeAssignmentFromFirstPrinciples(t *testing.T) {
	if dht.NodeCount != 16 {
		t.Fatalf("NodeCount = %d, want 16 (Q10)", dht.NodeCount)
	}
	seen := make([]bool, 16)
	for i := uint64(0); i < 4096; i++ {
		key := dht.KeyP(dht.TreeID(i, "access"), 9, i)
		h := sha256.Sum256(key[:])
		want := int(binary.BigEndian.Uint64(h[:8]) % 16)
		if got := dht.NodeIndex(key); got != want {
			t.Fatalf("key %d: node %d, want %d", i, got, want)
		}
		seen[want] = true
	}
	for n, ok := range seen {
		if !ok {
			t.Fatalf("no key among 4,096 reached node %d; the placement rule is not spanning all 16", n)
		}
	}
}

// Q10 replication factor 1: each stored object lives on exactly one node, and
// it is the node Q10's rule names.
func TestIndependentReplicationFactorIsOne(t *testing.T) {
	store := dht.New(nil, fixedClock())
	const n = 500
	for i := uint64(0); i < n; i++ {
		store.PutParent(dht.ParentNode{TreeID: dht.TreeID(1, "access"), J: 9, K: i, CommitSeq: 1})
	}
	total := 0
	for node := 0; node < dht.NodeCount; node++ {
		total += store.CountAt(node)
	}
	if total != n || store.Count() != n {
		t.Fatalf("%d objects across nodes and Count()=%d for %d puts; replication factor must be 1", total, store.Count(), n)
	}
	// Place one object and confirm it is on the named node and no other.
	fresh := dht.New(nil, fixedClock())
	p := dht.ParentNode{TreeID: dht.TreeID(9, "api"), J: 3, K: 7, CommitSeq: 1}
	fresh.PutParent(p)
	home := dht.NodeIndex(dht.KeyP(p.TreeID, p.J, p.K))
	for node := 0; node < dht.NodeCount; node++ {
		want := 0
		if node == home {
			want = 1
		}
		if fresh.CountAt(node) != want {
			t.Fatalf("node %d holds %d objects, want %d", node, fresh.CountAt(node), want)
		}
	}
}

// Q13: tau_A = Ed25519.Sign(sk_CA, SHA256(TokenBody)). The signature must
// verify over the SHA-256 of the body and must NOT verify over the raw body.
func TestIndependentTokenSignatureIsOverBodyHash(t *testing.T) {
	c := fixedClock()
	ca := newCA(t, c)
	tok := ca.Issue("auditor-1", 4, []string{"access"})
	body := tok.Body.Serialize()
	digest := sha256.Sum256(body)
	if !ed25519.Verify(ca.PublicKey(), digest[:], tok.Signature) {
		t.Fatal("tau_A does not verify over SHA256(TokenBody)")
	}
	if ed25519.Verify(ca.PublicKey(), body, tok.Signature) {
		t.Fatal("tau_A verifies over the raw body; Q13 signs the SHA-256 of the body")
	}
}

// Q40: expiresAt is serialized in whole Unix seconds, so an issuance at a
// fractional second yields a token whose expiry is that instant plus one hour,
// truncated to the second. The token is then valid strictly before that
// second and rejected at it. This pins the boundary Q40's own serialization
// produces; it is the specified behaviour, not a defect.
func TestIndependentExpiryBoundaryAtFractionalIssuance(t *testing.T) {
	c := fixedClock()
	c.Set(time.Date(2026, 9, 11, 4, 30, 0, 700_000_000, time.UTC))
	ca := newCA(t, c)
	tok := ca.Issue("auditor-1", 4, []string{"access"})

	wantExpiry := time.Date(2026, 9, 11, 5, 30, 0, 0, time.UTC).Unix()
	if tok.Body.ExpiresAtUnix != wantExpiry {
		t.Fatalf("expiresAt = %d, want %d (issuance + 1h, truncated to the second)", tok.Body.ExpiresAtUnix, wantExpiry)
	}
	edge := time.Unix(wantExpiry, 0).UTC()
	if err := auth.Verify(ca.PublicKey(), &tok, edge.Add(-time.Nanosecond), 4, "access"); err != nil {
		t.Fatalf("rejected 1ns before expiresAt: %v", err)
	}
	if err := auth.Verify(ca.PublicKey(), &tok, edge, 4, "access"); !errors.Is(err, auth.ErrTokenExpired) {
		t.Fatalf("at expiresAt: %v, want ErrTokenExpired (no grace period)", err)
	}
}

// envelope is one of the four Q41 request types, reduced to a closure so every
// failure mode can be driven through each of them.
type envelope struct {
	name string
	get  func(tok *auth.Token, epoch uint64, logType string) ([]byte, error)
}

func envelopes(store *dht.DHT) []envelope {
	return []envelope{
		{"parent", func(tok *auth.Token, e uint64, lt string) ([]byte, error) {
			return store.GetParent(tok, dht.ParentRequest{Epoch: e, LogType: lt, J: 9, K: 3, Key: dht.KeyP(dht.TreeID(e, lt), 9, 3)})
		}},
		{"cert", func(tok *auth.Token, e uint64, lt string) ([]byte, error) {
			return store.GetCert(tok, dht.CertRequest{Epoch: e, LogType: lt, S: 8, K: 1, Key: dht.KeyC(dht.TreeID(e, lt), 8, 1)})
		}},
		{"root", func(tok *auth.Token, e uint64, lt string) ([]byte, error) {
			return store.GetRoot(tok, dht.RootRequest{Epoch: e, LogType: lt, Key: dht.KeyR(dht.TreeID(e, lt))})
		}},
		{"micro-root", func(tok *auth.Token, e uint64, lt string) ([]byte, error) {
			return store.GetMicroRoot(tok, dht.MicroRootRequest{Epoch: e, LogType: lt, WindowID: 0, Key: dht.KeyM(dht.TreeID(e, lt), 0)})
		}},
	}
}

// Q13, R-AUTH-04: through every one of the four envelopes, a valid token
// releases data and each authorization failure releases none.
func TestIndependentGateHoldsForAllFourEnvelopes(t *testing.T) {
	f, advance := newGate(t)
	for _, env := range envelopes(f.store) {
		if b, err := env.get(&f.tok, 4, "access"); err != nil || len(b) == 0 {
			t.Fatalf("%s: valid token released nothing: %v", env.name, err)
		}

		malformed := f.tok
		malformed.Signature = []byte{1}
		wrongSig := f.tok
		wrongSig.Signature = append([]byte(nil), f.tok.Signature...)
		wrongSig.Signature[5] ^= 0xFF

		failures := []struct {
			why     string
			tok     *auth.Token
			epoch   uint64
			logType string
			want    error
		}{
			{"absent token", nil, 4, "access", auth.ErrNoToken},
			{"malformed token", &malformed, 4, "access", auth.ErrMalformedToken},
			{"wrong signature", &wrongSig, 4, "access", auth.ErrBadSignature},
			{"wrong epoch", &f.tok, 5, "access", auth.ErrEpochMismatch},
			{"disallowed type", &f.tok, 4, "security", auth.ErrTypeNotPermitted},
		}
		for _, fc := range failures {
			b, err := env.get(fc.tok, fc.epoch, fc.logType)
			if !errors.Is(err, fc.want) {
				t.Errorf("%s / %s: err = %v, want %v", env.name, fc.why, err, fc.want)
			}
			if b != nil {
				t.Errorf("%s / %s: released %d bytes of proof data", env.name, fc.why, len(b))
			}
		}
	}
	advance(time.Hour)
	for _, env := range envelopes(f.store) {
		b, err := env.get(&f.tok, 4, "access")
		if !errors.Is(err, auth.ErrTokenExpired) || b != nil {
			t.Errorf("%s / expired token: err = %v, %d bytes", env.name, err, len(b))
		}
	}
}

// Q41: for every envelope, a key that does not match the recomputed key is
// rejected even under a fully valid token — the caller cannot claim an
// authorized type while pointing the key at another type's object.
func TestIndependentKeySpoofingRejectedForAllFourEnvelopes(t *testing.T) {
	f, _ := newGate(t)
	other := dht.TreeID(4, "security") // same epoch, a type the token does not allow
	spoofs := []struct {
		name string
		get  func() ([]byte, error)
	}{
		{"parent", func() ([]byte, error) {
			return f.store.GetParent(&f.tok, dht.ParentRequest{Epoch: 4, LogType: "access", J: 9, K: 3, Key: dht.KeyP(other, 9, 3)})
		}},
		{"cert", func() ([]byte, error) {
			return f.store.GetCert(&f.tok, dht.CertRequest{Epoch: 4, LogType: "access", S: 8, K: 1, Key: dht.KeyC(other, 8, 1)})
		}},
		{"root", func() ([]byte, error) {
			return f.store.GetRoot(&f.tok, dht.RootRequest{Epoch: 4, LogType: "access", Key: dht.KeyR(other)})
		}},
		{"micro-root", func() ([]byte, error) {
			return f.store.GetMicroRoot(&f.tok, dht.MicroRootRequest{Epoch: 4, LogType: "access", WindowID: 0, Key: dht.KeyM(other, 0)})
		}},
	}
	for _, s := range spoofs {
		b, err := s.get()
		if !errors.Is(err, dht.ErrKeyMismatch) || b != nil {
			t.Errorf("%s spoof: err = %v, %d bytes; want ErrKeyMismatch and no data", s.name, err, len(b))
		}
	}
}

// Q45: a newer write to the same logical object overwrites at the same key,
// and the record carries seq as metadata rather than as part of the key.
func TestIndependentSeqIsMetadataNotKey(t *testing.T) {
	store := dht.New(nil, fixedClock())
	tid := dht.TreeID(1, "access")
	var v1, v2 hashing.Digest
	v1[0], v2[0] = 0xA1, 0xB2
	store.PutParent(dht.ParentNode{TreeID: tid, J: 9, K: 3, Node: v1, CommitSeq: 1})
	store.PutParent(dht.ParentNode{TreeID: tid, J: 9, K: 3, Node: v2, CommitSeq: 2})
	if store.Count() != 1 {
		t.Fatalf("two writes to one logical object produced %d entries; seq must not be in the key", store.Count())
	}
	raw, err := store.GetRaw(dht.KeyP(tid, 9, 3))
	if err != nil {
		t.Fatal(err)
	}
	p, err := dht.ParseParentNode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.Node != v2 || p.CommitSeq != 2 {
		t.Fatalf("stored (node %x, seq %d); want the newer write (b2.., seq 2)", p.Node[:1], p.CommitSeq)
	}
}
