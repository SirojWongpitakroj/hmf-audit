// Package blockchain anchors each epoch's root-of-roots on the local EVM chain
// (Q14, Q15, Q38, Q42).
//
// The chain is a real external Hardhat v2.20 process running Solidity 0.8.20;
// it is not replaced by an in-memory Go map.
package blockchain

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"dhtbacked/blockchain/binding"
	"dhtbacked/internal/hashing"
)

// RPCTimeout is the JSON-RPC request timeout: 5 seconds, with no automatic
// retry during benchmark measurement (Q32). Failures are recorded explicitly.
const RPCTimeout = 5 * time.Second

// ErrNotAnchored reports that an epoch holds no on-chain root.
var ErrNotAnchored = errors.New("blockchain: epoch is not anchored")

// RootSource reads an anchored epoch root. Phase 4's step 4 depends on nothing
// more than this (R-VER-05).
type RootSource interface {
	GetRoot(ctx context.Context, epoch uint64) (hashing.Digest, error)
}

// Anchor is the Go client for the EpochAnchor contract, reached over local
// JSON-RPC with abigen-generated bindings (Q42).
type Anchor struct {
	client   *ethclient.Client
	contract *binding.EpochAnchor
	txOpts   *bind.TransactOpts
	address  common.Address
	chainID  *big.Int
}

// Open dials the local JSON-RPC endpoint, verifies the chain id and fails fast
// on a mismatch, loads the committer key, and binds the deployed contract at
// the configured address (Q42, Q46).
func Open(ctx context.Context, rpcURL string, chainID uint64, contractAddress, privateKeyHex string) (*Anchor, error) {
	dialCtx, cancel := context.WithTimeout(ctx, RPCTimeout)
	defer cancel()
	client, err := ethclient.DialContext(dialCtx, rpcURL)
	if err != nil {
		return nil, fmt.Errorf("blockchain: dial %s: %w", rpcURL, err)
	}

	idCtx, cancelID := context.WithTimeout(ctx, RPCTimeout)
	defer cancelID()
	actual, err := client.ChainID(idCtx)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("blockchain: read chain id: %w", err)
	}
	want := new(big.Int).SetUint64(chainID)
	if actual.Cmp(want) != 0 {
		client.Close()
		return nil, fmt.Errorf("blockchain: chain id is %s, want %s", actual, want)
	}

	key, err := crypto.HexToECDSA(trim0x(privateKeyHex))
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("blockchain: committer key: %w", err)
	}
	txOpts, err := bind.NewKeyedTransactorWithChainID(key, want)
	if err != nil {
		client.Close()
		return nil, err
	}

	if !common.IsHexAddress(contractAddress) {
		client.Close()
		return nil, fmt.Errorf("blockchain: %q is not a contract address", contractAddress)
	}
	address := common.HexToAddress(contractAddress)
	codeCtx, cancelCode := context.WithTimeout(ctx, RPCTimeout)
	defer cancelCode()
	code, err := client.CodeAt(codeCtx, address, nil)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("blockchain: read contract code: %w", err)
	}
	if len(code) == 0 {
		client.Close()
		return nil, fmt.Errorf("blockchain: no contract deployed at %s", contractAddress)
	}

	contract, err := binding.NewEpochAnchor(address, client)
	if err != nil {
		client.Close()
		return nil, err
	}
	return &Anchor{client: client, contract: contract, txOpts: txOpts, address: address, chainID: want}, nil
}

func trim0x(s string) string {
	if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		return s[2:]
	}
	return s
}

// ChainID returns the verified chain id.
func (a *Anchor) ChainID() uint64 { return a.chainID.Uint64() }

// Address returns the anchoring contract address.
func (a *Anchor) Address() string { return a.address.Hex() }

// Committer returns the single authorized committer recorded on-chain.
func (a *Anchor) Committer(ctx context.Context) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, RPCTimeout)
	defer cancel()
	addr, err := a.contract.Committer(&bind.CallOpts{Context: callCtx})
	if err != nil {
		return "", err
	}
	return addr.Hex(), nil
}

// CommitterAddress returns the address derived from the configured committer
// key.
func (a *Anchor) CommitterAddress() string { return a.txOpts.From.Hex() }

// OnChainPayloadBytes is the number of bytes written on-chain per epoch: one
// uint256 epoch key and one bytes32 root. It is independent of the number of
// records in the epoch (R-EPO-05).
const OnChainPayloadBytes = 32

// Commit anchors (epoch, R_agg) — and nothing else: no client timestamp and no
// CA signature (Q15). Gas is estimated normally by the client (Q42).
//
// It waits for the transaction receipt so a rejected commit surfaces as an
// error rather than silently.
func (a *Anchor) Commit(ctx context.Context, epoch uint64, root hashing.Digest) error {
	txCtx, cancel := context.WithTimeout(ctx, RPCTimeout)
	defer cancel()
	opts := *a.txOpts
	opts.Context = txCtx
	// Hardhat v2.20 does not implement eth_maxPriorityFeePerGas, so the price
	// is taken from the client's own eth_gasPrice suggestion. The gas *limit*
	// is still estimated normally by the client; no fixed gas value is used
	// (Q42).
	gasPrice, err := a.client.SuggestGasPrice(txCtx)
	if err != nil {
		return fmt.Errorf("blockchain: suggest gas price: %w", err)
	}
	opts.GasPrice = gasPrice
	var r [32]byte
	copy(r[:], root[:])
	tx, err := a.contract.Commit(&opts, new(big.Int).SetUint64(epoch), r)
	if err != nil {
		return fmt.Errorf("blockchain: commit epoch %d: %w", epoch, err)
	}
	receiptCtx, cancelReceipt := context.WithTimeout(ctx, RPCTimeout)
	defer cancelReceipt()
	receipt, err := bind.WaitMined(receiptCtx, a.client, tx)
	if err != nil {
		return fmt.Errorf("blockchain: commit epoch %d: %w", epoch, err)
	}
	if receipt.Status != 1 {
		return fmt.Errorf("blockchain: commit epoch %d reverted", epoch)
	}
	return nil
}

// GetRoot reads the anchored root-of-roots for an epoch (R-VER-05).
func (a *Anchor) GetRoot(ctx context.Context, epoch uint64) (hashing.Digest, error) {
	callCtx, cancel := context.WithTimeout(ctx, RPCTimeout)
	defer cancel()
	v, err := a.contract.GetRoot(&bind.CallOpts{Context: callCtx}, new(big.Int).SetUint64(epoch))
	if err != nil {
		return hashing.Digest{}, fmt.Errorf("blockchain: getRoot(%d): %w", epoch, err)
	}
	var d hashing.Digest
	copy(d[:], v[:])
	if d == (hashing.Digest{}) {
		return d, ErrNotAnchored
	}
	return d, nil
}

// Close releases the JSON-RPC client.
func (a *Anchor) Close() { a.client.Close() }
