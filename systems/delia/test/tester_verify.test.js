// Independent tester-pass verification (2026-09-18) of HMSC.sol's M6/M7
// Tier-A slices, written fresh against the actual deployed bytecode rather
// than re-reading the developer's own test/*.test.js files. None of the
// developer's own test files are modified by this file.
const { expect } = require("chai");
const { ethers, network } = require("hardhat");
const {
  PURPOSE_STATE,
  PURPOSE_CLOSE,
  collectSignatures,
  randomWallets,
  randomEntries,
} = require("./helpers");

async function deployWithInstance(p, nu, entries, offList = [], parent = ethers.ZeroHash, childList = []) {
  const HMSC = await ethers.getContractFactory("HMSC");
  const hmsc = await HMSC.deploy();
  const sid = ethers.id("tester-verify-" + Math.random());
  await hmsc.createInstanceForTesting(
    sid,
    p.map((w) => w.address),
    nu,
    entries,
    offList.map((w) => w.address),
    parent,
    childList
  );
  return { hmsc, sid };
}

describe("TESTER: update() Rule 1 boundary (UPDATE-004, TEST-23)", function () {
  it("rejects the exact boundary nu == nu* with a fresh nu* value, distinct from the developer's own vectors", async function () {
    const [a, b] = randomWallets(2);
    const entries0 = randomEntries(2);
    const { hmsc, sid } = await deployWithInstance([a, b], 777n, entries0);

    const entries1 = randomEntries(2);
    const sigs = collectSignatures([a, b], PURPOSE_STATE, 777n, entries1); // nu == nu* exactly
    await expect(
      hmsc.update(sid, 777n, entries1, sigs.signers, sigs.v, sigs.r, sigs.s)
    ).to.be.revertedWith("HMSC: Rule 1 violated");

    // one more than the boundary must succeed
    const sigsOk = collectSignatures([a, b], PURPOSE_STATE, 778n, entries1);
    await expect(hmsc.update(sid, 778n, entries1, sigsOk.signers, sigsOk.v, sigsOk.r, sigsOk.s)).to.not.be.reverted;
  });
});

describe("TESTER: update() Rule 2 -- frozen OffList entry vs unrelated entry (UPDATE-005, TEST-24/36)", function () {
  it("rejects only when the OffList member's OWN entry changes, with multiple offline members and multiple entries changing elsewhere", async function () {
    const [a, b, c, d] = randomWallets(4);
    const entries0 = randomEntries(4);
    // b and d are offline (finalized OffList); their entries (index 1, 3) are frozen.
    const { hmsc, sid } = await deployWithInstance([a, b, c, d], 10n, entries0, [b, d]);

    // Change a's and c's entries (indices 0, 2) -- unrelated to OffList -- must be accepted.
    const entriesOk = [...entries0];
    entriesOk[0] = ethers.hexlify(ethers.randomBytes(32));
    entriesOk[2] = ethers.hexlify(ethers.randomBytes(32));
    const sigsOk = collectSignatures([a, c], PURPOSE_STATE, 11n, entriesOk);
    await expect(hmsc.update(sid, 11n, entriesOk, sigsOk.signers, sigsOk.v, sigsOk.r, sigsOk.s)).to.not.be.reverted;

    // Now try to also change d's frozen entry (index 3) -- must be rejected,
    // even though a/c's entries in the same submission are fine.
    const [, nuAfter, entriesAfter] = await hmsc.getInstance(sid);
    const entriesBad = [...entriesAfter];
    entriesBad[3] = ethers.hexlify(ethers.randomBytes(32)); // d is offline, frozen
    const sigsBad = collectSignatures([a, c], PURPOSE_STATE, nuAfter + 1n, entriesBad);
    await expect(
      hmsc.update(sid, nuAfter + 1n, entriesBad, sigsBad.signers, sigsBad.v, sigsBad.r, sigsBad.s)
    ).to.be.revertedWith("HMSC: Rule 2 violated");
  });
});

