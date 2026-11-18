// Deploys EpochAnchor before the Go process starts (Q42).
//
// The deployer is the first Hardhat development account and becomes the single
// authorized committer.
const hre = require("hardhat");
const { ethers } = require("ethers");

async function main() {
  const artifact = await hre.artifacts.readArtifact("EpochAnchor");
  // DHT_ETH_RPC_URL points the deployment at a remote node (the cloud anchor VM
  // runs it against its own loopback); the default is the local Hardhat node.
  const provider = new ethers.JsonRpcProvider(process.env.DHT_ETH_RPC_URL || "http://127.0.0.1:8545");
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
