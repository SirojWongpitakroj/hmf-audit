// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.24;

// HMSC.sol is DELIA's hierarchical multi-party state channel contract (HMSC,
// §4.2-§4.3), the SCC (state channel contract). SC-001/SC-002: this is a real
// Solidity contract, deployed on a real Ethereum-compatible node (Hardhat's
// local EVM for day-to-day development/testing — see package.json/
// hardhat.config.js and TRACKER.md's M5 section; the SRS's own experimental
// target, GAP-50, is a private 10-node Geth-compatible PoA network). It is
// never reimplemented in Go and never a mock/simulated ledger (SRS §26 item
// 18).
//
// ARCH-001/002 (GAP-57(D), DEV-15): the SCC comprises six named protocols —
// Init, Confirm, Update, DisputeResolving, OfflineResolving, Close — but
// Confirm has NO contract function; it is off-chain only (leader election,
// signed/no-signed round state generation, hmsc/confirm). Every function
// below belongs to Init/Update/DisputeResolving/OfflineResolving/Close.
//
// M5's own "Do NOT implement yet" callout (ARCH-004, 006, 008, 011 — Tier
// C/B, GAP-82 residual) keeps this contract out of the on-chain
// leader-rotation-from-anchor bookkeeping and the extra state it would need
// (a recorded per-instance leader, a round counter, vote tallies, federation
// submission tracking, offline challenge start-block storage). The only
// extra contract state beyond Gamma's seven fields this file adds is
// `closed`, which the SRS's own ARCH-011 text names as already
// user-approved (GAP-45) independent of ARCH-011's still-blocked items.
//
// Init itself (INIT-002..009) is entirely Tier C/B (GAP-20 OPEN, GAP-82
// residual) and is NOT implemented here — no InitMsg/FederateMsg entry
// point exists. M6/M7's narrow Tier-A test slices (TEST-23/24/26/29/36/37/
// 38/39/40/44/45/47) still need *some* instance on chain to exercise
// Update/Close/Disassociate/DisputeResolving's new-leader sub-computation
// against, so this file adds one clearly-labelled TEST-ONLY entry point,
// `createInstanceForTesting`, that sets Gamma's storage directly — the same
// documented, reversible test-harness carve-out GO-004/GAP-54 already uses
// for the in-process multi-node Go harness. It is not a claim that INIT-002..
// 009 are implemented; TEST-15/16 (which exercise the real Init/Federate
// entry points) remain correctly unimplemented.
contract HMSC {
    // Purpose codes, matching internal/spec.Purpose (GAP-72/73). Only the
    // two this contract's Tier-A signing points use are needed here.
    uint8 constant PURPOSE_STATE = 0x01;
    uint8 constant PURPOSE_CLOSE = 0x02;

    // Signer-role code, matching internal/spec.RoleSigner (GAP-86 residual:
    // does not distinguish leader from participant; see that constant's doc
    // comment for the full scoping rationale).
    uint8 constant ROLE_SIGNER = 0x01;

    // Gamma := {sid, P, G, OffList, NewLeader, Parent, ChildList}
    // (ARCH-003, §4.3.1 p.3291). G is represented here by its two GAP-73
    // wire-layout fields (nu, entries) — together they ARE the single "G"
    // field, not extra state (globalstate.GlobalState.Encode: uint64 nu ||
    // uint32 entry_count || entry_count x 32-byte LocalState). OffList is
    // represented by the addresses it holds (CRYPTO-008: no second identity
    // mapping); this contract does not also store each offline server's
    // H(psi) on chain because no Tier-A on-chain check this milestone builds
    // needs it (the audit cross-check that would, NET-032/037, is Tier B/C).
    struct Instance {
        bool initialized;
        bool closed; // extra state beyond Gamma's 7 fields, user-approved (GAP-45, ARCH-011 text)
        address[] P; // the P order frozen at "init" (test harness here; GAP-08)
        uint64 nu; // G.nu
        bytes32[] entries; // G.entries, GAP-73 order
        address[] offList; // OffList, addresses only (GAP-08 position lookup uses P)
        address newLeader; // NewLeader; unused by any Tier-A item this milestone builds, stays address(0)
        bytes32 parent; // Parent; bytes32(0) = null
        bytes32[] childList; // ChildList
    }

    mapping(bytes32 => Instance) private instances;

    // UpdateAccepted is emitted exactly once per accepted Update (UPDATE-011,
    // TEST-45), carrying the full signed state; SC-007: no on-chain history
    // array is kept, this event plus each server's own retained history is
    // the only record.
    event UpdateAccepted(
        bytes32 indexed sid,
        uint64 nu,
        bytes32[] entries,
        address[] signers,
        uint8[] v,
        bytes32[] r,
        bytes32[] s
    );

    event InstanceClosed(bytes32 indexed sid);
    event InstanceDisassociated(bytes32 indexed sid, bytes32 indexed parentSid);

    // ---- TEST-ONLY instance creation (see file-level doc comment) ----

    function createInstanceForTesting(
        bytes32 sid,
        address[] calldata p,
        uint64 nu,
        bytes32[] calldata entries,
        address[] calldata offList,
        bytes32 parent,
        bytes32[] calldata childList
    ) external {
        require(!instances[sid].initialized, "HMSC: sid already exists");
        require(p.length == entries.length, "HMSC: P/entries length mismatch");
        Instance storage inst = instances[sid];
        inst.initialized = true;
        inst.closed = false;
        for (uint256 i = 0; i < p.length; i++) {
            inst.P.push(p[i]);
            inst.entries.push(entries[i]);
        }
        inst.nu = nu;
        for (uint256 i = 0; i < offList.length; i++) {
            inst.offList.push(offList[i]);
        }
        inst.newLeader = address(0);
        inst.parent = parent;
        for (uint256 i = 0; i < childList.length; i++) {
            inst.childList.push(childList[i]);
        }
    }

    function getInstance(bytes32 sid)
        external
        view
        returns (
            address[] memory p,
            uint64 nu,
            bytes32[] memory entries,
            address[] memory offList,
            address newLeader,
            bytes32 parent,
            bytes32[] memory childList,
            bool closed
        )
    {
        Instance storage inst = instances[sid];
        require(inst.initialized, "HMSC: no such instance");
        return (
            inst.P,
            inst.nu,
            inst.entries,
            inst.offList,
            inst.newLeader,
            inst.parent,
            inst.childList,
            inst.closed
        );
    }

    // ---- shared helpers ----

    function _digest(uint8 purpose, uint64 nu, bytes32[] memory entries) private pure returns (bytes32) {
        bytes memory encodedObject = abi.encodePacked(nu, uint32(entries.length), _flatten(entries));
        bytes memory signed = abi.encodePacked(purpose, ROLE_SIGNER, encodedObject);
        return sha256(signed); // CRYPTO-002: SHA-256 is the sole hash; the EVM's ecrecover
        // precompile is hash-function-agnostic, so signing/verifying a
        // SHA-256 digest (not keccak256) is compatible with ecrecover.
    }

    function _flatten(bytes32[] memory entries) private pure returns (bytes memory out) {
        for (uint256 i = 0; i < entries.length; i++) {
            out = abi.encodePacked(out, entries[i]);
        }
    }

    function _isMember(address[] storage set, address a) private view returns (bool) {
        for (uint256 i = 0; i < set.length; i++) {
            if (set[i] == a) return true;
        }
        return false;
    }

    function _indexOf(address[] storage set, address a) private view returns (int256) {
        for (uint256 i = 0; i < set.length; i++) {
            if (set[i] == a) return int256(i);
        }
        return -1;
    }

    // _verifySigners checks that signers.length == v.length == r.length ==
    // s.length, that every (v[i], r[i], s[i]) recovers to signers[i] over
    // digest, and that every signers[i] is a distinct member of `members`
    // (no duplicates). This is UPDATE-007's Rule 3 VALIDITY slice only — it
    // does NOT check exact signer count against num-numoff or leader
    // presence (UPDATE-006/008, Tier C/blocked); callers apply whatever
    // count check their own Tier-A test needs on top of this.
    function _verifySigners(address[] storage members, address[] calldata signers, uint8[] calldata v, bytes32[] calldata r, bytes32[] calldata s, bytes32 digest)
        private
        view
        returns (bool)
    {
        if (signers.length != v.length || signers.length != r.length || signers.length != s.length) {
            return false;
        }
        for (uint256 i = 0; i < signers.length; i++) {
            address recovered = ecrecover(digest, v[i], r[i], s[i]);
            if (recovered == address(0) || recovered != signers[i]) return false;
            if (!_isMember(members, signers[i])) return false;
            for (uint256 j = 0; j < i; j++) {
                if (signers[j] == signers[i]) return false; // no duplicate signers
            }
        }
        return true;
    }

    // ---- Update (UPDATE-001/004/005/007/011, State Update Rules 1/2/3-validity) ----

    // update implements the Tier-A slice of the State Update Rules:
    //   Rule 1 (UPDATE-004, TEST-23): nu > nu*.
    //   Rule 2 (UPDATE-005, TEST-24/36): OffList members' entries frozen.
    //   Rule 3, validity only (UPDATE-007, TEST-26): every included
    //     signature verifies, against a distinct member of P.
    //   TEST-29/CONFIRM-018 narrow proxy (UPDATE-001): a submission with
    //     fewer than 2 signatures is rejected outright as "not a signed
    //     round state" (a no-signed state, CONFIRM-005, carries exactly the
    //     leader's one signature). This is deliberately NOT the general
    //     Rule 3 exact-count check (num - numoff, UPDATE-006 — Tier C,
    //     blocked): it only distinguishes "one signature" from "more than
    //     one", nothing else. Do not read this as UPDATE-006 implemented.
    //
    // Hierarchy (federation, ARCH-005/006/011) is out of scope this
    // milestone: every instance here is treated as its own root, so GAP-66's
    // "nu* / G_nu* = the latest signed state stored on-chain in the root
    // instance" resolves to this same instance's own currently stored
    // (nu, entries).
    function update(
        bytes32 sid,
        uint64 nu,
        bytes32[] calldata entries,
        address[] calldata signers,
        uint8[] calldata v,
        bytes32[] calldata r,
        bytes32[] calldata s
    ) external {
        Instance storage inst = instances[sid];
        require(inst.initialized, "HMSC: no such instance");
        require(!inst.closed, "HMSC: instance closed"); // CLOSE-001, TEST-38
        require(entries.length == inst.P.length, "HMSC: entries/P length mismatch");
        require(signers.length >= 2, "HMSC: no-signed state rejected"); // TEST-29 narrow proxy, see doc comment
        require(nu > inst.nu, "HMSC: Rule 1 violated"); // TEST-23

        // Rule 2: every OffList member's entry must be unchanged (TEST-24/36).
        for (uint256 i = 0; i < inst.offList.length; i++) {
            int256 idx = _indexOf(inst.P, inst.offList[i]);
            if (idx >= 0) {
                require(entries[uint256(idx)] == inst.entries[uint256(idx)], "HMSC: Rule 2 violated");
            }
        }

        bytes32 digest = _digest(PURPOSE_STATE, nu, entries);
        require(_verifySigners(inst.P, signers, v, r, s, digest), "HMSC: Rule 3 (validity) violated"); // TEST-26

        inst.nu = nu;
        delete inst.entries;
        for (uint256 i = 0; i < entries.length; i++) {
            inst.entries.push(entries[i]);
        }

        emit UpdateAccepted(sid, nu, entries, signers, v, r, s);
    }

    // ---- OnlineMsg (OFFLINE-011..015, TEST-37) ----

    // onlineMsg implements the Tier-A slice of OFFLINE-013/014: accepted
    // without requiring the returning participant's own signature
    // (DEV-12), and removes `returning` from OffList on acceptance. It
    // applies Rule 1 (this instance's own nu*) but not the exact-count Rule
    // 3 (blocked, same as update()).
    function onlineMsg(
        bytes32 sid,
        uint64 nu,
        bytes32[] calldata entries,
        address[] calldata signers,
        uint8[] calldata v,
        bytes32[] calldata r,
        bytes32[] calldata s,
        address returning
    ) external {
        Instance storage inst = instances[sid];
        require(inst.initialized, "HMSC: no such instance");
        require(!inst.closed, "HMSC: instance closed");
        require(entries.length == inst.P.length, "HMSC: entries/P length mismatch");
        require(nu > inst.nu, "HMSC: Rule 1 violated");
        require(_isMember(inst.offList, returning), "HMSC: not in OffList");

        bytes32 digest = _digest(PURPOSE_STATE, nu, entries);
        require(_verifySigners(inst.P, signers, v, r, s, digest), "HMSC: signatures invalid");
        // `returning` is deliberately NOT required to be among signers (DEV-12: "S_pi is not required").

        inst.nu = nu;
        delete inst.entries;
        for (uint256 i = 0; i < entries.length; i++) {
            inst.entries.push(entries[i]);
        }

        int256 idx = _indexOf(inst.offList, returning);
        uint256 last = inst.offList.length - 1;
        inst.offList[uint256(idx)] = inst.offList[last];
        inst.offList.pop();
    }

    // ---- Close (CLOSE-001/003/004/005, TEST-38/44) ----

    // close implements the Tier-A slice of Close: purpose CLOSE, signatures
    // from EVERY member of P (offline members included, LIM-03 — a single
    // missing offline signer blocks the whole Close), rejected outright
    // while Parent != null (CLOSE-004/009, TEST-44; disassociate first).
    // Rules 1/2/4 are not stated to apply to Close and are not applied here.
    function close(
        bytes32 sid,
        uint64 nu,
        bytes32[] calldata entries,
        address[] calldata signers,
        uint8[] calldata v,
        bytes32[] calldata r,
        bytes32[] calldata s
    ) external {
        Instance storage inst = instances[sid];
        require(inst.initialized, "HMSC: no such instance");
        require(!inst.closed, "HMSC: instance already closed");
        require(inst.parent == bytes32(0), "HMSC: instance has a parent, disassociate first"); // TEST-44
        require(entries.length == inst.P.length, "HMSC: entries/P length mismatch");
        require(signers.length == inst.P.length, "HMSC: Close needs every member of P"); // LIM-03

        bytes32 digest = _digest(PURPOSE_CLOSE, nu, entries);
        require(_verifySigners(inst.P, signers, v, r, s, digest), "HMSC: signatures invalid");
        // _verifySigners already rejects duplicates; combined with the
        // length == P.length check above, this forces exactly one valid
        // signature per member of P (every member, offline or not).
        for (uint256 i = 0; i < inst.P.length; i++) {
            bool found = false;
            for (uint256 j = 0; j < signers.length; j++) {
                if (signers[j] == inst.P[i]) { found = true; break; }
            }
            require(found, "HMSC: missing a P member's signature");
        }

        inst.closed = true;
        emit InstanceClosed(sid);
    }

    // ---- Disassociate (CLOSE-006/007, TEST-39/40) ----

    // disassociate closes a parent instance once every direct child has
    // produced its own signed close state (one valid CLOSE-purpose
    // signature from a member of that child's own P — determining that
    // signer is specifically that child's *leader*, GAP-60/82, is out of
    // scope, same simplification as elsewhere this milestone). Each direct
    // child's Parent is reset to null; children stay open (their `closed`
    // flag is untouched). Rejected while the parent's own Parent != null
    // (LIM-04: no nested disassociation). A single corrupted child
    // signature leaves the parent open (the whole call reverts).
    function disassociate(
        bytes32 parentSid,
        bytes32[] calldata childSids,
        address[] calldata childSigners,
        uint8[] calldata v,
        bytes32[] calldata r,
        bytes32[] calldata s
    ) external {
        Instance storage parentInst = instances[parentSid];
        require(parentInst.initialized, "HMSC: no such parent instance");
        require(!parentInst.closed, "HMSC: parent already closed");
        require(parentInst.parent == bytes32(0), "HMSC: nested disassociation not supported"); // LIM-04
        require(
            childSids.length == childSigners.length && childSids.length == v.length &&
                childSids.length == r.length && childSids.length == s.length,
            "HMSC: array length mismatch"
        );
        require(childSids.length == parentInst.childList.length, "HMSC: must cover every direct child");

        for (uint256 i = 0; i < childSids.length; i++) {
            bool found = false;
            for (uint256 j = 0; j < parentInst.childList.length; j++) {
                if (parentInst.childList[j] == childSids[i]) { found = true; break; }
            }
            require(found, "HMSC: not a direct child");

            Instance storage childInst = instances[childSids[i]];
            require(childInst.initialized, "HMSC: no such child instance");

            bytes32 digest = _digest(PURPOSE_CLOSE, childInst.nu, childInst.entries);
            address recovered = ecrecover(digest, v[i], r[i], s[i]);
            require(recovered != address(0) && recovered == childSigners[i], "HMSC: child signature invalid");
            require(_isMember(childInst.P, childSigners[i]), "HMSC: child signer not in child's P");

            childInst.parent = bytes32(0);
        }

        parentInst.closed = true;
        emit InstanceDisassociated(parentSid, parentSid);
    }

    // ---- New-leader selection (DISPUTE-012, TEST-47) ----

    // selectNewLeader is the isolated on-chain sub-computation DISPUTE-012
    // needs: index = uint(blockhash(block.number - 1)) mod
    // eligible-candidate-count. Filtering `candidates` down to the eligible
    // set (excluding the removed participant and finalized OffList members;
    // a pending, unfinalized OfflineMsg does NOT exclude; ordering per
    // GAP-22) happens off-chain before this call (hmsc/confirm.Eligible,
    // shared with TEST-17/17b's rotation logic) — this function is only the
    // block-hash-derived pick, not the full DisputeResolving entry point
    // (which stays out of scope: DISPUTE-010/011 are Tier B, blocked by
    // GAP-88).
    function selectNewLeader(address[] calldata candidates) external view returns (address) {
        require(candidates.length > 0, "HMSC: no eligible candidates");
        uint256 idx = uint256(blockhash(block.number - 1)) % candidates.length;
        return candidates[idx];
    }
}
