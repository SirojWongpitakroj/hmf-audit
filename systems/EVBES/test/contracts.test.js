// Hardhat tests for the EVSEB contracts against the real compiled bytecode.
const { expect } = require("chai");
const { ethers } = require("hardhat");

const sha256 = (b) => ethers.sha256(b);
const utf8 = (s) => ethers.toUtf8Bytes(s);

// H(Permissions): uint32 count, then per entry uint32 length + bytes.
function hPerms(perms) {
  const types = ["uint32"];
  const vals = [perms.length];
  for (const p of perms) {
    types.push("uint32", "bytes");
    vals.push(utf8(p).length, utf8(p));
  }
  return sha256(ethers.solidityPacked(types, vals));
}

function policy(userID, perms, lType, texp) {
  const hUser = sha256(utf8(userID));
  const commitment = sha256(
    ethers.solidityPacked(["bytes32", "bytes32", "bytes32", "uint64"], [hUser, hPerms(perms), sha256(utf8(lType)), texp]),
  );
  return { hUser, hLType: sha256(utf8(lType)), commitment };
}

async function now() {
  return (await ethers.provider.getBlock("latest")).timestamp;
}

describe("VerifyAccess", function () {
  let va;
  beforeEach(async function () {
    va = await (await ethers.getContractFactory("VerifyAccess")).deploy();
  });

  it("grantAccess stores CTPolicy and commitment keyed by H(UserID)", async function () {
    const p = policy("alice", ["Firewall Logs"], "Firewall Logs", 2n ** 40n);
    await va.grantAccess(p.hUser, "0xdeadbeef", p.commitment);
    const [ct, c, exists] = await va.getPolicy(p.hUser);
    expect(ct).to.equal("0xdeadbeef");
    expect(c).to.equal(p.commitment);
    expect(exists).to.equal(true);
    const [, , none] = await va.getPolicy(sha256(utf8("bob")));
    expect(none).to.equal(false);
  });

  it("allows matching claims for a permitted LType, denies others", async function () {
    const perms = ["Firewall Logs", "IDPS Logs"];
    const texp = BigInt(await now()) + 3600n;
    const p = policy("alice", perms, "Firewall Logs", texp);
    await va.grantAccess(p.hUser, "0x01", p.commitment);
    expect(await va.verifyAccess(p.hUser, perms, texp, p.hLType, "Firewall Logs")).to.equal(true);
    expect(await va.verifyAccess(p.hUser, perms, texp, p.hLType, "IDPS Logs")).to.equal(true);
    expect(await va.verifyAccess(p.hUser, perms, texp, p.hLType, "Application Server Logs")).to.equal(false);
    // Forged claims: extra permission, other Texp, other user, other H(LType).
    expect(await va.verifyAccess(p.hUser, [...perms, "Application Server Logs"], texp, p.hLType, "Application Server Logs")).to.equal(false);
    expect(await va.verifyAccess(p.hUser, perms, texp + 1n, p.hLType, "Firewall Logs")).to.equal(false);
    expect(await va.verifyAccess(sha256(utf8("bob")), perms, texp, p.hLType, "Firewall Logs")).to.equal(false);
    expect(await va.verifyAccess(p.hUser, perms, texp, sha256(utf8("IDPS Logs")), "Firewall Logs")).to.equal(false);
  });

  it("denies expired claims", async function () {
    const perms = ["Firewall Logs"];
    const texp = BigInt(await now()) - 1n;
    const p = policy("alice", perms, "Firewall Logs", texp);
    await va.grantAccess(p.hUser, "0x01", p.commitment);
    expect(await va.verifyAccess(p.hUser, perms, texp, p.hLType, "Firewall Logs")).to.equal(false);
  });
});

describe("VerifyIntegrity", function () {
  it("updateMerkleRoot then verifyIntegrity returns the root", async function () {
    const vi = await (await ethers.getContractFactory("VerifyIntegrity")).deploy();
    expect(await vi.verifyIntegrity()).to.equal(ethers.ZeroHash);
    const r = sha256(utf8("root"));
    await vi.updateMerkleRoot(r);
    expect(await vi.verifyIntegrity()).to.equal(r);
  });
});
