// M7: New-leader selection (DISPUTE-012, TEST-47).
// Candidate-list filtering (excluding the removed participant and finalized
// OffList members; a pending OfflineMsg does not exclude) is Go-side
// (hmsc/confirm.Eligible, shared with TEST-17/17b) — this contract function
// is only the isolated block-hash-derived pick.
const { expect } = require("chai");
const { ethers, network } = require("hardhat");
const { randomWallets } = require("./helpers");

describe("HMSC.selectNewLeader (DISPUTE-012, TEST-47)", function () {
  it("picks index = uint(blockhash(block.number - 1)) mod candidate count", async function () {
    const HMSC = await ethers.getContractFactory("HMSC");
    const hmsc = await HMSC.deploy();
    const wallets = randomWallets(4);
    const candidates = wallets.map((w) => w.address);

    // Mine one block so block.number - 1 has a known, retrievable hash.
    await network.provider.send("evm_mine");
    const latest = await ethers.provider.getBlockNumber();
    // Hardhat's eth_call executes with block.number == the current chain
    // head (`latest`), so block.number - 1 resolves to `latest - 1` —
    // confirmed empirically against Hardhat 2.29's eth_call semantics.
    const prevBlock = await ethers.provider.getBlock(latest - 1);
    const expectedIndex = Number(BigInt(prevBlock.hash) % BigInt(candidates.length));

    const picked = await hmsc.selectNewLeader(candidates);
    expect(picked).to.equal(candidates[expectedIndex]);
  });

  it("reverts with no eligible candidates", async function () {
    const HMSC = await ethers.getContractFactory("HMSC");
    const hmsc = await HMSC.deploy();
    await expect(hmsc.selectNewLeader([])).to.be.revertedWith("HMSC: no eligible candidates");
  });

  it("candidate list passed in already excludes the removed participant and finalized OffList members", async function () {
    // This contract function trusts its input list; the exclusion itself is
    // hmsc/confirm.Eligible's job (Go-side, shared with TEST-17/17b). Here
    // we just confirm the picked address is always drawn from the supplied
    // candidates, so a caller that has already filtered out ineligible
    // participants gets a leader chosen only from what it supplied.
    const HMSC = await ethers.getContractFactory("HMSC");
    const hmsc = await HMSC.deploy();
    const wallets = randomWallets(3);
    const candidates = wallets.map((w) => w.address);
    const picked = await hmsc.selectNewLeader(candidates);
    expect(candidates).to.include(picked);
  });
});
