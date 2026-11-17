package access

import (
	"bytes"
	"crypto/sha256"
	"testing"
	"time"

	"evseb/internal/crypto"
)

var t0 = time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)

func fixedNow(t time.Time) func() time.Time { return func() time.Time { return t } }

// fakeStore records GrantAccess calls (Blockchain_Store).
type fakeStore map[string][]byte

func (f fakeStore) GrantAccess(userID string, ct []byte) error {
	f[userID] = bytes.Clone(ct)
	return nil
}

func TestPolicyEq5Fields(t *testing.T) {
	p := NewPolicy("alice", []string{"Firewall Logs"}, "Firewall Logs", 42)
	if p.HUserID != crypto.H([]byte("alice")) || p.HLType != crypto.H([]byte("Firewall Logs")) {
		t.Fatal("H(UserID)/H(LType) wrong")
	}
	if p.Texp != 42 {
		t.Fatal("Texp not carried")
	}
	q, err := DecodePolicy(p.Encode())
	if err != nil || q != p {
		t.Fatalf("policy round trip: %v", err)
	}
	if p.Commitment() != sha256.Sum256(p.Encode()) {
		t.Fatal("commitment is not SHA-256(Policy)")
	}
}

func registrar(store PolicyStore, keys crypto.SystemKeys, validity time.Duration) *Registrar {
	return &Registrar{
		Chain: store, Kindex: keys.Kindex, Kauth: keys.Kauth, Validity: validity, Now: fixedNow(t0),
		VerifyCredentials: func(u, c string) bool { return c == "pw-"+u },
	}
}

// Q6 ruling: CTPolicy is exactly Eq. 5-6 and Tokenu (Eq. 7) is issued at
// registration from the plaintext inputs.
func TestRegisterUserStoresEq5PolicyAndIssuesToken(t *testing.T) {
	keys, _ := crypto.GenerateKeys()
	store := fakeStore{}
	st, tok, err := registrar(store, keys, time.Hour).RegisterUser("alice", "pw-alice", []string{"Firewall Logs"}, "Firewall Logs")
	if err != nil || st != StatusRegistered {
		t.Fatalf("status %q err %v", st, err)
	}
	ct, ok := store["alice"]
	if !ok {
		t.Fatal("no CTPolicy stored")
	}
	if bytes.Contains(ct, []byte("alice")) {
		t.Fatal("plaintext UserID visible in CTPolicy")
	}
	pt, err := crypto.DecAES(ct, keys.Kindex)
	if err != nil || len(pt) != 104 {
		t.Fatalf("CTPolicy plaintext must be exactly the 104-byte Eq. 5 tuple: %d %v", len(pt), err)
	}
	p, _ := DecodePolicy(pt)
	want := NewPolicy("alice", []string{"Firewall Logs"}, "Firewall Logs", t0.Add(time.Hour).Unix())
	if p != want {
		t.Fatalf("stored policy %+v, want %+v", p, want)
	}
	ut, err := DecryptToken(tok, keys.Kauth)
	if err != nil || ut.UserID != "alice" || len(ut.Permissions) != 1 || ut.Permissions[0] != "Firewall Logs" || ut.Texp != p.Texp {
		t.Fatalf("issued token %+v %v", ut, err)
	}
}

func TestRegisterUserRejectsBadCredentials(t *testing.T) {
	keys, _ := crypto.GenerateKeys()
	store := fakeStore{}
	st, tok, err := registrar(store, keys, time.Hour).RegisterUser("mallory", "x", []string{"IDPS Logs"}, "IDPS Logs")
	if err != nil || st != StatusAuthFailed || tok != nil {
		t.Fatalf("status %q tok %v err %v", st, tok, err)
	}
	if _, ok := store["mallory"]; ok {
		t.Fatal("policy stored for failed registration")
	}
}

func TestTokenRoundTrip(t *testing.T) {
	k, _ := crypto.Rand256()
	tok := UserToken{UserID: "alice", Permissions: []string{"Firewall Logs", "IDPS Logs"}, Texp: 99}
	ct, err := EncryptToken(tok, k)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecryptToken(ct, k)
	if err != nil || got.UserID != tok.UserID || got.Texp != tok.Texp || len(got.Permissions) != 2 ||
		got.Permissions[1] != "IDPS Logs" {
		t.Fatalf("token round trip: %+v %v", got, err)
	}
}

// Claims (off-chain, Q1) checked against the policy commitment (on-chain half).
func TestClaimsAgainstPolicy(t *testing.T) {
	keys, _ := crypto.GenerateKeys()
	store := fakeStore{}
	perms := []string{"Firewall Logs", "IDPS Logs"}
	_, tokCT, _ := registrar(store, keys, time.Hour).RegisterUser("alice", "pw-alice", perms, "Firewall Logs")
	p, _ := DecryptPolicy(store["alice"], keys.Kindex)
	commit := p.Commitment()
	tok, _ := DecryptToken(tokCT, keys.Kauth)
	c, err := BuildClaims(tok, store["alice"], keys.Kindex)
	if err != nil {
		t.Fatal(err)
	}
	now := t0.Unix()
	if !CheckClaims(c, commit, "Firewall Logs", now) || !CheckClaims(c, commit, "IDPS Logs", now) {
		t.Fatal("permitted LType denied")
	}
	if CheckClaims(c, commit, "Application Server Logs", now) {
		t.Fatal("unpermitted LType allowed")
	}
	if CheckClaims(c, commit, "Firewall Logs", t0.Add(2*time.Hour).Unix()) {
		t.Fatal("expired claims allowed")
	}
	forged := c
	forged.Permissions = append(forged.Permissions, "Application Server Logs")
	if CheckClaims(forged, commit, "Application Server Logs", now) {
		t.Fatal("claims with an added permission allowed")
	}
	forged = c
	forged.Texp += 3600
	if CheckClaims(forged, commit, "Firewall Logs", now) {
		t.Fatal("claims with an extended Texp allowed")
	}
	if c.HUserID != crypto.H([]byte("alice")) {
		t.Fatal("claims do not carry H(UserID)")
	}
	forged = c
	forged.HUserID = crypto.H([]byte("bob"))
	if CheckClaims(forged, commit, "Firewall Logs", now) {
		t.Fatal("claims with another H(UserID) allowed")
	}
	// Bob's token against Alice's policy: BuildClaims refuses.
	bobTok := UserToken{UserID: "bob", Permissions: perms, Texp: tok.Texp}
	if _, err := BuildClaims(bobTok, store["alice"], keys.Kindex); err == nil {
		t.Fatal("token of another user accepted against the policy")
	}
}
