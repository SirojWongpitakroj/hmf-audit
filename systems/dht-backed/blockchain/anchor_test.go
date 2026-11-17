package blockchain_test

import (
	"context"
	"crypto/sha256"
	"os"
	"strings"
	"testing"

	"dhtbacked/blockchain"
	"dhtbacked/internal/config"
	"dhtbacked/internal/hashing"
)

// open dials the local Hardhat chain. It skips when the chain configuration is
// absent so the rest of the suite still runs.
func open(t *testing.T) *blockchain.Anchor {
	t.Helper()
	addr := os.Getenv(config.EnvContractAddress)
	key := os.Getenv(config.EnvEVMPrivateKeyHex)
	if addr == "" || key == "" {
		t.Skipf("%s / %s are not set; skipping the Hardhat integration test",
			config.EnvContractAddress, config.EnvEVMPrivateKeyHex)
	}
	rpc := os.Getenv(config.EnvEthRPCURL)
	if rpc == "" {
		rpc = config.DefaultEthRPCURL
	}
	a, err := blockchain.Open(context.Background(), rpc, config.DefaultChainID, addr, key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(a.Close)
	return a
}

func root(seed string) hashing.Digest { return sha256.Sum256([]byte(seed)) }

// Q42/Q46: chain id 31337 is verified at startup and a mismatch fails fast.
func TestChainIDIsVerifiedAtStartup(t *testing.T) {
	a := open(t)
	if a.ChainID() != 31337 {
		t.Fatalf("chain id = %d", a.ChainID())
	}
	addr := os.Getenv(config.EnvContractAddress)
	key := os.Getenv(config.EnvEVMPrivateKeyHex)
	rpc := os.Getenv(config.EnvEthRPCURL)
	if rpc == "" {
		rpc = config.DefaultEthRPCURL
	}
	if _, err := blockchain.Open(context.Background(), rpc, 1, addr, key); err == nil {
		t.Fatal("Open accepted a mismatched chain id")
	}
	if _, err := blockchain.Open(context.Background(), rpc, 31337, "0x"+strings.Repeat("0", 40), key); err == nil {
		t.Fatal("Open accepted an address with no deployed contract")
	}
}

// Q14/Q42: the contract has exactly one configured authorized committer, and
// it is the key supplied by DHT_EVM_PRIVATE_KEY_HEX — never hard-coded.
func TestConfiguredCommitterIsTheOnlyCommitter(t *testing.T) {
	a := open(t)
	onChain, err := a.Committer(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(onChain, a.CommitterAddress()) {
		t.Fatalf("on-chain committer %s does not match the configured key's address %s",
			onChain, a.CommitterAddress())
	}
}

// Q14: commit stores (epoch, root) and getRoot reads it back.
func TestCommitAndGetRoot(t *testing.T) {
	a := open(t)
	ctx := context.Background()
	epoch := freeEpoch(t, a)
	r := root("commit-and-get")
	if err := a.Commit(ctx, epoch, r); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	got, err := a.GetRoot(ctx, epoch)
	if err != nil {
		t.Fatalf("GetRoot: %v", err)
	}
	if got != r {
		t.Fatalf("anchored root = %x, want %x", got, r)
	}
}

// Q14: commit rejects overwriting an existing epoch.
func TestCommitRejectsOverwrite(t *testing.T) {
	a := open(t)
	ctx := context.Background()
	epoch := freeEpoch(t, a)
	if err := a.Commit(ctx, epoch, root("first")); err != nil {
		t.Fatalf("first commit: %v", err)
	}
	if err := a.Commit(ctx, epoch, root("second")); err == nil {
		t.Fatal("the contract accepted an overwrite of an anchored epoch")
	}
	got, err := a.GetRoot(ctx, epoch)
	if err != nil {
		t.Fatal(err)
	}
	if got != root("first") {
		t.Fatal("the anchored root changed despite the rejected overwrite")
	}
}

func TestUnanchoredEpochIsReported(t *testing.T) {
	a := open(t)
	epoch := freeEpoch(t, a)
	if _, err := a.GetRoot(context.Background(), epoch); err != blockchain.ErrNotAnchored {
		t.Fatalf("unanchored epoch returned %v", err)
	}
}

// R-EPO-05: on-chain bytes written per epoch are constant.
func TestOnChainPayloadIsConstant(t *testing.T) {
	if blockchain.OnChainPayloadBytes != 32 {
		t.Fatalf("on-chain payload = %d bytes", blockchain.OnChainPayloadBytes)
	}
}

// freeEpoch returns an epoch number that is not yet anchored, so tests can be
// re-run against the same chain instance.
func freeEpoch(t *testing.T, a *blockchain.Anchor) uint64 {
	t.Helper()
	for e := uint64(1_000_000); e < 1_001_000; e++ {
		if _, err := a.GetRoot(context.Background(), e); err == blockchain.ErrNotAnchored {
			return e
		}
	}
	t.Fatal("no free epoch found on the chain")
	return 0
}

// Q46: startup fails fast when the Hardhat node is unreachable.
func TestOpenFailsFastOnUnreachableChain(t *testing.T) {
	_, err := blockchain.Open(context.Background(), "http://127.0.0.1:1",
		31337, "0x"+strings.Repeat("1", 40), strings.Repeat("ab", 32))
	if err == nil {
		t.Fatal("Open succeeded against an unreachable JSON-RPC endpoint")
	}
}

// Q42: the committer key must be a valid key; it is never defaulted.
func TestOpenRejectsAnInvalidCommitterKey(t *testing.T) {
	addr := os.Getenv(config.EnvContractAddress)
	if addr == "" {
		t.Skipf("%s is not set", config.EnvContractAddress)
	}
	rpc := os.Getenv(config.EnvEthRPCURL)
	if rpc == "" {
		rpc = config.DefaultEthRPCURL
	}
	if _, err := blockchain.Open(context.Background(), rpc, 31337, addr, "zz"); err == nil {
		t.Fatal("Open accepted a malformed committer key")
	}
}
