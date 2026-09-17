// Shared signing/encoding helpers for the HMSC.sol Hardhat test suite.
//
// These mirror the Go-side GAP-73 layouts exactly so the same digest the
// contract computes (see HMSC.sol's _digest) can be reproduced here:
//   encodedObject = uint64 nu (BE) || uint32 entry_count (BE) || entries (32 bytes each)
//   signedBytes   = purpose (1 byte) || signer_role (1 byte) || encodedObject
//   digest        = SHA-256(signedBytes)
//
// Signing uses ethers' SigningKey.sign(digest), which signs the given
// 32-byte digest directly (no re-hashing) — the EVM's ecrecover precompile
// is hash-function-agnostic, so a SHA-256 digest (CRYPTO-002: SHA-256 is the
// sole hash in the system) verifies correctly on chain via ecrecover even
// though Ethereum's own transaction/message signing convention normally
// hashes with keccak256. This lets the Hardhat tests use ordinary ethers
// wallets (same secp256k1 + Ethereum address derivation as the Go `sig`
// package, CRYPTO-008) while staying faithful to CRYPTO-002.
const { ethers } = require("hardhat");
const crypto = require("crypto");

const PURPOSE_STATE = 0x01;
const PURPOSE_CLOSE = 0x02;
const ROLE_SIGNER = 0x01;

function encodeEntries(nu, entries) {
  const parts = [
    ethers.toBeHex(nu, 8),
    ethers.toBeHex(entries.length, 4),
    ...entries,
  ];
  return ethers.concat(parts);
}

function digestFor(purpose, nu, entries) {
  const encodedObject = encodeEntries(nu, entries);
  const signedBytes = ethers.concat([
    ethers.toBeHex(purpose, 1),
    ethers.toBeHex(ROLE_SIGNER, 1),
    encodedObject,
  ]);
  const bytes = Buffer.from(ethers.getBytes(signedBytes));
  const digest = crypto.createHash("sha256").update(bytes).digest();
  return "0x" + digest.toString("hex");
}

// signState signs (nu, entries) under `purpose` with `wallet` (an
// ethers.Wallet), returning {signer, v, r, s} ready for the contract calls.
function signState(wallet, purpose, nu, entries) {
  const digest = digestFor(purpose, nu, entries);
  const signingKey = new ethers.SigningKey(wallet.privateKey);
  const sig = signingKey.sign(digest);
  return {
    signer: wallet.address,
    v: sig.v,
    r: sig.r,
    s: sig.s,
  };
}

function collectSignatures(wallets, purpose, nu, entries) {
  const signed = wallets.map((w) => signState(w, purpose, nu, entries));
  return {
    signers: signed.map((x) => x.signer),
    v: signed.map((x) => x.v),
    r: signed.map((x) => x.r),
    s: signed.map((x) => x.s),
  };
}

// randomWallets creates n standalone local ethers.Wallet instances to stand
// in for DELIA participants (CRYPTO-008/009: one secp256k1 key per
// participant, generated at provisioning, no runtime PKI — these tests
// generate them locally rather than reusing Hardhat's funded JSON-RPC
// signer accounts, which do not expose a private key for local digest
// signing). They need no ETH: SigningKey.sign is a pure local operation:
// the funded default Hardhat signer sends the actual transactions.
function randomWallets(n) {
  return Array.from({ length: n }, () => ethers.Wallet.createRandom());
}

function randomEntries(n) {
  const out = [];
  for (let i = 0; i < n; i++) {
    out.push(ethers.hexlify(ethers.randomBytes(32)));
  }
  return out;
}

module.exports = {
  PURPOSE_STATE,
  PURPOSE_CLOSE,
  ROLE_SIGNER,
  digestFor,
  signState,
  collectSignatures,
  randomWallets,
  randomEntries,
};
