# EVSEB — Requirements Analysis for Go Reproduction

Source of truth: `EVSEB_Efficient_and_Verifiable_Searchable_Encryption_With_Boolean_Search_for_Encrypted_Cloud_Logs.pdf`
(Fugkeaw, Deevijit, Ueasathitwong, Thanyasukpaisal, IEEE Access, vol. 13, 2025, pp. 101177–101195, DOI 10.1109/ACCESS.2025.3577466)

Every requirement below is tagged **[EXPLICIT]**, **[DERIVED]**, or **[UNSPECIFIED BY PAPER]**, and cites the paper's section/phase/equation/algorithm/figure. Category A (paper requirements) and Category B (minimal prototype decisions forced by paper gaps) are kept strictly separate — Category B never appears without an explicit label.

No repo-wide `tasks.md` or cross-system architecture convention document exists in `hmf-log-integrity/` at the time of writing (checked). The sibling system `../dht-backed` uses Solidity + Hardhat (JS tooling) for contracts plus a Go client-binding package (`hmsc/chain`, per its commit history) to talk to Ethereum from Go. This is noted only as a possible point of stack alignment (see "Open questions," item 2) — it is **not** an EVSEB requirement.

---

## 1. Implementation objective

Build a Go research prototype that reproduces EVSEB's algorithms — key generation, blockchain-anchored access control, hierarchical log-type tree + Bloom filter + inverted index, keyword extraction, log/metadata/index encryption, HMAC-based trapdoor generation, Boolean (AND/OR/NOT) search, and DHT+quaternary-Merkle-tree integrity verification with per-file tamper localization — closely enough to compare it against the other systems in `hmf-log-integrity/systems/` (dht-backed, delia, blas). The goal is fidelity to the published construction, not an improved or production-grade redesign. **[EXPLICIT]** basis: Abstract, Sec. I (contributions), Sec. IV.D (cryptographic construction).

---

## 2. Scope

**In scope** (must be traceable to a specific paper citation):
- Phase 1–6 algorithms exactly as specified (Sec. IV.D).
- The six named cryptographic constructs: `Kenc, Kindex, Kauth, Ksearch`, AES, SHA-256, HMAC-SHA256/PRF (Eq. 3, 19).
- Hierarchical log-type tree, Bloom filter, inverted index (Sec. IV.D Phase 2/4, Def. 2–3, Alg. 1–2).
- Boolean search AND/OR/NOT (Eq. 25–27, Alg. 3).
- DHT + quaternary Merkle tree integrity verification and tampered-node tracing (Alg. 4.1–4.2, Eq. 28, Fig. 3, Theorem 2).
- Blockchain-based `VerifyAccess`/`VerifyIntegrity` smart contracts (Sec. IV.D Phase 1 Step 2).
- User registration, access policy, access token generation (Sec. IV.D Phase 1 Step 3).

**Out of scope**: see Section 17. In short — no caching, no extra DBs/indexes, no concurrency/worker pools, no distributed systems beyond what the paper names (DHT, blockchain), no alternative search or Merkle-tree designs, no revocation beyond token expiry, no cross-domain sharing (the paper itself names these last two as future work — Sec. VII Conclusion).

---

## 3. System actors/components

All six entities are **[EXPLICIT]**, Sec. IV.A ("System Model") and Fig. 1:

| # | Component | Paper responsibility |
|---|---|---|
| 1 | **Cloud Storage Provider** | Stores encrypted log files and associated metadata; hosts the hierarchical tree index for categorization/search (Sec. IV.A.1). |
| 2 | **Blockchain Platform** | Decentralized ledger for authentication, access control, and search-operation records; hosts smart contracts; stores "metadata such as file indices, file locations, and pre-hashed keywords" (Sec. IV.A.2). |
| 3 | **Smart Contracts** | Two named contracts: `VerifyAccess` (decrypt token, check access rights for a log type) and `VerifyIntegrity` (compare recomputed hash to on-chain Merkle root) (Sec. IV.A.3, Phase 1 Step 2). |
| 4 | **Search Engine** | Processes queries using the hybrid index (tree + inverted index + Bloom filter); hashes the query keyword and matches against preprocessed keywords in the tree (Sec. IV.A.4). |
| 5 | **Data Users (DU)** | Authorized individuals issuing search queries; assumed semi-honest/potentially malicious in the threat model (Sec. IV.A.5, IV.B). |
| 6 | **Sources of log files** | IDS, IPS, firewalls, web servers (Sec. IV.A.6). |

**[EXPLICIT — implied, unnamed actor]**: a "Data Owner (DO)" / "Registration Authority (RA)" is referenced in Phase 1 Step 3 ("users submit their identity credentials to the Registration Authority") and in the Threat Model ("The DO is fully trusted," Sec. IV.B). The paper never formally lists DO/RA as numbered system-model entities alongside the six above, but both perform actions load-bearing to the protocol (RA runs `Register_User`; DO is assumed to hold `Kenc`/`Kindex`/`Kauth`/`Ksearch` and encrypt logs). Treat as **[DERIVED]** actors required to make Phase 1 and Phase 3 executable.

---

## 4. Functional requirements

Numbered FR items, each traced to a paper location. All **[EXPLICIT]** unless noted.

- **FR-1** Generate four independent 256-bit keys `Kenc, Kindex, Kauth, Ksearch` via a CSPRNG (`Rand(256)`). — Eq. 3.
- **FR-2** Encrypt log entries and their metadata under `Kenc` using AES. — Eq. 14, 15.
- **FR-3** Encrypt the entire index structure (tree + Bloom filter + inverted index) under `Kindex`. — Eq. 17.
- **FR-4** Encrypt/decrypt user access tokens under `Kauth`; verify token validity (expiry) before honoring a query. — Eq. 4, 7; Step 3.2 pseudocode line 5–7.
- **FR-5** Derive a PRF-based trapdoor `Tw = HMAC-SHA256(Ksearch, SHA-256(w))` for a keyword `w`, both at index-build time and at query time (same function on both sides so trapdoors can be equality-matched against index entries). — Eq. 18–19; Phase 3 Step 1 ("hashed and encrypted using HMAC with … Ksearch"); Alg. 2 lines 8–9.
- **FR-6** Register a user: verify credentials, generate `Texp`, build `Policy = {H(UserID), H(Permissions), H(LType), Texp}`, encrypt it under `Kindex`, store on blockchain. — Eq. 5–6; Pseudocode Step 3.1.
- **FR-7** Generate a user token from a stored policy, rejecting if expired. — Pseudocode Step 3.2. **[Note: see Ambiguity A7 — this pseudocode is not directly executable as written.]**
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q6 (QUESTIONS.md, ruled 2026-09-19): B7 rejected. CTPolicy stays exactly Eq. 5–6. Tokenu is generated by the trusted registration/token authority while it still holds the plaintext registration inputs. Step 3.2 as written is internally inconsistent (it would have to reverse H(UserID)/H(Permissions)), and FR-7 is closed on that basis.
- **FR-8** `VerifyAccess(Tokenu, LType) → Allow/Deny`: decrypt token with `Kauth`, check permissions against requested log type. — Phase 1 Step 2 item 1; Eq. 21.
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q11 (QUESTIONS.md, ruled 2026-09-19): the contract checks the off-chain claims (H(UserID), Permissions, Texp, H(LType)) against C = SHA-256(Eq. 5 Policy bytes), stored next to CTPolicy. C is a minimal EVM compatibility mechanism, NOT an EVSEB-defined construction. It hides the Eq. 5 fields only in stored contract state: `verifyAccess` arguments sent as a transaction are public calldata, and no further policy confidentiality is claimed. No plaintext UserID is sent.
- **FR-9** `VerifyIntegrity() → Merkle Root`: return the on-chain Merkle root as the anchor value for comparison. — Phase 1 Step 2 item 2; Alg. 4.1 line 2 (`StoredProof ← verifyIntegrity()`).
- **FR-10** Build the hierarchical tree: create a primary node per log type if absent, attach an inverted index and (per sub-node) a Bloom filter. — Def. 2; Alg. 1 (`treeSetup`).
- **FR-11** Preprocess raw log content: lowercase, strip special characters/punctuation, remove stopwords. — Eq. 11.
- **FR-12** Tokenize preprocessed content into a token set. — Eq. 12.
- **FR-13** Select keywords from tokens by "domain relevance, length, and criteria" (**[UNSPECIFIED BY PAPER]** — see A18). — Eq. 13.
- **FR-14** Assign each log entry a unique `LID` linking it to its position in the index. — Phase 3 Step 2 prose.
- **FR-15** Insert each extracted keyword's trapdoor into the correct sub-node's inverted index and Bloom filter, keyed by `LID`. — Alg. 2.
- **FR-16** On query, check Bloom-filter membership before touching the inverted index; skip the branch on a negative. — Eq. 16, 23; Alg. 3 lines 9–11.
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q8 (QUESTIONS.md, ruled 2026-09-19): LocateTreeNode resolves LType to its primary node. Every sub-node under it gets the Bloom-filter test. Negative sub-nodes are skipped, and candidate LIDs from every positive sub-node are unioned. For NOT, Lall = all LIDs under that primary LType node.
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q12 item 4 (QUESTIONS.md, ruled 2026-09-19): "No matching records found" follows the Boolean definitions. AND: when any keyword has no candidate. OR (and no operator): when every keyword has none. NOT: never; if its keyword has no candidate, the result is Lall. A keyword has no candidate when no sub-node yields an LID for it.
- **FR-17** On a Bloom-filter hit, retrieve candidate `LID`s from the inverted index. — Eq. 24; Alg. 3 line 12.
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q8 (QUESTIONS.md, ruled 2026-09-19): LocateTreeNode resolves LType to its primary node. Every sub-node under it gets the Bloom-filter test. Negative sub-nodes are skipped, and candidate LIDs from every positive sub-node are unioned. For NOT, Lall = all LIDs under that primary LType node.
- **FR-18** Apply Boolean AND/OR/NOT as set intersection/union/difference over retrieved `LID` sets. — Eq. 25–27; Alg. 3 lines 18–37.
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q6 (QUESTIONS.md, ruled 2026-09-19): B9 approved. Tw is read as a list/set of trapdoors, so one query can combine two or more keywords under AND/OR/NOT. This is a reading of the paper's query, not a different search algorithm.
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q8 (QUESTIONS.md, ruled 2026-09-19): NOT's Lall is all LIDs under the primary LType node.
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q12 item 3 (QUESTIONS.md, ruled 2026-09-19): AND and OR may take several trapdoors. NOT takes exactly one (Eq. 27 is unary), and a multi-keyword NOT is rejected as unsupported, never evaluated as Lall \ (L1 ∪ … ∪ Lk).
- **FR-19** Verify integrity of the candidate result set by recomputing the quaternary Merkle root over the corresponding leaf hashes and comparing to the on-chain root; on mismatch, trace and exclude tampered `LID`s. — Alg. 4.1–4.2.
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q9 (QUESTIONS.md, ruled 2026-09-19): Each candidate's hLID is verified by its quaternary Merkle proof path against the full-tree root R stored on-chain. Alg. 4.1's wording, which recomputes a root over only the candidates, is recorded as an inconsistency in the paper, because that root can't equal R for a partial result set. No per-query root is anchored.
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q10 (QUESTIONS.md, ruled 2026-09-19): At Level 0, only the leaves under the affected level-1 branches are inspected. Alg. 4.2's literal loop over every leaf conflicts with the paper's Fig. 9 description ("only drills down into the relevant branches"). This is the chosen resolution of that conflict.
- **FR-20** Return the verified, encrypted log entries (not plaintext) corresponding to the surviving `LID`s as the final result. — end of Phase 6 prose.
- **FR-21** DHT stores `H(LID) → hLID` where `hLID = H(EncAES(Ln, Kenc))`. — Eq. 28; Theorem 2 proof text.
- **FR-22** Merkle tree is quaternary (4 children per non-leaf node); leaves are the `hLID` values; root `R` is anchored on-chain. — Theorem 2; Fig. 3.

