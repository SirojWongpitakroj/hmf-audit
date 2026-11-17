# EVSEB — Go reproduction

Go reproduction of EVSEB (Fugkeaw et al., IEEE Access 13, 2025). The spec is `REQUIREMENTS.md`.
Progress is in `TRACKER.md`, and decisions and open questions are in `QUESTIONS.md`.

- Go 1.27 with go-ethereum v1.12.2. Build with `CGO_ENABLED=0`, as `../dht-backed` does
  (go-ethereum's cgo path needs a 64-bit C compiler).
- Solidity 0.8.24 / Hardhat 2.29.1 / ethers 6 for the `VerifyAccess` and `VerifyIntegrity`
  contracts, following `../delia` and `../dht-backed`.

## Build and test

```
npm install
npx hardhat compile && npx hardhat test      # contract tests on Hardhat's in-process chain
npm run export                                # after changing a contract: refresh the ABI/bytecode Go embeds

export CGO_ENABLED=0
go test ./...                                 # everything, with the in-memory contract backend

# Same suite, plus the Ethereum backend, against a local chain:
npx hardhat node                              # separate terminal
export EVSEB_ETH_RPC_URL=http://127.0.0.1:8545
export EVSEB_EVM_PRIVATE_KEY_HEX=<account #0 key printed by `hardhat node`>
go test -p 1 ./...                            # -p 1: all test packages send from one account
```

## Run

```
go run ./cmd/evseb -mode demo [-chain hardhat]    # Phase 1-6 on a small synthetic dataset
go run ./cmd/evseb -mode bench [-chain hardhat]   # Fig. 4-11 and Table 3 (paper parameters by default)
go run ./cmd/evseb -h                             # all flags
```

`-chain` selects the contract backend for the demo and for Fig. 4–9. Fig. 10–11 and Table 3
always use the chain named by `EVSEB_ETH_RPC_URL` / `EVSEB_EVM_PRIVATE_KEY_HEX`, and are skipped
(with a notice) when those are unset. The corpus is a deterministic synthetic one. The paper does
not specify its original corpus, and none is claimed here.

The shared persistence-inclusive fair benchmark uses PostgreSQL for encrypted
cloud logs and stored-tree authentication nodes, and Cassandra for the
persistent `H(LID) -> hLID` mapping. Cassandra supplies hash-partitioned
key-value persistence here; it is not presented as a Kademlia/IPFS DHT or as
having O(log(peers)) overlay-routing cost.

## How the paper's inconsistencies are resolved

EVSEB's published construction contains several internal inconsistencies and underspecified
steps. None of the choices below is an optimization or an extension of EVSEB. Each one resolves
a specific inconsistency or gap in the paper, as ruled on 2026-09-19 (`QUESTIONS.md`), and
`REQUIREMENTS.md` marks each at the requirement it touches.

- **Contract-side token decryption (Q1).** The paper has the `VerifyAccess` contract decrypt
  Tokenu with the secret Kauth. A public EVM chain can't keep a key secret, so the two
  requirements are incompatible. Resolution: Tokenu is decrypted off-chain, and the contract
  checks the resulting claims (H(UserID), Permissions, Texp, H(LType)) against the user's
  on-chain policy record. The contract can't read that record's `CTPolicy` (Eq. 6), so it
  checks against C = SHA-256 of the same Eq. 5 Policy bytes (Q11). **C is a minimal EVM
  compatibility mechanism, not an EVSEB-defined construction.** It hides the Eq. 5 fields
  only in stored contract state. `verifyAccess` arguments sent as a transaction are public
  calldata, so no policy confidentiality is claimed beyond that.
- **Token generation (Q6, B7 rejected).** Step 3.2 has to recover plaintext UserID and
  Permissions from a policy that holds only their hashes (Eq. 5), which is impossible.
  Resolution: the on-chain policy stays exactly Eq. 5–6. Tokenu (Eq. 7) is issued by the
  trusted registration authority while it still holds the plaintext registration inputs.
- **Multi-keyword queries (Q6, B9, Q12).** Eq. 20's query carries one Tw, but AND/OR (Eq.
  25–26) need two or more operand sets. Resolution: Tw is read as a list/set of trapdoors
  combined by `Q.boolean_operator`. This is a reading of the paper's query, not a different
  search algorithm. NOT (Eq. 27) is unary: it takes exactly one trapdoor, and a multi-keyword
  NOT is rejected as unsupported.
- **"No matching records found" (Q12).** It follows from the Boolean definitions. AND returns
  it when any keyword has no candidate, and OR (or no operator) when every keyword has none.
  NOT never returns it: if its keyword has no candidate, the result is Lall.
- **Target node (Q8).** Eq. 22–24 name a single `targetNode`, but the tree has several
  sub-nodes per log type. Resolution: every sub-node under the LType's primary node gets the
  Bloom test, negatives are skipped (Eq. 16 prose), and positives are unioned. Lall for NOT is
  all LIDs under that primary node.
- **Integrity root (Q9).** Alg. 4.1 compares a root recomputed over only the candidates with
  the on-chain root R, which covers every hLID (Fig. 3). The two can't be equal for a partial
  result. Resolution: each candidate's live hLID is verified by its quaternary Merkle proof
  path against R (Theorem 2's wording). No per-query root is anchored.
- **Level-0 tracing (Q10).** Alg. 4.2's loop over every leaf conflicts with the Fig. 9
  description ("only drills down into the relevant branches"). Resolution: at Level 0 only the
  leaves under the affected level-1 branches are inspected.

On-chain access logging, which the paper claims but never specifies, is not implemented.

## Experiment settings the paper leaves unstated (Q12)

These are prototype choices, not EVSEB statements. Each one is a flag and is printed in the
bench header.

- `-query-op` (default `AND`): the Fig. 6/7 Boolean operator. **Not specified by EVSEB.**
- `-fig6-file-kb` (default 3200) is the paper's "fixed file size". `-fig6-files` (default 30)
  is the number of files, which is **unspecified by the paper**.
- `-file-bytes` (default 1024): the per-file size for Fig. 7–9, which is unstated in the paper.
- Fig. 10/11 on-chain: the N `verifyAccess` requests are submitted as one workload. No send
  waits for an earlier one to be mined. TPS = N / wall time from the first send to the last
  receipt, and latency is per request, send to receipt. `blocks=` reports how many blocks the
  workload landed in. Hardhat's default automine mines every transaction on arrival, so to
  let requests share blocks, run the node with interval mining. The application's normal
  Allow/Deny path uses `eth_call`, a separate implementation choice.
- Table 3: gas units and ETH at the local chain's effective gas price. No USD column is
  computed, because the paper's USD figures imply inconsistent gas prices.
