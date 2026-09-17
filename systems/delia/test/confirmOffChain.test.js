// M5: ARCH-001/002 — Confirm has no contract function; state confirmation
// is off-chain only. This is a structural check on the deployed contract's
// own ABI, not a claim about the off-chain hmsc/confirm package (tested
// separately in Go).
const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("HMSC ABI has no state-confirmation entry point (ARCH-001/002, DEV-15)", function () {
  it("no function name mentions confirm/leader-election/sign a round state", async function () {
    const HMSC = await ethers.getContractFactory("HMSC");
    const hmsc = await HMSC.deploy();
    const fragments = hmsc.interface.fragments.filter((f) => f.type === "function");
    const names = fragments.map((f) => f.name.toLowerCase());
    for (const n of names) {
      expect(n).to.not.match(/confirm/);
      expect(n).to.not.match(/electleader/);
      expect(n).to.not.match(/signround/);
    }
    // Sanity: the contract does have Init-adjacent-but-blocked-labelled and
    // Update/Close/Dispute-adjacent entry points, so this isn't a vacuous
    // pass over an empty ABI.
    expect(names).to.include("update");
    expect(names).to.include("close");
    expect(names.length).to.be.greaterThan(3);
  });
});
