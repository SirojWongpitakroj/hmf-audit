// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.20;

/// @title EpochAnchor
/// @notice Minimal append-only anchoring contract (Q14).
///
/// The on-chain payload is exactly (epoch, R_agg) — no client timestamp and no
/// CA signature (Q15). Storage per epoch is one bytes32, independent of the
/// number of records in the epoch (R-EPO-05).
contract EpochAnchor {
    /// @notice The single configured authorized committer (Q14, Q42).
    address public immutable committer;

    /// @notice Append-only epoch -> root-of-roots mapping.
    mapping(uint256 => bytes32) public epochRoots;

    event RootCommitted(uint256 epoch, bytes32 root);

    constructor() {
        committer = msg.sender;
    }

    /// @notice Anchors one epoch's root-of-roots. Overwriting an epoch that
    /// already holds a root is rejected (Q14).
    function commit(uint256 epoch, bytes32 root) external {
        require(msg.sender == committer, "EpochAnchor: not the authorized committer");
        require(epochRoots[epoch] == bytes32(0), "EpochAnchor: epoch already committed");
        epochRoots[epoch] = root;
        emit RootCommitted(epoch, root);
    }

    /// @notice Reads the anchored root for an epoch.
    function getRoot(uint256 epoch) external view returns (bytes32) {
        return epochRoots[epoch];
    }
}
