// Package tests runs EVSEB Phase 1-6 end to end across the internal
// packages (Section 12 tests/). The Phase 1-6 run is repeated on the
// Ethereum backend when EVSEB_ETH_RPC_URL / EVSEB_EVM_PRIVATE_KEY_HEX are set.
package tests

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"evseb/internal/access"
	"evseb/internal/blockchain"
	"evseb/internal/bloom"
	"evseb/internal/crypto"
	"evseb/internal/integrity"
	"evseb/internal/keyword"
	"evseb/internal/logstore"
	"evseb/internal/search"
	"evseb/internal/trapdoor"
	"evseb/internal/tree"
)

func backends(t *testing.T, keys crypto.SystemKeys) map[string]blockchain.BlockchainContracts {
	t.Helper()
	out := map[string]blockchain.BlockchainContracts{"memory": blockchain.NewMemory(keys.Kauth, keys.Kindex, time.Now)}
	if rpc, key, ok := blockchain.FromEnv(); ok {
		e, err := blockchain.Deploy(rpc, key, keys.Kauth, keys.Kindex)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(e.Close)
		out["ethereum"] = e
	} else {
		t.Log("no chain configured: Ethereum backend not exercised")
	}
	return out
}

func TestPhase1To6(t *testing.T) {
	keys, err := crypto.GenerateKeys() // Phase 1 Step 1
	if err != nil {
		t.Fatal(err)
	}
	for name, chain := range backends(t, keys) { // Phase 1 Step 2
		t.Run(name, func(t *testing.T) { phase1To6(t, keys, chain) })
	}
}

func phase1To6(t *testing.T, keys crypto.SystemKeys, chain blockchain.BlockchainContracts) {
	// Phase 1 Step 3: registration issues Tokenu (Q6 ruling).
	ra := &access.Registrar{Chain: chain, Kindex: keys.Kindex, Kauth: keys.Kauth, Validity: 24 * time.Hour, Now: time.Now,
		VerifyCredentials: func(u, c string) bool { return c == "secret-"+u }}
	perms := []string{"Firewall Logs", "IDPS Logs"}
	st, tok, err := ra.RegisterUser("alice", "secret-alice", perms, "Firewall Logs")
	if err != nil || st != access.StatusRegistered || tok == nil {
		t.Fatalf("register: %q %v", st, err)
	}

	// Phase 2.
	m, k := bloom.Params(500, 0.001)
	tr := tree.TreeSetup(m, k)

	// Phase 3-4 on a small corpus with two firewall sources.
	storage, dht := logstore.NewStorage(), integrity.NewDHT()
	var assigner logstore.LIDAssigner
	var lids []string
	byContent := map[string]string{}
	corpus := []struct{ si, lt, c string }{
		{"fw01", "Firewall Logs", "DENY tcp 10.0.0.1:22 policy block-ssh"},
		{"fw01", "Firewall Logs", "ALLOW tcp 10.0.0.2:443 policy web"},
		{"fw02", "Firewall Logs", "DENY udp 10.0.0.3:53 policy block-dns"},
		{"snort01", "IDPS Logs", "ALERT portscan detected from 10.9.9.9"},
		{"snort01", "IDPS Logs", "ALERT bruteforce ssh from 10.9.9.8"},
		{"tomcat01", "Application Server Logs", "ERROR NullPointerException in handler"},
	}
	for i, x := range corpus {
		lg := logstore.Log{LID: assigner.Next(), Content: x.c, Ti: time.Now().Unix() + int64(i), Si: x.si, LType: x.lt}
		enc, err := logstore.EncryptLog(lg, keys.Kenc)
		if err != nil {
			t.Fatal(err)
		}
		storage.Put(enc)
		dht.Put(lg.LID, integrity.HashLog(enc.CTLog))
		tree.InsertIndices(tr, lg.LType, lg, keyword.Extract(lg.Content), keys.Ksearch)
		lids = append(lids, lg.LID)
		byContent[x.c] = lg.LID
	}
	stored, err := integrity.AnchorRoot(lids, dht, chain)
	if err != nil {
		t.Fatal(err)
	}

	// Eq. 17: the index is stored encrypted; search runs on the decrypted copy.
	ei, err := tree.Encrypt(tr, keys.Kindex)
	if err != nil {
		t.Fatal(err)
	}
	tr2, err := tree.Decrypt(ei, keys.Kindex)
	if err != nil {
		t.Fatal(err)
	}
	eng := &search.Engine{Tree: tr2, Chain: chain, Stored: stored, Storage: storage}

	// Phase 5-6.
	q := func(lt, op string, words ...string) search.Query {
		var tws []trapdoor.Trapdoor
		for _, w := range words {
			tws = append(tws, trapdoor.Generate(w, keys.Ksearch))
		}
		return search.Query{Tw: tws, LType: lt, Tokenu: tok, BooleanOperator: op}
	}
	got := func(r search.Result) []string {
		var out []string
		for _, e := range r.Entries {
			pt, err := logstore.DecryptLog(e, keys.Kenc)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, pt.Content)
		}
		return out
	}
	run := func(q search.Query) search.Result {
		t.Helper()
		r, err := eng.SecureQueryExecution(q)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}

	// Q8: fw01 and fw02 both searched.
	if r := run(q("Firewall Logs", "", "deny")); !r.Integrity || !slices.Equal(got(r), []string{corpus[0].c, corpus[2].c}) {
		t.Fatalf("deny = %v", got(r))
	}
	// B9: multi-keyword.
	if r := run(q("Firewall Logs", search.OpAND, "deny", "udp")); !slices.Equal(got(r), []string{corpus[2].c}) {
		t.Fatalf("deny AND udp = %v", got(r))
	}
	if r := run(q("IDPS Logs", search.OpOR, "portscan", "bruteforce")); len(r.Entries) != 2 {
		t.Fatalf("portscan OR bruteforce: %d", len(r.Entries))
	}
	if r := run(q("Firewall Logs", search.OpNOT, "deny")); !slices.Equal(got(r), []string{corpus[1].c}) {
		t.Fatalf("NOT deny = %v", got(r))
	}
	if _, err := eng.SecureQueryExecution(q("Application Server Logs", "", "error")); !errors.Is(err, search.ErrAccessDenied) {
		t.Fatalf("unpermitted LType: %v", err)
	}
	if _, err := eng.SecureQueryExecution(q("Firewall Logs", "", "portscan")); !errors.Is(err, search.ErrNoMatch) {
		t.Fatalf("keyword under other LType: %v", err)
	}

	// Q9: tamper a matching ciphertext AND its DHT entry: still excluded.
	lid := byContent[corpus[2].c]
	enc, _ := storage.Get(lid)
	enc.CTLog[0] ^= 0x01
	storage.Put(enc)
	dht.Put(lid, integrity.HashLog(enc.CTLog))
	r := run(q("Firewall Logs", "", "deny"))
	if r.Integrity || len(r.Entries) != 1 || r.Entries[0].LID != byContent[corpus[0].c] {
		t.Fatalf("after coordinated tamper: %+v", r)
	}

	// Section 11: the chain holds CTPolicy (exactly Eq. 5-6), nothing in clear.
	var ct []byte
	switch c := chain.(type) {
	case *blockchain.Memory:
		ct, _ = c.StoredPolicy("alice")
	case *blockchain.Ethereum:
		ct, _, _, _ = c.PolicyRecord("alice")
	}
	if strings.Contains(string(ct), "alice") {
		t.Fatal("clear UserID on chain")
	}
	p, err := access.DecryptPolicy(ct, keys.Kindex)
	if err != nil || p != access.NewPolicy("alice", perms, "Firewall Logs", p.Texp) {
		t.Fatalf("on-chain policy is not the Eq. 5 tuple: %v", err)
	}
}

