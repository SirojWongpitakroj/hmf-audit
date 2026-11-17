package dht_test

import (
	"testing"
	"time"

	"dhtbacked/auth"
	"dhtbacked/dht"
	"dhtbacked/internal/hashing"
)

type gateFixture struct {
	store *dht.DHT
	ca    *auth.CA
	tok   auth.Token
	tid   hashing.Digest
}

func newGate(t *testing.T) (gateFixture, func(d time.Duration)) {
	t.Helper()
	c := fixedClock()
	ca := newCA(t, c)
	store := dht.New(ca.PublicKey(), c)
	tid := dht.TreeID(4, "access")
	var node, root, micro hashing.Digest
	node[0], root[0], micro[0] = 1, 2, 3
	store.PutParent(dht.ParentNode{TreeID: tid, J: 9, K: 3, Node: node, CommitSeq: 1})
	store.PutCert(dht.AncestorCert{TreeID: tid, Level: 8, Index: 1, AncestorHash: node, CommitSeq: 1})
	store.PutRoot(dht.RootRecord{TreeID: tid, Seq: 1, Root: root, LeafCnt: 10, LogType: "access"})
	store.PutMicroRoot(dht.MicroRootRecord{TreeID: tid, WindowID: 0, IMin: 0, IMax: 9, MicroRoot: micro, Seq: 1})

	// A second tree the auditor is not authorized for.
	otherTID := dht.TreeID(4, "security")
	store.PutParent(dht.ParentNode{TreeID: otherTID, J: 9, K: 3, Node: node, CommitSeq: 1})

	return gateFixture{
		store: store,
		ca:    ca,
		tok:   ca.Issue("auditor-1", 4, []string{"access", "api"}),
		tid:   tid,
	}, c.Advance
}

// Q13 / R-AUTH-04: a valid token releases proof data.
func TestValidTokenReleasesProofData(t *testing.T) {
	f, _ := newGate(t)
	for name, call := range map[string]func() ([]byte, error){
		"parent": func() ([]byte, error) {
			return f.store.GetParent(&f.tok, dht.ParentRequest{Epoch: 4, LogType: "access", J: 9, K: 3, Key: dht.KeyP(f.tid, 9, 3)})
		},
		"cert": func() ([]byte, error) {
			return f.store.GetCert(&f.tok, dht.CertRequest{Epoch: 4, LogType: "access", S: 8, K: 1, Key: dht.KeyC(f.tid, 8, 1)})
		},
		"root": func() ([]byte, error) {
			return f.store.GetRoot(&f.tok, dht.RootRequest{Epoch: 4, LogType: "access", Key: dht.KeyR(f.tid)})
		},
		"microroot": func() ([]byte, error) {
			return f.store.GetMicroRoot(&f.tok, dht.MicroRootRequest{Epoch: 4, LogType: "access", WindowID: 0, Key: dht.KeyM(f.tid, 0)})
		},
	} {
		b, err := call()
		if err != nil {
			t.Errorf("%s: authorized request failed: %v", name, err)
		}
		if len(b) == 0 {
			t.Errorf("%s: no proof data returned", name)
		}
	}
}

