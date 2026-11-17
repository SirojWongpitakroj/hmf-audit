package blockchain

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	geth "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"evseb/internal/access"
	"evseb/internal/crypto"
)

// ABI and creation bytecode exported from the Hardhat artifacts by
// scripts/export-bindings.js (`npm run export`).
//
//go:embed contracts/*.abi contracts/*.bin
var contractFS embed.FS

// Environment variables that point the Ethereum backend at a local chain.
// Neither has a default: the key is a secret, and a silent default endpoint
// would hide a missing chain.
const (
	EnvRPCURL     = "EVSEB_ETH_RPC_URL"         // e.g. http://127.0.0.1:8545 (npx hardhat node)
	EnvPrivateKey = "EVSEB_EVM_PRIVATE_KEY_HEX" // deployer/sender key
)

// RPCTimeout bounds each JSON-RPC operation (including waiting for a receipt).
const RPCTimeout = 30 * time.Second

// FromEnv returns the configured endpoint and key, and whether both are set.
func FromEnv() (rpcURL, keyHex string, ok bool) {
	rpcURL, keyHex = os.Getenv(EnvRPCURL), os.Getenv(EnvPrivateKey)
	return rpcURL, keyHex, rpcURL != "" && keyHex != ""
}

// Ethereum is the BlockchainContracts backend on a real EVM chain. Each value
// deploys its own VerifyAccess and VerifyIntegrity contracts. It also acts as
// the off-chain verifier of the Q1 ruling, holding Kauth and Kindex; neither
// key is ever sent to the chain.
type Ethereum struct {
	client        *ethclient.Client
	opts          *bind.TransactOpts
	va, vi        *bind.BoundContract
	kauth, kindex [32]byte

	AccessAddress, IntegrityAddress common.Address
	// Deployment receipts of the two contracts (Table 3 deployment gas).
	DeployVerifyAccess, DeployVerifyIntegrity *types.Receipt
}

// Deploy dials rpcURL, loads the sender key, and deploys both contracts.
func Deploy(rpcURL, privateKeyHex string, kauth, kindex [32]byte) (*Ethereum, error) {
	ctx, cancel := context.WithTimeout(context.Background(), RPCTimeout)
	defer cancel()
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, fmt.Errorf("blockchain: dial %s: %w", rpcURL, err)
	}
	chainID, err := client.ChainID(ctx)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("blockchain: chain id: %w", err)
	}
	key, err := ethcrypto.HexToECDSA(strings.TrimPrefix(privateKeyHex, "0x"))
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("blockchain: sender key: %w", err)
	}
	opts, err := bind.NewKeyedTransactorWithChainID(key, chainID)
	if err != nil {
		client.Close()
		return nil, err
	}
	e := &Ethereum{client: client, opts: opts, kauth: kauth, kindex: kindex}
	if e.AccessAddress, e.va, e.DeployVerifyAccess, err = e.deploy("VerifyAccess"); err != nil {
		client.Close()
		return nil, err
	}
	if e.IntegrityAddress, e.vi, e.DeployVerifyIntegrity, err = e.deploy("VerifyIntegrity"); err != nil {
		client.Close()
		return nil, err
	}
	return e, nil
}

// Close releases the JSON-RPC connection.
func (e *Ethereum) Close() { e.client.Close() }

func loadContract(name string) (abi.ABI, []byte, error) {
	a, err := contractFS.ReadFile("contracts/" + name + ".abi")
	if err != nil {
		return abi.ABI{}, nil, err
	}
	parsed, err := abi.JSON(strings.NewReader(string(a)))
	if err != nil {
		return abi.ABI{}, nil, err
	}
	b, err := contractFS.ReadFile("contracts/" + name + ".bin")
	if err != nil {
		return abi.ABI{}, nil, err
	}
	return parsed, common.FromHex(strings.TrimSpace(string(b))), nil
}

// txOpts returns sender options with a context and a legacy gas price taken
// from eth_gasPrice. The gas limit is estimated by the client.
func (e *Ethereum) txOpts(ctx context.Context) (*bind.TransactOpts, error) {
	gp, err := e.client.SuggestGasPrice(ctx)
	if err != nil {
		return nil, fmt.Errorf("blockchain: gas price: %w", err)
	}
	o := *e.opts
	o.Context = ctx
	o.GasPrice = gp
	return &o, nil
}

func (e *Ethereum) deploy(name string) (common.Address, *bind.BoundContract, *types.Receipt, error) {
	parsed, code, err := loadContract(name)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), RPCTimeout)
	defer cancel()
	o, err := e.txOpts(ctx)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	addr, tx, c, err := bind.DeployContract(o, parsed, code, e.client)
	if err != nil {
		return common.Address{}, nil, nil, fmt.Errorf("blockchain: deploy %s: %w", name, err)
	}
	r, err := e.wait(ctx, tx)
	if err != nil {
		return common.Address{}, nil, nil, fmt.Errorf("blockchain: deploy %s: %w", name, err)
	}
	return addr, c, r, nil
}

