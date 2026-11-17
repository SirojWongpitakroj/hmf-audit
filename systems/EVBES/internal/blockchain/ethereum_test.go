package blockchain

import (
	"errors"
	"testing"
	"time"

	"evseb/internal/access"
	"evseb/internal/crypto"
)

// Requires a running chain: EVSEB_ETH_RPC_URL and EVSEB_EVM_PRIVATE_KEY_HEX
// (e.g. `npx hardhat node` and its account #0 key). Skipped otherwise.
func TestEthereumReceiptsForTable3AndFig10(t *testing.T) {
	rpc, key, ok := FromEnv()
	if !ok {
		t.Skipf("%s/%s not set", EnvRPCURL, EnvPrivateKey)
	}
	keys, _ := crypto.GenerateKeys()
	e, err := Deploy(rpc, key, keys.Kauth, keys.Kindex)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if e.DeployVerifyAccess.GasUsed == 0 || e.DeployVerifyIntegrity.GasUsed == 0 {
		t.Fatal("deployment gas not recorded")
	}
	ct, _ := access.EncryptPolicy(access.NewPolicy("alice", []string{"Firewall Logs"}, "Firewall Logs",
		time.Now().Add(time.Hour).Unix()), keys.Kindex)
	r, err := e.GrantAccessReceipt("alice", ct)
	if err != nil || r.GasUsed == 0 {
		t.Fatalf("grantAccess: %v", err)
	}
	got, commit, exists, err := e.PolicyRecord("alice")
	p, _ := access.DecryptPolicy(ct, keys.Kindex)
	if err != nil || !exists || string(got) != string(ct) || commit != p.Commitment() {
		t.Fatalf("on-chain record mismatch: %v", err)
	}
	root := crypto.H([]byte("R"))
	if r, err = e.UpdateMerkleRootReceipt(root[:]); err != nil || r.GasUsed == 0 {
		t.Fatalf("updateMerkleRoot: %v", err)
	}
	if err := e.UpdateMerkleRoot([]byte{1}); err == nil {
		t.Fatal("non-32-byte root accepted")
	}
	tok, _ := access.EncryptToken(access.UserToken{UserID: "alice", Permissions: []string{"Firewall Logs"}, Texp: p.Texp}, keys.Kauth)
	if r, err = e.VerifyAccessReceipt(tok, "Firewall Logs"); err != nil || r.GasUsed == 0 {
		t.Fatalf("verifyAccess as a transaction: %v", err)
	}
	if GasCost(r).Sign() <= 0 {
		t.Fatal("gas cost not positive")
	}

	// Fig. 10/11 workload (Q12 item 5): three submissions with consecutive
	// nonces, none waiting for the previous one, then every receipt.
	nonce, err := e.PendingNonce()
	if err != nil {
		t.Fatal(err)
	}
	w := e.NewReceiptWatcher()
	for i := range 3 {
		tx, err := e.SendVerifyAccess(tok, "Firewall Logs", nonce+uint64(i))
		if err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
		if tx.Nonce() != nonce+uint64(i) {
			t.Fatalf("submit %d: nonce %d", i, tx.Nonce())
		}
		w.Add(tx)
		if err := w.Poll(); err != nil {
			t.Fatal(err)
		}
	}
	receipts, seen, err := w.Wait()
	if err != nil || len(receipts) != 3 || len(seen) != 3 {
		t.Fatalf("workload receipts: %d %v", len(receipts), err)
	}
	for i, r := range receipts {
		if r.GasUsed == 0 || (i > 0 && seen[i].Before(seen[i-1])) {
			t.Fatalf("receipt %d: gas %d", i, r.GasUsed)
		}
	}
	if _, err := e.SendVerifyAccess([]byte("garbage"), "Firewall Logs", nonce+3); !errors.Is(err, access.ErrNoClaims) {
		t.Fatalf("undecodable token submitted: %v", err)
	}
}
