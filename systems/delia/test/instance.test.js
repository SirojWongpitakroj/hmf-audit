// M5: Gamma instance representation (ARCH-003) and multiple simultaneous
// instances (ARCH-005/007).
const { expect } = require("chai");
const { ethers } = require("hardhat");
const { randomEntries } = require("./helpers");

describe("HMSC instance representation (ARCH-003)", function () {
  async function deploy() {
    const HMSC = await ethers.getContractFactory("HMSC");
    return HMSC.deploy();
  }

  it("stores exactly the seven Gamma fields, readable back", async function () {
    const hmsc = await deploy();
    const [p1, p2, p3] = await ethers.getSigners();
    const sid = ethers.id("instance-A");
    const entries = randomEntries(3);

    await hmsc.createInstanceForTesting(
      sid,
      [p1.address, p2.address, p3.address],
      1n,
      entries,
      [],
      ethers.ZeroHash,
      []
    );

    const [P, nu, gotEntries, offList, newLeader, parent, childList, closed] =
      await hmsc.getInstance(sid);

    expect(P).to.deep.equal([p1.address, p2.address, p3.address]);
    expect(nu).to.equal(1n);
    expect(gotEntries.map((e) => e.toLowerCase())).to.deep.equal(
      entries.map((e) => e.toLowerCase())
    );
    expect(offList).to.deep.equal([]);
    expect(newLeader).to.equal(ethers.ZeroAddress);
    expect(parent).to.equal(ethers.ZeroHash);
    expect(childList).to.deep.equal([]);
    expect(closed).to.equal(false);
  });

  it("rejects a second creation of the same sid (no silent overwrite)", async function () {
    const hmsc = await deploy();
    const [p1] = await ethers.getSigners();
    const sid = ethers.id("dup");
    await hmsc.createInstanceForTesting(sid, [p1.address], 1n, randomEntries(1), [], ethers.ZeroHash, []);
    await expect(
      hmsc.createInstanceForTesting(sid, [p1.address], 1n, randomEntries(1), [], ethers.ZeroHash, [])
    ).to.be.revertedWith("HMSC: sid already exists");
  });
});

describe("HMSC multiple simultaneous instances (ARCH-005/007)", function () {
  it("two independent instances coexist without either being closed/recreated", async function () {
    const HMSC = await ethers.getContractFactory("HMSC");
    const hmsc = await HMSC.deploy();
    const [p1, p2, p3, p4] = await ethers.getSigners();

    const sidA = ethers.id("gammaA");
    const sidB = ethers.id("gammaB");

    await hmsc.createInstanceForTesting(sidA, [p1.address, p2.address], 1n, randomEntries(2), [], ethers.ZeroHash, []);
    await hmsc.createInstanceForTesting(sidB, [p3.address, p4.address], 1n, randomEntries(2), [], ethers.ZeroHash, []);

    const [, nuA] = await hmsc.getInstance(sidA);
    const [, nuB] = await hmsc.getInstance(sidB);
    expect(nuA).to.equal(1n);
    expect(nuB).to.equal(1n);

    // Neither instance references or depends on the other; both remain
    // open and independently readable (no automatic partitioning/merge).
    const [, , , , , , , closedA] = await hmsc.getInstance(sidA);
    const [, , , , , , , closedB] = await hmsc.getInstance(sidB);
    expect(closedA).to.equal(false);
    expect(closedB).to.equal(false);
  });
});