---

## 5. Security/cryptographic requirements

**[EXPLICIT]**, Sec. IV.B–C and Sec. V:

- Threat model: honest-but-curious Cloud Storage Provider (follows protocol but may analyze indexes/trapdoors/access patterns, or return incomplete results); semi-honest/malicious DU (may forge tokens, link trapdoors, or dispute valid results). DO is fully trusted; no user–server collusion. AES-256, HMAC-SHA256, SHA-256 assumed secure. — Sec. IV.B.
- Security model: simulation-based, real-vs-ideal, PPT adversary `A`, security parameter `λ`, leakage profile `L = {LSetup, LKeyGen, LTrapdoor, LSearch, LVerify, LDecrypt}`, advantage bound `negl(λ)`. — Eq. 2.
- Leakage functions: `LSetup = LKeyGen = LVerify = ⊥` (no leakage); `LTrapdoor` reveals keyword repetition pattern; `LSearch` reveals number of matching entries; `LDecrypt` outputs valid pointers only if access is granted. — Sec. IV.C.
- Security goals: (1) IND-CKA2 confidentiality of trapdoors under HMAC-SHA256-as-PRF (Def. 4, Theorem 1); (2) trapdoor unlinkability and keyword-guessing resistance (Def. 5, Theorem 4); (3) access-control enforcement via `Kauth`-encrypted tokens (Theorem 3); (4) integrity/verifiability via Merkle proofs + on-chain anchor (Theorem 2); (5) Bloom-filter privacy — only PRF outputs are inserted, not plaintext keywords (Theorem 5).
- All named primitives and required parameters:

| Primitive | Requirement | Citation |
|---|---|---|
| CSPRNG | 256-bit key generation for all 4 keys | Eq. 3 |
| AES (256-bit key) | `EncAES`/`DecAES` for logs, metadata, index, tokens, policy | Eq. 4, 6, 7, 14, 15, 17 |
| SHA-256 | `H(x)` / `h(w)` hashing of keywords, UserID, Permissions, LType, LID, and encrypted-log bytes | Eq. 5, 18–19, 28, Theorem 2 |
| HMAC-SHA256 | PRF: `PRF(K, x) = HMAC-SHA256(K, x)`, used both as the trapdoor function and inside Alg. 2's indexing | Eq. 19; Alg. 2 line 9 |

Do not substitute any of these primitives (e.g., no switching AES for ChaCha20, no switching HMAC-SHA256 for a different MAC/PRF) — the paper's IND-CKA2 proof (Theorem 1) and unlinkability proof (Theorem 4) are stated specifically in terms of HMAC-SHA256's PRF security.

---

## 6. Required data structures

Minimal Go types, derived only from what the paper's equations/algorithms require. Fields marked "(UNSPEC.)" have no concrete type/format given by the paper.

### `SystemKeys` **[EXPLICIT]** — Eq. 3
```go
type SystemKeys struct {
    Kenc    [32]byte // AES key: encrypts logs + metadata (Eq.14,15)
    Kindex  [32]byte // AES key: encrypts tree/BF/inverted-index and Policy (Eq.6,17)
    Kauth   [32]byte // AES key: encrypts/decrypts user tokens (Eq.4,7)
    Ksearch [32]byte // HMAC key: PRF for trapdoors (Eq.18-19)
}
```

### `AccessPolicy` **[EXPLICIT]** — Eq. 5
```go
type AccessPolicy struct {
    HUserID      [32]byte // H(UserID)
    HPermissions [32]byte // H(Permissions) — paper alternates "Permission"/"Permissions" naming; see A6
    HLType       [32]byte // H(LType)
    Texp         int64    // expiration timestamp
}
```
Only the four fields Eq. 5 names. No additional metadata (no version numbers, no issuer field, etc.) — none of that is stated.

### `UserToken` **[EXPLICIT + gap]** — Eq. 4, 7
```go
type UserToken struct {
    UserID      string // "u" (Eq.4) / "user" (Eq.7)
    Permissions PermissionSet // "Pu" (Eq.4) / "APuser" (Eq.7) — (UNSPEC. concrete type; see A6)
    Texp        int64
}
```
`Tokenu = EncAES(UserID ∥ Permissions ∥ Texp, Kauth)` is the wire/stored form; the struct above is the plaintext the ciphertext decrypts to. **[UNSPECIFIED BY PAPER]**: the concrete representation of "Permissions"/`APuser` — is it a list of allowed `LType`s, a bitmask, or the full `AccessPolicy` object? Never defined. Minimal prototype choice in Section 16 (B6).

### `Tree` / `PrimaryNode` / `SubNode` **[EXPLICIT, with structural ambiguity — see A1/A2]** — Def. 2, Alg. 1
```go
type Tree struct {
    PrimaryNodes map[string]*PrimaryNode // key: LType
}

type PrimaryNode struct {
    LType         string
    InvertedIndex *InvertedIndex // Alg.1: NLType.SET_INDEX(In) — see A2 re: whether this is ever read at query time
    SubNodes      map[string]*SubNode
}

type SubNode struct {
    Key           string          // sub-category key (UNSPEC. — see A21)
    BloomFilter   *BloomFilter    // Def.3; one per sub-node
    InvertedIndex *InvertedIndex  // Eq.10; Alg.2 populates entries here
}
```
Whether `PrimaryNode.InvertedIndex` (Alg. 1) or `SubNode.InvertedIndex` (Eq. 10, Alg. 2) is the one actually queried is **not resolved by the paper** — see Ambiguity A2. Both fields are kept because both are explicitly instantiated somewhere in the paper's pseudocode, but only the sub-node index is populated by the algorithm that actually inserts keywords (Alg. 2), so it is the one the minimal prototype treats as authoritative for search (Section 16, B2).

### `BloomFilter` **[EXPLICIT, with content ambiguity — see A3]** — Def. 3, Eq. 1, 8, 16
```go
type BloomFilter struct {
    Bits []bool // or a bitset; length m
    K    int    // number of hash functions
    M    int    // bit array size
}
```
Neither `m` nor `k` is given a concrete value anywhere in the paper — only the general false-positive formula `Pfp = (1 - e^(-kn/m))^k` (Eq. 1). **[UNSPECIFIED BY PAPER]**: concrete `m`, `k`. Minimal choice in Section 16 (B3b).

### `InvertedIndex` **[EXPLICIT]** — Eq. 9–10
```go
type InvertedIndex struct {
    Entries map[string][]string // key: hex(PRF(h(w))), value: list of LID (paper: "PID" — see A4)
}
```

### `Log` / `Metadata` / `EncryptedLog` **[EXPLICIT]** — Phase 3
```go
type Log struct {
    LID     string
    Content string // Ci
    Ti      int64  // timestamp
    Si      string // source
    LType   string
}

type EncryptedLog struct {
    LID         string
    CTLog       []byte // EncAES(Ln, Kenc) — Eq.14
    CTMetadata  []byte // EncAES({Ti,Si,LType}, Kenc) — Eq.15
}
```

### `Trapdoor` / `Query` **[EXPLICIT single-keyword part; UNSPECIFIED multi-keyword part — see A9]** — Eq. 18–20; Alg. 3
```go
type Trapdoor struct {
    Tw []byte // HMAC-SHA256(Ksearch, SHA-256(w)) — 32 bytes
}

type Query struct {
    Tw              Trapdoor
    LType           string
    Tokenu          []byte // encrypted token, ciphertext form
    BooleanOperator string // referenced as Q.boolean_operator in Alg.3 but absent from Eq.20 — see A9
}
```
Eq. 20 defines `Q = (Tw, LType, Tokenu)` — a *single* trapdoor. Alg. 3 references `Q.boolean_operator` and calls `PerformBooleanOperations` with operations that require **two** operand sets (`L1, L2` in Eq. 25–27). The paper does not specify how a query carries more than one trapdoor. This is Ambiguity A9 — flagged, not silently resolved.
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q6 (QUESTIONS.md, ruled 2026-09-19): B9 approved. Tw is read as a list/set of trapdoors, so one query can combine two or more keywords under AND/OR/NOT. This is a reading of the paper's query, not a different search algorithm.

