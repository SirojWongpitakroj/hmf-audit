// Hardhat v2.20 / Solidity 0.8.20, exactly as Table 5 specifies (Q38, Q42).
// No Hardhat plugin is required: the deployment script uses ethers directly.

module.exports = {
  solidity: "0.8.20",
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