func (e *Ethereum) wait(ctx context.Context, tx *types.Transaction) (*types.Receipt, error) {
	r, err := bind.WaitMined(ctx, e.client, tx)
	if err != nil {
		return nil, err
	}
	if r.Status != types.ReceiptStatusSuccessful {
		return r, errors.New("transaction reverted")
	}
	return r, nil
}

func (e *Ethereum) transact(c *bind.BoundContract, method string, args ...any) (*types.Receipt, error) {
	ctx, cancel := context.WithTimeout(context.Background(), RPCTimeout)
	defer cancel()
	o, err := e.txOpts(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := c.Transact(o, method, args...)
	if err != nil {
		return nil, fmt.Errorf("blockchain: %s: %w", method, err)
	}
	r, err := e.wait(ctx, tx)
	if err != nil {
		return r, fmt.Errorf("blockchain: %s: %w", method, err)
	}
	return r, nil
}

func (e *Ethereum) call(c *bind.BoundContract, method string, args ...any) ([]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), RPCTimeout)
	defer cancel()
	var out []any
	if err := c.Call(&bind.CallOpts{Context: ctx}, &out, method, args...); err != nil {
		return nil, fmt.Errorf("blockchain: %s: %w", method, err)
	}
	return out, nil
}

// GrantAccessReceipt sends grantAccess(H(UserID), CTPolicy, SHA-256(Policy)).
// The commitment is computed off-chain by decrypting CTPolicy with Kindex.
func (e *Ethereum) GrantAccessReceipt(userID string, policyCT []byte) (*types.Receipt, error) {
	c, err := commitmentOf(policyCT, e.kindex)
	if err != nil {
		return nil, err
	}
	return e.transact(e.va, "grantAccess", crypto.H([]byte(userID)), policyCT, c)
}

func (e *Ethereum) GrantAccess(userID string, policyCT []byte) error {
	_, err := e.GrantAccessReceipt(userID, policyCT)
	return err
}

// UpdateMerkleRootReceipt sends updateMerkleRoot(R).
func (e *Ethereum) UpdateMerkleRootReceipt(newRoot []byte) (*types.Receipt, error) {
	if len(newRoot) != 32 {
		return nil, fmt.Errorf("blockchain: merkle root must be 32 bytes, got %d", len(newRoot))
	}
	return e.transact(e.vi, "updateMerkleRoot", [32]byte(newRoot))
}

func (e *Ethereum) UpdateMerkleRoot(newRoot []byte) error {
	_, err := e.UpdateMerkleRootReceipt(newRoot)
	return err
}

// VerifyIntegrity reads the anchored root.
func (e *Ethereum) VerifyIntegrity() ([]byte, error) {
	out, err := e.call(e.vi, "verifyIntegrity")
	if err != nil {
		return nil, err
	}
	r := out[0].([32]byte)
	if r == ([32]byte{}) {
		return nil, ErrNoRoot
	}
	return r[:], nil
}

// PolicyRecord reads a user's on-chain record (Blockchain_Retrieve).
func (e *Ethereum) PolicyRecord(userID string) (ct []byte, commitment [32]byte, ok bool, err error) {
	out, err := e.call(e.va, "getPolicy", crypto.H([]byte(userID)))
	if err != nil {
		return nil, commitment, false, err
	}
	return out[0].([]byte), out[1].([32]byte), out[2].(bool), nil
}

// claims is the off-chain half of VerifyAccess. ok is false when the token
// or policy yields no claims (the request is then denied).
func (e *Ethereum) claims(tokenCT []byte) (access.Claims, bool, error) {
	tok, err := access.DecryptToken(tokenCT, e.kauth)
	if err != nil {
		return access.Claims{}, false, nil
	}
	ct, _, exists, err := e.PolicyRecord(tok.UserID)
	if err != nil {
		return access.Claims{}, false, err
	}
	if !exists {
		return access.Claims{}, false, nil
	}
	c, err := access.BuildClaims(tok, ct, e.kindex)
	if err != nil {
		return access.Claims{}, false, nil
	}
	return c, true, nil
}

func claimArgs(c access.Claims, lType string) []any {
	perms := c.Permissions
	if perms == nil {
		perms = []string{}
	}
	return []any{c.HUserID, perms, uint64(c.Texp), c.HLType, lType}
}

// VerifyAccess is Eq. 21: off-chain claims, then the contract's
// verifyAccess evaluated by the node (eth_call).
func (e *Ethereum) VerifyAccess(tokenCT []byte, lType string) (bool, error) {
	c, ok, err := e.claims(tokenCT)
	if err != nil || !ok {
		return false, err
	}
	out, err := e.call(e.va, "verifyAccess", claimArgs(c, lType)...)
	if err != nil {
		return false, err
	}
	return out[0].(bool), nil
}

