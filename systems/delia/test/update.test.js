// M7: State Update Rules, Tier-A slice — TEST-23 (Rule 1), TEST-24/TEST-36
// (Rule 2), TEST-26 (Rule 3 validity), TEST-29 (no-signed rejected),
// TEST-45 (event emission, no on-chain history array), CLOSE-001/TEST-38's
// "subsequent Update on a closed instance is rejected" half.
const { expect } = require("chai");
const { ethers } = require("hardhat");
const { PURPOSE_STATE, PURPOSE_CLOSE, collectSignatures, randomWallets, randomEntries } = require("./helpers");

async function deployWithInstance(p, nu, entries, offList = []) {
  const HMSC = await ethers.getContractFactory("HMSC");
  const hmsc = await HMSC.deploy();
  const sid = ethers.id("update-test-" + Math.random());
  await hmsc.createInstanceForTesting(
    sid,
    p.map((w) => w.address),
    nu,
    entries,
    offList.map((w) => w.address),
    ethers.ZeroHash,
    []
  );
  return { hmsc, sid };
}

describe("HMSC.update Rule 1 (UPDATE-004, TEST-23)", function () {
  it("rejects nu <= nu*", async function () {
    const [a, b, c] = randomWallets(3);
    const entries0 = randomEntries(3);
    const { hmsc, sid } = await deployWithInstance([a, b, c], 5n, entries0);

    const entries1 = randomEntries(3);
    const sigs = collectSignatures([a, b], PURPOSE_STATE, 5n, entries1); // nu == nu*
    await expect(
      hmsc.update(sid, 5n, entries1, sigs.signers, sigs.v, sigs.r, sigs.s)
    ).to.be.revertedWith("HMSC: Rule 1 violated");

    const sigsLower = collectSignatures([a, b], PURPOSE_STATE, 3n, entries1); // nu < nu*
    await expect(
      hmsc.update(sid, 3n, entries1, sigsLower.signers, sigsLower.v, sigsLower.r, sigsLower.s)
    ).to.be.revertedWith("HMSC: Rule 1 violated");
  });

  it("accepts nu > nu* with valid signatures", async function () {
    const [a, b, c] = randomWallets(3);
    const entries0 = randomEntries(3);
    const { hmsc, sid } = await deployWithInstance([a, b, c], 5n, entries0);

    const entries1 = randomEntries(3);
    const sigs = collectSignatures([a, b], PURPOSE_STATE, 6n, entries1);
    await expect(hmsc.update(sid, 6n, entries1, sigs.signers, sigs.v, sigs.r, sigs.s)).to.not.be.reverted;
    const [, nu] = await hmsc.getInstance(sid);
    expect(nu).to.equal(6n);
  });
});

describe("HMSC.update Rule 2 (UPDATE-005/OFFLINE-010, TEST-24/TEST-36)", function () {
  it("rejects a submission changing an OffList member's entry", async function () {
    const [a, b, c] = randomWallets(3);
    const entries0 = randomEntries(3); // position 1 = b, frozen
    const { hmsc, sid } = await deployWithInstance([a, b, c], 5n, entries0, [b]);

    const entries1 = [...entries0];
    entries1[1] = ethers.hexlify(ethers.randomBytes(32)); // changed b's entry
    const sigs = collectSignatures([a, c], PURPOSE_STATE, 6n, entries1);
    await expect(
      hmsc.update(sid, 6n, entries1, sigs.signers, sigs.v, sigs.r, sigs.s)
    ).to.be.revertedWith("HMSC: Rule 2 violated");
  });

  it("accepts a submission that leaves the OffList member's entry unchanged", async function () {
    const [a, b, c] = randomWallets(3);
    const entries0 = randomEntries(3);
    const { hmsc, sid } = await deployWithInstance([a, b, c], 5n, entries0, [b]);

    const entries1 = [...entries0];
    entries1[0] = ethers.hexlify(ethers.randomBytes(32)); // change a's entry, not b's
    const sigs = collectSignatures([a, c], PURPOSE_STATE, 6n, entries1);
    await expect(hmsc.update(sid, 6n, entries1, sigs.signers, sigs.v, sigs.r, sigs.s)).to.not.be.reverted;
  });
});