// Q13 / R-AUTH-04: on any failure the gate returns no proof data.
func TestTokenFailuresReleaseNoProofData(t *testing.T) {
	f, advance := newGate(t)
	req := dht.ParentRequest{Epoch: 4, LogType: "access", J: 9, K: 3, Key: dht.KeyP(f.tid, 9, 3)}

	if b, err := f.store.GetParent(nil, req); err != auth.ErrNoToken || b != nil {
		t.Errorf("absent token: %v, %d bytes", err, len(b))
	}
	malformed := f.tok
	malformed.Signature = []byte{1}
	if b, err := f.store.GetParent(&malformed, req); err != auth.ErrMalformedToken || b != nil {
		t.Errorf("malformed token: %v, %d bytes", err, len(b))
	}
	wrongSig := f.tok
	wrongSig.Signature = append([]byte(nil), f.tok.Signature...)
	wrongSig.Signature[3] ^= 0xFF
	if b, err := f.store.GetParent(&wrongSig, req); err != auth.ErrBadSignature || b != nil {
		t.Errorf("wrong signature: %v, %d bytes", err, len(b))
	}

	otherEpoch := dht.TreeID(5, "access")
	wrongEpochReq := dht.ParentRequest{Epoch: 5, LogType: "access", J: 9, K: 3, Key: dht.KeyP(otherEpoch, 9, 3)}
	if b, err := f.store.GetParent(&f.tok, wrongEpochReq); err != auth.ErrEpochMismatch || b != nil {
		t.Errorf("wrong epoch: %v, %d bytes", err, len(b))
	}

	otherType := dht.TreeID(4, "security")
	disallowedReq := dht.ParentRequest{Epoch: 4, LogType: "security", J: 9, K: 3, Key: dht.KeyP(otherType, 9, 3)}
	if b, err := f.store.GetParent(&f.tok, disallowedReq); err != auth.ErrTypeNotPermitted || b != nil {
		t.Errorf("disallowed type: %v, %d bytes", err, len(b))
	}

	// Expiry is exercised through the injectable Clock, never by sleeping.
	advance(time.Hour + time.Second)
	if b, err := f.store.GetParent(&f.tok, req); err != auth.ErrTokenExpired || b != nil {
		t.Errorf("expired token: %v, %d bytes", err, len(b))
	}
}

// Q41: the DHT recomputes the expected key and rejects a mismatch. A caller
// cannot claim an authorized type while requesting a key belonging to another.
func TestKeyAndTypeSpoofingIsRejected(t *testing.T) {
	f, _ := newGate(t)
	securityTID := dht.TreeID(4, "security")

	// Claim the permitted type, supply the forbidden type's key.
	spoof := dht.ParentRequest{Epoch: 4, LogType: "access", J: 9, K: 3, Key: dht.KeyP(securityTID, 9, 3)}
	if b, err := f.store.GetParent(&f.tok, spoof); err != dht.ErrKeyMismatch || b != nil {
		t.Fatalf("type spoofing: %v, %d bytes", err, len(b))
	}
	// Claim different coordinates than the key encodes.
	wrongCoords := dht.ParentRequest{Epoch: 4, LogType: "access", J: 9, K: 4, Key: dht.KeyP(f.tid, 9, 3)}
	if _, err := f.store.GetParent(&f.tok, wrongCoords); err != dht.ErrKeyMismatch {
		t.Fatalf("coordinate spoofing: %v", err)
	}
	// The same rejection applies to every envelope type.
	if _, err := f.store.GetCert(&f.tok, dht.CertRequest{Epoch: 4, LogType: "access", S: 8, K: 1, Key: dht.KeyC(securityTID, 8, 1)}); err != dht.ErrKeyMismatch {
		t.Errorf("cert key spoofing: %v", err)
	}
	if _, err := f.store.GetRoot(&f.tok, dht.RootRequest{Epoch: 4, LogType: "access", Key: dht.KeyR(securityTID)}); err != dht.ErrKeyMismatch {
		t.Errorf("root key spoofing: %v", err)
	}
	if _, err := f.store.GetMicroRoot(&f.tok, dht.MicroRootRequest{Epoch: 4, LogType: "access", WindowID: 0, Key: dht.KeyM(securityTID, 0)}); err != dht.ErrKeyMismatch {
		t.Errorf("micro-root key spoofing: %v", err)
	}
}

// A well-formed authorized request for a key that holds nothing returns
// not-found, not data.
func TestAuthorizedRequestForAbsentRecord(t *testing.T) {
	f, _ := newGate(t)
	req := dht.ParentRequest{Epoch: 4, LogType: "access", J: 9, K: 400, Key: dht.KeyP(f.tid, 9, 400)}
	if _, err := f.store.GetParent(&f.tok, req); err != dht.ErrNotFound {
		t.Fatalf("absent record returned %v", err)
	}
}
