// M7: Disassociation, Tier-A slice — TEST-39 (disassociation),
// TEST-40 (invalid child signature leaves the parent open).
const { expect } = require("chai");
const { ethers } = require("hardhat");
const { PURPOSE_CLOSE, collectSignatures, randomWallets, randomEntries } = require("./helpers");

async function setup() {
  const HMSC = await ethers.getContractFactory("HMSC");
  const hmsc = await HMSC.deploy();

  const [g1leader, g2leader] = randomWallets(2);
  const sidG1 = ethers.id("G1");
  const sidG2 = ethers.id("G2");
  const sidG3 = ethers.id("G3"); // parent

  const entriesG1 = randomEntries(1);
  const entriesG2 = randomEntries(1);
  const entriesG3 = randomEntries(2);

  await hmsc.createInstanceForTesting(sidG1, [g1leader.address], 3n, entriesG1, [], ethers.ZeroHash, []);
  await hmsc.createInstanceForTesting(sidG2, [g2leader.address], 3n, entriesG2, [], ethers.ZeroHash, []);
  await hmsc.createInstanceForTesting(
    sidG3,
    [g1leader.address, g2leader.address],
    3n,
    entriesG3,
    [],
    ethers.ZeroHash,
    [sidG1, sidG2]
  );

  return { hmsc, g1leader, g2leader, sidG1, sidG2, sidG3, entriesG1, entriesG2 };
}

describe("HMSC.disassociate (CLOSE-006/007, TEST-39)", function () {
  it("closes Gamma3 once both child leaders sign; children stay open with Parent reset to null", async function () {
    const { hmsc, g1leader, g2leader, sidG1, sidG2, sidG3, entriesG1, entriesG2 } = await setup();

    const sig1 = collectSignatures([g1leader], PURPOSE_CLOSE, 3n, entriesG1);
    const sig2 = collectSignatures([g2leader], PURPOSE_CLOSE, 3n, entriesG2);

    await hmsc.disassociate(
      sidG3,
      [sidG1, sidG2],
      [sig1.signers[0], sig2.signers[0]],
      [sig1.v[0], sig2.v[0]],
      [sig1.r[0], sig2.r[0]],
      [sig1.s[0], sig2.s[0]]
    );

    const [, , , , , , , closedG3] = await hmsc.getInstance(sidG3);
    expect(closedG3).to.equal(true);

    const [, , , , , parentG1, , closedG1] = await hmsc.getInstance(sidG1);
    const [, , , , , parentG2, , closedG2] = await hmsc.getInstance(sidG2);
    expect(parentG1).to.equal(ethers.ZeroHash);
    expect(parentG2).to.equal(ethers.ZeroHash);
    expect(closedG1).to.equal(false); // children stay open
    expect(closedG2).to.equal(false);
  });

  it("rejects DisassociateMsg while Gamma3 itself has a non-null Parent (LIM-04)", async function () {
    const HMSC = await ethers.getContractFactory("HMSC");
    const hmsc = await HMSC.deploy();
    const [leader] = randomWallets(1);
    const sidChild = ethers.id("only-child");
    const sidParent = ethers.id("nested-parent");
    const grandparent = ethers.id("grandparent");
    const entries = randomEntries(1);

    await hmsc.createInstanceForTesting(sidChild, [leader.address], 1n, entries, [], ethers.ZeroHash, []);
    await hmsc.createInstanceForTesting(sidParent, [leader.address], 1n, entries, [], grandparent, [sidChild]);

    const sig = collectSignatures([leader], PURPOSE_CLOSE, 1n, entries);
    await expect(
      hmsc.disassociate(sidParent, [sidChild], sig.signers, sig.v, sig.r, sig.s)
    ).to.be.revertedWith("HMSC: nested disassociation not supported");
  });
});

describe("HMSC.disassociate invalid child signature (CLOSE-007, TEST-40)", function () {
  it("leaves Gamma3 open when one child signature is corrupted", async function () {
    const { hmsc, g1leader, g2leader, sidG1, sidG2, sidG3, entriesG1, entriesG2 } = await setup();

    const sig1 = collectSignatures([g1leader], PURPOSE_CLOSE, 3n, entriesG1);
    const sig2 = collectSignatures([g2leader], PURPOSE_CLOSE, 3n, entriesG2);
    const corruptedS = ethers.zeroPadValue(ethers.toBeHex(BigInt(sig2.s[0]) ^ 1n), 32);

    await expect(
      hmsc.disassociate(
        sidG3,
        [sidG1, sidG2],
        [sig1.signers[0], sig2.signers[0]],
        [sig1.v[0], sig2.v[0]],
        [sig1.r[0], sig2.r[0]],
        [sig1.s[0], corruptedS]
      )
    ).to.be.revertedWith("HMSC: child signature invalid");

    const [, , , , , , , closedG3] = await hmsc.getInstance(sidG3);
    expect(closedG3).to.equal(false);
  });
});