describe("HMSC.update Rule 3, validity slice only (UPDATE-007, TEST-26)", function () {
  it("rejects an Update with one corrupted signature", async function () {
    const [a, b, c] = randomWallets(3);
    const entries0 = randomEntries(3);
    const { hmsc, sid } = await deployWithInstance([a, b, c], 5n, entries0);

    const entries1 = randomEntries(3);
    const sigs = collectSignatures([a, b], PURPOSE_STATE, 6n, entries1);
    // Corrupt b's signature by flipping a bit in s.
    const corruptedS = ethers.zeroPadValue(ethers.toBeHex(BigInt(sigs.s[1]) ^ 1n), 32);
    const badS = [sigs.s[0], corruptedS];
    await expect(
      hmsc.update(sid, 6n, entries1, sigs.signers, sigs.v, sigs.r, badS)
    ).to.be.revertedWith("HMSC: Rule 3 (validity) violated");
  });

  it("rejects a signature from a non-member of P", async function () {
    const [a, b, c, outsider] = randomWallets(4);
    const entries0 = randomEntries(3);
    const { hmsc, sid } = await deployWithInstance([a, b, c], 5n, entries0);

    const entries1 = randomEntries(3);
    const sigs = collectSignatures([a, outsider], PURPOSE_STATE, 6n, entries1);
    await expect(
      hmsc.update(sid, 6n, entries1, sigs.signers, sigs.v, sigs.r, sigs.s)
    ).to.be.revertedWith("HMSC: Rule 3 (validity) violated");
  });
});

describe("HMSC.update no-signed-state rejection (UPDATE-001/CONFIRM-018, TEST-29)", function () {
  it("rejects a submission carrying only the leader's (one) signature", async function () {
    const [a, b, c] = randomWallets(3);
    const entries0 = randomEntries(3);
    const { hmsc, sid } = await deployWithInstance([a, b, c], 5n, entries0);

    const entries1 = randomEntries(3);
    const sigs = collectSignatures([a], PURPOSE_STATE, 6n, entries1); // exactly 1 signature
    await expect(
      hmsc.update(sid, 6n, entries1, sigs.signers, sigs.v, sigs.r, sigs.s)
    ).to.be.revertedWith("HMSC: no-signed state rejected");
  });

  it("accepts a submission with more than one valid signature", async function () {
    const [a, b, c] = randomWallets(3);
    const entries0 = randomEntries(3);
    const { hmsc, sid } = await deployWithInstance([a, b, c], 5n, entries0);

    const entries1 = randomEntries(3);
    const sigs = collectSignatures([a, b], PURPOSE_STATE, 6n, entries1);
    await expect(hmsc.update(sid, 6n, entries1, sigs.signers, sigs.v, sigs.r, sigs.s)).to.not.be.reverted;
  });
});

describe("HMSC.update on a closed instance (CLOSE-001, TEST-38 half)", function () {
  it("rejects Update once the instance is closed", async function () {
    const [a, b] = randomWallets(2);
    const entries0 = randomEntries(2);
    const { hmsc, sid } = await deployWithInstance([a, b], 1n, entries0);

    const closeSigs = collectSignatures([a, b], PURPOSE_CLOSE, 1n, entries0);
    await hmsc.close(sid, 1n, entries0, closeSigs.signers, closeSigs.v, closeSigs.r, closeSigs.s);

    const entries1 = randomEntries(2);
    const sigs = collectSignatures([a, b], PURPOSE_STATE, 2n, entries1);
    await expect(
      hmsc.update(sid, 2n, entries1, sigs.signers, sigs.v, sigs.r, sigs.s)
    ).to.be.revertedWith("HMSC: instance closed");
  });
});

describe("HMSC.update event emission (UPDATE-011/SC-007, TEST-45)", function () {
  it("emits exactly one UpdateAccepted event per accepted Update, no history array", async function () {
    const [a, b] = randomWallets(2);
    const entries0 = randomEntries(2);
    const { hmsc, sid } = await deployWithInstance([a, b], 1n, entries0);

    let nu = 1n;
    const seen = [];
    for (let i = 0; i < 3; i++) {
      nu += 1n;
      const entries = randomEntries(2);
      const sigs = collectSignatures([a, b], PURPOSE_STATE, nu, entries);
      const tx = await hmsc.update(sid, nu, entries, sigs.signers, sigs.v, sigs.r, sigs.s);
      const receipt = await tx.wait();
      const events = receipt.logs.filter((l) => {
        try {
          return hmsc.interface.parseLog(l).name === "UpdateAccepted";
        } catch {
          return false;
        }
      });
      expect(events.length).to.equal(1);
      const parsed = hmsc.interface.parseLog(events[0]);
      expect(parsed.args.nu).to.equal(nu);
      seen.push(nu);
    }
    expect(seen).to.deep.equal([2n, 3n, 4n]);

    // SC-007: the contract itself is not asked to return a history array —
    // getInstance only ever exposes the latest (nu, entries), confirming no
    // history array is kept on chain.
    const [, latestNu] = await hmsc.getInstance(sid);
    expect(latestNu).to.equal(4n);
  });
});