describe("TESTER: update() Rule 3 -- validity only, count is NOT checked either direction (UPDATE-007, TEST-26)", function () {
  it("rejects a bad signature (v flip) distinct from the developer's own r/s-flip vector", async function () {
    const [a, b, c] = randomWallets(3);
    const entries0 = randomEntries(3);
    const { hmsc, sid } = await deployWithInstance([a, b, c], 1n, entries0);

    const entries1 = randomEntries(3);
    const sigs = collectSignatures([a, b], PURPOSE_STATE, 2n, entries1);
    const badV = [sigs.v[0] === 27 ? 28 : 27, sigs.v[1]]; // flip recovery id on signer 0
    await expect(
      hmsc.update(sid, 2n, entries1, sigs.signers, badV, sigs.r, sigs.s)
    ).to.be.reverted; // either "Rule 3 (validity) violated" or an ecrecover(0) mismatch path
  });

  it("does NOT reject a submission with the 'wrong' signer count in the under-inclusive direction (2 of 4 signing, one offline member's own signature absent)", async function () {
    const [a, b, c, d] = randomWallets(4);
    const entries0 = randomEntries(4);
    // d is finalized-offline. If the still-blocked exact-count rule
    // (UPDATE-006: num - numoff) were live, the "correct" count here would
    // be 3 (a, b, c). Submitting with only 2 signers (a, b) must NOT be
    // rejected on a count basis -- only Rule 3's own validity check applies.
    const { hmsc, sid } = await deployWithInstance([a, b, c, d], 1n, entries0, [d]);
    const entries1 = [...entries0];
    // leave d's entry (index 3) unchanged to satisfy Rule 2 independently
    entries1[0] = ethers.hexlify(ethers.randomBytes(32));
    const sigs = collectSignatures([a, b], PURPOSE_STATE, 2n, entries1);
    await expect(hmsc.update(sid, 2n, entries1, sigs.signers, sigs.v, sigs.r, sigs.s)).to.not.be.reverted;
  });

  it("does NOT reject a submission with the 'wrong' signer count in the over-inclusive direction (all 4 of 4 signing, including the offline member)", async function () {
    const [a, b, c, d] = randomWallets(4);
    const entries0 = randomEntries(4);
    const { hmsc, sid } = await deployWithInstance([a, b, c, d], 1n, entries0, [d]);
    const entries1 = [...entries0];
    entries1[0] = ethers.hexlify(ethers.randomBytes(32));
    // ALL FOUR sign, including the finalized-offline member d -- an
    // over-inclusive count relative to any plausible num-numoff rule. Must
    // still be accepted: this contract applies no count check at all.
    const sigs = collectSignatures([a, b, c, d], PURPOSE_STATE, 2n, entries1);
    await expect(hmsc.update(sid, 2n, entries1, sigs.signers, sigs.v, sigs.r, sigs.s)).to.not.be.reverted;
  });
});

describe("TESTER: selectNewLeader against a genuinely mined block (DISPUTE-012, TEST-47)", function () {
  it("matches an independently-fetched raw block hash via eth_getBlockByNumber, across multiple distinct mined blocks", async function () {
    const HMSC = await ethers.getContractFactory("HMSC");
    const hmsc = await HMSC.deploy();
    const wallets = randomWallets(5);
    const candidates = wallets.map((w) => w.address);

    const picks = [];
    for (let i = 0; i < 4; i++) {
      await network.provider.send("evm_mine");
      const latest = await ethers.provider.getBlockNumber();
      // Fetch the previous block's hash via the raw JSON-RPC call directly
      // (not ethers' getBlock convenience wrapper the developer's own test
      // used), to rule out a shared bug between this test and the
      // contract/dev-test's own hash-fetching path.
      const rawBlock = await network.provider.send("eth_getBlockByNumber", [
        ethers.toBeHex(latest - 1),
        false,
      ]);
      const expectedIndex = Number(BigInt(rawBlock.hash) % BigInt(candidates.length));
      const picked = await hmsc.selectNewLeader(candidates);
      expect(picked).to.equal(candidates[expectedIndex]);
      picks.push(picked);
    }
    // Randomness sanity: across 4 distinct mined blocks, picks should not
    // all be identical (would strongly suggest a stubbed/constant value
    // rather than a genuine blockhash-derived one). Not a hard protocol
    // requirement, just a smoke check against a hardcoded implementation.
    const distinct = new Set(picks);
    expect(distinct.size).to.be.greaterThan(1);
  });

  it("is deterministic within the same block (same block.number-1 hash -> same pick, called twice without mining in between)", async function () {
    const HMSC = await ethers.getContractFactory("HMSC");
    const hmsc = await HMSC.deploy();
    const wallets = randomWallets(6);
    const candidates = wallets.map((w) => w.address);
    await network.provider.send("evm_mine");
    const pick1 = await hmsc.selectNewLeader(candidates);
    const pick2 = await hmsc.selectNewLeader(candidates);
    expect(pick1).to.equal(pick2);
  });
});

