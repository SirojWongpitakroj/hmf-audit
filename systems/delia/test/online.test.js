// M7: OnlineMsg, Tier-A slice (OFFLINE-011..015, TEST-37 on-chain half).
// The off-chain halves of TEST-37 (sync from direct leader, frozen
// LocalState chaining, paused log reader) are covered by
// hmsc/offline's Go tests, not here.
const { expect } = require("chai");
const { ethers } = require("hardhat");
const { PURPOSE_STATE, collectSignatures, randomWallets, randomEntries } = require("./helpers");

describe("HMSC.onlineMsg (OFFLINE-013/014, TEST-37)", function () {
  it("accepts OnlineMsg without S_pi and removes pi from OffList", async function () {
    const HMSC = await ethers.getContractFactory("HMSC");
    const hmsc = await HMSC.deploy();
    const [a, b, pi] = randomWallets(3);
    const entries0 = randomEntries(3);
    const sid = ethers.id("online-test");
    await hmsc.createInstanceForTesting(
      sid,
      [a.address, b.address, pi.address],
      1n,
      entries0,
      [pi.address], // pi starts offline
      ethers.ZeroHash,
      []
    );

    const entries1 = randomEntries(3);
    const sigs = collectSignatures([a, b], PURPOSE_STATE, 2n, entries1); // no S_pi
    await expect(hmsc.onlineMsg(sid, 2n, entries1, sigs.signers, sigs.v, sigs.r, sigs.s, pi.address)).to.not.be
      .reverted;

    const [, nu, , offList] = await hmsc.getInstance(sid);
    expect(nu).to.equal(2n);
    expect(offList).to.deep.equal([]);
  });

  it("rejects OnlineMsg for a participant not currently in OffList", async function () {
    const HMSC = await ethers.getContractFactory("HMSC");
    const hmsc = await HMSC.deploy();
    const [a, b] = randomWallets(2);
    const entries0 = randomEntries(2);
    const sid = ethers.id("online-test-2");
    await hmsc.createInstanceForTesting(sid, [a.address, b.address], 1n, entries0, [], ethers.ZeroHash, []);

    const entries1 = randomEntries(2);
    const sigs = collectSignatures([a, b], PURPOSE_STATE, 2n, entries1);
    await expect(
      hmsc.onlineMsg(sid, 2n, entries1, sigs.signers, sigs.v, sigs.r, sigs.s, a.address)
    ).to.be.revertedWith("HMSC: not in OffList");
  });
});
