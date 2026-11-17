// Copies each compiled contract's ABI and creation bytecode from the Hardhat
// artifacts into internal/blockchain/contracts/, where the Go client embeds
// them. Run with `npm run export` after changing a contract.
const fs = require("fs");
const path = require("path");

const out = path.join(__dirname, "..", "internal", "blockchain", "contracts");
fs.mkdirSync(out, { recursive: true });
for (const name of ["VerifyAccess", "VerifyIntegrity"]) {
  const art = JSON.parse(
    fs.readFileSync(path.join(__dirname, "..", "artifacts", "contracts", `${name}.sol`, `${name}.json`), "utf8"),
  );
  fs.writeFileSync(path.join(out, `${name}.abi`), JSON.stringify(art.abi) + "\n");
  fs.writeFileSync(path.join(out, `${name}.bin`), art.bytecode.replace(/^0x/, "") + "\n");
  console.log(`exported ${name}`);
}