describe("TESTER: close() full-P-including-offline signature requirement (CLOSE-001/005, TEST-38)", function () {
  it("rejects when exactly one offline member's signature is missing, out of four, then accepts once added", async function () {
    const [a, b, c, d] = randomWallets(4);
    const entries = randomEntries(4);
    const { hmsc, sid } = await deployWithInstance([a, b, c, d], 1n, entries);

    // d is conceptually "offline" but LIM-03 requires it to sign anyway.
    const missing = collectSignatures([a, b, c], PURPOSE_CLOSE, 1n, entries);
    await expect(
      hmsc.close(sid, 1n, entries, missing.signers, missing.v, missing.r, missing.s)
    ).to.be.revertedWith("HMSC: Close needs every member of P");

    const full = collectSignatures([a, b, c, d], PURPOSE_CLOSE, 1n, entries);
    await expect(hmsc.close(sid, 1n, entries, full.signers, full.v, full.r, full.s)).to.not.be.reverted;
  });

  it("rejects a Close where one signer's signature is under the correct purpose but for a DIFFERENT signer's identity (mixed valid+wrong-purpose set)", async function () {
    const [a, b, c] = randomWallets(3);
    const entries = randomEntries(3);
    const { hmsc, sid } = await deployWithInstance([a, b, c], 1n, entries);

    // a and b sign correctly under CLOSE; c signs under STATE instead (a
    // plausible confusion between an Update signature and a Close
    // signature) -- the whole submission must be rejected, not just have
    // c's contribution silently ignored.
    const abClose = collectSignatures([a, b], PURPOSE_CLOSE, 1n, entries);
    const cState = collectSignatures([c], PURPOSE_STATE, 1n, entries);
    const signers = [...abClose.signers, ...cState.signers];
    const v = [...abClose.v, ...cState.v];
    const r = [...abClose.r, ...cState.r];
    const s = [...abClose.s, ...cState.s];
    await expect(hmsc.close(sid, 1n, entries, signers, v, r, s)).to.be.revertedWith("HMSC: signatures invalid");
  });
});

describe("TESTER: close() rejects outright while Parent != null (CLOSE-004/009, TEST-44)", function () {
  it("still rejects even when every P member's signature is otherwise perfectly valid", async function () {
    const [a, b, c] = randomWallets(3);
    const entries = randomEntries(3);
    const parentSid = ethers.id("tester-parent");
    const { hmsc, sid } = await deployWithInstance([a, b, c], 1n, entries, [], parentSid);
    const full = collectSignatures([a, b, c], PURPOSE_CLOSE, 1n, entries);
    await expect(
      hmsc.close(sid, 1n, entries, full.signers, full.v, full.r, full.s)
    ).to.be.revertedWith("HMSC: instance has a parent, disassociate first");
    const [, , , , , , , closed] = await hmsc.getInstance(sid);
    expect(closed).to.equal(false);
  });
});

describe("TESTER: disassociate() corrupted child signature leaves parent open (CLOSE-006/007, TEST-39/40)", function () {
  it("with 3 children instead of 2: a corruption in the THIRD child's signature still blocks the whole call, and no child's Parent is reset", async function () {
    const HMSC = await ethers.getContractFactory("HMSC");
    const hmsc = await HMSC.deploy();
    const [g1, g2, g3] = randomWallets(3);
    const sidG1 = ethers.id("tester-G1");
    const sidG2 = ethers.id("tester-G2");
    const sidG3 = ethers.id("tester-G3");
    const sidParent = ethers.id("tester-parent-of-3");
    const e1 = randomEntries(1);
    const e2 = randomEntries(1);
    const e3 = randomEntries(1);

    await hmsc.createInstanceForTesting(sidG1, [g1.address], 5n, e1, [], ethers.ZeroHash, []);
    await hmsc.createInstanceForTesting(sidG2, [g2.address], 5n, e2, [], ethers.ZeroHash, []);
    await hmsc.createInstanceForTesting(sidG3, [g3.address], 5n, e3, [], ethers.ZeroHash, []);
    await hmsc.createInstanceForTesting(
      sidParent,
      [g1.address, g2.address, g3.address],
      5n,
      randomEntries(3),
      [],
      ethers.ZeroHash,
      [sidG1, sidG2, sidG3]
    );

    const sig1 = collectSignatures([g1], PURPOSE_CLOSE, 5n, e1);
    const sig2 = collectSignatures([g2], PURPOSE_CLOSE, 5n, e2);
    const sig3 = collectSignatures([g3], PURPOSE_CLOSE, 5n, e3);
    const corrupted3R = ethers.zeroPadValue(ethers.toBeHex(BigInt(sig3.r[0]) ^ 1n), 32);

    await expect(
      hmsc.disassociate(
        sidParent,
        [sidG1, sidG2, sidG3],
        [sig1.signers[0], sig2.signers[0], sig3.signers[0]],
        [sig1.v[0], sig2.v[0], sig3.v[0]],
        [sig1.r[0], sig2.r[0], corrupted3R],
        [sig1.s[0], sig2.s[0], sig3.s[0]]
      )
    ).to.be.reverted;

    const [, , , , , , , closedParent] = await hmsc.getInstance(sidParent);
    expect(closedParent).to.equal(false);
    const [, , , , , parentOfG1] = await hmsc.getInstance(sidG1);
    const [, , , , , parentOfG2] = await hmsc.getInstance(sidG2);
    const [, , , , , parentOfG3] = await hmsc.getInstance(sidG3);
    expect(parentOfG1).to.equal(ethers.ZeroHash);
    expect(parentOfG2).to.equal(ethers.ZeroHash);
    expect(parentOfG3).to.equal(ethers.ZeroHash);
  });

  it("accepts a signature from ANY member of a child's P, not specifically that child's leader (documented GAP-60/82 simplification)", async function () {
    const HMSC = await ethers.getContractFactory("HMSC");
    const hmsc = await HMSC.deploy();
    const [g1leader, g1other, g2leader] = randomWallets(3);
    const sidG1 = ethers.id("tester-G1-multi");
    const sidG2 = ethers.id("tester-G2-multi");
    const sidParent = ethers.id("tester-parent-multi");
    const e1 = randomEntries(2);
    const e2 = randomEntries(1);

    await hmsc.createInstanceForTesting(sidG1, [g1leader.address, g1other.address], 2n, e1, [], ethers.ZeroHash, []);
    await hmsc.createInstanceForTesting(sidG2, [g2leader.address], 2n, e2, [], ethers.ZeroHash, []);
    await hmsc.createInstanceForTesting(
      sidParent,
      [g1leader.address, g2leader.address],
      2n,
      randomEntries(2),
      [],
      ethers.ZeroHash,
      [sidG1, sidG2]
    );

    // g1other (not g1leader) signs G1's close state.
    const sig1 = collectSignatures([g1other], PURPOSE_CLOSE, 2n, e1);
    const sig2 = collectSignatures([g2leader], PURPOSE_CLOSE, 2n, e2);
    await expect(
      hmsc.disassociate(
        sidParent,
        [sidG1, sidG2],
        [sig1.signers[0], sig2.signers[0]],
        [sig1.v[0], sig2.v[0]],
        [sig1.r[0], sig2.r[0]],
        [sig1.s[0], sig2.s[0]]
      )
    ).to.not.be.reverted;
  });
});

