package blockchain

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"evseb/internal/access"
	"evseb/internal/crypto"
)

var (
	_  BlockchainContracts = (*Memory)(nil)
	_  BlockchainContracts = (*Ethereum)(nil)
	t0                     = time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
)

// contractsUnderTest returns the in-memory backend and, when EVSEB_ETH_RPC_URL
// and EVSEB_EVM_PRIVATE_KEY_HEX are set, the Ethereum backend on that chain.
// Both must behave the same.
func contractsUnderTest(t *testing.T, keys crypto.SystemKeys, now func() time.Time) map[string]BlockchainContracts {
	t.Helper()
	out := map[string]BlockchainContracts{"memory": NewMemory(keys.Kauth, keys.Kindex, now)}
	if rpc, key, ok := FromEnv(); ok {
		e, err := Deploy(rpc, key, keys.Kauth, keys.Kindex)
		if err != nil {
			t.Fatalf("Deploy: %v", err)
		}
		t.Cleanup(e.Close)
		out["ethereum"] = e
	} else {
		t.Logf("%s/%s not set: Ethereum backend not exercised", EnvRPCURL, EnvPrivateKey)
	}
	return out
}

func TestMerkleRootAnchor(t *testing.T) {
	keys, _ := crypto.GenerateKeys()
	for name, c := range contractsUnderTest(t, keys, time.Now) {
		if _, err := c.VerifyIntegrity(); !errors.Is(err, ErrNoRoot) {
			t.Fatalf("%s: want ErrNoRoot, got %v", name, err)
		}
		r := crypto.H([]byte("root"))
		if err := c.UpdateMerkleRoot(r[:]); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, err := c.VerifyIntegrity()
		if err != nil || !bytes.Equal(got, r[:]) {
			t.Fatalf("%s: VerifyIntegrity = %x, %v", name, got, err)
		}
	}
}

func TestVerifyAccessChecksClaimsAgainstOnChainPolicy(t *testing.T) {
	keys, _ := crypto.GenerateKeys()
	for name, c := range contractsUnderTest(t, keys, time.Now) {
		now := time.Now()
		r := &access.Registrar{Chain: c, Kindex: keys.Kindex, Kauth: keys.Kauth, Validity: time.Hour,
			Now: func() time.Time { return now }, VerifyCredentials: func(u, p string) bool { return true }}
		_, tok, err := r.RegisterUser("alice", "x", []string{"Firewall Logs"}, "Firewall Logs")
		if err != nil {
			t.Fatalf("%s: register: %v", name, err)
		}
		if ok, err := c.VerifyAccess(tok, "Firewall Logs"); !ok || err != nil {
			t.Fatalf("%s: permitted LType denied (%v)", name, err)
		}
		if ok, _ := c.VerifyAccess(tok, "IDPS Logs"); ok {
			t.Fatalf("%s: unpermitted LType allowed", name)
		}
		// A validly encrypted token whose claims don't match the on-chain
		// policy (extra permission) is denied by the contract check.
		forged, _ := access.EncryptToken(access.UserToken{UserID: "alice",
			Permissions: []string{"Firewall Logs", "IDPS Logs"}, Texp: now.Add(time.Hour).Unix()}, keys.Kauth)
		if ok, _ := c.VerifyAccess(forged, "IDPS Logs"); ok {
			t.Fatalf("%s: forged permissions allowed", name)
		}
		// Token under the wrong key, garbage, unregistered user.
		wrongKey, _ := access.EncryptToken(access.UserToken{UserID: "alice", Permissions: []string{"Firewall Logs"},
			Texp: now.Add(time.Hour).Unix()}, keys.Kenc)
		unknown, _ := access.EncryptToken(access.UserToken{UserID: "bob", Permissions: []string{"Firewall Logs"},
			Texp: now.Add(time.Hour).Unix()}, keys.Kauth)
		for i, bad := range [][]byte{wrongKey, []byte("garbage"), unknown} {
			if ok, err := c.VerifyAccess(bad, "Firewall Logs"); ok || err != nil {
				t.Fatalf("%s: bad token %d allowed (err %v)", name, i, err)
			}
		}
		// Expired registration.
		r.Validity = -time.Hour
		_, old, _ := r.RegisterUser("carol", "x", []string{"Firewall Logs"}, "Firewall Logs")
		if ok, _ := c.VerifyAccess(old, "Firewall Logs"); ok {
			t.Fatalf("%s: expired token allowed", name)
		}
	}
}

func TestMemoryStoresCTPolicy(t *testing.T) {
	keys, _ := crypto.GenerateKeys()
	m := NewMemory(keys.Kauth, keys.Kindex, func() time.Time { return t0 })
	ct, _ := access.EncryptPolicy(access.NewPolicy("alice", []string{"Firewall Logs"}, "Firewall Logs", 1), keys.Kindex)
	if err := m.GrantAccess("alice", ct); err != nil {
		t.Fatal(err)
	}
	got, ok := m.StoredPolicy("alice")
	if !ok || !bytes.Equal(got, ct) {
		t.Fatal("CTPolicy not stored unchanged")
	}
	if err := m.GrantAccess("bob", []byte("not a ciphertext")); err == nil {
		t.Fatal("GrantAccess accepted a CTPolicy that does not decrypt under Kindex")
	}
}