// VerifyAccessReceipt sends the same verifyAccess as a transaction and waits
// for its receipt (Table 3's verifyAccess gas row). Fig. 10-11 submit their
// workload with SendVerifyAccess / ReceiptWatcher instead.
func (e *Ethereum) VerifyAccessReceipt(tokenCT []byte, lType string) (*types.Receipt, error) {
	c, ok, err := e.claims(tokenCT)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, access.ErrNoClaims
	}
	return e.transact(e.va, "verifyAccess", claimArgs(c, lType)...)
}

// PendingNonce is the sender's next nonce, counting transactions still
// pending.
func (e *Ethereum) PendingNonce() (uint64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), RPCTimeout)
	defer cancel()
	return e.client.PendingNonceAt(ctx, e.opts.From)
}

// SendVerifyAccess submits verifyAccess as a transaction with the given
// nonce and returns once the node has accepted it, without waiting for it to
// be mined. Fig. 10/11 on-chain mode submits its N requests this way so they
// can be pending together (QUESTIONS.md Q12 item 5). The caller assigns
// consecutive nonces.
func (e *Ethereum) SendVerifyAccess(tokenCT []byte, lType string, nonce uint64) (*types.Transaction, error) {
	c, ok, err := e.claims(tokenCT)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, access.ErrNoClaims
	}
	ctx, cancel := context.WithTimeout(context.Background(), RPCTimeout)
	defer cancel()
	o, err := e.txOpts(ctx)
	if err != nil {
		return nil, err
	}
	o.Nonce = new(big.Int).SetUint64(nonce)
	tx, err := e.va.Transact(o, "verifyAccess", claimArgs(c, lType)...)
	if err != nil {
		return nil, fmt.Errorf("blockchain: verifyAccess: %w", err)
	}
	return tx, nil
}

// receiptPoll is the pause between polls while receipts are outstanding. It
// bounds how late a receipt can be observed.
const receiptPoll = time.Millisecond

// ReceiptWatcher records when the receipts of a workload of transactions from
// this sender appear. A sender's transactions are mined in nonce order, so
// only the oldest outstanding one needs checking. It uses no goroutines
// (REQUIREMENTS.md §17): the caller polls.
type ReceiptWatcher struct {
	e        *Ethereum
	txs      []*types.Transaction
	receipts []*types.Receipt
	seen     []time.Time
}

// NewReceiptWatcher starts an empty workload. Transactions must be added in
// nonce order.
func (e *Ethereum) NewReceiptWatcher() *ReceiptWatcher { return &ReceiptWatcher{e: e} }

// Add appends a submitted transaction to the workload.
func (w *ReceiptWatcher) Add(tx *types.Transaction) { w.txs = append(w.txs, tx) }

// Poll records every receipt that is already available, oldest first, and
// stops at the first transaction not yet mined. It never waits for mining,
// so calling it between submissions doesn't hold the next one back. A
// reverted transaction is an error.
func (w *ReceiptWatcher) Poll() error {
	ctx, cancel := context.WithTimeout(context.Background(), RPCTimeout)
	defer cancel()
	for len(w.receipts) < len(w.txs) {
		tx := w.txs[len(w.receipts)]
		r, err := w.e.client.TransactionReceipt(ctx, tx.Hash())
		if errors.Is(err, geth.NotFound) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("blockchain: receipt %s: %w", tx.Hash(), err)
		}
		if r.Status != types.ReceiptStatusSuccessful {
			return fmt.Errorf("blockchain: transaction %s reverted", tx.Hash())
		}
		w.receipts = append(w.receipts, r)
		w.seen = append(w.seen, time.Now())
	}
	return nil
}

// Wait polls until every added transaction is mined, bounded by RPCTimeout,
// and returns the receipts with the time each was first observed.
func (w *ReceiptWatcher) Wait() ([]*types.Receipt, []time.Time, error) {
	deadline := time.Now().Add(RPCTimeout)
	for {
		if err := w.Poll(); err != nil {
			return nil, nil, err
		}
		if len(w.receipts) == len(w.txs) {
			return w.receipts, w.seen, nil
		}
		if time.Now().After(deadline) {
			return nil, nil, fmt.Errorf("blockchain: %d receipts outstanding after %s", len(w.txs)-len(w.receipts), RPCTimeout)
		}
		time.Sleep(receiptPoll)
	}
}

// GasCost is gasUsed x effectiveGasPrice, in wei.
func GasCost(r *types.Receipt) *big.Int {
	return new(big.Int).Mul(new(big.Int).SetUint64(r.GasUsed), r.EffectiveGasPrice)
}