### `DHT` **[EXPLICIT]** — Eq. 28
```go
type DHT struct {
    Entries map[string][]byte // key: hex(H(LID)), value: hLID
}
```

### `MerkleTree` **[EXPLICIT arity; UNSPECIFIED concatenation/padding — see A14]** — Theorem 2, Fig. 3
```go
type MerkleTree struct {
    Leaves [][]byte   // hLID values, ordered
    Levels [][][]byte // Levels[0] = leaves; each parent = hash of up to 4 children
    Root   []byte
}
```
Arity is fixed at 4 per non-leaf node (Theorem 2: "a quaternary tree in which each non-leaf node is the hash of its four child nodes"). Do **not** implement a binary Merkle tree.

### Smart contract interface **[EXPLICIT signatures for 2 of 4 named functions]**
```go
type BlockchainContracts interface {
    VerifyAccess(tokenCT []byte, lType string) (bool, error)       // Eq.21
    VerifyIntegrity() (merkleRoot []byte, err error)                 // Phase1 Step2 item2
    UpdateMerkleRoot(newRoot []byte) error                           // named only in Table 3, no formal signature given
    GrantAccess(userID string, policyCT []byte) error                // named only in Table 3, no formal signature given
}
```

---

## 7. Phase-by-phase workflow

### Phase 1 — System Initialization (Sec. IV.D, "Phase 1")

**Step 1: Key Generation**
- Purpose: produce the four symmetric keys the rest of the system depends on.
- Actor: Data Owner (implied — see Sec. 3 note).
- Input: none (uses CSPRNG).
- Processing: `Kenc, Kindex, Kauth, Ksearch = Rand(256)` — Eq. 3.
- Output: 4 × 256-bit keys.
- State created: `SystemKeys`.
- Dependencies: none (first step).

**Step 2: Blockchain Setup**
- Purpose: deploy the two smart contracts.
- Actor: Data Owner / deployer.
- Input: none specified.
- Processing: deploy `VerifyAccess` and `VerifyIntegrity` contracts. — Phase 1 Step 2.
- Output: deployed contract addresses/state (not specified further).
- State created: on-chain contract instances.
- Dependencies: none explicit, but logically after Step 1 (contracts reference `Kauth`-encrypted tokens conceptually, though the key itself is never put on-chain).

**Step 3: User Registration and Access Control**
- Purpose: register a user and issue them an access token.
- Actor: Registration Authority (RA) for registration; the token-generation function for the second stage.
- Input (Register_User): `UserID, Credentials, Permissions, LType`.
- Processing: verify credentials → generate `Texp` → build `Policy = {H(UserID), H(Permissions), H(LType), Texp}` (Eq. 5) → `CTPolicy = EncAES(Policy, Kindex)` (Eq. 6) → store on blockchain. — Pseudocode Step 3.1.
- Output: registration status string.
- Input (Generate_User_Token): `UserID`.
- Processing: retrieve `CTPolicy` from chain → decrypt with `Kindex` → check `Texp` → build `Tokenu = EncAES(UserID ∥ Permissions ∥ Texp, Kauth)` (Eq. 7). — Pseudocode Step 3.2. **See A7: this step cannot be executed as literally written because only hashes were stored.**
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q6 (QUESTIONS.md, ruled 2026-09-19): B7 rejected. CTPolicy stays exactly Eq. 5–6. Tokenu is generated by the trusted registration/token authority while it still holds the plaintext registration inputs. Step 3.2 as written is internally inconsistent (it would have to reverse H(UserID)/H(Permissions)), and FR-7 is closed on that basis.
- Output: `Tokenu`.
- State created/modified: on-chain `CTPolicy` record (Step 3.1); returned token (Step 3.2, not persisted anywhere per the paper).
- Dependencies: Step 1 (`Kindex`, `Kauth`), Step 2 (blockchain must be live).

### Phase 2 — Tree Structure and Indexing Formation (Sec. IV.D, "Phase 2"; Def. 2–3; Alg. 1)

