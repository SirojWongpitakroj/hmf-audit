# DELIA BUILD TRACKER

**Authority:** `DELIA_REQUIREMENTS.md` in this directory is the governing document. This tracker
is a work breakdown *only*; it never overrides the SRS. Where the two differ, the SRS wins. This
file was seeded on 2026-09-16 directly from the SRS's own **"Developer Handoff (revision 3)"**
section (near the end of the file) and the **"Tests runnable now"** list in §22.

**Status as of 2026-09-17:** M0-M4 are implemented and tested (see the checkboxes and
Verification log below); M5-M8 are not started. See the end-of-run handoff in the session history
for a full summary.

**Tester sign-off (2026-09-17):** an independent tester agent re-verified every `- [x]` box in
M0-M4 by writing and running its own tests (not just reading the developer's tests/notes) — see
"Tester verification pass" in the Verification log below. All checked boxes held; nothing was
unchecked; no scope creep into M5-M8/blocked territory was found. M0-M4 are DONE.

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

- [ ] **Tester-filed (2026-09-17), scope-creep check on the M0 box above:** at verification time,
      `internal/spec/spec.go` had an *uncommitted* working-tree change (not part of any commit on
      this branch) adding `SignerRole` constants `RoleLeader = 0x01` / `RoleParticipant = 0x02`,
      with a doc comment explicitly citing CONFIRM-008/CONFIRM-009 and labelling them "M6 Tier-A
      scope". CONFIRM-008 and CONFIRM-009 are on M6's own "Do NOT implement yet" list
      (blocked by GAP-81/GAP-82/GAP-24 OPEN) and M6 has not been started (all its boxes are still
      `- [ ]`). Neither constant is referenced anywhere else in the tree (`grep -rn
      "RoleLeader\|RoleParticipant"` returns only the two declarations) — no M0-M4 logic depends on
      them, so no M0-M4 checked box's actual claim is affected and none was unchecked over this.
      But the constants themselves reach past the box's own documented scope ("freeze only the
      roles a Tier-A item actually needs" — no Tier-A/M0-M4 item needs these two) and into M6's
      explicitly-blocked IDs. Action needed: either remove the two constants until M6 actually
      starts, or, if the developer wants to keep them as inert scaffolding, get that logged as a
      documented, reversible call (per the developer agent's own ambiguity-handling rule) rather
      than left as a silent uncommitted diff — and either way this should be committed or reverted,
      not left dangling uncommitted. Not a failure of the M0 box above (its own documented content
      — purpose codes, message types, GlobalState layout, signed-bytes helper, record hash rule,
      frame codec — is all present, tested, and unaffected); filed as a separate item because it is
      additional content the box never claimed.

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

- [ ] `Γ` instance representation `{sid, P, G, OffList, NewLeader, Parent, ChildList}` — exactly
      these seven fields as contract storage (extra state, if any, is out of scope until
      ARCH-011 clears). (ARCH-003)
- [ ] SCC comprises six *named* protocols (Init, Confirm, Update, DisputeResolving,
      OfflineResolving, Close) but **Confirm has no contract function** — it is off-chain only
      (DEV-15). Init/Update/DisputeResolving/OfflineResolving/Close execute on Ethereum; nothing
      off-chain sends a transaction for state confirmation itself. (ARCH-001, ARCH-002)
- [ ] Multiple independent instances can exist simultaneously without being closed/recreated to
      cooperate (topology from manual config, no automatic partitioning — ARCH-005, ARCH-007).
- [ ] `beta` is required per-instance configuration, no protocol default; a round is
      "one signed round state, then up to beta no-signed round states" as a *concept* here — the
      on-chain enforcement of round boundaries is ARCH-008, deferred. (ARCH-009, ARCH-010)
- [ ] Solidity contract, deployed on a **real** Ethereum-compatible node — not a Go ledger
      simulator (SRS §26 item 18 is a hard prohibition). The SRS's own experimental target is a
      private 10-node Geth-compatible PoA network (GAP-50, GAP-78) — that is the eventual
      deployment target for the §23 experiments; for day-to-day development/unit testing of the
      contract itself, choosing a standard local Solidity test environment (e.g. Hardhat/Foundry
      running a real EVM against the same bytecode) is engineering tooling, not a protocol
      deviation — note whichever choice is made and why. (SC-001, SC-002, SC-006)
- [ ] `hmsc/chain`: Go-side Ethereum client binding package. Contract logic itself stays in
      Solidity; this package only calls it. Client library choice is unconstrained (GAP-51, not
      needed) beyond "a Go Ethereum client compatible with the node." (SC-003)
- [ ] Contract entry points for the Tier-A protocols only, for now: whatever M6/M7 below actually
      need. Do not pre-build entry points for blocked protocols. (SC-004, scoped)

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

- [ ] At most one leader per instance at any time; every participant gets equal opportunity
      across rounds. (CONFIRM-001, CONFIRM-002)
- [ ] **TEST-17**: off-chain-only rotation over `P = {K1 < K2 < K3}` (keys compared as
      fixed-width unsigned big-endian values of the M0 canonical encoding): round 1 leader = min
      key; leader doesn't repeat next round; rotation restarts at the minimum once exhausted →
      `K1, K2, K3, K1` over 4 rounds.
- [ ] **TEST-17b** (DEV-11): same rotation, but a participant in the *finalized* on-chain OffList
      is skipped; a merely *pending, unfinalized* OfflineMsg must **not** affect eligibility.
- [ ] `NET-006` message plumbing only: a participant can send its generated `G_nu` to its current
      leader (CONFIRM-007 — a plain message send, no signature semantics). This is legitimate
      Tier-A scaffolding *only* if it stays a dumb message send — do not attach any signing logic
      to the leader's receipt of it; that's CONFIRM-008, still blocked. Leave CONFIRM-005 and
      CONFIRM-013 (which describe properties of the *complete* signed-round-state flow) as
      unchecked design notes, not deliverables, until CONFIRM-008/009 unblock — there is no way to
      demonstrate them without the signing step they describe.
- [ ] No-signed round states are relayed/cached off-chain only and are **never** accepted
      on-chain (a leader-only-signed submission to Update must be rejected). Scope this to a
      single instance only — general upward relay through a hierarchy (CONFIRM-014's "recursively")
      is GAP-24 OPEN territory. (CONFIRM-018 single-instance slice) — **TEST-29**
- [ ] Desync recovery: a receiver whose local latest serial ≠ `nu' - 1` requests
      `(fromSerial, toSerial)` from its **direct** leader only (no recursion to higher leaders),
      verifies returned signatures and serial continuity before adopting. (CONFIRM-016,
      CONFIRM-017) — **TEST-21**
- [ ] Manual dispute-trigger entry point: any participant can invoke it explicitly; no automatic
      detection thresholds (GAP-40). (CONFIRM-020)

## M7 — State update rules (partial), dispute/offline/online/close basics

**Requirements:** UPDATE-001, UPDATE-004, UPDATE-005 (+OFFLINE-010), UPDATE-007, DISPUTE-005,
DISPUTE-009, DISPUTE-012, OFFLINE-005, OFFLINE-006, OFFLINE-010, OFFLINE-011..015, CLOSE-001..008

**Do NOT implement yet:** UPDATE-002/003/006/008/009..012 (signer-count/leader-derivation/Rule-4
mechanics — GAP-81/82/87), DISPUTE-001..004/006..008/010/011/013..019 (full dispute flow —
GAP-88 residual, except the two Tier-A pieces below), OFFLINE-001..004/007..009/016 (the
offline-detection/challenge-window mechanics — GAP-85 residual). Build exactly the Tier-A slices
listed per test below; each is deliberately narrower than its protocol section.

- [ ] **TEST-23** Rule 1: reject an Update submission with `nu <= nu*` (`nu*` = latest signed
      state stored on-chain in the root instance, GAP-66). (UPDATE-004)
- [ ] **TEST-24** / **TEST-36** Rule 2: reject an Update that changes the LocalState entry of a
      participant currently in OffList (frozen entry). (UPDATE-005, OFFLINE-010)
- [ ] **TEST-26** Rule 3 (validity slice only): reject an Update if any included signature fails
      `Vrfy`. Do not yet build the exact-signer-count / leader-presence sub-checks (UPDATE-006,
      UPDATE-008 — blocked). (UPDATE-007)
- [ ] **TEST-29** reject a state on Update that carries only the leader's signature (a no-signed
      state). (UPDATE-001, CONFIRM-018 — cross-reference with M6)
- [ ] **TEST-30** `Alg4.CheckpointSelection`, no-children case only (`ChildList == null`):
      implement the **prose semantics** (DEV-02), not the literal off-by-one listing — checkpoint
      = the instance's latest signed round state. (DISPUTE-005, DISPUTE-009)
- [ ] **TEST-47** new-leader selection: `index = uint(blockhash(block.number - 1)) mod
      eligible-candidate-count`; candidates exclude the removed participant and finalized
      OffList members (pending, unfinalized OfflineMsgs do not exclude). (DISPUTE-012)
- [ ] **TEST-48** OfflineMsg signer-set check in isolation: exactly `n-1` valid signatures
      (excluding `pi`) accepted, `n-2` rejected; `H(psi)` computed over `pi`'s **active**
      LogCacheQueue including a partial batch (DEV-03, DEV-17). (OFFLINE-005, OFFLINE-006)
- [ ] **TEST-37** online transition: `OnlineMsg` accepted **without** requiring `S_pi`;
      acceptance removes `pi` from OffList; `pi`'s GlobalState and current-batch LogCacheQueue
      are synced from its **direct leader only** (not "other servers in the domain" — DEV-12);
      `pi`'s next batch chains from its frozen LocalState; `pi`'s log reader stays paused for the
      whole time it was offline (EXPERIMENT-ONLY, GAP-69/LIM-01), so no backlog-ingestion logic
      is needed or wanted. (OFFLINE-011..015)
- [ ] **TEST-38** Close: a signed `(G_nu | Close)` via `CloseMsg` is accepted only with purpose
      `CLOSE` signatures from **every** member of the instance's P, offline members included — a
      single missing offline signer blocks the whole Close (LIM-03, intentional). Subsequent
      Update on a closed instance is rejected; closed data stays readable (no cleanup).
      (CLOSE-001..005)
- [ ] **TEST-44** Close rejected outright while the instance has a non-null Parent (disassociate
      first). (CLOSE-004, CLOSE-009)
- [ ] **TEST-39** / **TEST-40** Disassociation: parent `Γa` with children `Γ1, Γ2` closes once
      both child leaders submit signed close states (purpose CLOSE); each direct child's Parent
      resets to null, children stay open; `DisassociateMsg` rejected while `Γa.Parent != null`
      (LIM-04, no nested disassociation); a corrupted child signature leaves `Γa` open.
      (CLOSE-006, CLOSE-007)
- [ ] Audit-closure coupling: stopping the log reader/LSG is coupled with invoking Close; no
      separate module teardown exists. (CLOSE-008)
- [ ] **TEST-45** every accepted Update emits exactly one event carrying the full signed state;
      the contract keeps **no** on-chain history array (SC-007 scope: event emission only, not
      the auditor's cross-check consumer — that's still AUDIT-012, blocked).

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

## Verification log

All commands below were run with `CGO_ENABLED=0` (see the environment note above) from
`systems/delia`. `go build ./...`, `go vet ./...` and `go test ./...` are clean across the whole
module as of 2026-09-17 (13 test packages, 0 failures; 117 `--- PASS` cases after the tester
verification pass below added its own files).

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
