// SPDX-License-Identifier: MIT
pragma solidity 0.8.24;

/// EVSEB VerifyAccess contract (Phase 1 Step 2 item 1; grantAccess, Table 3).
///
/// Q1 ruling: Tokenu is decrypted with Kauth OFF-chain (a secret key can't be
/// kept on a public EVM chain). The off-chain verifier sends the resulting
/// claims here, and this contract checks them against the on-chain policy.
///
/// The on-chain policy record is CTPolicy = EncAES(Policy, Kindex) (Eq. 6,
/// stored unchanged) plus commitment C = SHA-256(Policy), where Policy is the
/// Eq. 5 tuple H(UserID) || H(Permissions) || H(LType) || Texp (uint64
/// big-endian). The record is keyed by H(UserID).
///
/// C is a minimal EVM compatibility mechanism, NOT an EVSEB-defined
/// construction (QUESTIONS.md Q11). The contract can't read CTPolicy, so it
/// checks the claims against C instead.
///
/// Confidentiality caveat (Q11): C hides the Eq. 5 fields only in stored
/// contract state. When verifyAccess is sent as a transaction, its arguments
/// (H(UserID), Permissions, Texp, H(LType), LType) are public calldata. This
/// reproduction claims no policy confidentiality beyond that.
///
/// B17: grantAccess stores what it is given. There is no access control and
/// no event (on-chain access logging is unspecified by the paper and stays
/// open).
contract VerifyAccess {
    struct PolicyRecord {
        bytes ctPolicy;
        bytes32 commitment;
        bool exists;
    }

    mapping(bytes32 => PolicyRecord) private policies;

    /// Blockchain_Store(UserID, CTPolicy) (Step 3.1 line 9).
    function grantAccess(bytes32 hUserID, bytes calldata ctPolicy, bytes32 commitment) external {
        policies[hUserID] = PolicyRecord(ctPolicy, commitment, true);
    }

    /// Blockchain_Retrieve(UserID) (Step 3.2 line 2).
    function getPolicy(bytes32 hUserID) external view returns (bytes memory ctPolicy, bytes32 commitment, bool exists) {
        PolicyRecord storage r = policies[hUserID];
        return (r.ctPolicy, r.commitment, r.exists);
    }

    /// VerifyAccess(Tokenu, LType) -> Allow/Deny (Eq. 21), over the claims
    /// decoded off-chain from Tokenu: H(UserID), Permissions and Texp, plus
    /// H(LType) from the decrypted policy. The plaintext UserID is not needed:
    /// the record is keyed by H(UserID), and the commitment covers H(UserID).
    function verifyAccess(
        bytes32 hUser,
        string[] calldata permissions,
        uint64 texp,
        bytes32 hLType,
        string calldata lType
    ) external view returns (bool) {
        PolicyRecord storage r = policies[hUser];
        if (!r.exists) {
            return false;
        }
        // H(Permissions): uint32 count, then per entry uint32 length + bytes.
        bytes memory enc = abi.encodePacked(uint32(permissions.length));
        for (uint256 i = 0; i < permissions.length; i++) {
            enc = abi.encodePacked(enc, uint32(bytes(permissions[i]).length), bytes(permissions[i]));
        }
        bytes32 hPerm = sha256(enc);
        if (sha256(abi.encodePacked(hUser, hPerm, hLType, texp)) != r.commitment) {
            return false;
        }
        if (block.timestamp > texp) {
            return false;
        }
        bytes32 want = keccak256(bytes(lType));
        for (uint256 i = 0; i < permissions.length; i++) {
            if (keccak256(bytes(permissions[i])) == want) {
                return true;
            }
        }
        return false;
    }
}