- Purpose: build the hierarchical log-type classification tree with per-sub-node Bloom filters and inverted indexes.
- Actor: Search Engine / indexing service.
- Input: the fixed set of log-type names to seed (paper's example: `{"Firewall Logs", "IDPS Logs", "Application Server Logs"}` — Alg. 1 line 3).
- Processing: `treeSetup()` — initialize tree; for each `LType`, create a primary node, attach an inverted index; for each sub-node under it, create a Bloom filter. — Alg. 1.
- Output: `Tree` object (unpopulated with actual keywords yet).
- State created: `Tree`, `PrimaryNode`s, `SubNode`s, empty `BloomFilter`s and `InvertedIndex`(es).
- Dependencies: Phase 1 (needs `Kindex` conceptually for eventual encryption, though `treeSetup` itself doesn't encrypt anything).

### Phase 3 — Keyword Extraction and Log Encryption (Sec. IV.D, "Phase 3")

- Purpose: extract searchable keywords from raw log content and encrypt the log + metadata.
- Actor: Data Owner / ingestion pipeline.
- Input: raw log content `Ci` for each log entry.
- Processing:
  - `Preprocess(Ci) → C'i` (lowercase, strip special chars/punctuation, remove stopwords) — Eq. 11.
  - `Tokenize(C'i) → Tc = {t1,...,tn}` — Eq. 12.
  - `K(LType) = {tj ∈ Tc | tj satisfies selection criteria}` — Eq. 13 (**criteria UNSPECIFIED, A18**).
  - Hash + HMAC each selected keyword with `Ksearch` (same construction as Eq. 18–19).
  - `EncAES(Ln, Kenc) → CTLn` — Eq. 14.
  - `EncAES(Metadata={Ti,Si,LType}, Kenc)` — Eq. 15.
  - Assign unique `LID` to `Ln`.
- Output: `CTLn`, `CTMetadata`, `LID`, and the set of keyword trapdoors for this log entry.
- State created: `EncryptedLog` record.
- Dependencies: Phase 1 (`Kenc`, `Ksearch`).

### Phase 4 — Indexing (Sec. IV.D, "Phase 4"; Eq. 16–17; Alg. 2)

- Purpose: insert each log's keyword trapdoors into the tree's inverted index and Bloom filter, then encrypt the whole index structure.
- Actor: Search Engine / indexing service.
- Input: `LType`, `Ln` (to resolve sub-node), and the keyword set from Phase 3.
- Processing: `Alg. 2` — locate/create primary node; locate sub-node; for each keyword `w`: `h(w) = SHA-256(w)`, `PRFK(w) = HMAC-SHA256(Ksearch, h(w))`; insert `(PRFK(w), LID)` into the sub-node's inverted index; insert `PRFK(w)` into the sub-node's Bloom filter.
- Output: populated `Tree`.
- Processing (encryption): `Enc(T, BF, Inv, Kindex) = (CTT, CTBF, CTInv)` — Eq. 17; stored in cloud storage.
- State created/modified: populated `InvertedIndex` and `BloomFilter` per sub-node; encrypted index blobs in cloud storage.
- Dependencies: Phase 2 (tree must exist), Phase 3 (keywords + `LID` must exist).

### Phase 5 — Trapdoor Generation (Sec. IV.D, "Phase 5")

- Purpose: let a Data User generate a query-side trapdoor for a keyword, identical in construction to the index-side value from Phase 4.
- Actor: Data User.
- Input: keyword `w`, `Ksearch` (assumed available to the authorized user — distribution mechanism unspecified, A22).
- Processing: `h(w) = SHA-256(w)`; `Tw = HMAC-SHA256(Ksearch, h(w))` — Eq. 18–19.
- Output: `Tw`.
- State created: none persistent (ephemeral, per query).
- Dependencies: Phase 1 (`Ksearch` must have been distributed to the user).

### Phase 6 — Search Execution and Result Verification (Sec. IV.D, "Phase 6"; Alg. 3–4.2)

See Section 8 (dedicated search-workflow breakdown) and Section 9 (dedicated integrity-workflow breakdown).

- Dependencies: all of Phases 1–5 (token, tree/index, trapdoor, DHT/Merkle state from log ingestion).

---

## 8. Search workflow

Exactly the 7 steps the paper names (Phase 6 prose + Alg. 3), no additions:

1. **Query submission**: `Q = (Tw, LType, Tokenu)` — Eq. 20. (Per A9, `Q` must also carry a Boolean operator and — for multi-keyword queries — more than one `Tw`; not resolved by the paper as written.)
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q6 (QUESTIONS.md, ruled 2026-09-19): B9 approved. Tw is read as a list/set of trapdoors, so one query can combine two or more keywords under AND/OR/NOT. This is a reading of the paper's query, not a different search algorithm.
2. **Authorization verification**: `VerifyAccess(Tokenu, LType) → Allow/Deny` — Eq. 21; Alg. 3 lines 5–7. On deny, return `"Access Denied"` immediately.
3. **Locate tree node**: `targetNode = LocateTreeNode(T, Tw)` — Eq. 22; Alg. 3 line 8. (Per A10, the literal signature omits `LType` even though the surrounding prose says `LType` drives node routing.)
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q8 (QUESTIONS.md, ruled 2026-09-19): LocateTreeNode resolves LType to its primary node. Every sub-node under it gets the Bloom-filter test. Negative sub-nodes are skipped, and candidate LIDs from every positive sub-node are unioned. For NOT, Lall = all LIDs under that primary LType node.
4. **Bloom-filter check**: `BloomFilterCheck(targetNode, Tw) → True/False` — Eq. 23; Alg. 3 lines 9–11. On `False`, return `"No matching records found"`.
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q8 (QUESTIONS.md, ruled 2026-09-19): LocateTreeNode resolves LType to its primary node. Every sub-node under it gets the Bloom-filter test. Negative sub-nodes are skipped, and candidate LIDs from every positive sub-node are unioned. For NOT, Lall = all LIDs under that primary LType node.
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q12 item 4 (QUESTIONS.md, ruled 2026-09-19): with several trapdoors, "No matching records found" is operator-dependent (AND: any keyword without a candidate; OR/no operator: all; NOT: never, the result is Lall).
5. **Inverted-index lookup**: `candidateEntries = RetrieveFromIndex(targetNode, Tw)` — Eq. 24; Alg. 3 line 12.
6. **Boolean operations**: apply AND (`∩`), OR (`∪`), or NOT (`Lall \ L1`) over the retrieved `LID` sets per `Q.boolean_operator` — Eq. 25–27; Alg. 3 lines 13–37. (Per A8, the pseudocode never actually feeds `candidateEntries` into this step — `valid_pointers` is hard-coded to an empty list.)
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q6 (QUESTIONS.md, ruled 2026-09-19): B9 approved. Tw is read as a list/set of trapdoors, so one query can combine two or more keywords under AND/OR/NOT. This is a reading of the paper's query, not a different search algorithm.
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q12 item 3 (QUESTIONS.md, ruled 2026-09-19): NOT is unary and takes exactly one trapdoor; multi-keyword NOT is rejected.
7. **Integrity verification and tampered-file identification**: `(verified_result_set, Integrity) = identifyTamperedFiles(result_set, DHT, EncryptedStorage)` — Alg. 3 line 15; see Section 9 for the sub-steps.
8. **Result return**: retrieve and return the corresponding *encrypted* log entries for the surviving `LID`s — end of Phase 6 prose.

Do not add stages the paper doesn't name (e.g., no query-result ranking, no pagination, no rate limiting, no query caching).

---

## 9. Integrity-verification workflow

**[EXPLICIT]**, Alg. 4.1–4.2, Eq. 28, Fig. 3, Theorem 2:

- **What gets hashed**: each encrypted log entry `EncAES(Ln, Kenc)` is hashed to produce `hLID = H(EncAES(Ln, Kenc))` (Theorem 2 proof text). `LID` itself is separately hashed to `H(LID)` to form the DHT key (Eq. 28).
- **What's stored in the DHT**: `DHT[H(LID)] = hLID` — a key-value map from hashed log ID to the integrity hash of that log's ciphertext (Eq. 28).
- **What's placed in the Merkle tree**: the `hLID` values are the tree's leaves (Fig. 3: leaves labeled `h.1...h.8`, each linked to a `DHT` entry `h_iDHT`).
- **What's stored on-chain**: only the Merkle root `R` (Fig. 3; "The R is stored on-chain as a secure anchor").
- **Merkle tree arity**: quaternary — each non-leaf node is the hash of exactly four child nodes (Theorem 2). **Do not implement a binary Merkle tree.**
- **What the verifier recomputes**: given a `candidateEntries` result set, `RecomputeMerkleRoot(candidateEntries)` produces `(ComputedProof, ComputedTree)` — i.e., a fresh quaternary tree (and its root) built *only* over the leaves corresponding to the retrieved results, not the full dataset (Alg. 4.1 line 3; also stated in Sec. VI.B cost analysis: "recomputing only the quaternary Merkle tree hash over the returned set of LID").
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q9 (QUESTIONS.md, ruled 2026-09-19): Each candidate's hLID is verified by its quaternary Merkle proof path against the full-tree root R stored on-chain. Alg. 4.1's wording, which recomputes a root over only the candidates, is recorded as an inconsistency in the paper, because that root can't equal R for a partial result set. No per-query root is anchored.
- **Mismatch detection**: `if ComputedProof != StoredProof` → call `TraceTamperedNode` (Alg. 4.1 line 4–5).
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q9 (QUESTIONS.md, ruled 2026-09-19): Each candidate's hLID is verified by its quaternary Merkle proof path against the full-tree root R stored on-chain. Alg. 4.1's wording, which recomputes a root over only the candidates, is recorded as an inconsistency in the paper, because that root can't equal R for a partial result set. No per-query root is anchored.
- **Tampered-file localization** (`TraceTamperedNode`, Alg. 4.2): starting from the root level and walking down (levels sorted descending), at each internal level compare `hash_concat_group` of the recomputed vs. stored node; if they differ, expand into that node's children for the next (lower) level; at "Level 0" (the leaf level), compare each `LID`'s recomputed hash against its stored hash directly and append mismatches to `tampered_lids`.
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q10 (QUESTIONS.md, ruled 2026-09-19): At Level 0, only the leaves under the affected level-1 branches are inspected. Alg. 4.2's literal loop over every leaf conflicts with the paper's Fig. 9 description ("only drills down into the relevant branches"). This is the chosen resolution of that conflict.
- **Result exclusion**: if any `tampered_lids` are found, exclude them from the result set and set `Integrity = FALSE`; otherwise `Integrity = TRUE` and the full `candidateEntries` set is returned unmodified (Alg. 4.1 lines 7–14).

Relationship diagram (as stated by the paper, not re-derived):
`Encrypted Log → hLID=H(EncAES(Ln,Kenc)) → DHT[H(LID)]=hLID (off-chain) → Merkle leaves = hLID values → quaternary Merkle tree → Root R (on-chain)`. Verification reverses this: `candidateEntries → recompute quaternary tree over their hLID → compare root to on-chain R → on mismatch, descend via TraceTamperedNode → tampered LIDs`.

Known pseudocode defects in this workflow are documented in Section 15 (A12, A13, A14) — not silently repaired here.

---

## 10. Blockchain requirements

**[EXPLICIT]** scope only, Sec. IV.A.2–3, Phase 1 Step 2, Table 3:

- **Access-control functionality**: `VerifyAccess(Tokenu, LType) → Allow/Deny` — decrypts token with `Kauth`, checks permissions vs. requested log type.
- **Smart-contract functionality**: exactly two contracts are formally specified — `VerifyAccess` and `VerifyIntegrity`. Two additional function *names* appear only in the evaluation section's gas table (Table 3) with no formal definition elsewhere: `updateMerkleRoot()` (stores a new Merkle root) and `grantAccess()` (assigns search privileges to a user).
- **Policy storage**: `Blockchain_Store(UserID, CTPolicy)` — the AES-encrypted `Policy` (Eq. 5–6) is stored on-chain, keyed by `UserID` (Pseudocode Step 3.1 line 9).
- **Query/access logging**: the paper *asserts* (Sec. I, contribution bullet 1) that "every access is transparently recorded for compliance and auditability," but **no concrete logging schema, function, or algorithm is given anywhere in Sec. IV.D**. This is a stated capability with no specified mechanism — **[UNSPECIFIED BY PAPER]**.
- **Integrity-root anchoring**: the Merkle root `R` is stored on-chain as the tamper-evidence anchor (Fig. 3; `VerifyIntegrity() → Merkle Root`).
- **`VerifyAccess`**: as above.
- **`VerifyIntegrity`**: returns the stored on-chain Merkle root for comparison against a freshly recomputed one (Alg. 4.1 line 2).

No other blockchain functionality (no token minting, no staking, no cross-chain bridging, no on-chain storage of raw logs, no on-chain keyword indexes) is described. Per the user's development-strategy allowance (instruction #11): representing these two/four contract functions behind a Go interface before wiring up a real chain is a **development strategy**, not a modification of EVSEB — see Section 12 (`BlockchainContracts` interface, Section 6).

---

## 11. Storage requirements

**[EXPLICIT locations, UNSPECIFIED formats]** — Sec. IV.A.1, Phase 3–4, Eq. 17, 28, Fig. 3:

| Location | What is stored | Citation |
|---|---|---|
| Cloud storage (off-chain) | `CTLn` (encrypted log), `CTMetadata` (encrypted metadata) | Eq. 14–15 |
| Cloud storage (off-chain) | `CTT, CTBF, CTInv` — encrypted tree, Bloom filter, inverted index | Eq. 17 |
| DHT (off-chain) | `H(LID) → hLID` mapping | Eq. 28 |
| On-chain (blockchain) | `CTPolicy` per registered user, keyed by `UserID` | Pseudocode Step 3.1 line 9 |
| On-chain (blockchain) | Merkle root `R` | Fig. 3; `VerifyIntegrity()` |
| Inside the hierarchical index | `InvertedIndex` entries (`PRF(h(w)) → [LID,...]`); `BloomFilter` bit arrays | Eq. 9–10, 8 |

**[UNSPECIFIED BY PAPER]**: the paper names *where* things live but never specifies:
- File format for encrypted logs/metadata on disk (no JSON/protobuf/binary schema given).
- Serialization method for the tree/Bloom filter/inverted index before AES encryption (Eq. 17 just says `Enc(T, BF, Inv, Kindex)` — the pre-encryption byte representation of `T`, `BF`, `Inv` is not defined).
- Concrete DHT protocol/implementation (no Kademlia, no Chord, no IPFS API call is specified in the algorithmic sections — see A16 re: Fig. 1's "IPFS" box).
- On-chain data layout beyond "a policy record keyed by UserID" and "a Merkle root value" (no Solidity struct/mapping schema given).

Do **not** invent PostgreSQL/Cassandra/Redis/MongoDB or any other concrete database — none is named or implied by the paper.

---

## 12. Minimal Go package architecture

Derived directly from the phases/components above — one package per paper-named responsibility, no extra layering:

```
evseb/
├── cmd/
│   └── evseb/                 # CLI driver: run phases end-to-end, run experiment harness
├── internal/
│   ├── crypto/                # AES-256 Enc/Dec, SHA-256, HMAC-SHA256 PRF, Rand(256)  — Eq.3,19; FR-1,5
│   ├── access/                # AccessPolicy, UserToken, Register_User, Generate_User_Token, VerifyAccess logic — Phase1 Step2-3
│   ├── tree/                  # Tree, PrimaryNode, SubNode, treeSetup, LocateTreeNode — Def.2, Alg.1, Eq.22
│   ├── bloom/                 # BloomFilter — Def.3, Eq.1,8,16
│   ├── index/                 # InvertedIndex, insertion (Alg.2), RetrieveFromIndex — Eq.9,10,24
│   ├── keyword/                # Preprocess, Tokenize, KeywordSelection — Eq.11-13
│   ├── logstore/               # Log, EncryptedLog, Metadata, LID assignment, log/metadata encryption — Phase3 Step2, Eq.14-15
│   ├── trapdoor/               # Trapdoor generation — Eq.18-19
│   ├── search/                 # Query, SecureQueryExecution, Boolean AND/OR/NOT — Alg.3, Eq.20,25-27
│   ├── integrity/              # DHT, quaternary MerkleTree, identifyTamperedFiles, RecomputeMerkleRoot, TraceTamperedNode — Alg.4.1-4.2, Eq.28
│   └── blockchain/             # BlockchainContracts interface + concrete (e.g. go-ethereum-backed) implementation of VerifyAccess/VerifyIntegrity/updateMerkleRoot/grantAccess — Phase1 Step2
├── tests/                       # Integration tests reproducing Phase 1-6 end-to-end and the Sec.VI experiments
├── go.mod
└── README.md
```

No `cache/`, no `worker/`, no `queue/`, no `api/` (REST/gRPC) layer, no `db/` abstraction beyond what `logstore`/`integrity` need to hold their own state — none of these are named or implied by the paper (instruction #2).

---

## 13. Interfaces between components

Only the calls the paper's algorithms actually make:

- `access.RegisterUser(userID, credentials, permissions, lType) → status` → calls `blockchain.GrantAccess` / on-chain store (Pseudocode Step 3.1).
- `access.GenerateUserToken(userID) → Tokenu` → calls chain-retrieve + `crypto.DecAES`/`EncAES` (Pseudocode Step 3.2).
- `access.VerifyAccess(tokenCT, lType) → bool` → delegates to `blockchain.VerifyAccess` (Eq. 21).
- `tree.TreeSetup() → *Tree` (Alg. 1); `tree.LocateTreeNode(t, tw) → *SubNode` (Eq. 22, modulo A10).
- `index.InsertIndices(tree, lType, log, keywords, ksearch)` (Alg. 2) → calls `bloom.Insert`, `index.Add`, `crypto.HMACSHA256`.
- `keyword.Preprocess/Tokenize/Select` (Eq. 11–13) feed into `index.InsertIndices` and `trapdoor.Generate`.
- `logstore.EncryptLog(log, kenc) → EncryptedLog` (Eq. 14–15); assigns `LID`.
- `trapdoor.Generate(keyword, ksearch) → Trapdoor` (Eq. 18–19) — used identically by both the indexer (Alg. 2) and the querying Data User (Phase 5).
- `search.SecureQueryExecution(q *Query) → verifiedResults` (Alg. 3) orchestrates: `access.VerifyAccess` → `tree.LocateTreeNode` → `bloom.Check` → `index.RetrieveFromIndex` → `search.PerformBooleanOperations` → `integrity.IdentifyTamperedFiles`.
- `integrity.IdentifyTamperedFiles(candidateEntries, dht, storage) → (verifiedSet, bool)` (Alg. 4.1) → calls `blockchain.VerifyIntegrity`, `integrity.RecomputeMerkleRoot`, `integrity.TraceTamperedNode` (Alg. 4.2).
- `blockchain.VerifyAccess`, `blockchain.VerifyIntegrity`, `blockchain.UpdateMerkleRoot`, `blockchain.GrantAccess` — the only chain-facing calls named anywhere in the paper (Phase 1 Step 2, Table 3).

---

## 14. Requirements needed for the paper's experiments

Sec. VI describes six experiments the authors ran (Python/PyCryptodome/Web3.py/Ganache, not a Go requirement per user instruction #1 — only the *measured quantities* matter). To reproduce comparable curves in Go, the harness (in `cmd/evseb` + `tests/`, not the core protocol) needs:

- A synthetic-log generator with a controllable file size (200 KB–3200 KB, Fig. 4).
- A controllable keyword count per query/index build (20–100, Fig. 5–6).
- A controllable dataset size (1,000–5,000 log files, Fig. 7–9).
- A tamper-injection utility that flips bytes in a chosen number of encrypted logs (100 of N for Fig. 8; 10 of 20 matching files for Fig. 9) to simulate corruption.
- Wall-clock timing instrumentation around: encryption (Fig. 4), trapdoor generation (Fig. 5), search (Fig. 6–7), global integrity verification (Fig. 8), individual-file integrity verification (Fig. 9).
- A local Ethereum-compatible chain (needed only if reproducing Table 3's gas costs and Fig. 10–11's on-chain-vs-off-chain `verifyAccess` throughput/latency comparison) with both an on-chain code path (real contract call) and an off-chain code path (pre-validated token, no chain round-trip) for `VerifyAccess`, driven with 10–100 simulated requests.
  > **Prototype choices, not EVSEB statements** — Q12 items 1, 2, 5, 6 (QUESTIONS.md, ruled 2026-09-19):
  > - Fig. 6/7 operator: AND by default (`-query-op`). NOT SPECIFIED BY EVSEB.
  > - Fig. 6: 3200 KB is the **file size** (`-fig6-file-kb`). The number of files is UNSPECIFIED BY PAPER and is a separate setting (`-fig6-files`, default 30).
  > - Fig. 10/11 on-chain: the N requests are submitted as one workload, with no send waiting for an earlier receipt. TPS = N / workload wall time, latency = per-request send to receipt. The off-chain mode is a pre-validated token. The application's normal Allow/Deny path uses `eth_call`, a separately documented implementation choice.
  > - Table 3: measured gas units and ETH at the local chain's effective gas price. No computed USD column; the paper's USD numbers are paper-reported reference values only.

These are **experiment-harness** requirements, not EVSEB protocol requirements — they exist only to let the user reproduce the paper's own measurement methodology for comparison purposes, per instruction #14 ("reproducing a specific experiment" is the only sanctioned reason to add anything beyond the bare algorithm).

---

## 15. Ambiguities/inconsistencies in the paper

Each entry: what the paper says → why it's ambiguous/inconsistent → what information would be needed.

**A1 — Tree hierarchy: three conflicting category lists.**
- Says: (a) System model (Sec. IV.A.6) lists log *sources* as IDS, IPS, Firewall, Web Server. (b) Def. 2 / Alg. 1 line 3 iterate primary nodes over `{"Firewall Logs", "IDPS Logs", "Application Server Logs"}`. (c) Fig. 2 shows Root → Sources `{Web Server Logs, Application Server Logs}` → Type of Logs `{Apache, Nginx, Tomcat, Node.js}` → Bloom filter leaf.
- Ambiguous because: three different, non-overlapping category sets are each presented as *the* tree's top-level structure, and it's unclear whether the tree has 2 levels of categorization (per Def. 2/Alg. 1: root → primary node directly) or 3 (per Fig. 2: root → sources → types).
- Needed: a single authoritative category list and level count from the authors; absent that, this determines the exact shape of `PrimaryNode`/`SubNode`.

**A2 — Inverted index lives at two different levels, inconsistently populated.**
- Says: Eq. 9 defines a primary-node-level index `INL`; Alg. 1 initializes it (`NLType.SET_INDEX(In)`). Eq. 10 separately defines a sub-node-level index `Inij`; Alg. 2 is the only algorithm that actually inserts entries, and it inserts into the *sub-node* index (`nij.INIT_INVERTED_INDEX`, `nij.ADD_TO_INVERTED_INDEX`) — it never touches the primary-node index created in Alg. 1.
- Ambiguous because: `RetrieveFromIndex(targetNode, Tw)` (Eq. 24) doesn't say which of the two indexes it reads, and one of them (`INL`) is created but never populated by any given algorithm.
- Needed: confirmation of which index the search path actually queries.

**A3 — Bloom filter content: raw hash vs. PRF output.**
- Says: Def. 3 / Eq. 8 insert `h(w)` (plain SHA-256 hash) into the filter. Alg. 2 line 14–15 and Eq. 16 (search-time check) and Theorem 5's proof all insert/check `PRF(Ksearch, h(w))` (i.e., the trapdoor `Tw`).
- Ambiguous because: these are two different values — one keyed and one not — and a Bloom filter built one way cannot be correctly checked the other way.
- Needed: which value is authoritative (the later, more detailed, and internally-consistent Alg. 2 / Eq. 16 / Theorem 5 formulation is used for Section 16's minimal choice, but this is a judgment call, not a paper statement).

**A4 — Three names for the same pointer/identifier.**
- Says: Eq. 9–10 call it `PID`; Alg. 2 line 13 calls it `LID`; the worked example on p. 8 calls it generic `ID1, ID2, ...`.
- Ambiguous because: it's not explicitly stated these are the same concept, though context strongly implies it.
- Needed: none really required to proceed — treated as synonyms, standardized on `LID` (the name Phase 3 formally introduces) for the Go code; flagged so the substitution isn't silently invisible.

**A5 — `Kenc` is stated to encrypt keywords, but no algorithm ever does that.**
- Says: Phase 1 Step 1 item 1: "`Kenc`... is used to encrypt log entries (Ln) and extracted keywords using the AES algorithm."
- Ambiguous/inconsistent because: every subsequent phase (3, 4, 5) processes keywords exclusively via `SHA-256` + `HMAC-SHA256(Ksearch, ...)` — never via `EncAES(..., Kenc)`. The keyword-encryption-under-`Kenc` claim is never exercised by any equation or algorithm.
- Needed: clarification of whether this is a leftover/incorrect sentence, or whether keywords really are meant to be separately AES-encrypted somewhere not shown.

**A6 — Access-policy/token field naming drifts across four spots.**
- Says: Eq. 4 uses `(u, Pu)`; Eq. 7 uses `(user, APuser)`; Pseudocode Step 3.1 builds `Policy` from `(UserID, Permissions, LType)`; Eq. 5 (prose) calls the same field `Permission` (singular).
- Ambiguous because: it's unclear if `Pu`, `APuser`, and `Permissions` are the same object type (a full `AccessPolicy`? a permission list? a bitmask?).
- Needed: one canonical field name and a concrete type for "the user's permissions."

**A7 — `Generate_User_Token` cannot be executed as written.**
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q6 (QUESTIONS.md, ruled 2026-09-19): B7 rejected. CTPolicy stays exactly Eq. 5–6. Tokenu is generated by the trusted registration/token authority while it still holds the plaintext registration inputs. Step 3.2 as written is internally inconsistent (it would have to reverse H(UserID)/H(Permissions)), and FR-7 is closed on that basis.
- Says: Step 3.1 stores `Policy = {H(UserID), H(Permissions), H(LType), Texp}` (hashes only) on-chain (Eq. 5–6). Step 3.2 retrieves and decrypts `CTPolicy`, destructures it as `(H(UserID), H(Permissions), H(LType), Texp)` (line 4), then on line 8 builds `Tokenu ← EncAES(UserID ∥ Permissions ∥ Texp, Kauth)` using the *plaintext* `UserID` and `Permissions` — values that cannot be recovered from a one-way hash.
- Ambiguous/inconsistent because: this is a logical impossibility as written, not just missing detail — hashing is (assumed) one-way, so no algorithm can invert `H(UserID)` back to `UserID`.
- Needed: either the on-chain `Policy` must also store (or be accompanied by) the plaintext fields, or `Generate_User_Token` must source `UserID`/`Permissions` from elsewhere (e.g., a separate off-chain user record) that the pseudocode doesn't show.

**A8 — Boolean-operation input is never wired to the retrieved candidates.**
- Says: Alg. 3 line 12 computes `candidateEntries ← RetrieveFromIndex(targetNode, Tw)`; line 13 then sets `valid_pointers ← []` (a fresh empty list); line 14 calls `PerformBooleanOperations(valid_pointers, Q.boolean_operator)`. `candidateEntries` is never assigned into `valid_pointers` or otherwise used again.
- Ambiguous/inconsistent because: as written, every query's Boolean-operation stage always operates on an empty set, regardless of what was actually retrieved from the index.
- Needed: the missing assignment step connecting `candidateEntries` to whatever the Boolean-operation stage actually consumes.

**A9 — Query structure supports only one trapdoor; Boolean ops need at least two operand sets.**
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q6 (QUESTIONS.md, ruled 2026-09-19): B9 approved. Tw is read as a list/set of trapdoors, so one query can combine two or more keywords under AND/OR/NOT. This is a reading of the paper's query, not a different search algorithm.
- Says: Eq. 20 defines `Q = (Tw, LType, Tokenu)` — one trapdoor. Eq. 25–27 define AND/OR/NOT over `L1, L2` (two sets) or `L1` vs. `Lall`. Alg. 3 references `Q.boolean_operator`, a field Eq. 20 never lists.
- Ambiguous because: there is no described mechanism for a user to submit a genuinely multi-keyword Boolean query (e.g., "firewall AND deny") — the formal query object only ever carries one keyword's trapdoor.
- Needed: the actual multi-trapdoor query format (list of trapdoors + operator(s), or a small expression tree) is not given.

**A10 — `LocateTreeNode` signature omits `LType`.**
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q8 (QUESTIONS.md, ruled 2026-09-19): LocateTreeNode resolves LType to its primary node. Every sub-node under it gets the Bloom-filter test. Negative sub-nodes are skipped, and candidate LIDs from every positive sub-node are unioned. For NOT, Lall = all LIDs under that primary LType node.
- Says: Eq. 22: `targetNode = LocateTreeNode(T, Tw)`. Surrounding prose (Phase 6 Step 3): "By specifying LType, the system directs the query to the relevant primary node within T."
- Ambiguous/inconsistent because: the function as formally defined has no way to use `LType` for routing, contradicting the prose explanation of how routing works.
- Needed: the true signature/parameter list of `LocateTreeNode`.

**A11 — `Lall` in the NOT operation is undefined in scope.**
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q8 (QUESTIONS.md, ruled 2026-09-19): LocateTreeNode resolves LType to its primary node. Every sub-node under it gets the Bloom-filter test. Negative sub-nodes are skipped, and candidate LIDs from every positive sub-node are unioned. For NOT, Lall = all LIDs under that primary LType node.
- Says: Eq. 27: `NOT(L1) = Lall \ L1`.
- Ambiguous because: `Lall` — "the list of all logs" — is never scoped. Is it all `LID`s in the entire system, or all `LID`s under the query's `LType`/`targetNode`? These give very different (and differently expensive) results.
- Needed: an explicit definition of `Lall`'s scope.

**A12 — `identifyTamperedFiles` reads an uninitialized variable on one code path.**
- Says: Alg. 4.1: `tampered_lids` is assigned only inside `if ComputedProof != StoredProof` (line 4–5); the next statement, `if tampered_lids != []` (line 7), executes unconditionally, including on the path where the `if` on line 4 was false.
- Ambiguous/inconsistent because: on the "integrity OK" path, `tampered_lids` was never assigned, so the check on line 7 is operating on an undefined value in the literal pseudocode.
- Needed: an explicit `tampered_lids ← []` initialization at function entry (this is a straightforward pseudocode-writing slip, not a deep ambiguity, but must not be silently patched without saying so).

**A13 — `TraceTamperedNode` parameters don't match the variables its body uses.**
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q9 (QUESTIONS.md, ruled 2026-09-19): Each candidate's hLID is verified by its quaternary Merkle proof path against the full-tree root R stored on-chain. Alg. 4.1's wording, which recomputes a root over only the candidates, is recorded as an inconsistency in the paper, because that root can't equal R for a partial result set. No per-query root is anchored.
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q10 (QUESTIONS.md, ruled 2026-09-19): At Level 0, only the leaves under the affected level-1 branches are inspected. Alg. 4.2's literal loop over every leaf conflicts with the paper's Fig. 9 description ("only drills down into the relevant branches"). This is the chosen resolution of that conflict.
- Says: Alg. 4.2 signature: `TraceTamperedNode(DHT, ComputedTree)`. Body references `stored_tree` and `recomputed_tree` throughout — names that appear nowhere in the parameter list, and it's never explained how `stored_tree` is derived from the `DHT` parameter.
- Ambiguous because: the mapping from actual parameters to the variables used inside the function isn't stated.
- Needed: an explicit definition of `stored_tree` (presumably reconstructed from `DHT`'s stored `hLID` values) and confirmation that `recomputed_tree ≡ ComputedTree`.

**A14 — No padding rule for quaternary grouping when leaf count isn't a multiple of 4.**
- Says: Theorem 2 states the tree is quaternary; Fig. 3's example has exactly 8 leaves (evenly divisible by 4 at each level).
- Ambiguous because: the paper's own experiments use dataset sizes 1,000–5,000 (Fig. 7–9), which will frequently not divide evenly into groups of 4 at every level. No duplication/padding/zero-fill rule is given for incomplete groups.
- Needed: the padding convention for incomplete quaternary groups.

**A15 — Figure 1's message flow doesn't match Phase 6 / Algorithm 3.**
- Says: Fig. 1 shows: user's keyword sent to the blockchain (step 4), blockchain returns a hashed keyword to the Search Engine (step 5), SE compares against the data tree/Bloom filter (step 6), SE sends the matched log file name to the blockchain (step 8), which resolves a file location from an IPFS-labeled store (step 9). Phase 6 prose and Alg. 3 instead compute the hash/trapdoor locally (client-side or search-engine-side) and never describe a blockchain round-trip for hashing a keyword or resolving a file location.
- Ambiguous/inconsistent because: it's unclear whether Fig. 1 is a loose illustrative simplification or an actual required message sequence with on-chain keyword-hashing and file-location-resolution calls that Alg. 3 simply omits.
- Needed: clarification of whether any blockchain transaction is required per search query beyond `VerifyAccess`/`VerifyIntegrity`.

**A16 — "Off-chain DHT" vs. Figure 1's "IPFS" box.**
- Says: Phase 6 prose repeatedly says "off-chain DHT." Fig. 1 depicts a box labeled "IPFS" as the encrypted-log storage/retrieval component, separate from the "DATA TREE AND BLOOM FILTER" box.
- Ambiguous because: IPFS itself is backed by a Kademlia DHT, so it's unclear whether the authors mean "the off-chain DHT described in Alg. 4.1/Eq. 28 *is* IPFS's DHT" or "IPFS is just where encrypted log blobs live, and the integrity DHT is a separate, unnamed data structure."
- Needed: whether IPFS is a literal requirement or just an illustrative cloud-storage stand-in.

**A17 — `updateMerkleRoot()` and `grantAccess()` are named only in the evaluation section.**
- Says: Table 3 (Gas Consumption) lists gas costs for `updateMerkleRoot()` and `grantAccess()` as runtime contract functions. Neither is defined, given a signature, or mentioned anywhere in Phase 1's protocol description.
- Ambiguous because: their parameters, callers, and exact triggering conditions are unknown.
- Needed: formal function signatures for both.

**A18 — Keyword "selection criteria" is asserted, never made concrete.**
- Says: Eq. 13 / prose: "Keywords are selected based on domain relevance, length, and criteria."
- Ambiguous because: no stopword list, length threshold, or domain dictionary is given.
- Needed: the actual selection rule(s) used in the authors' experiments.

**A19 — AES mode of operation, IV/nonce handling, and padding are never specified.**
- Says: every `EncAES`/`DecAES` reference (Eq. 4, 6, 7, 14, 15, 17) just says "AES" with a 256-bit key; the Experimental Evaluation section (VI.C) mentions PyCryptodome "supporting AES encryption and proper block padding" but names no mode.
- Ambiguous because: AES-CBC, AES-GCM, AES-CTR, etc. all fit "AES" + "256-bit key," and each has different IV/authentication requirements.
- Needed: explicit mode of operation and IV/nonce handling convention.

**A20 — Prose algorithm numbers don't match the algorithm-box captions.**
- Says: prose on p. 7 calls the tree-formation pseudocode "Algorithm 3.1" while its caption reads "Algorithm 1 Tree and Index Construction." Prose on p. 8 calls the insertion pseudocode "Algorithm 3.2" while its caption reads "Algorithm 2 Inserting Indices Into Tree T." Prose on p. 9 refers to the Boolean-search pseudocode as "Algorithm 1," while its caption (p. 10) reads "Algorithm 3 Secure Query Execution With Boolean Operations."
- Ambiguous because: purely a citation/labeling defect; doesn't affect algorithm logic, but risks propagating confused naming into code comments/docs if copied uncritically.
- Needed: none for implementation — noted so this report's own citations (which use the algorithm-box numbers: Alg. 1, 2, 3, 4.1, 4.2) aren't mistaken for an error.

**A21 — `get_sub_node(NL, Ln)` never specifies its routing key.**
- Says: Alg. 2 line 6: `sub_node nij ← get_sub_node(NL, Ln)`.
- Ambiguous because: no field of `Ln` (content, source `Si`, timestamp `Ti`, `LType`) is identified as the sub-node selector. Same root cause as A1/A10.
- Needed: the concrete attribute of a log entry that determines its sub-node.

**A22 — No described mechanism for Data Users to obtain `Kenc`.**
- Says: end of Phase 6: the system "retrieves the corresponding encrypted log entries and returns them to the user as the final search results" — i.e., results are returned *still encrypted*.
- Ambiguous because: the paper never describes how an authorized Data User acquires `Kenc` to decrypt those results locally (no key-wrapping, no per-user re-encryption, no KEM).
- Needed: the key-distribution mechanism from Data Owner to Data User.

---

## 16. Minimal implementation choices required because of those ambiguities

Each is explicitly a **Category B — NOT specified by EVSEB** decision, made only where required to produce a runnable prototype. Numbered to match the ambiguity they resolve.

- **B1** (resolves A1/A21): use a 2-level primary/sub-node hierarchy — primary node = `LType` (using Def. 2/Alg. 1's example set, `{"Firewall Logs","IDPS Logs","Application Server Logs"}`, as the seed list); sub-node key = a caller-supplied "log subtype" string taken from the log's `Si` (source) field at ingestion time. Chosen because Alg. 1/Def. 2 (the pseudocode actually implementing tree formation) is more detailed than Fig. 2's illustration, and because Fig. 2's category names ("Web Server Logs," "Apache," etc.) never appear in any algorithm.
- **B2** (resolves A2): treat the **sub-node-level** inverted index (Eq. 10) as authoritative for both insertion (matches Alg. 2) and query-time retrieval (`RetrieveFromIndex`/`BloomFilterCheck` both take `targetNode`, and only the sub-node has a populated index per Alg. 2). The primary-node-level index (`INL`, Eq. 9 / Alg. 1) is still created (to match Alg. 1's `SET_INDEX` call) but is not read during search in this prototype.
- **B3a** (resolves A3): the Bloom filter stores `PRF(Ksearch, h(w))` (i.e., the trapdoor value `Tw`), matching Alg. 2's actual insertion code, Eq. 16's actual check formula, and Theorem 5's security proof — not the plain `h(w)` from Def. 3/Eq. 8.
- **B3b** (resolves the unstated `m`, `k` from Section 6): pick fixed, documented values (e.g., `m` sized for an assumed max keyword count per sub-node at a target false-positive rate via Eq. 1, `k` computed from `m` and expected `n`). Any concrete numbers chosen here are prototype configuration, not a paper requirement, and must be called out as tunable in code/config rather than hardcoded as "the EVSEB parameters."
- **B4** (resolves A4): standardize on the field/identifier name `LID` everywhere in Go code (the name Phase 3 formally introduces), treating "PID" (Eq. 9–10) and generic "ID" (p. 8 example) as the same concept.
- **B5** (resolves A5): do not implement "encrypt keywords with `Kenc`" — keywords are processed exclusively through `SHA-256` + `HMAC-SHA256(Ksearch, ·)`, matching every algorithm that actually touches keywords (Phase 3 Step 1 prose, Alg. 2, Phase 5).
- **B6** (resolves A6): canonical `UserToken`/`AccessPolicy` field names are `UserID`, `Permissions`, `LType`, `Texp`. `Permissions` is represented as a Go string-slice of authorized `LType` values (the simplest structure consistent with "permissions... to determine log-type-level permissions," Sec. I contribution bullet 1) — this concrete type is a prototype choice, not a paper statement.
- **B7** (resolves A7 — the one structural fix required to make registration/token-issuance runnable at all): the on-chain `CTPolicy` record additionally carries the AES-encrypted **plaintext** `UserID`, `Permissions`, `LType`, `Texp` (encrypted under `Kindex`, alongside or instead of their hashes) so `Generate_User_Token` can actually decrypt and reuse them. The hashes from Eq. 5 are still computed and can still be stored/compared, but they are not treated as the only stored form. This is a direct structural change from Eq. 5's literal "hashes only" `Policy`, made because the paper's own Step 3.2 requires it to function.
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q6 (QUESTIONS.md, ruled 2026-09-19): B7 is REJECTED. B7 rejected. CTPolicy stays exactly Eq. 5–6. Tokenu is generated by the trusted registration/token authority while it still holds the plaintext registration inputs. Step 3.2 as written is internally inconsistent (it would have to reverse H(UserID)/H(Permissions)), and FR-7 is closed on that basis.
- **B8** (resolves A8): `valid_pointers` is assigned from `candidateEntries` before being passed into `PerformBooleanOperations` (i.e., the missing line `valid_pointers ← candidateEntries` is added). This is a direct, minimal, one-line completion of Alg. 3, not a redesign.
- **B9** (resolves A9): `Query` carries a slice of `(Trapdoor, LType)` pairs plus a single `BooleanOperator` (applied pairwise/left-to-right across the trapdoor list) instead of Eq. 20's single `Tw`. This is the simplest extension that makes AND/OR/NOT (Eq. 25–27) computable at all; no attempt is made to support arbitrary nested Boolean expressions, since the paper shows none.
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q6 (QUESTIONS.md, ruled 2026-09-19): B9 approved. Tw is read as a list/set of trapdoors, so one query can combine two or more keywords under AND/OR/NOT. This is a reading of the paper's query, not a different search algorithm.
- **B10** (resolves A10): `LocateTreeNode(tree, lType, tw)` takes `LType` explicitly, matching the prose's stated behavior rather than Eq. 22's literal two-argument signature.
- **B11** (resolves A11): `Lall` = the set of all `LID`s under the query's resolved `targetNode` (sub-node scope), not system-wide. Chosen because the query always carries `LType` and the tree's entire purpose (per Sec. IV.D Phase 2 intro) is to scope search away from the full corpus.
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q8 (QUESTIONS.md, ruled 2026-09-19): B11 is SUPERSEDED. LocateTreeNode resolves LType to its primary node. Every sub-node under it gets the Bloom-filter test. Negative sub-nodes are skipped, and candidate LIDs from every positive sub-node are unioned. For NOT, Lall = all LIDs under that primary LType node.
- **B12** (resolves A12): `tampered_lids` is initialized to an empty slice unconditionally at the start of `identifyTamperedFiles`.
- **B13** (resolves A13): `stored_tree` is rebuilt on demand from the `DHT` parameter's stored `hLID` values (grouped identically to how `ComputedTree` groups the live/recomputed values); `recomputed_tree` is treated as an alias for the `ComputedTree` parameter.
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q9 (QUESTIONS.md, ruled 2026-09-19): B13 is SUPERSEDED. Each candidate's hLID is verified by its quaternary Merkle proof path against the full-tree root R stored on-chain. Alg. 4.1's wording, which recomputes a root over only the candidates, is recorded as an inconsistency in the paper, because that root can't equal R for a partial result set. No per-query root is anchored. stored_tree is the quaternary tree built from the DHT's hLID values when R is anchored, and is kept off-chain.
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q10 (QUESTIONS.md, ruled 2026-09-19): At Level 0, only the leaves under the affected level-1 branches are inspected. Alg. 4.2's literal loop over every leaf conflicts with the paper's Fig. 9 description ("only drills down into the relevant branches"). This is the chosen resolution of that conflict.
- **B14** (resolves A14): incomplete quaternary groups (fewer than 4 siblings at a given level) are padded by duplicating the last available sibling hash until the group reaches size 4, then hashed as usual. This is the simplest documented convention available (analogous to standard binary-Merkle-tree odd-node duplication) and is explicitly a prototype choice, not a paper rule.
- **B15** (resolves A18): a minimal, fixed keyword-selection rule — token length ≥ 3 characters, alphabetic-only, filtered against a small fixed English stopword list (already removed in preprocessing per Eq. 11, so this mainly enforces the length threshold). Documented as configurable, not as "the" EVSEB rule.
- **B16** (resolves A19): AES-256-CBC with PKCS#7 padding and a randomly generated 16-byte IV prepended to each ciphertext. Chosen as the simplest AES construction consistent with "AES" + "proper block padding" (the only two concrete hints the paper/its cited library give).
- **B17** (resolves A17): `UpdateMerkleRoot(newRoot []byte) error` and `GrantAccess(userID string, policyCT []byte) error` are implemented as thin pass-through contract calls with no logic beyond storing the given value — no access control, no event emission, no additional validation beyond what Phase 1's other functions already specify.
- **B18** (resolves A22): the Data Owner distributes `Kenc` to a Data User out-of-band at registration time (alongside issuing `Tokenu`), stored client-side by the user. No key-wrapping/KEM scheme is implemented, since none is described.
- **B19** (development strategy, not a paper modification, per instruction #11): `internal/blockchain.BlockchainContracts` is defined as a Go interface first; an in-memory/mock implementation is used for early phases, and a real Ethereum-compatible client-backed implementation (Go equivalent of the paper's Web3.py/Solidity/Ganache stack — e.g., `go-ethereum` bindings against a local dev chain) is added when reproducing Table 3/Fig. 10–11.
- **B20** (development strategy): `internal/integrity.DHT` and the "off-chain DHT" are implemented as an in-memory Go map (`map[string][]byte`) satisfying Eq. 28's key-value semantics, since no concrete DHT protocol is specified (A16). A real distributed DHT (e.g., libp2p Kademlia) is out of scope unless the user specifically wants to benchmark network overhead (see Open Questions).

---

## 17. Explicitly out-of-scope features

Per instruction #2 and the paper's own stated future work (Sec. VII Conclusion: "we will entail the controllable searchable encryption solution to handle the keyword search capability revocation level. Also, the design and development of SE protocol for supporting cross-domain log file sharing... is worth to tackle"):

- Caching of any kind (query results, tree nodes, Bloom filter state) — not described.
- Additional databases, additional indexes beyond the tree/Bloom-filter/inverted-index/DHT/Merkle-tree named by the paper.
- Concurrency, goroutines, worker pools, or parallelism of any kind — every algorithm in the paper (Alg. 1–4.2) is sequential/loop-based as written.
- Distributed processing beyond the two explicitly named distributed components (blockchain, DHT).
- Extra authentication mechanisms beyond `Tokenu`/`VerifyAccess` (no OAuth, no mTLS-based identity, no MFA).
- Extra Merkle-tree optimizations (sparse Merkle trees, incremental/streaming recomputation, Merkle Mountain Ranges) — only the plain quaternary tree + full recompute-over-candidates described in Alg. 4.1 is required.
- Alternative search algorithms (no ranked search, no fuzzy search, no semantic/embedding search) — only exact-match Boolean AND/OR/NOT over trapdoor equality (Eq. 25–27).
- Additional API layers, REST/gRPC services, or microservices — the paper describes function-call pseudocode, not a service architecture.
- Message queues, batching, or speculative performance optimizations of any kind.
- Extra metadata fields beyond `Ti, Si, LType` (Phase 3 Step 2) and `H(UserID), H(Permissions), H(LType), Texp` (Eq. 5).
- Extra security protocols beyond AES-256/SHA-256/HMAC-SHA256 (no additional signature schemes, no zero-knowledge proofs, no TLS implementation as part of the SE core — Fig. 1's "SSL" box is a transport-layer detail, not part of the cryptographic construction analyzed here).
- New data structures beyond `Tree, PrimaryNode, SubNode, BloomFilter, InvertedIndex, DHT, MerkleTree, AccessPolicy, UserToken, Log, EncryptedLog, Trapdoor, Query`.
- Redesigned storage layouts (no custom file formats, no schema beyond what's in Section 11).
- Keyword search capability revocation beyond simple token expiry (`Texp`) — explicitly named as future work by the authors.
- Cross-domain log file sharing among multiple relying parties — explicitly named as future work by the authors.
- A real production key-management system/HSM/KMS for `Kenc/Kindex/Kauth/Ksearch` — the paper only specifies `Rand(256)` generation (Eq. 3), nothing about key storage/rotation/escrow.
- A real distributed multi-node blockchain network — a single local dev chain suffices for reproducing the paper's own Ganache-based methodology (Sec. VI.C).

---

## 18. Implementation order / milestones

Ordered by dependency, following the paper's own phase order, prioritizing correctness → paper fidelity → reproducibility → simplicity (instruction #14):

1. **M1 — Crypto primitives**: `internal/crypto` (AES-256-CBC per B16, SHA-256, HMAC-SHA256, `Rand(256)`). Depends on nothing.
2. **M2 — Access control (local/mocked chain)**: `internal/access` + `internal/blockchain` interface with an in-memory mock — `Register_User`, `Generate_User_Token` (using B7's fix), `VerifyAccess`. Depends on M1.
3. **M3 — Tree/Bloom filter/inverted index skeleton**: `internal/tree`, `internal/bloom`, `internal/index` — `treeSetup` (Alg. 1), empty structures (using B1/B2/B3a/B3b choices). Depends on M1.
4. **M4 — Keyword extraction + log/metadata encryption**: `internal/keyword`, `internal/logstore` — Eq. 11–15, `LID` assignment (using B15). Depends on M1.
5. **M5 — Index population + index encryption**: `internal/index` insertion (Alg. 2, using B4/B5), `Enc(T,BF,Inv,Kindex)` (Eq. 17). Depends on M3, M4.
6. **M6 — Trapdoor generation**: `internal/trapdoor` (Eq. 18–19), shared code path with M5's indexing HMAC step. Depends on M1.
7. **M7 — Search execution**: `internal/search` — `SecureQueryExecution` (Alg. 3, using B8/B9/B10/B11 fixes for the query object, node routing, Boolean-op wiring, and `Lall` scope). Depends on M2, M5, M6.
8. **M8 — Integrity verification**: `internal/integrity` — `DHT` (B20), quaternary `MerkleTree` (using B14's padding rule), `identifyTamperedFiles`/`RecomputeMerkleRoot`/`TraceTamperedNode` (Alg. 4.1–4.2, using B12/B13 fixes). Depends on M4 (for `hLID` inputs), M1.
9. **M9 — Real blockchain backend**: swap the M2 mock for a Go-Ethereum-backed `blockchain.BlockchainContracts` implementation (`VerifyAccess`, `VerifyIntegrity`, `UpdateMerkleRoot`, `GrantAccess` per B17/B19) against a local dev chain. Depends on M2, M8.
10. **M10 — End-to-end integration + experiment harness**: wire Phase 1→6 into a single runnable flow (`cmd/evseb`); add the Section 14 harness (configurable file size/keyword count/dataset size/tamper injection + timing instrumentation) to reproduce Fig. 4–11 / Table 3 for comparison against dht-backed/delia/blas. Depends on all prior milestones.

---

## Open questions for the user

> **Answered 2026-09-19.** Rulings for items 1–7 below, and for the development-time questions Q8–Q10 (single vs multi sub-node search, Alg. 4.1 root comparison, Alg. 4.2 Level-0 scan), are recorded in `QUESTIONS.md`. Q6, Q8, Q9 and Q10 each carry the paper-resolution note beside the requirements they touch (FR-7, FR-16–FR-19, §6 Query, §7 Step 3, §8 steps 1/3/4/6, §9, A7, A9–A11, A13, B7, B9, B11, B13). Q11–Q12, raised during development and ruled the same day, are marked at FR-8, FR-16, FR-18, §8 steps 4/6 and the §14 harness list.

These are environment/scope decisions the paper itself would never address — they require your sign-off before implementation starts, separate from the paper ambiguities in Section 15:

1. **Blockchain target**: do you want a real Ethereum-compatible chain (Solidity contracts + Go client bindings, e.g. `go-ethereum` against a local dev chain) so Table 3's gas costs and Fig. 10–11's on-chain/off-chain comparison are reproducible, or is an in-process Go interface/mock sufficient because the comparison against dht-backed/delia/blas only cares about SE/search/integrity performance, not blockchain cost?
2. **Stack alignment with `dht-backed`**: that sibling system already uses Solidity + Hardhat + a Go client-binding package for its blockchain layer. Should EVSEB's `internal/blockchain` follow the same pattern for consistency across the comparison suite, or is divergence acceptable?
3. **DHT realism**: an in-memory Go map (B20) satisfies Eq. 28's semantics for a single-process prototype. Do you want an actual networked DHT (e.g., libp2p Kademlia) or IPFS node instead, given Fig. 1 shows "IPFS" as the storage layer (A16), or is in-memory acceptable for "close enough to compare"?
4. **Experiment fidelity target**: should the harness (M10) reproduce the paper's exact parameter sweeps (200 KB–3200 KB file sizes, 20–100 keywords, 1,000–5,000 files) for apples-to-apples comparison against the numbers in Fig. 4–11, or should it instead match whatever benchmark parameters the other sibling systems already use for their own comparisons?
5. **Log corpus**: do you have real firewall/IDS/IPS/web-server log samples to test keyword extraction/classification against, or should the prototype ship a synthetic log generator?
6. **Sign-off on the two structural fixes (B7, B9)**: B7 changes what's stored on-chain for a user's policy (plaintext-recoverable fields, not just hashes, contradicting Eq. 5 as literally written) and B9 changes the `Query` shape to support genuine multi-keyword Boolean search (which Eq. 20 as literally written doesn't support). Both are necessary for the prototype to run at all, but both are real deviations from the paper's literal equations — please confirm these are acceptable before M2/M7 are built.
  > **Paper-resolution decision: This behavior resolves an inconsistency/underspecification in EVSEB. It is not presented as an optimization or extension.** — Q6 (QUESTIONS.md, ruled 2026-09-19): Answered: approve B9, reject B7. B7 rejected. CTPolicy stays exactly Eq. 5–6. Tokenu is generated by the trusted registration/token authority while it still holds the plaintext registration inputs. Step 3.2 as written is internally inconsistent (it would have to reverse H(UserID)/H(Permissions)), and FR-7 is closed on that basis. B9 approved. Tw is read as a list/set of trapdoors, so one query can combine two or more keywords under AND/OR/NOT. This is a reading of the paper's query, not a different search algorithm.
7. **`Kenc` distribution to Data Users (B18)**: if the other sibling systems already have a solved Data-Owner→Data-User key-distribution convention, should EVSEB reuse it instead of the ad hoc "out-of-band at registration" choice proposed here?