describe("TESTER: UpdateAccepted event count and absence of an on-chain history array (SC-007, TEST-45)", function () {
  it("emits exactly one event for exactly one accepted call, zero for a reverted call, and the contract ABI exposes no history-shaped read function", async function () {
    const [a, b] = randomWallets(2);
    const entries0 = randomEntries(2);
    const { hmsc, sid } = await deployWithInstance([a, b], 1n, entries0);

    // One accepted update -> exactly one event.
    const entries1 = randomEntries(2);
    const sigs1 = collectSignatures([a, b], PURPOSE_STATE, 2n, entries1);
    const tx1 = await hmsc.update(sid, 2n, entries1, sigs1.signers, sigs1.v, sigs1.r, sigs1.s);
    const receipt1 = await tx1.wait();
    const events1 = receipt1.logs.filter((l) => {
      try {
        return hmsc.interface.parseLog(l).name === "UpdateAccepted";
      } catch {
        return false;
      }
    });
    expect(events1.length).to.equal(1);

    // A reverted update (nu <= nu*) -> zero events, transaction itself reverts.
    const sigsBad = collectSignatures([a, b], PURPOSE_STATE, 1n, entries1); // nu < current nu (2)
    await expect(hmsc.update(sid, 1n, entries1, sigsBad.signers, sigsBad.v, sigsBad.r, sigsBad.s)).to.be.reverted;

    // ABI review: no function name suggests a retrievable history array
    // (e.g. "history", "getStates", "pastStates", "getUpdates").
    const fnNames = hmsc.interface.fragments
      .filter((f) => f.type === "function")
      .map((f) => f.name);
    const suspicious = fnNames.filter((n) => /hist|past|all.?updates|all.?states|getstates/i.test(n));
    expect(suspicious).to.deep.equal([]);

    // getInstance only ever returns the CURRENT (nu, entries), never an array of past ones.
    const [, currentNu] = await hmsc.getInstance(sid);
    expect(currentNu).to.equal(2n); // still just the latest, not 1 also somehow retrievable
  });
});

describe("TESTER: internal/spec RoleSigner does not distinguish leader vs participant (GAP-86 residual)", function () {
  it("a Close accepted with signatures collected in participant-first order (not leader-first) still succeeds -- no leader-specific ordering/role check exists on chain", async function () {
    const [a, b, c] = randomWallets(3);
    const entries = randomEntries(3);
    const { hmsc, sid } = await deployWithInstance([a, b, c], 1n, entries);
    // Sign in reverse order c, b, a -- if the contract secretly cared about
    // "leader signs first" or a leader/participant role distinction, this
    // would be a plausible place for it to leak in. It does not: _verifySigners
    // only checks set membership, not order or role.
    const sigs = collectSignatures([c, b, a], PURPOSE_CLOSE, 1n, entries);
    await expect(hmsc.close(sid, 1n, entries, sigs.signers, sigs.v, sigs.r, sigs.s)).to.not.be.reverted;
  });
});
