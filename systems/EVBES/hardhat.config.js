// Hardhat v2.29 / Solidity 0.8.24, pinned like ../delia. Default compiler
// settings (no optimizer); the paper states no compiler settings. Gas figures
// for Table 3 are measured with these settings.
require("@nomicfoundation/hardhat-ethers");

module.exports = {
  solidity: "0.8.24",
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
