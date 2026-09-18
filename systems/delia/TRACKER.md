# DELIA BUILD TRACKER

**Authority:** `DELIA_REQUIREMENTS.md` in this directory is the governing document. This tracker
is a work breakdown *only*; it never overrides the SRS. Where the two differ, the SRS wins. This
file was seeded on 2026-09-16 directly from the SRS's own **"Developer Handoff (revision 3)"**
section (near the end of the file) and the **"Tests runnable now"** list in §22.

**Status as of 2026-09-18:** M0-M4 are implemented and tested. M5, M6 and all of M7's scoped
bullets are now also implemented and tested (see the checkboxes and Verification log below). M8's
TEST-42 faithfulness review is a continuous item re-run after this pass too (see its note); it
stays unchecked by design (it covers the whole project, not one milestone). See the end-of-run
handoff in the session history for a full summary of this pass. **M5-M7 are DONE** (see the
2026-09-18 tester sign-off directly below).

**Tester sign-off (2026-09-17):** an independent tester agent re-verified every `- [x]` box in
M0-M4 by writing and running its own tests (not just reading the developer's tests/notes) — see
"Tester verification pass" in the Verification log below. All checked boxes held; nothing was
unchecked; no scope creep into M5-M8/blocked territory was found. M0-M4 are DONE.

**Tester sign-off (2026-09-18):** an independent tester agent re-verified every `- [x]` box in
M5, M6 and M7 by writing and running its own tests against the actual claims (not just reading the
developer's own tests/notes) — see "Tester verification pass (2026-09-18)" in the Verification log
below. All checked boxes held; nothing was unchecked; the CONFIRM-008/009 Tier-B correction was
confirmed respected (no `RoleLeader`/`RoleParticipant` anywhere in the tree, only the generic
`RoleSigner`); the `hmsc/chain` real-EVM test was confirmed to leave no orphaned `node.exe`/hardhat
process behind. M5-M7 are DONE.

**Read before working:** `DELIA_REQUIREMENTS.md` §1 (scope), §2 (source-of-truth rules, esp. 2.3
"no unstated optimization" and 2.5 documented deviations DEV-01..20), §2.7 (documented limitations
LIM-01..05), §21 (Go package layout), §26 (explicit prohibitions), and the **"Developer Handoff
(revision 3)"** + **"Blocked by User Decision (revision 3)"** sections at the end of the file.

**Status of the SRS itself:** the document's own header says `Status: DRAFT (not ready for dev)`.
That is true of the document as a whole — most requirements are gated behind OPEN gaps or
unresolved conflicts. **Tier A below is the carve-out the SRS authors explicitly marked
implementable now** ("Open gap? = N" in the §24 traceability matrix). Do not treat the DRAFT
status as blocking Tier A work; do not treat Tier A's existence as clearing anything outside it.

**Local environment note (2026-09-17):** this machine's `gcc` (MinGW 6.3.0) cannot compile 64-bit
cgo output (`cc1.exe: sorry, unimplemented: 64-bit mode not compiled in`), so a plain
`go build ./...` / `go vet ./...` / `go test ./...` fails as soon as any dependency pulls in a
cgo path (observed via `go-ethereum`'s transitive deps once `internal/pki`/`sig` were added).
**Always run Go commands in this repo with `CGO_ENABLED=0`** — the dependency graph already has a
pure-Go path for everything used so far (confirmed: `CGO_ENABLED=0 go build/vet/test ./...` all
pass clean). This is a local toolchain limitation, not a DELIA requirement — do not "fix" it by
adding cgo-avoidance workarounds beyond just setting the env var, and do not read a bare
`go build` failure as a code regression without first retrying with `CGO_ENABLED=0`.

## Binding rules for whoever works this tracker

1. **No unstated optimization** (SRS §2.3, §26). No caching beyond what Eq. (2)/Alg. 1-3
   literally carry forward, no incremental/alternative Merkle structures, no inclusion proofs, no
   deduplication, no batching of record-hash broadcasts, no parallel/goroutine verification, no
   binary-search audit, no alternative leader-election/consensus libraries, no signature
   aggregation, no on-chain Close challenge period, no REST/gRPC/queues, no Go-simulated ledger.
   Read SRS §26 in full before writing HMSC or audit code — it is a 22-item explicit prohibition
   list and TEST-42 checks it.
2. **No defaults, no assumptions.** If you hit a decision the SRS does not make (search it — most
   have already been ruled on as `GAP-NN (D)`), **stop that portion**, leave its box unchecked,
   and add a dated note under "Open gaps found during development" below. Do not guess on
   anything load-bearing; small, clearly-labelled implementation ambiguities (e.g. "which Solidity
   test framework to develop against locally") may get a documented, reversible call per the
   developer agent's own ambiguity rule.
3. **Check a box only when the criterion is actually met and a test demonstrates it.** Leave
   partial work unchecked.
4. **Determinism.** Raw byte encodings, never hex strings, for hashes/keys/GlobalState. Fixed
   field order everywhere per the GAP-73 layout (see M0).
5. **Language mapping** (GO-001, SC-002): all off-chain algorithms/protocols in Go, identical
   algorithms/message-flows/data-structures/asymptotics to the paper. HMSC itself stays a
   **Solidity contract on a real Ethereum-compatible node** — never reimplemented in Go, never a
   mock/simulated ledger, without written user approval (SRS §26 item 18).
6. **Go boundaries** (GO-003, GO-004): no `go` statements, worker pools or memoization inside
   Alg. 1-4 or the Confirm steps. One OS process per DELIA server in the real deployment; an
   in-process multi-node harness is for tests only.

## Package layout (DELIA-GO-002, proposal not paper-mandated — follow unless you find a reason not to)

`hash`, `logcache`, `merkle`, `localstate`, `globalstate`, `lsg`, `auditor`, `hmsc/confirm`,
`hmsc/dispute`, `hmsc/offline`, `hmsc/closure`, `hmsc/chain`, `sig`, `messages`, `contracts/`
(Solidity). No other top-level packages. See SRS §21 for the "must not contain" column per
package — it enumerates several of the §26 prohibitions again in package-specific terms.

---

## M0 — Scaffolding, config, and the frozen encoding/test-vector file

**Requirements:** GO-001, GO-002, GO-004, SYS-001, SYS-002, SYS-003, SYS-005, SYS-006, SYS-007,
DATA-018 (encoding half), CRYPTO-007 (encoding half), NET-033 (framing half)

- [x] Go module + the exact package skeleton above (`internal/` permitted for shared primitives
      not otherwise homed). No off-chain Java/JS component. (GO-001, GO-002)
      — `go.mod` (module `delia`), packages `hash, sig, logcache, merkle, localstate, globalstate,
      lsg, auditor, messages`, plus `internal/{spec,pki,nettransport,config,provision}` and
      `cmd/provision`. `go build ./...` / `go vet ./...` clean. No Java/JS anywhere.
- [x] Process model: one OS process per server, its own TLS endpoint, sequential protocol
      handler; in-process multi-node harness reserved for tests only (GAP-54). (GO-004)
      — `internal/nettransport`: real TLS listener/dial + the GAP-47/73 frame codec; every
      connection's frames funnel into one sequential dispatch goroutine (no worker pool). Tested
      with a real localhost TLS handshake + multi-connection framing (`nettransport_test.go`).
      The `lsg` package tests use a separate in-process synchronous hub, per GAP-54's "tests
      only" carve-out. NOTE: no `cmd/delia-server` binary wires this to LSG/HMSC yet — nothing
      meaningful to serve until GAP-05 (stage 1 broadcast) clears and HMSC (M5+) exists.
- [ ] Deployment has exactly three entity kinds — server (LSG module + HMSC module), auditor,
      ledger node — no coordinator/broker/DB-server/cloud entity. (SYS-001, SYS-003, SYS-005)
      — NOT CHECKED: nothing in the codebase violates this (no coordinator/broker/DB-server/cloud
      package exists), but there is no complete running deployment yet to positively demonstrate
      it against (no `cmd/delia-server`, no `cmd/delia-auditor`, no ledger). Revisit once M5+
      produces real server/auditor binaries.
- [x] Module structure maps onto the three layers: data (LogCacheQueue/LocalState/GlobalState),
      network (LSG/HMSC/ledger protocol), audit (Integrity Audit/Auditor Forensics). (SYS-006)
      — data: `logcache/merkle/localstate/globalstate`; network: `lsg` +
      `internal/{nettransport,pki}`; audit: `auditor`. HMSC's network half (M5+) not yet present.
- [x] mTLS from a private test CA for server-to-server connections; P (participant set) comes
      from experiment config; one secp256k1 key per participant generated at provisioning.
      (SYS-002, GAP-48, GAP-49) — cert/key generation tooling belongs here.
      — `internal/pki` (CA + mTLS leaf certs, TLS 1.2 min, tested with a real mutual handshake and
      an untrusted-CA rejection test); `internal/provision` + `cmd/provision` (keygen + cert
      issuance + `domain.json`, tested end-to-end incl. a CLI smoke run).
- [ ] No separate log-collector/preprocessor/verifier components: "enable" = start the log
      reader that feeds LSG, "close" = stop it, coupled with Init and Close. (SYS-007, DEV-08)
      — PARTIAL, left unchecked: `lsg.LogReader.Start/Stop` implements "enable"/"close" (tested:
      `TestLogReaderStartStopLifecycle`). The "coupled with Init and Close" half cannot be built
      or demonstrated yet — Init is entirely Tier C (GAP-20 OPEN) and Close is M7 (not reached
      this run).
- [x] **Create the normative test-vector / constants file** (GAP-73 implementer delegation —
      noted in the SRS as awaiting final user confirmation but treated as decided/frozen for
      implementation). Freeze in one place, with test vectors:
      - one-byte purpose codes: STATE, CLOSE, RECOVERY, VOTE
      - canonical secp256k1 public-key encoding (used for both signing and the CONFIRM-003
        big-endian key comparison)
      - GlobalState wire/disk layout: `uint64 nu (BE) || uint32 entry_count (BE) || entry_count
        × 32-byte LocalState`, P-order positions frozen at Init
      - signed-bytes layout: `purpose || signer_role || canonical-encoded object` (leave the
        full signer-role enumeration open where SRS marks it open — GAP-86 residual; freeze only
        the roles a Tier-A item actually needs)
      - record hash rule: SHA-256 over the UTF-8 log line, excluding line-ending bytes
      - frame format for off-chain messages: `uint32 frame_length (BE) || uint8 message_type ||
        message-specific fields`, raw bytes not hex (GAP-47; message_type codes delegated here)
      Document this file's own authority note: these are implementer-frozen choices per GAP-73,
      not paper-mandated. (DATA-018, CRYPTO-007, NET-033)
      — `internal/spec` (`spec.go`, `frame.go`, `spec_test.go`): purpose codes, message_type
      codes, GAP-73 GlobalState layout (implemented in `globalstate.Encode/Decode`), signed-bytes
      layout helper, record hash rule (`logcache.RecordHash`), frame codec. Authority note is the
      package doc comment. Cross-package normative vector test included.

- [x] **Tester-filed (2026-09-17), scope-creep check on the M0 box above — RESOLVED 2026-09-18:**
      at verification time, `internal/spec/spec.go` had an *uncommitted* working-tree change (not
      part of any commit on this branch) adding `SignerRole` constants `RoleLeader = 0x01` /
      `RoleParticipant = 0x02`, with a doc comment explicitly citing CONFIRM-008/CONFIRM-009 and
      labelling them "M6 Tier-A scope". CONFIRM-008 and CONFIRM-009 are on M6's own "Do NOT
      implement yet" list (Tier B, GAP-86 residual per the 2026-09-17 tracker correction below) and
      M6 had not been started. Neither constant was referenced anywhere else in the tree.
      — Resolved by reverting the uncommitted diff outright (`git checkout -- internal/spec/spec.go`
      before any other work this session) — with the M6 tracker correction already in place,
      CONFIRM-008/009 stayed Tier B for the whole of this run too, so there was no live
      justification left for those two constants at any point. `internal/spec/spec.go` later gained
      a *different*, narrower constant, `RoleSigner = 0x01` — a single generic signer-role value
      that M6/M7's actual Tier-A Update/Close/Disassociate signature verification needs (it does
      NOT distinguish leader from participant, unlike the reverted pair, so it doesn't reach into
      CONFIRM-008/009's blocked territory). See the M5 commit and the Verification log below.

## M1 — Cryptographic primitives & signing

**Requirements:** CRYPTO-001, CRYPTO-002, CRYPTO-004, CRYPTO-008, CRYPTO-009

- [x] `H(.)` = SHA-256 via `crypto/sha256`, and it is the *only* hash in the system — used at
      every cited call site (record hash, Eq. (2), Alg. 1-3, `H(psi)` in OfflineResolving).
      (CRYPTO-001, CRYPTO-002)
      — `hash.H`; used by `logcache.RecordHash`, `merkle.MHTGen`, `localstate.Compute`,
      `globalstate`, `auditor.LogVerify`. `H(psi)` in OfflineResolving is M7, not reached.
- [x] `sig` package: `Gen/Sign/Vrfy` over secp256k1 ECDSA on a SHA-256 digest. This is an
      implementation decision (paper only permits ECDSA as an example) — record it as such.
      (CRYPTO-004)
      — `sig.Gen/Sign/Vrfy`, documented as IMPL/GAP-27(b) in the package doc, not paper-mandated.
- [x] Ethereum address = the address derived from a participant's one secp256k1 key; no second
      identity mapping. This is what OffList/NewLeader/OnlineMsg "addresses" mean. (CRYPTO-008)
      — `sig.KeyPair.Address` / `sig.AddressFromPublicKey`, tested for consistency between the two
      derivation paths.
- [x] Key generation at provisioning, local private-key storage, no runtime PKI. (CRYPTO-009)
      — `sig.KeyPair.Bytes/Load` round trip; wired into `internal/provision` for actual
      provisioning-time generation + local file storage.
- [ ] Note for later milestones: `Sign`/`Vrfy` callers use the M0 signed-bytes layout
      (`purpose || signer_role || encoding`). **LIM-02**: signatures are deliberately not bound
      to `sid` — do not "fix" this; it is a documented, accepted limitation, not a bug.
      — Left unchecked: informational note for M6+ callers (none exist yet); nothing in the
      current code binds a signature to `sid`, so LIM-02 is not violated so far.

## M2 — Data structures: LogCacheQueue, Merkle/LocalState, GlobalState

**Requirements:** DATA-001..017 (DATA-018 encoding lives in M0)

- [x] `logcache`: `psi_{pi,li}` = ordered sequence of exactly `theta` record-hash digests in
      record order, duplicates kept, no deduplication. `theta` is required configuration with no
      global default — a node started without it refuses to run. (DATA-001..007)
      — `logcache.New` rejects `theta <= 0`; `Append` builds an ordered, duplicate-preserving
      sequence; tested (`TestDuplicatesKeptNoDeduplication`, `TestAppendBatchBoundaries`).
- [x] Every server retains every other server's current-batch LogCacheQueue (domain-wide scope).
      (DATA-006)
      — `logcache.Store` keys batches per `ServerID`; tested directly
      (`TestDomainWideSeparatePerServer`) and via `lsg` nodes each holding every other
      participant's current psi.
- [x] `merkle.MHTGen(psi)`: leaves = the psi digests as-is (not re-hashed), in record order;
      internal node = `SHA256(left || right)` over raw 32-byte digests; odd node count at a
      level duplicates the last node; deterministic. Test vectors: theta=3 →
      `H(H(L1||L2) || H(L3||L3))`; theta=1 → root = the single leaf (no self-hash at the root
      for a single-leaf tree — GAP-74). (DATA-008, DATA-009)
      — `merkle.MHTGen`; both frozen vectors plus theta=4 checked byte-for-byte
      (`merkle_test.go`), determinism and any-leaf-change-alters-root also tested.
- [x] `localstate`: Eq. (2) — `li = 0`: `omega = H(root)` (raw 32-byte root as input); `li > 0`:
      `omega = H(root || omega_prev)` (root first, 64-byte raw concatenation). (DATA-010,
      DATA-011, DATA-012)
      — `localstate.Compute`; TEST-03/TEST-04 vectors, operand-order test.
- [x] `globalstate`: `G_nu` = array of exactly `n` LocalState entries, positions fixed by the P
      order frozen at Init. `G_0` = all n positions set to the all-zero 32-byte sentinel,
      `nu = 0`, never submitted on-chain; the first *generated* state is `G_1`. A server's slot
      stays the zero sentinel until its first completed batch — that sentinel is never treated
      as a produced LocalState (GAP-75, consumed later by Alg. 3 in M4). (DATA-013..017)
      — `globalstate.Zero/GlobalState.Encode/Decode/Equal`; tested (`TestZeroState`,
      `TestEncodeDecodeRoundTrip`, `TestEncodeLayout`, `TestEqualComparesNu`). "Never submitted
      on-chain" has no positive test yet (nothing submits to a chain — M5+); no code path submits
      `G_0` either, so not violated.

## M3 — LSG stage 2, Algorithm 1 (GlobalGen), Algorithm 2 (StateVerify)

**Requirements:** LSG-001, LSG-004, LSG-006..012, ALG1-001..006, ALG2-001..007

**Do NOT implement yet:** LSG-002, LSG-003, LSG-005 (the record-hash broadcast: sender identity,
xi/li fields, out-of-order/missing handling) — blocked by **GAP-05 (OPEN)**. Build LSG stage 2
against an already-populated `psi` (however it got there in a test harness); do not invent the
wire format for stage-1 broadcasts.

- [x] LSG runs only at the network layer (no ledger interaction in this package). A server
      stores its own record hash in its own LogCacheQueue too. (LSG-001, LSG-004)
      — `lsg.LogReader` (no chain/contract imports anywhere in `lsg`); tested
      (`TestLogReaderStoresOwnHashes`).
- [x] Stage 2 trigger: batch `li` full → `MHTGen(psi)` → `Alg1(root, G_{nu-1})` → broadcast
      `(root, G_nu)`. No GlobalState is generated for a partial batch. (LSG-006, LSG-007)
      — `lsg.Node.Propose`; tested (`TestStage2HonestProposalAdoptedByAll`,
      `TestLogReaderNoPartialBatchTrigger`).
- [x] Receiver calls `Alg2` with its **own** locally maintained psi and `G_{nu-1}` — never a
      psi/state supplied by the proposer. Invalid → broadcast failure notification
      `(sender, pi, nu, reason)`, abort this attempt, no dispute invoked, recipients only record
      it. Valid → replace local `G_{nu-1}` with `G_nu` (provisional until HMSC confirmation,
      DEV-07). (LSG-008, LSG-009, LSG-010, LSG-011)
      — `lsg.Node.Receive`; tested (`TestStage2CorruptedProposalRejected`, checks the failure
      notification's exact `(sender, proposer, nu)` fields and that receivers keep `G_{nu-1}`).
- [x] Concurrent-proposal handling: the losing proposer re-runs Alg. 1 against the newly accepted
      state and proposes again; no merge (DEV-07). TEST-43 covers this but is itself blocked by
      GAP-05 (needs the wire format) — build the re-proposal *logic* now, wire it to stage-1 once
      GAP-05 clears.
      — `lsg.Node.Reconcile`; tested (`TestStage2ConcurrentProposalReconciles`: only the losing
      proposer's own entry changes, network converges on one chain). The caller (currently the
      test) supplies which state "won" a conflict, since that ordering is HMSC Confirm's job
      (blocked); ready to wire to a real conflict signal once GAP-05/M6 land.
- [x] `ALG1.GlobalGen(root, G_{nu-1})`: parse n entries, `omega_i = H(root || omega_{i,prev})`,
      copy all other entries unchanged, return `G_nu` (nu = input serial + 1). Apply the Eq. (2)
      `li=0` branch in full — `H(root)`, not `H(root || zero-digest)` (DEV-13, TEST-05b). Caller
      supplies index `i` (the generating server's position in the frozen P order) — it is not
      derivable from the inputs alone. (ALG1-001..006)
      — `globalstate.GlobalGen`; TEST-05/TEST-05b covered, plus out-of-range index rejection.
- [x] `ALG2.StateVerify(psi, root, G_{nu-1}, G_nu)`: recompute `root'` fresh every call (no
      cache); mismatch → return 0 **before** calling GlobalGen; else call `GlobalGen` and compare
      the full canonical encoding of `(nu, entries)` — `nu` is part of the comparison, `sid` is
      not. (ALG2-001..007)
      — `globalstate.StateVerify`; TEST-06/07/08(a,b,c) covered, incl. a call-count spy proving
      GlobalGen is not invoked after a root mismatch.

## M4 — Algorithm 3 / Audit — structural requirements only

**Requirements:** AUDIT-002, AUDIT-003, AUDIT-004, AUDIT-006, AUDIT-007, AUDIT-008, AUDIT-009,
AUDIT-010

**Do NOT implement yet:** AUDIT-001, AUDIT-005, AUDIT-011, AUDIT-012, AUDIT-013 — blocked by
**GAP-81 (CONFLICT)** / residual **GAP-83, GAP-84**. This means: do not build the auditor's
end-to-end retrieval path (fetching `{G_1..G_nu}` from a server, cross-checking anchors against
Ethereum event logs). Build `LogVerify` as a pure function over an in-memory record slice and an
in-memory `{G_1..G_nu}` slice supplied by the caller/test harness.

- [x] Periodic audit trigger with a configured period; on-demand is a test/CLI-only trigger
      (DEV-09). (AUDIT-002)
      — `auditor.Scheduler` (`Start/Stop` periodic + separate `TriggerNow`, passes never overlap);
      tested (`TestSchedulerRunsPeriodically`, `TestSchedulerTriggerNow`, `TestSchedulerNoOverlap`).
- [x] `auditor.LogVerify({records}, {G_1..G_nu}) -> ErrList`, signature matches Alg. 3.
      (AUDIT-003)
      — `auditor.LogVerify(records []Record, theta, index int, states []State) []uint64`.
- [x] `l` initialized once before the outer loop, never reset per `k` (DEV-01); `k++` after each
      `G_k`, `l++` per reconstructed batch, single linear pass, every batch rebuilt exactly once.
      (AUDIT-004, AUDIT-006)
      — verified by instrumentation (`TestLogVerifyInstrumentation`: exactly one `H` per record,
      one `MHTGen` per rebuilt batch, across the whole run).
- [x] Comparison against the printed indentation: after reconstructing up to the current `li`,
      mismatch → add `li` to `ErrList`, then unconditionally resume the chain from the ledger
      value. While a server's slot is still the `G_0` zero sentinel (no completed batch yet), no
      comparison is made (GAP-75). (AUDIT-007)
      — TEST-11/12/13 (`TestLogVerifyValidLogs`, `TestLogVerifyTamperedBatch`,
      `TestLogVerifyDeletedRecord`/`InsertedRecord`/`TrailingPartialIgnored`) and the GAP-75 skip
      (`TestLogVerifySkipsZeroSentinel`).
- [x] Return `ErrList`; empty ⇔ audit passes; it carries only batch numbers, nothing else.
      (AUDIT-008, AUDIT-009)
      — `[]uint64` of batch numbers only; empty-list case tested.
- [x] Single goroutine, no caching, no Merkle proofs/multiproofs/binary search — instrument and
      test this directly (one `H` per record per rebuilt batch, one `MHTGen` per inner
      iteration, zero `go` statements). (AUDIT-010, GO-003) — **TEST-14**
      — `auditor/logverify.go` contains zero `go` statements (code review); call-count
      instrumentation above is TEST-14.

## M5 — HMSC architecture scaffolding + Solidity contract skeleton

**Requirements:** ARCH-001, ARCH-002, ARCH-003, ARCH-005, ARCH-007, ARCH-009, ARCH-010, SC-001,
SC-002, SC-003, SC-004 (Tier-A entry points only), SC-006

**Do NOT implement yet:** ARCH-004, ARCH-006, ARCH-008, ARCH-011 — blocked (residual GAP-82,
Tier B) — this means the on-chain leader-rotation-from-anchor bookkeeping and the extra contract
state it needs stay out of scope here; build the plain instance struct and deployment plumbing
only.

- [x] `Γ` instance representation `{sid, P, G, OffList, NewLeader, Parent, ChildList}` — exactly
      these seven fields as contract storage (extra state, if any, is out of scope until
      ARCH-011 clears). (ARCH-003)
      — `contracts/HMSC.sol`'s `Instance` struct: exactly these seven fields (G realized as its two
      GAP-73 wire-layout components, nu+entries — together they ARE the single G field, not extra
      state), plus one user-approved extra field (`closed`, GAP-45, explicitly named in ARCH-011's
      own text independent of ARCH-011's still-blocked items). Tested (`test/instance.test.js`).
- [x] SCC comprises six *named* protocols (Init, Confirm, Update, DisputeResolving,
      OfflineResolving, Close) but **Confirm has no contract function** — it is off-chain only
      (DEV-15). Init/Update/DisputeResolving/OfflineResolving/Close execute on Ethereum; nothing
      off-chain sends a transaction for state confirmation itself. (ARCH-001, ARCH-002)
      — No Confirm-shaped entry point exists in the contract's ABI (`test/confirmOffChain.test.js`
      reviews the deployed ABI directly); Update/Close/Disassociate/OnlineMsg/selectNewLeader are
      the only entry points, all on Ethereum. Init and the full DisputeResolving/OfflineResolving
      flows stay unimplemented (Tier C/B), so no entry point for them exists either.
- [x] Multiple independent instances can exist simultaneously without being closed/recreated to
      cooperate (topology from manual config, no automatic partitioning — ARCH-005, ARCH-007).
      — `test/instance.test.js` (`HMSC multiple simultaneous instances`): two independently-created
      instances coexist, neither closed/recreated, no code path partitions or federates them
      automatically (no Federate entry point exists at all this milestone).
- [x] `beta` is required per-instance configuration, no protocol default; a round is
      "one signed round state, then up to beta no-signed round states" as a *concept* here — the
      on-chain enforcement of round boundaries is ARCH-008, deferred. (ARCH-009, ARCH-010)
      — `hmsc/confirm.RoundConfig`/`NewRoundConfig`: rejects beta=0 (no default), independently
      configurable per instance. Tested (`round_test.go`). No on-chain round counter/beta
      enforcement was added to the contract (ARCH-008 stays deferred, per the Do-NOT-implement
      callout).
- [x] Solidity contract, deployed on a **real** Ethereum-compatible node — not a Go ledger
      simulator (SRS §26 item 18 is a hard prohibition). The SRS's own experimental target is a
      private 10-node Geth-compatible PoA network (GAP-50, GAP-78) — that is the eventual
      deployment target for the §23 experiments; for day-to-day development/unit testing of the
      contract itself, choosing a standard local Solidity test environment (e.g. Hardhat/Foundry
      running a real EVM against the same bytecode) is engineering tooling, not a protocol
      deviation — note whichever choice is made and why. (SC-001, SC-002, SC-006)
      — Chose Hardhat 2.29 + Solidity 0.8.24 + a real EVM (Hardhat Network) for local
      development/testing, following the sibling `systems/dht-backed` project's own precedent
      (Hardhat v2, ethers v6, no toolbox plugin beyond ethers/chai-matchers for assertions).
      Documented in `package.json`'s description and `hardhat.config.js`'s header comment. The
      10-node Geth-PoA network stays the eventual §23 experimental deployment target, not built
      this milestone (no experiment harness exists yet). `contracts/HMSC.sol` compiles clean
      (`npx hardhat compile`, `viaIR: true` — a standard solc pipeline option needed because the
      contract's calldata-array-heavy functions overflow legacy codegen's stack, not a
      behavior/protocol change) and all 26 Hardhat tests pass against the real local EVM.
- [x] `hmsc/chain`: Go-side Ethereum client binding package. Contract logic itself stays in
      Solidity; this package only calls it. Client library choice is unconstrained (GAP-51, not
      needed) beyond "a Go Ethereum client compatible with the node." (SC-003)
      — `hmsc/chain.Client`/`Dial`/`Close`/`Raw`, a thin wrapper around go-ethereum's
      `ethclient.Client` (already a transitive dependency via `sig`). No HMSC logic reimplemented
      in Go. Tested against a real EVM: `TestDialConnectsToRealEVM` spawns an actual Hardhat node
      subprocess, dials it, and confirms `BlockNumber`/`ChainID` (31337) against the real running
      node — not a mock. `go.mod`/`go.sum` updated (`go mod tidy`) for ethclient/rpc's additional
      transitive dependencies.
- [x] Contract entry points for the Tier-A protocols only, for now: whatever M6/M7 below actually
      need. Do not pre-build entry points for blocked protocols. (SC-004, scoped)
      — `update`, `close`, `disassociate`, `onlineMsg`, `selectNewLeader`, plus the
      `createInstanceForTesting`/`getInstance` test-harness pair (see M7 below for what each
      enforces). No `init`/`federate`/`disputeMsg`/`offlineMsg`(challenge-window)/`finalizeOffline`
      entry point exists — those protocols stay Tier B/C this milestone.

## M6 — Off-chain state confirmation: leader election & signed/no-signed round flow

**Requirements:** CONFIRM-001, CONFIRM-002, CONFIRM-005, CONFIRM-006, CONFIRM-007, CONFIRM-013,
CONFIRM-014, CONFIRM-016, CONFIRM-017, CONFIRM-018, CONFIRM-020

**Do NOT implement yet:** CONFIRM-003, CONFIRM-004, **CONFIRM-008, CONFIRM-009** (correction,
2026-09-17: these two are Tier **B**, not Tier A — blocked by residual **GAP-86**, the signer-role
enumeration; an earlier version of this tracker wrongly listed them as buildable in the bullet
below, and the developer agent started staging role-byte constants for them before a tester pass
caught it — see the Verification log entry below and `internal/spec/spec.go`'s history),
CONFIRM-010, CONFIRM-011, CONFIRM-012, CONFIRM-015, CONFIRM-019 — blocked
(GAP-81/GAP-82/GAP-24 OPEN/GAP-86). This is a narrower cut than it looks: **TEST-17** and
**TEST-17b** (basic off-chain leader rotation, including OffList-skip) are explicitly listed as
runnable now even though the full CONFIRM-003/004 requirement rows are gated — build the *basic*
election rule those two tests need; do NOT build the SCC on-chain "expected leader derived from
the latest accepted signed-round anchor" mechanic (that part of CONFIRM-003/004 is what's
actually blocked), and do NOT build parent/child upward-propagation recursion (CONFIRM-010/011/012,
GAP-24 OPEN). **Also do not implement the leader's actual `(G_nu, S_leader)` signing/broadcast
step or a recipient's countersignature step** — that's exactly CONFIRM-008/009, and per the SRS's
own "Tests runnable now" list, **TEST-18** (the test that would exercise it) is *not* on that list
either, so there is no concrete Tier-A target for it yet regardless of the ID classification.

- [x] At most one leader per instance at any time; every participant gets equal opportunity
      across rounds. (CONFIRM-001, CONFIRM-002)
      — `hmsc/confirm.NextLeader` returns exactly one key per call (CONFIRM-001 trivially, by
      signature); `TestEqualOpportunityOverFullCycle` confirms every participant leads exactly once
      over `|P|` consecutive rounds with no OffList members (CONFIRM-002).
- [x] **TEST-17**: off-chain-only rotation over `P = {K1 < K2 < K3}` (keys compared as
      fixed-width unsigned big-endian values of the M0 canonical encoding): round 1 leader = min
      key; leader doesn't repeat next round; rotation restarts at the minimum once exhausted →
      `K1, K2, K3, K1` over 4 rounds.
      — `hmsc/confirm.RotationSequence`/`Eligible`/`NextLeader`; `TestRotationSequenceBasic` (exact
      `K1,K2,K3,K1` vector) and `TestRotationSequenceOrderIndependent` (P supplied out of order).
- [x] **TEST-17b** (DEV-11): same rotation, but a participant in the *finalized* on-chain OffList
      is skipped; a merely *pending, unfinalized* OfflineMsg must **not** affect eligibility.
      — `TestRotationSequenceFinalizedOffListSkipped` (K2 finalized-offline → `K1,K3,K1`) and
      `TestRotationSequencePendingOfflineDoesNotExclude` (K2 never passed as finalized → `K1,K2,K3`;
      the function signature itself has no "pending" parameter for that state to leak through).
- [x] `NET-006` message plumbing only: a participant can send its generated `G_nu` to its current
      leader (CONFIRM-007 — a plain message send, no signature semantics). This is legitimate
      Tier-A scaffolding *only* if it stays a dumb message send — do not attach any signing logic
      to the leader's receipt of it; that's CONFIRM-008, still blocked. Leave CONFIRM-005 and
      CONFIRM-013 (which describe properties of the *complete* signed-round-state flow) as
      unchecked design notes, not deliverables, until CONFIRM-008/009 unblock — there is no way to
      demonstrate them without the signing step they describe.
      — `messages.StateToLeader`/`DecodeStateToLeader`, `spec.MsgStateToLeader`: encodes/decodes a
      bare `GlobalState`, no signature field, no verification on receipt. Tested
      (`TestStateToLeaderEncodeDecode`, `TestStateToLeaderOverFrame`,
      `TestDecodeStateToLeaderRejectsShortInput`). CONFIRM-005/013 remain undemonstrated design
      notes, exactly as scoped — not implemented, not checked off as their own claim.
- [x] No-signed round states are relayed/cached off-chain only and are **never** accepted
      on-chain (a leader-only-signed submission to Update must be rejected). Scope this to a
      single instance only — general upward relay through a hierarchy (CONFIRM-014's "recursively")
      is GAP-24 OPEN territory. (CONFIRM-018 single-instance slice) — **TEST-29**
      — `contracts/HMSC.sol`'s `update()`: `require(signers.length >= 2, ...)` rejects a
      leader-only (one-signature) submission outright — a deliberately narrow TEST-29-only proxy
      for "not a signed round state", documented in the contract as NOT the blocked UPDATE-006
      exact-count rule. Tested (`test/update.test.js`, "no-signed-state rejection" describe block,
      both the rejection and the "more than one signature accepted" cases).
- [x] Desync recovery: a receiver whose local latest serial ≠ `nu' - 1` requests
      `(fromSerial, toSerial)` from its **direct** leader only (no recursion to higher leaders),
      verifies returned signatures and serial continuity before adopting. (CONFIRM-016,
      CONFIRM-017) — **TEST-21**
      — `hmsc/confirm.NeedsSync`/`VerifySyncResponse`: no parent-leader concept anywhere in the
      type (structurally no recursion possible). Tested: gap detection at various offsets, valid
      continuous-range acceptance, serial-gap rejection, bad-signature rejection, incomplete-range
      rejection (`sync_test.go`).
- [x] Manual dispute-trigger entry point: any participant can invoke it explicitly; no automatic
      detection thresholds (GAP-40). (CONFIRM-020)
      — `hmsc/dispute.TriggerDispute`/`NewTriggerDispute`: callable with any participant's key, no
      leader-only restriction; no periodic/background trigger exists anywhere in the package (code
      review — the only producer of a `TriggerDispute` value is the explicit constructor). Tested
      (`TestTriggerDisputeAnyParticipant`).

## M7 — State update rules (partial), dispute/offline/online/close basics

**Requirements:** UPDATE-001, UPDATE-004, UPDATE-005 (+OFFLINE-010), UPDATE-007, DISPUTE-005,
DISPUTE-009, DISPUTE-012, OFFLINE-005, OFFLINE-006, OFFLINE-010, OFFLINE-011..015, CLOSE-001..008

**Do NOT implement yet:** UPDATE-002/003/006/008/009..012 (signer-count/leader-derivation/Rule-4
mechanics — GAP-81/82/87), DISPUTE-001..004/006..008/010/011/013..019 (full dispute flow —
GAP-88 residual, except the two Tier-A pieces below), OFFLINE-001..004/007..009/016 (the
offline-detection/challenge-window mechanics — GAP-85 residual). Build exactly the Tier-A slices
listed per test below; each is deliberately narrower than its protocol section.

- [x] **TEST-23** Rule 1: reject an Update submission with `nu <= nu*` (`nu*` = latest signed
      state stored on-chain in the root instance, GAP-66). (UPDATE-004)
      — `HMSC.sol`'s `update()`: `require(nu > inst.nu, ...)`. Hierarchy/federation stays out of
      scope this milestone, so every instance here is treated as its own root — `nu*` resolves to
      this instance's own currently stored `nu` (documented in the function's doc comment).
      Tested: `test/update.test.js` (`nu == nu*` and `nu < nu*` both rejected; `nu > nu*` accepted).
- [x] **TEST-24** / **TEST-36** Rule 2: reject an Update that changes the LocalState entry of a
      participant currently in OffList (frozen entry). (UPDATE-005, OFFLINE-010)
      — `update()`: for each OffList address, looks up its frozen position in `P` and requires the
      submitted entry at that position to be unchanged. Tested: entry-changed rejected,
      entry-unchanged (with a different member's entry changed) accepted.
- [x] **TEST-26** Rule 3 (validity slice only): reject an Update if any included signature fails
      `Vrfy`. Do not yet build the exact-signer-count / leader-presence sub-checks (UPDATE-006,
      UPDATE-008 — blocked). (UPDATE-007)
      — `update()`'s `_verifySigners`: `ecrecover` per signature, checked against the claimed
      signer and P membership; no signer-count or leader-presence check anywhere. Tested: one
      corrupted signature rejected; a signature from a non-P-member rejected.
- [x] **TEST-29** reject a state on Update that carries only the leader's signature (a no-signed
      state). (UPDATE-001, CONFIRM-018 — cross-reference with M6)
      — see M6 above (same `update()` check); cross-referenced here per the tracker's own note.
- [x] **TEST-30** `Alg4.CheckpointSelection`, no-children case only (`ChildList == null`):
      implement the **prose semantics** (DEV-02), not the literal off-by-one listing — checkpoint
      = the instance's latest signed round state. (DISPUTE-005, DISPUTE-009)
      — `hmsc/dispute.CheckpointSelection`: returns the maximum serial in the retained signed-state
      history. Tested with two illustrative vectors (contiguous and non-contiguous/unordered
      input) — the paper's own Fig. 9 diagram content isn't available as extractable text, so exact
      G_4/G_3 figure reproduction wasn't possible; the vectors instead directly test the stated
      prose rule ("the checkpoint is Γx's latest signed round state").
- [x] **TEST-47** new-leader selection: `index = uint(blockhash(block.number - 1)) mod
      eligible-candidate-count`; candidates exclude the removed participant and finalized
      OffList members (pending, unfinalized OfflineMsgs do not exclude). (DISPUTE-012)
      — `HMSC.sol`'s `selectNewLeader(address[] candidates)`: the isolated block-hash-mod-count
      pick only; candidate-list filtering is `hmsc/confirm.Eligible` (Go-side, shared with
      TEST-17/17b). Tested against a real Hardhat EVM block (`test/selectNewLeader.test.js`),
      including a no-candidates revert case.
- [x] **TEST-48** OfflineMsg signer-set check in isolation: exactly `n-1` valid signatures
      (excluding `pi`) accepted, `n-2` rejected; `H(psi)` computed over `pi`'s **active**
      LogCacheQueue including a partial batch (DEV-03, DEV-17). (OFFLINE-005, OFFLINE-006)
      — `hmsc/offline.CheckOfflineMsgSigners`/`HashActiveQueue`. Tested: exactly n-1 (excluding pi)
      accepted, n-2 rejected, a duplicate signer rejected, a bad signature rejected;
      `HashActiveQueue` shown deterministic, order-sensitive, and not truncating a partial batch.
      Deliberately isolated from the still-blocked challenge-window mechanics (GAP-85) — no
      startBlock/cancel/FinalizeOffline code exists.
- [x] **TEST-37** online transition: `OnlineMsg` accepted **without** requiring `S_pi`;
      acceptance removes `pi` from OffList; `pi`'s GlobalState and current-batch LogCacheQueue
      are synced from its **direct leader only** (not "other servers in the domain" — DEV-12);
      `pi`'s next batch chains from its frozen LocalState; `pi`'s log reader stays paused for the
      whole time it was offline (EXPERIMENT-ONLY, GAP-69/LIM-01), so no backlog-ingestion logic
      is needed or wanted. (OFFLINE-011..015)
      — On-chain half: `HMSC.sol`'s `onlineMsg()` — accepted without `S_pi`, removes `pi` from
      OffList (`test/online.test.js`). Off-chain half: `hmsc/offline.RejoinSyncRequest/Response`/
      `VerifyRejoinSync` — syncs GlobalState (serial continuity + leader signature) and
      LogCacheQueue data from the direct leader only, no parent-leader concept in the type (same
      shape as M6's desync recovery). Tested (`offline_test.go`). No backlog-ingestion logic was
      added (nothing to add — EXPERIMENT-ONLY/GAP-69/LIM-01 licenses skipping it entirely).
- [x] **TEST-38** Close: a signed `(G_nu | Close)` via `CloseMsg` is accepted only with purpose
      `CLOSE` signatures from **every** member of the instance's P, offline members included — a
      single missing offline signer blocks the whole Close (LIM-03, intentional). Subsequent
      Update on a closed instance is rejected; closed data stays readable (no cleanup).
      (CLOSE-001..005)
      — `HMSC.sol`'s `close()`: requires `signers.length == P.length` and every P member covered by
      a valid purpose-CLOSE signature (a missing signer, or a signature under purpose STATE,
      rejected). `update()` rejects on a closed instance. `getInstance` still reads a closed
      instance's data back correctly. Tested (`test/close.test.js`, plus the "on a closed
      instance" describe block in `test/update.test.js`).
      — Note (self-review, 2026-09-18): `close()` never inspects `ChildList` at all — whether a
      CloseMsg may close an instance whose ChildList is non-null is still OPEN (GAP-89, "User
      decision required: YES") and no TEST-38/44 vector exercises that case. Flagged explicitly in
      the contract's doc comment so the omission reads as genuinely undecided, not as a silent
      resolution either way.
- [x] **TEST-44** Close rejected outright while the instance has a non-null Parent (disassociate
      first). (CLOSE-004, CLOSE-009)
      — `close()`: `require(inst.parent == bytes32(0), ...)`. Tested (`test/close.test.js`, "HMSC
      close with a parent").
- [x] **TEST-39** / **TEST-40** Disassociation: parent `Γa` with children `Γ1, Γ2` closes once
      both child leaders submit signed close states (purpose CLOSE); each direct child's Parent
      resets to null, children stay open; `DisassociateMsg` rejected while `Γa.Parent != null`
      (LIM-04, no nested disassociation); a corrupted child signature leaves `Γa` open.
      (CLOSE-006, CLOSE-007)
      — `HMSC.sol`'s `disassociate()`: one purpose-CLOSE signature per direct child, verified
      against a member of that child's own stored P (determining that signer is specifically that
      child's *leader* is out of scope, same GAP-60/82 simplification used elsewhere this
      milestone — documented in the function's doc comment); every direct child must be covered;
      each covered child's Parent reset to null; the parent's own Parent must already be null
      (LIM-04). Tested (`test/disassociate.test.js`): both children sign → parent closes, children
      stay open with Parent reset; nested-disassociation rejected; one corrupted child signature
      leaves the parent open.
- [x] Audit-closure coupling: stopping the log reader/LSG is coupled with invoking Close; no
      separate module teardown exists. (CLOSE-008)
      — `hmsc/closure.CloseInstance`: stops the reader first, unconditionally, then runs the
      caller's own Close invocation; no separate teardown step exists even if that invocation
      fails. Tested against a real `lsg.LogReader` (not just a mock), confirming no further records
      are consumed after closure (`closure_test.go`).
- [x] **TEST-45** every accepted Update emits exactly one event carrying the full signed state;
      the contract keeps **no** on-chain history array (SC-007 scope: event emission only, not
      the auditor's cross-check consumer — that's still AUDIT-012, blocked).
      — `HMSC.sol`'s `UpdateAccepted` event (sid, nu, entries, signers, v, r, s), emitted exactly
      once per accepted `update()` call; `getInstance` only ever exposes the latest (nu, entries) —
      no history array. Tested: 3 accepted Updates → exactly 3 events, correct nu per event
      (`test/update.test.js`). This checks off only the narrow TEST-45 slice of UPDATE-011 (itself
      Tier C/blocked in full, GAP-81) — the full "if all four State Update Rules hold" precondition
      is not built (Rule 3 count/Rule 4 stay unimplemented), consistent with this milestone's own
      narrower `update()` acceptance condition.

## M8 — Faithfulness review (run continuously; formal pass before declaring a milestone done)

- [ ] **TEST-42**: walk SRS §26's 22-item prohibition list against the actual diff. Nothing on
      that list should be findable anywhere in the implementation. Do this after each milestone,
      not just once at the end.
      — Left unchecked (this item covers the whole project, not one pass): a full 22-item review
      was done against the M0-M4 diff (`hash, sig, logcache, merkle, localstate, globalstate, lsg,
      auditor, messages, internal/*, cmd/provision`) on 2026-09-17 — no caching beyond Eq.(2)/
      Alg.1-3, no alternative Merkle structures/proofs, no dedup, no record-hash batching, no
      goroutines inside Alg.1-4 (only I/O concurrency in `lsg.LogReader`/`nettransport`/
      `auditor.Scheduler`, all sequential-handler/no-overlap by design), no binary-search audit, no
      alternative election/consensus libs, no signature aggregation, no on-chain Close challenge,
      no REST/gRPC/queues as DELIA wire messages (JSON is used only for the local `internal/config`
      deployment file, documented as tooling not a DELIA requirement), no Go-simulated ledger (none
      built yet). Re-run this walk after every later milestone before checking the box.
      — 2026-09-18 re-run against the M5/M6/M7 diff (`contracts/HMSC.sol`, `hmsc/confirm`,
      `hmsc/dispute`, `hmsc/offline`, `hmsc/chain`, `hmsc/closure`, `internal/spec` RoleSigner
      addition, `messages.StateToLeader`, `test/*.js`, `package.json`/`hardhat.config.js`): no
      caching beyond what's licensed; no alternative Merkle/election/consensus (leader rotation is
      exactly the paper's min-key rule, `selectNewLeader` uses randomness only for NewLeader
      selection per item 10's own carve-out); no signature aggregation/BLS/Schnorr/Ed25519 (plain
      secp256k1 ECDSA + `ecrecover`, one signature verified at a time); no on-chain Close challenge
      period (`close()` takes effect in one transaction); no cleanup/pruning after Close (closed
      data stays readable, tested); no extra cryptographic fields beyond the already-frozen
      purpose/role bytes (the new `RoleSigner` constant is a labelled IMPL addition, GAP-86,
      exactly like the pattern item 17 permits); the Solidity contract is the real, only HMSC
      logic — `hmsc/chain` is a thin RPC wrapper that reimplements nothing, and the Hardhat local
      EVM used for its own tests is a real EVM executing the real compiled bytecode, not a
      Go-simulated ledger (item 18); no REST/gRPC/queues added as DELIA wire messages (`package.json`
      is npm tooling config, not a DELIA message format); no goroutines inside `HMSC.sol`,
      `hmsc/confirm`, `hmsc/dispute`, `hmsc/offline`, or the Solidity contract itself (the only new
      goroutines are test-harness orchestration in `hmsc/chain/chain_test.go` and
      `hmsc/closure/closure_test.go`, not inside any algorithm/protocol step). No new scope-creep
      issue found. Two small, documented implementation calls made this pass (both reversible, both
      noted where they're made): (a) `RoleSigner`, one generic signer-role constant for
      Update/Close/Disassociate signatures that doesn't distinguish leader from participant; (b) a
      resync/rejoin state entry's signature is verified under the already-frozen
      PurposeSTATE/RoleSigner pair rather than inventing a new purpose, since the SRS doesn't pin
      one down for that specific relay. `close()`'s silence on GAP-89's ChildList question is
      flagged in its own doc comment (see the M7 TEST-38 note above) rather than resolved either
      way.

---

## Explicitly BLOCKED — do not implement, do not "helpfully" unblock

Reproduced from the SRS's own "Developer Handoff (revision 3)" so this tracker doesn't silently
drift from it. If SRS gets a revision 4, re-sync this section from the new handoff before trusting
it.

**Tier B — blocked only by revision-3 residuals (GAP-82, 83, 84, 85, 86, 88, 89):**
AUDIT-013; ARCH-006; INIT-001, 008, 009; CONFIRM-008, 009, 015; UPDATE-007; DISPUTE-010, 011, 015,
019; OFFLINE-007, 008, 009, 016; CLOSE-001, 003, 004, 005, 006, 007, 009; CRYPTO-003, 005;
NET-007, 008, 012, 017, 018, 021, 022, 023, 025, 026, 028, 029, 030, 036; SC-005; EXP-017, 022.

*(Note: some of these IDs — e.g. UPDATE-007, CLOSE-001/003..007/009, OFFLINE-007/008/009 —
overlap with Tier-A test coverage above only for the narrow slice a specific runnable test
exercises. Where M6/M7 above carve out a Tier-A slice of one of these IDs, that slice is fine;
everything else on the ID stays blocked.)*

**Tier C — blocked by an OPEN gap or unresolved conflict (GAP-05, 20, 24, 81, 87):**
SYS-004, 008; LSG-002, 003, 005; AUDIT-001, 005, 011, 012; ARCH-004, 008, 011; INIT-002..007;
CONFIRM-003, 004, 010, 011, 012, 019; UPDATE-002, 003, 006, 008, 011, 012; DISPUTE-017; CRYPTO-006,
007; NET-003, 009, 010, 014, 015, 016, 032, 033, 037; SC-007; PERSIST-003, 005; EXP-015, 018, 019,
020, 021.

**Open gaps (never resolve unilaterally — these need the user):**
GAP-05 (LSG record-hash broadcast payload/ordering), GAP-20 (InitMsg aggregation + sid
derivation), GAP-24 (upward propagation: forward-unchanged vs re-sign, what root distributes),
GAP-81 (CONFLICT: every-anchor-on-chain vs periodic submission), GAP-87 (CONFLICT: one signature
satisfying two roles vs role bound into signed bytes), GAP-88 (dispute checkpoint signature/vote
binding details), GAP-82/83/84/85/86/89 (residuals listed in SRS "Blocked by User Decision
(revision 3)" table).

---

## Open gaps found during development

No new protocol gap was hit during M0-M4 (2026-09-17): every choice made was already licensed by
a DECIDED gap (mostly GAP-73's implementer delegation — see `internal/spec`'s package doc for the
specific frozen values: 33-byte compressed secp256k1 public-key encoding, purpose/message_type
byte values, 64 MiB frame size cap). Nothing here needed to go back to the user.

**Development-environment note (not a protocol gap):** this machine's local gcc cannot compile
64-bit cgo output ("sorry, unimplemented: 64-bit mode not compiled in"), which breaks
`go-ethereum/crypto`'s default cgo secp256k1 path. All `go build`/`go vet`/`go test` invocations in
this repo were run with `CGO_ENABLED=0`, which selects go-ethereum's pure-Go secp256k1
implementation instead — same algorithm, no protocol effect, but anyone (including the tester
agent) re-running these commands on this machine needs the same env var or will hit the same
build failure. Worth fixing at the toolchain level eventually, but out of scope for this tracker.

**M5-M7 pass (2026-09-18):** no new OPEN protocol gap was hit; every choice made was either
already licensed by a DECIDED gap or a small, documented, reversible implementation call within
this tracker's own ambiguity-handling rule (recorded at the specific bullet where each was made:
M5's Hardhat/Solidity-version tooling choice, M6/M7's `RoleSigner` constant, the resync-entry
signature purpose choice, disassociate's "child leader" → "any member of the child's P"
simplification, TEST-29's narrow `>=2`-signature proxy, and `close()`'s deliberate silence on
GAP-89's ChildList question). Two loose ends resolved:
- The stray uncommitted `internal/spec/spec.go` diff the 2026-09-17 tester pass filed
  (`RoleLeader`/`RoleParticipant`, citing the now-corrected-to-Tier-B CONFIRM-008/009) was reverted
  outright (`git checkout -- internal/spec/spec.go`) before any other work this session — see the
  M0 tracker item above, now marked resolved.
- `go.mod`/`go.sum` gained additional transitive dependencies (`go mod tidy`) once `hmsc/chain`
  started importing go-ethereum's `ethclient`/`rpc` packages (previously only `go-ethereum/crypto`,
  via `sig`, was used) — same `CGO_ENABLED=0` pure-Go path applies; no new toolchain issue.

**Node/npm tooling note (M5, not a protocol gap):** `package.json`/`hardhat.config.js`/`test/*.js`
are development/testing tooling for `contracts/HMSC.sol` only (SC-001/002/006's own documented
carve-out — see the M5 Verification log entry below), not a DELIA off-chain component; GO-001's
"no off-chain Java/JS component" is about DELIA's actual algorithms/protocols (which stay in Go),
not this contract's local test harness, matching the sibling `systems/dht-backed` project's own
precedent for the same distinction.

## Verification log

All commands below were run with `CGO_ENABLED=0` (see the environment note above) from
`systems/delia`. `go build ./...`, `go vet ./...` and `go test ./...` are clean across the whole
module as of 2026-09-17 (13 test packages, 0 failures; 117 `--- PASS` cases after the tester
verification pass below added its own files). As of 2026-09-18 (after the M5-M7 pass below): 18 Go
test packages, 0 failures, 153 `--- PASS` cases (`go test ./... -v | grep -c '^--- PASS'`); plus
`npx hardhat test` clean, 26 passing, against `contracts/HMSC.sol` on a real local EVM.

- 2026-09-17, M0 box 1 (module/package skeleton): `go build ./...`, `go vet ./...` clean.
- 2026-09-17, M0 box 2 (process model): `go test ./internal/nettransport/...` —
  `TestServeAndSendRoundTrip`, `TestServeMultipleConnections` (real TLS handshake + framing).
- 2026-09-17, M0 box 4 (module/layer mapping): code review of package layout against SYS-006.
- 2026-09-17, M0 box 5 (mTLS + provisioning tooling): `go test ./internal/pki/...` (incl.
  `TestMutualTLSHandshake`, `TestMutualTLSRejectsUntrustedClient`) and
  `go test ./internal/provision/...`; CLI smoke test via `go run ./cmd/provision ...`.
- 2026-09-17, M0 box 7 (normative test-vector file): `go test ./internal/spec/...`.
- 2026-09-17, M1 (crypto/sig, all 4 checked boxes): `go test ./hash/... ./sig/...`.
- 2026-09-17, M2 (data structures, all 5 boxes): `go test ./logcache/... ./merkle/... ./localstate/... ./globalstate/...`.
- 2026-09-17, M3 (LSG stage 2 + Alg.1/2, all 6 boxes): `go test ./lsg/... ./globalstate/...`.
- 2026-09-17, M4 (Algorithm 3 / audit, all 6 boxes): `go test ./auditor/...` (incl.
  `TestLogVerifyInstrumentation` for TEST-14's call-count requirement and
  `TestSchedulerNoOverlap`/`TestSchedulerRunsPeriodically` for AUDIT-002).

### M5-M7 pass (2026-09-18)

- M5 box 1 (Gamma struct), box 2 (Confirm has no contract function), box 3 (multiple instances):
  `npx hardhat test test/instance.test.js test/confirmOffChain.test.js` — all passing.
- M5 box 4 (beta per-instance config): `go test ./hmsc/confirm/... -run RoundConfig` (also covered
  by the full-package run below).
- M5 box 5 (Hardhat/Solidity tooling choice): `npx hardhat compile` clean (`viaIR: true`,
  Solidity 0.8.24); documented in `package.json`/`hardhat.config.js`.
- M5 box 6 (`hmsc/chain`): `go test ./hmsc/chain/...` — `TestDialRejectsMalformedURL`,
  `TestDialConnectsToRealEVM` (spawns a real Hardhat node subprocess, confirms `BlockNumber`/
  `ChainID` against it).
- M5 box 7 (contract entry points, Tier-A only): `npx hardhat test` (full suite, 26 passing) +
  code review of `contracts/HMSC.sol`'s function list (no Init/Federate/DisputeMsg/OfflineMsg
  challenge-window entry point exists).
- M6 (all 7 boxes): `go test ./hmsc/confirm/... ./hmsc/dispute/...` (`TriggerDispute` covers the
  manual-trigger box) plus `npx hardhat test test/update.test.js` (TEST-29 half) and
  `go test ./messages/...` (NET-006 `StateToLeader`).
- M7 (all 13 boxes): `npx hardhat test` (`test/update.test.js`, `test/close.test.js`,
  `test/disassociate.test.js`, `test/online.test.js`, `test/selectNewLeader.test.js` — 26 passing
  total) plus `go test ./hmsc/dispute/... ./hmsc/offline/... ./hmsc/closure/...`.
- Full module: `go build ./...`, `go vet ./...`, `go test ./...` clean (18 test packages, 153
  `--- PASS` cases); `npx hardhat compile` and `npx hardhat test` clean (26 passing) from
  `systems/delia`, `CGO_ENABLED=0` throughout for the Go commands.

### Tester verification pass (2026-09-17)

An independent tester agent re-verified every `- [x]` box in M0-M4 by writing and running its own
tests against the actual claims (new `*/tester_verify_test.go` files added alongside the
developer's own tests, none of them modified; nothing in `internal/*`, `hash`, `sig`, `logcache`,
`merkle`, `localstate`, `globalstate`, `lsg`, `auditor` implementation code was touched). All
commands run with `CGO_ENABLED=0`. `go build ./...`, `go vet ./...` clean; `go test ./...` clean,
117 `--- PASS` cases total (developer's tests + tester's).

New tests added, by package, and what each specifically targets beyond the developer's own
coverage:

- `merkle/tester_verify_test.go`: theta=2 (no duplication), theta=5 and theta=6 (multi-level odd
  duplication), leaf-order sensitivity (swap changes root), single-leaf identity — all
  cross-checked against `crypto/sha256` directly rather than going through `hash.H`, so a shared
  bug between `hash.H` and `MHTGen`'s use of it can't hide.
- `localstate/tester_verify_test.go`: li=0 and li>0 branches cross-checked byte-for-byte against
  raw `crypto/sha256.Sum256`, including an explicit check that li=0 does NOT equal
  `H(root||zero-digest)`, and an independently-reconstructed 4-batch chain.
- `globalstate/tester_verify_test.go`: `Decode` rejects a short header, a truncated buffer, and a
  buffer with trailing garbage; n=0 domain round-trips; `Equal` catches a single flipped bit deep
  inside an entry; a 4-generation chain (G_0..G_3) verifies serial increment and the
  untouched-other-positions invariant at every step; `StateVerify` called back-to-back with two
  different honest inputs then a tampered variant proves no cross-call caching/staleness; an
  independent GlobalGen call-count spy proves a root mismatch skips GlobalGen entirely (not just
  discards its result) while a valid root still invokes it exactly once (rules out a broken spy
  producing a false pass); an independent li=0 case with a different index/psi shape than
  TEST-05b.
- `auditor/tester_verify_test.go`: GAP-75 skip re-tested with NON-empty leftover records supplied
  for the never-completed server (rules out the developer's own test only passing because it used
  nil records); a mixed 2-server history (server A completes batches while B is still the zero
  sentinel, then B completes its own batches with a tamper) verifying ErrList reports B's own li
  numbering, not a global k-index or A's batch numbers; an independent single-linear-pass MHTGen
  call-count check across 6 states; a no-cascade check (a tampered early batch must not also flag
  later, honest batches).
- `sig/tester_verify_test.go`: recovers the actual signing key from a signature via
  go-ethereum's own `Ecrecover`/`SigToPub` path and cross-checks it against `KeyPair.PublicKey`,
  confirming this is real secp256k1 ECDSA and not a stub; a one-bit flip at every byte/bit position
  of the 64-byte signature (not just byte 0) is rejected; a one-bit flip in the signed digest is
  rejected.
- `internal/pki/tester_verify_test.go`: a client presenting NO certificate at all (not just a
  wrong-CA certificate, which the developer's own test already covers) is rejected by a real TLS
  handshake.
- `lsg/tester_verify_test.go`: three independently-corrupted proposals in a row are each rejected,
  produce exactly one failure notification apiece, and do not prevent a subsequent genuinely honest
  proposal for the same nu from being accepted (no permanent lockup, no stale-failure bleed-through);
  `Receive` is shown to recompute `StateVerify` fresh per call by accepting a valid nu=1 proposal
  and then correctly rejecting a mismatched nu=2 proposal from the same proposer right afterward.

Also checked (no new test file, verified by direct execution/inspection): CLI smoke run of
`go run ./cmd/provision -out ... -theta 5 -participants p1=...,p2=...` produced a real CA, per-participant
keys/certs, and a `domain.json` with the correct P order and public keys; `import` review of every
`.go` file in `lsg` confirms no `contracts`/chain/ledger import anywhere (LSG-001's "no ledger
interaction" claim); repo-wide search for `ethclient`/`abigen`/`bind.`/`core/types` found no hits
(no premature HMSC/chain wiring); `contracts/` directory and any `hmsc*` package are absent (no
M5-M8 scope creep).

No M0-M4 box was unchecked. No new protocol gap or scope-creep issue was found; the LSG-002/003/005
stage-1 broadcast wire format remains correctly unimplemented (GAP-05 still OPEN), and no HMSC/chain
code exists yet.

### Tracker correction (2026-09-17, orchestrating session)

The tester's scope-creep item traced back to a drafting error in this tracker's own M6 section,
not to a developer mistake: M6 had listed CONFIRM-008 and CONFIRM-009 (the leader's actual
`(G_nu, S_leader)` signing/broadcast step and a recipient's countersignature step) as part of a
buildable "signed round state, single instance" bullet, while M6's own "Do NOT implement yet"
callout directly above it *correctly* named CONFIRM-008..012 as blocked — a direct
self-contradiction. Per the SRS's own Developer Handoff (revision 3), CONFIRM-008/009 are Tier
**B**, blocked on residual GAP-86 (the signer-role enumeration isn't frozen), not Tier A. Separately,
**TEST-18** — the only test that would exercise that signed-round-state flow — is not on the SRS's
own "tests runnable now" list either, so there was never a concrete Tier-A target for it regardless
of the ID's tier. The uncommitted `internal/spec/spec.go` edit the tester found (staging
`RoleLeader`/`RoleParticipant` role-byte constants, citing CONFIRM-008/009 directly) was the
developer agent correctly following this tracker's incorrect instruction. Fixed M6 above: CONFIRM-
008/009 moved to the blocked list; the bullet demoted to plumbing-only (a bare `G_nu`-to-leader
message send, CONFIRM-007) plus unchecked design notes (CONFIRM-005/013) that can't be
demonstrated until CONFIRM-008/009 unblock. The stray `RoleLeader`/`RoleParticipant` constants in
`internal/spec/spec.go` were left for the next developer pass to remove (or explicitly re-justify
as reversible pre-staging, per the tester's own recommendation) rather than being touched here —
this session edits the tracker, not implementation code, per the same separation of concerns the
tester and developer agents observe.

### Tester verification pass (2026-09-18)

An independent tester agent re-verified every `- [x]` box in M5, M6 and M7 by writing and running
its own tests against the actual claims (new `hmsc/confirm/tester_verify_test.go`,
`hmsc/dispute/tester_verify_test.go`, `hmsc/offline/tester_verify_test.go` and
`test/tester_verify.test.js` added alongside the developer's own tests, none of them modified;
nothing in `contracts/HMSC.sol`, `hmsc/*`, `internal/spec`, `messages` implementation code was
touched). All Go commands run with `CGO_ENABLED=0`. `go build ./...`, `go vet ./...` clean;
`go test ./...` clean, 168 `--- PASS` cases total (developer's 153 + tester's 15 new Go cases);
`npx hardhat test` clean, 40 passing (developer's 26 + tester's 14 new JS cases).

What each new file specifically targets, beyond the developer's own coverage:

- `hmsc/confirm/tester_verify_test.go`: TEST-17's exact rotation vector re-derived against REAL
  secp256k1 public keys (33-byte compressed encoding via `sig.Gen`), not just the developer's
  single-byte stand-in keys, to actually exercise the "fixed-width unsigned big-endian comparison
  of the M0 canonical encoding" claim on realistic multi-byte inputs; TEST-17b's finalized-vs-
  pending distinction re-checked directly against `Eligible` (not just `RotationSequence`) with
  real keys; multiple-simultaneously-offline rotation; `NextLeader` wrap-at-maximum in isolation; a
  structural (reflection-based) check that `SyncRequest` has exactly two fields (`FromSerial`,
  `ToSerial`) and nothing that could encode a parent-leader/hop-count, i.e. CONFIRM-016/017's
  "no recursion to a higher leader" claim is checked as a type-shape fact, not just an assertion;
  an end-to-end desync-recovery exercise (`NeedsSync` -> sign real states with a real leader key ->
  `VerifySyncResponse`) in one test, not just each half in isolation.
- `hmsc/dispute/tester_verify_test.go`: a targeted check that `CheckpointSelection` returns the
  landmark maximum serial itself (9, from history `{9,5,1}`) and NOT `nu-1`, which is what Alg. 4's
  literal decrement-before-test listing (INC-07) would produce if it had leaked back in despite
  DEV-02's prose-semantics ruling; single-entry, duplicate-entry and single-zero-entry (distinct
  from the empty/not-found case) boundary vectors; a structural check that `TriggerDispute` has no
  role-restriction field, and that a zero checkpoint round-trips (the constructor itself applies no
  validation, matching "any participant, no automatic detection threshold" literally).
- `hmsc/offline/tester_verify_test.go`: the n-1/n-2 boundary re-checked at n=6 (the developer's own
  vectors used n=4) with both sides of the boundary asserted in the same test; an n=1
  single-participant domain (expected signer set is empty, accepted correctly as a natural
  consequence of the n-1 formula, not a separately-built special case); a signer list padded with
  an extra duplicate, otherwise-complete entry, confirmed rejected on count rather than silently
  accepted by ignoring the duplicate.
- `test/tester_verify.test.js` (Hardhat, against the real compiled `contracts/HMSC.sol` bytecode on
  Hardhat's local EVM, 14 new passing cases): Rule 1's `nu == nu*` boundary re-checked with a fresh
  `nu*` value; Rule 2 re-checked with 4 participants and 2 simultaneously-offline members, changing
  entries elsewhere in the same submission that also (separately) tries to touch a frozen entry;
  Rule 3's validity check re-checked with a `v`-flip (distinct from the developer's own r/s-flip
  vector) — and, the specific concern this pass was asked to probe hardest: **two direct tests that
  a "wrong" signer count is NOT rejected in either direction** (2-of-4 signing when a naive
  num-numoff rule would expect 3; all 4-of-4 signing, including the finalized-offline member, when
  a naive rule would expect fewer) — both accepted, confirming UPDATE-006 truly is not
  accidentally implemented; `selectNewLeader` cross-checked against `eth_getBlockByNumber` fetched
  via the raw JSON-RPC call directly (not ethers' `getBlock` convenience wrapper the developer's own
  test used) across 4 distinct freshly-mined blocks, with a sanity check that picks are not all
  identical (rules out a stubbed/constant value) and a same-block determinism check; `close()`
  re-checked with 4 participants (missing exactly one offline signer's signature blocks the whole
  close, LIM-03) and a mixed valid-CLOSE-plus-valid-STATE-purpose signer set (rejected as a whole,
  not partially ignored); `close()` with `Parent != null` re-checked with an otherwise-fully-valid
  signature set, confirming the parent check is a hard gate, not something a complete signer set
  can route around; `disassociate()` re-checked with 3 children (not the developer's own 2) and the
  corruption placed on the THIRD child specifically, confirming no direct child's `Parent` is reset
  when any one signature fails (not just "the last one checked"); a direct confirmation that
  disassociate() accepts a signature from a non-leader member of a child's own P, matching the
  contract's own documented GAP-60/82 simplification; `UpdateAccepted` event count re-checked
  (exactly 1 for an accepted call, 0 for a reverted one) plus an ABI-level scan of every exported
  function name for anything history/past/all-states-shaped (none found), on top of the direct
  source-code review below; a direct test that Close signatures collected in a non-leader-first
  order still succeed, probing for any hidden leader/participant role distinction in `RoleSigner`'s
  on-chain use.

Also checked (no new test file, verified by direct execution/inspection):
- `contracts/HMSC.sol` read in full: confirmed the `Instance` struct has exactly Gamma's seven
  fields plus the one user-approved `closed` field (ARCH-003/ARCH-011); confirmed the function list
  is exactly `createInstanceForTesting`, `getInstance`, `update`, `onlineMsg`, `close`,
  `disassociate`, `selectNewLeader` — no `init`/`federate`/`disputeMsg`/`offlineMsg`
  challenge-window/`finalizeOffline` entry point exists; confirmed there is no on-chain history
  array anywhere in contract storage (only `instances[sid].entries`, the single current state, plus
  the `UpdateAccepted` event) — SC-007; confirmed no `startBlock`/cancellation/`FinalizeOffline`
  code exists anywhere in the contract (grepped) beyond doc-comment mentions of what is *not* built.
- `hmsc/chain`: ran `TestDialConnectsToRealEVM` in isolation (`-count=1`, bypassing the test cache)
  and as part of the full suite; checked running `node.exe`/`cmd.exe` processes via
  `Get-CimInstance Win32_Process` before and after each run (PowerShell) — the only `node.exe`
  present before, during and after every run was an unrelated pre-existing Adobe Creative Cloud
  process (confirmed by its command line), never a Hardhat node; also confirmed TCP port 8598 (the
  test's fixed port) is not left listening after the test exits. No orphaned process found; the
  developer's documented `npx`/cmd.exe-avoidance fix (invoking `node cli.js` directly) holds.
  Deliberately re-ran this check fresh (not relying on the developer's own note) per the task's
  explicit instruction to verify, not trust, the process-leak fix.
- `internal/spec/spec.go`: confirmed via `git log -p -- internal/spec/spec.go` (all commits, not
  just the working tree) that commit `63d73a3` both reverted the stray uncommitted
  `RoleLeader`/`RoleParticipant` staging (per its own commit message, matching the tracker's
  correction narrative) and added the single generic `RoleSigner = 0x01` constant in the same
  commit — i.e. the fix was actually committed, not just claimed. Repo-wide `Grep` for
  `RoleLeader|RoleParticipant` across the whole tree found zero hits in any tracked or working-tree
  file except TRACKER.md's own prose describing the history. `RoleSigner`'s doc comment and its two
  Solidity call sites (`update`/`close`/`disassociate` and `onlineMsg` all use the single
  `ROLE_SIGNER` constant) confirmed to carry no leader/participant branching anywhere on chain —
  corroborated by the new "non-leader-first signature order still succeeds" JS test above.
- `hmsc/dispute`: confirmed by reading `dispute.go` that `CheckpointSelection`'s signature is
  `func CheckpointSelection(signedSerials []uint64) (uint64, bool)` — no `ChildList` parameter, no
  own-state/child-state distinction — i.e. it genuinely implements only the no-children case
  (DISPUTE-005/009), not a secretly-broader Alg. 4 lines 10-24 implementation.
- Cross-language layout consistency: read `globalstate.GlobalState.Encode` (Go) and
  `contracts/HMSC.sol`'s `_digest`/`test/helpers.js`'s `digestFor` side by side and confirmed all
  three produce the identical `uint64 nu (BE) || uint32 entry_count (BE) || entries` byte layout
  the GAP-73 freeze specifies — not just trusted by doc-comment claim.
- M8/TEST-42 spot check on the M5-M7 diff: `Grep` for `^\s*go |go func` across
  `hmsc/confirm`, `hmsc/dispute`, `hmsc/offline`, `hmsc/closure` and `contracts/HMSC.sol` found
  goroutines only in `hmsc/closure/closure_test.go` (test-harness orchestration), none in any
  protocol/algorithm implementation file.

No M5, M6 or M7 box was unchecked. No new protocol gap was found. One thing double-checked and
confirmed NOT a problem: the M6/M7 "Do NOT implement yet" callouts (CONFIRM-003/004's on-chain
anchor-derived leader mechanic, CONFIRM-010/011/012 parent/child propagation, CONFIRM-008/009's
leader-signs/countersigns flow, UPDATE-002/003/006/008/009..012, DISPUTE-001..004/006..008/010/
011/013..019, OFFLINE-001..004/007..009/016) were all confirmed absent from the actual diff by
direct source review, not just by the tracker's own prose — in particular, UPDATE-006's exact
signer-count rule is confirmed genuinely unimplemented in either direction (both under- and
over-inclusive signer counts accepted by `update()`), which was this pass's single most
safety-critical thing to get right (an accidental partial implementation of a blocked rule would be
a "helpfully" unblocked violation of the tracker's own binding rules, not just a missed feature).
