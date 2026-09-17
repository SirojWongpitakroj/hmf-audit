// Package chain is the Go-side Ethereum client binding package SC-003
// requires: "the reproduction replaces web3js with a Go Ethereum client
// interface... implementation-specific... unconstrained beyond 'a Go
// Ethereum client compatible with the node'" (GAP-50/51, not-needed-now
// (NND)). HMSC's actual contract logic stays entirely in Solidity
// (contracts/HMSC.sol); this package only calls it — it must never
// reimplement HMSC logic in Go (SRS Section 26 item 18) and never stand in
// as a simulated ledger for production use (the Hardhat local-EVM tooling
// in package.json/hardhat.config.js is for contract development/testing
// only, per M5's own documented tooling choice).
//
// This package intentionally stays thin: connectivity plumbing
// (ethclient.Client, already a transitive dependency via the sig package's
// go-ethereum use) plus whatever a caller needs to reach a deployed
// HMSC.sol instance through its ABI. It does not generate or hand-maintain
// abigen bindings for HMSC.sol's specific functions — the contract's ABI
// and semantics are exercised end-to-end by the Hardhat/JS test suite
// against the real compiled bytecode (test/*.test.js), which is the actual
// protocol-behavior verification this milestone needs; Go callers that need
// typed method bindings can generate them with abigen from
// artifacts/contracts/HMSC.sol/HMSC.json once a concrete deployment target
// is chosen, without changing this package.
package chain

import (
	"context"

	"github.com/ethereum/go-ethereum/ethclient"
)

// Client wraps a connection to a Go-Ethereum-compatible JSON-RPC endpoint.
type Client struct {
	eth *ethclient.Client
}

// Dial connects to rpcURL (e.g. a private Geth-compatible PoA node per
// GAP-50, or a local Hardhat node for development). It does not deploy or
// know anything about HMSC.sol.
func Dial(ctx context.Context, rpcURL string) (*Client, error) {
	eth, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, err
	}
	return &Client{eth: eth}, nil
}

// Close releases the underlying RPC connection.
func (c *Client) Close() {
	if c != nil && c.eth != nil {
		c.eth.Close()
	}
}

// Raw returns the underlying go-ethereum client for callers that need
// lower-level access (e.g. building/sending a raw transaction against
// HMSC.sol using its ABI loaded at runtime from the Hardhat build artifact,
// rather than a generated abigen binding).
func (c *Client) Raw() *ethclient.Client {
	if c == nil {
		return nil
	}
	return c.eth
}
