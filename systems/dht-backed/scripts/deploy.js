// Deploys EpochAnchor before the Go process starts (Q42).
//
// The deployer is the first Hardhat development account and becomes the single
// authorized committer.
const hre = require("hardhat");
const { ethers } = require("ethers");

async function main() {
  const artifact = await hre.artifacts.readArtifact("EpochAnchor");
  const provider = new ethers.JsonRpcProvider("http://127.0.0.1:8545");
  const network = await provider.getNetwork();
  if (network.chainId !== 31337n) {
    throw new Error("expected chain id 31337, got " + network.chainId);
  }
  const signer = await provider.getSigner(0);
  const factory = new ethers.ContractFactory(artifact.abi, artifact.bytecode, signer);
  const contract = await factory.deploy();
  await contract.waitForDeployment();
  console.log("DHT_CONTRACT_ADDRESS=" + (await contract.getAddress()));
  console.log("committer=" + (await signer.getAddress()));
}

main().catch((e) => {
  console.error(e);
  process.exitCode = 1;
});
