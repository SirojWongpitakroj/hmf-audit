package chain

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestDialRejectsMalformedURL confirms Dial actually validates its input
// rather than silently succeeding on garbage.
func TestDialRejectsMalformedURL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := Dial(ctx, "not-a-url"); err == nil {
		t.Fatal("expected an error dialing a malformed URL")
	}
}

// TestDialConnectsToRealEVM starts a real Hardhat local-EVM node (the same
// one contracts/HMSC.sol is developed/tested against, M5) as a subprocess,
// connects to it through this package's Dial (the actual Go Ethereum
// client, not a Go-simulated ledger, SRS Section 26 item 18), and fetches
// the current block number — demonstrating SC-003's "off-chain Go code
// reaches the contract only through a Go Ethereum client library" for the
// connectivity layer itself. HMSC.sol's actual protocol behavior is
// exercised by the Hardhat/JS suite (test/*.test.js); this test only proves
// the Go-side connection works against a real node.
//
// The node process is launched by running `node` directly on Hardhat's own
// CLI entry script (node_modules/hardhat/internal/cli/cli.js) rather than
// through `npx`/`npx.cmd` — on Windows, npx.cmd is a batch script that
// CreateProcess cannot execute directly without an intermediate cmd.exe,
// which then makes the launched node.exe a grandchild process that
// Process.Kill cannot reach cleanly (observed: an orphaned node.exe kept
// listening on the test port after the test exited). Invoking `node
// cli.js` directly means the node.exe this test starts IS the process
// Process.Kill terminates — no intermediate shell layer, no orphan.
func TestDialConnectsToRealEVM(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess-based EVM integration test in -short mode")
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not determine test file location")
	}
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..") // systems/delia
	cliScript := filepath.Join(repoRoot, "node_modules", "hardhat", "internal", "cli", "cli.js")

	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available on PATH; skipping real-EVM integration test")
	}

	const port = "8598" // fixed test-only port, distinct from hardhat.config.js's default 8545
	cmd := exec.Command(nodePath, cliScript, "node", "--port", port, "--hostname", "127.0.0.1")
	cmd.Dir = repoRoot
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start hardhat node: %v", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	url := "http://127.0.0.1:" + port
	deadline := time.Now().Add(30 * time.Second)
	var client *Client
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		c, dialErr := Dial(ctx, url)
		if dialErr == nil {
			if _, bnErr := c.Raw().BlockNumber(ctx); bnErr == nil {
				client = c
				cancel()
				break
			}
			c.Close()
		}
		cancel()
		time.Sleep(500 * time.Millisecond)
	}
	if client == nil {
		t.Fatal("could not connect to the Hardhat node within the deadline")
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	bn, err := client.Raw().BlockNumber(ctx)
	if err != nil {
		t.Fatalf("BlockNumber: %v", err)
	}
	if bn < 0 {
		t.Fatalf("unexpected block number %d", bn)
	}

	chainID, err := client.Raw().ChainID(ctx)
	if err != nil {
		t.Fatalf("ChainID: %v", err)
	}
	if chainID.Int64() != 31337 {
		t.Fatalf("got chain id %d, want 31337 (hardhat.config.js)", chainID.Int64())
	}
}
