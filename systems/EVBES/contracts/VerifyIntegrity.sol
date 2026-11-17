// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

/// EVSEB VerifyIntegrity contract (Phase 1 Step 2 item 2; updateMerkleRoot,
/// Table 3). It holds the quaternary Merkle root R over every hLID, the
/// on-chain anchor of Fig. 3.
///
/// B17: updateMerkleRoot stores what it is given, with no access control and
/// no event.
contract VerifyIntegrity {
    bytes32 private root;

    function updateMerkleRoot(bytes32 newRoot) external {
        root = newRoot;
    }

    /// VerifyIntegrity() -> Merkle Root. Zero means no root has been anchored.
    function verifyIntegrity() external view returns (bytes32) {
        return root;
    }
}
