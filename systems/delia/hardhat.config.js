// Hardhat v2.29 / Solidity 0.8.24. Local development/testing tooling only
// (M5, SC-001/002/006) — see package.json's description and TRACKER.md's M5
// section for why this differs from the SRS's own experimental deployment
// target (GAP-50: a private 10-node Geth-compatible PoA network).
// hardhat-ethers and hardhat-chai-matchers are standard test-assertion
// plumbing (not hardhat-toolbox's much larger bundle); no other plugin.
require("@nomicfoundation/hardhat-ethers");
require("@nomicfoundation/hardhat-chai-matchers");

module.exports = {
  solidity: {
    version: "0.8.24",
    settings: {
      optimizer: { enabled: true, runs: 200 },
      // viaIR: this contract's functions take many calldata array
      // parameters (signer/v/r/s tuples), which overflows the legacy
      // codegen's stack even with the optimizer on. viaIR is a standard
      // solc compilation pipeline option, not a protocol/behavior change.
      viaIR: true,
    },
  },
  networks: {
    hardhat: {
      chainId: 31337,
    },
    localhost: {
      url: "http://127.0.0.1:8545",
      chainId: 31337,
    },
  },
};
