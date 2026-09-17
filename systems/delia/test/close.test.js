// M7: Close protocol, Tier-A slice — TEST-38 (instance closure),
// TEST-44 (Close rejected while a parent exists).
const { expect } = require("chai");
const { ethers } = require("hardhat");
const { PURPOSE_STATE, PURPOSE_CLOSE, collectSignatures, randomWallets, randomEntries } = require("./helpers");

async function deployWithInstance(p, nu, entries, parent = ethers.ZeroHash, childList = []) {
  const HMSC = await ethers.getContractFactory("HMSC");
  const hmsc = await HMSC.deploy();
  const sid = ethers.id("close-test-" + Math.random());
  await hmsc.createInstanceForTesting(sid, p.map((w) => w.address), nu, entries, [], parent, childList);
  return { hmsc, sid };
}

describe("HMSC.close (CLOSE-001/003/004/005, TEST-38)", function () {
  it("closes only when every member of P (offline included) signs purpose CLOSE", async function () {
    const [a, b, c] = randomWallets(3);
    const entries = randomEntries(3);
    const { hmsc, sid } = await deployWithInstance([a, b, c], 1n, entries);

    // Missing c's signature (an "offline" member here): rejected.
    const partial = collectSignatures([a, b], PURPOSE_CLOSE, 1n, entries);
    await expect(
      hmsc.close(sid, 1n, entries, partial.signers, partial.v, partial.r, partial.s)
    ).to.be.revertedWith("HMSC: Close needs every member of P");

    // Full set: accepted.
    const full = collectSignatures([a, b, c], PURPOSE_CLOSE, 1n, entries);
    await expect(hmsc.close(sid, 1n, entries, full.signers, full.v, full.r, full.s)).to.not.be.reverted;
    const [, , , , , , , closed] = await hmsc.getInstance(sid);
    expect(closed).to.equal(true);
  });

  it("rejects a Close state signed with purpose STATE instead of CLOSE (TEST-49 half)", async function () {
    const [a, b] = randomWallets(2);
    const entries = randomEntries(2);
    const { hmsc, sid } = await deployWithInstance([a, b], 1n, entries);

    const wrongPurpose = collectSignatures([a, b], PURPOSE_STATE, 1n, entries);
    await expect(
      hmsc.close(sid, 1n, entries, wrongPurpose.signers, wrongPurpose.v, wrongPurpose.r, wrongPurpose.s)
    ).to.be.revertedWith("HMSC: signatures invalid");
  });

  it("data stays readable after closure; a second Close is rejected", async function () {
    const [a, b] = randomWallets(2);
    const entries = randomEntries(2);
    const { hmsc, sid } = await deployWithInstance([a, b], 1n, entries);
    const sigs = collectSignatures([a, b], PURPOSE_CLOSE, 1n, entries);
    await hmsc.close(sid, 1n, entries, sigs.signers, sigs.v, sigs.r, sigs.s);

    const [P, nu] = await hmsc.getInstance(sid);
    expect(P).to.deep.equal([a.address, b.address]);
    expect(nu).to.equal(1n);

    await expect(
      hmsc.close(sid, 1n, entries, sigs.signers, sigs.v, sigs.r, sigs.s)
    ).to.be.revertedWith("HMSC: instance already closed");
  });
});

describe("HMSC.close with a parent (CLOSE-004/009, TEST-44)", function () {
  it("rejects Close outright while Parent != null", async function () {
    const [a, b] = randomWallets(2);
    const entries = randomEntries(2);
    const parentSid = ethers.id("some-parent");
    const { hmsc, sid } = await deployWithInstance([a, b], 1n, entries, parentSid);

    const sigs = collectSignatures([a, b], PURPOSE_CLOSE, 1n, entries);
    await expect(
      hmsc.close(sid, 1n, entries, sigs.signers, sigs.v, sigs.r, sigs.s)
    ).to.be.revertedWith("HMSC: instance has a parent, disassociate first");
  });
});