// Global verification over a larger dataset: every tampered LID is found and
// no untampered LID is excluded.
func TestGlobalVerificationAtScale(t *testing.T) {
	keys, _ := crypto.GenerateKeys()
	chain := blockchain.NewMemory(keys.Kauth, keys.Kindex, time.Now)
	storage, dht := logstore.NewStorage(), integrity.NewDHT()
	var a logstore.LIDAssigner
	var lids []string
	for i := range 1000 {
		lg := logstore.Log{LID: a.Next(), Content: fmt.Sprintf("entry %d", i), Si: "fw01", LType: "Firewall Logs"}
		enc, _ := logstore.EncryptLog(lg, keys.Kenc)
		storage.Put(enc)
		dht.Put(lg.LID, integrity.HashLog(enc.CTLog))
		lids = append(lids, lg.LID)
	}
	stored, err := integrity.AnchorRoot(lids, dht, chain)
	if err != nil {
		t.Fatal(err)
	}
	var tampered []string
	for i := 7; i < 1000; i += 10 {
		e, _ := storage.Get(lids[i])
		e.CTLog[5] ^= 0x80
		storage.Put(e)
		tampered = append(tampered, lids[i])
	}
	v, ok, err := integrity.IdentifyTamperedFiles(lids, stored, storage, chain)
	if err != nil || ok || len(v) != 1000-len(tampered) {
		t.Fatalf("ok=%v err=%v verified=%d", ok, err, len(v))
	}
	for _, lid := range tampered {
		if slices.Contains(v, lid) {
			t.Fatalf("tampered %s kept", lid)
		}
	}
}
