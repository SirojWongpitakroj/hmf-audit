# Software Requirements Specification (SRS)

## DHT-Backed Ancestor-Assisted Merkle Verification for Scalable and Real-Time Log Integrity in Cloud Data Lakes

**Reproduction target (sole authoritative source):**
Saengthong, V., Mantharngkul, T., Wongsuwan, C., Ritthaisong, K., Fugkeaw, S.,
"DHT-Backed Ancestor-Assisted Merkle Verification for Scalable and Real-Time Log Integrity in Cloud Data Lakes,"
IEEE Open Journal of the Computer Society, DOI 10.1109/OJCS.2026.3663463 (accepted author version, 12 pages).

Local copy: `C:\Users\fookl\OneDrive\Desktop\Projects\hmf-log-integrity\systems\dht-backed\paper\DHT-Backed_Ancestor-Assisted_Merkle_Verification_f.pdf`

**Document status:** DRAFT — NOT READY FOR DEV
**Overall verdict:** **NOT READY — USER PERMISSION REQUIRED**
**Blocking items:** 39 open questions (Q1–Q39, with Q33 superseded by Q37). Q1–Q34 remain BLOCKED and unanswered.
**Implementation language:** **Go (golang)** — USER-APPROVED deviation from Table 5's Python 3.10. See §0.7.
**Global constraint:** no unstated optimization may be implemented. See §0.6.

---

## 0. HOW THIS DOCUMENT IS TO BE USED

### 0.1 Authority rule

The attached paper is the ONLY authoritative specification. Where the paper states a behavior, it is required exactly as stated. Where the paper does not state a behavior, no decision has been made and none may be made without explicit user approval.

### 0.2 Requirement classification

Every requirement below carries exactly one classification:

| Class | Meaning |
|---|---|
| **PAPER-DERIVED** | Directly supported by the paper, with section / equation / table / figure / page traceability. |
| **LOGICALLY NECESSARY** | Strictly necessary to realize a paper-defined behavior; introduces no new design decision. |
| **USER-APPROVED** | Not specified by the paper, but explicitly approved by the user. *(Currently exactly one item: the implementation language, Go — see §0.7. No question Q1–Q39 has been answered.)* |
| **BLOCKED — USER PERMISSION REQUIRED** | Necessary for implementation but neither specified by the paper nor approved by the user. |

No fifth category exists.

### 0.3 DEVELOPER CONSTRAINT (verbatim, binding)

> "Implement only requirements marked PAPER-DERIVED, LOGICALLY NECESSARY, or USER-APPROVED.
>
>  Do not implement requirements marked BLOCKED — USER PERMISSION REQUIRED.
>
>  If development reveals another unspecified implementation decision, stop work on that portion and return the question to the user."

The Developer Agent has NO authority to resolve specification gaps.

### 0.4 TESTER CONSTRAINT (verbatim, binding)

> The Tester Agent must test only behavior supported by:
>
> - the paper;
> - logically necessary consequences of paper-defined behavior; or
> - explicit user-approved implementation decisions.
>
> The Tester Agent must NOT create expected behavior based on assumptions.
>
> If a test cannot determine its expected result because the paper does not specify the behavior:
>
>     mark the test BLOCKED — USER PERMISSION REQUIRED
>
> and return the ambiguity to the user.
>
> Do not make an implementation assumption simply to make a test possible.

### 0.5 Prohibition on defaults

There is no "reasonable default" in this reproduction. The phrases "a reasonable implementation would", "typically this would use", "we can assume", "for simplicity", "a common approach is", "best practice would be", "the developer may choose", and "use X by default" are not acceptable justifications anywhere in this project.

### 0.6 GLOBAL NO-OPTIMIZATION RULE (binding on Developer and Tester)

> **No optimization of any kind may be implemented unless the paper explicitly specifies it. If an optimization is possible but the paper does not state it, it MUST NOT be implemented.**
>
> This includes, without limitation: caching, memoization, concurrency, goroutines, parallel execution, batching beyond what the paper defines, buffer or allocation reuse, precomputation, lazy evaluation, indexes, memory-layout tuning, algorithmic substitutions that change the paper's stated procedure, and any use of Go concurrency primitives not required by paper-defined behavior.
>
> Where the paper's stated procedure is less efficient than a known alternative, the paper's procedure MUST be implemented as stated.
>
> Performance shortfalls relative to the paper's reported figures are NOT grounds for adding an unstated optimization; report the shortfall instead.

**Binding on the Developer Agent:** the Developer shall implement the paper's stated procedure literally, even where a faster equivalent is obvious. An unstated optimization is a specification violation, not an improvement.

**Binding on the Tester Agent:** the Tester shall NOT treat the absence of an unstated optimization as a defect, and shall NOT require an unstated optimization in order to reach a performance figure. A failure to reach a reported performance number is reported as a measured shortfall against R-EVAL-03/04/05, never as grounds to add optimization.

**Boundary case — caching.** The paper's own verification procedure refers to "cached P_j,k" (Sec. III.B, p. 4, twice). Caching is therefore *partly paper-mentioned but wholly undefined*: the paper names a cache but specifies no location, scope, eviction policy, size, or lifetime. This rule does **not** authorize implementing a cache. The permissible scope of that one paper-mentioned cache remains **BLOCKED under Q32**. All caching not covered by a resolved Q32 answer falls under the prohibition above.

### 0.7 Implementation language — USER-APPROVED DEVIATION FROM THE PAPER

> **The implementation language is Go (golang).** Classification: **USER-APPROVED**.
> Authority: explicit user instruction — *"before that I would like you to insert that this should be done in golang. Moreover if there can be optimization I would like you to not do it if the paper have not stated"*.

**This deviates from the paper and does so deliberately.** Table 5 (p. 10) specifies **Python 3.10**. Under the Conflict Rule the paper normally wins; that rule's sole exception — "unless the user explicitly asks you to do so" — has been exercised by the user. The original Table 5 values are preserved throughout this document so the deviation remains visible and auditable.

**Scope of this approval is exactly one item: the implementation language.** It does not authorize any other decision. In particular it does **not** by inference approve any Go library, package, module, toolchain version, standard-library facility, serialization mechanism, or concurrency use. Every such consequence that the paper cannot supply is raised as a new numbered question (Q35–Q39) and remains BLOCKED.

**Known consequence:** Table 5's toolchain is Python-specific and is now partly unusable — `pycryptodome`, `merkletools (modified)`, `bitarray`, `time.perf_counter`, and NumPy have no automatic Go counterparts. No substitute has been selected. See Q35–Q39.

---

## 1. OPEN QUESTIONS FOR THE USER

**Nothing in this section may be resolved by any agent. Each item blocks the requirement IDs listed.**

Three kinds of item appear here:
- **[GAP]** — the paper simply does not specify the information.
- **[PAPER AMBIGUITY — USER DECISION REQUIRED]** — the paper says two different things in two places, and the contradiction must not be resolved by an agent.
- **[LANGUAGE CONSEQUENCE]** — the paper *did* specify something, but the USER-APPROVED switch to Go (§0.7) has invalidated it. The paper cannot supply a replacement, so the user must.

Q1–Q34 predate the Go decision and are unaffected by it except where explicitly noted (Q10, Q12, Q32, Q34). Q33 is **superseded** by Q37. Q35–Q39 are new language consequences.

---

### Q1 — [GAP] Concrete AEAD algorithm

- **What the paper says:** "Applies **AEAD** to output ciphertext c and tag t, binding metadata (e.g., log type, timestamp) as associated data" (Sec. III.A, item 2b, p. 3). "(c, t) ← Enc_K(M, AD)" (Sec. III.C Phase 1, Eq. (14), p. 5). Security assumption (i): "the AEAD scheme is IND-CPA and INT-CTXT secure" (Sec. IV.A, p. 7). Table 4 lists cost symbol `AEAD_enc` for our scheme's ingestion (p. 10).
- **What the paper does not specify:** The concrete AEAD construction. Table 5 (p. 10) lists the crypto library as "pycryptodome (SHA-256, HMAC)" and names **no AEAD cipher at all**. HMAC is a MAC, not an AEAD scheme, so Table 5 does not resolve this.
- **Why a decision is required:** Ciphertext c and tag t are direct inputs to the leaf digest d = H(0x00 ‖ c ‖ t ‖ AD). Tag length, nonce handling, and ciphertext framing all change d, therefore change every Merkle node, every ancestor certificate, and the on-chain root. No record can be produced or tested without a fixed AEAD scheme.
- **Known candidate choices (the paper authorizes none of them):** AES-GCM; AES-CCM; AES-EAX; AES-SIV; ChaCha20-Poly1305; an Encrypt-then-HMAC composition using the pycryptodome HMAC named in Table 5.
- **Blocked requirement IDs:** R-ENC-02, R-ENC-03, R-ENC-04, R-ENC-05, R-EVAL-09

---

### Q2 — [GAP] AEAD key K: generation, storage, scope, and distribution

- **What the paper says:** "The canonicalized record is encrypted with symmetric key K" (Sec. III.C Phase 1, Eq. (14), p. 5). Table 1 (p. 5): "K — Symmetric secret key used in AEAD encryption."
- **What the paper does not specify:** How K is generated; its length; where it is stored; whether there is one K system-wide, one per producer, one per log type τ, or one per epoch e; how it reaches the Encryptor; whether it is ever rotated. The paper also never states who holds K (auditors explicitly do not decrypt — Table 4, "audit does not decrypt", p. 10).
- **Why a decision is required:** Key scope determines the encryption module's interface and the system's data model. Key length is an input to the AEAD instantiation. Neither can be inferred.
- **Known candidate choices (none authorized):** single global key; per-producer key; per-log-type key; per-epoch key. Key length: 128-bit / 192-bit / 256-bit. Storage: in-memory only; key file; environment variable; KMS.
- **Blocked requirement IDs:** R-ENC-06, R-ENC-07, R-KEY-01

---

### Q3 — [GAP] Producer signature algorithm, key generation, and key sizes

- **What the paper says:** "σ ← Sign_skP(H(0xA3 ‖ d))" (Sec. III.C Phase 1, Eq. (16), p. 5). Table 1 (p. 5): "pk_P, sk_P — Public/Private key pair of the producer." Verification: "Ver_pkP(σ, H(0xA3 ‖ d')) =? 1" (Eq. (19), p. 6). Security assumption (ii): "producer and CA signatures are EUF-CMA secure" (Sec. IV.A, p. 7). Table 4 lists `Sig_S` in our scheme's ingestion column (p. 10).
- **What the paper does not specify:** The signature algorithm, curve or modulus, key size, key generation procedure, key storage, or signature encoding. Table 5 (p. 10) lists no signature library.
- **Why a decision is required:** Producer attestation is a mandatory ingestion gate (Eq. (19): "Only verified tuples are accepted; failures are discarded"). It cannot be implemented or tested without a concrete scheme.
- **Known candidate choices (none authorized):** RSA-PSS; RSA-PKCS#1 v1.5; ECDSA (P-256 / P-384 / secp256k1); Ed25519; Ed448.
- **Blocked requirement IDs:** R-ATT-02, R-ATT-03, R-ING-02, R-KEY-02

---

### Q4 — [GAP] CA signature algorithm, CA implementation, and credential/certificate format

- **What the paper says:** "Each auditor A is issued a credential by the Certification Authority (CA)" and "τ_A = Sign_skCA(auditor_id, epoch, policy)"; "DHT verifies: Ver_pkCA(τ_A) = 1" (Sec. III.C Phase 5(a), p. 6). Table 1 (p. 5): "τ_A — Authorization token issued by CA." Table 4 lists setup for our scheme as "CA + producers + DHT/epoch" (p. 10).
- **What the paper does not specify:** The CA signature algorithm and key size; how the CA is implemented (service, library, offline script); the format of the auditor "credential" issued in step 1 and whether it is distinct from the token τ_A in step 2; how pk_CA is distributed to the DHT; any certificate format.
- **Why a decision is required:** Auditor authorization is a required system function (Phase 5(a)); the DHT must verify the token before releasing proof material.
- **Known candidate choices (none authorized):** same algorithm as the producer signature or a different one; X.509 certificates; raw public keys; JWT-style tokens; custom binary credential.
- **Blocked requirement IDs:** R-AUTH-01, R-AUTH-02, R-AUTH-03, R-AUTH-05, R-KEY-03

---

### Q5 — [GAP] Trust binding for producer public keys

- **What the paper says:** The emitted tuple is "(c, t, AD, d, σ, pk_P)" (Eq. (17), p. 6) — the producer's public key travels with the record. Verification uses pk_P from that tuple (Eq. (19), p. 6).
- **What the paper does not specify:** How the verifier establishes that pk_P legitimately belongs to a trusted producer. As written, an adversary who controls the ingestion path could substitute its own (d, σ, pk_P). The paper's Theorem C.1 (p. 7) assumes a fixed pk_P but the construction transmits it in-band. The paper does not state that the CA certifies producer keys.
- **Why a decision is required:** Whether a producer key registry / allow-list exists changes the ingestion component's data model and its failure behavior, and determines whether Theorem C.1 is testable at all.
- **Known candidate choices (none authorized):** trust pk_P as transmitted, exactly as Eq. (17)/(19) are written; maintain a pre-registered producer public-key registry; have the CA issue producer certificates.
- **Blocked requirement IDs:** R-ING-03, R-KEY-02

---

### Q6 — [GAP] Concrete canonicalization function Canon(·) and raw log input format

- **What the paper says:** "M ← Canon(M_raw)" where "Canon(·) normalizes field order, encoding, and structure to ensure consistent representation across heterogeneous log sources" (Sec. III.C Phase 1, Eq. (13), p. 5). "Canonicalizes the structure for uniformity across sources" (Sec. III.A item 2a, p. 3). Table 1 (p. 5): "M_raw — Raw log record emitted by producer before preprocessing; M — Canonicalized log record after normalization."
- **What the paper does not specify:** The input log format (JSON, syslog, CEF, key-value, CSV, ...); the canonical output format; the field-ordering rule; the character encoding; number/timestamp normalization; whitespace handling; the field set of a log record.
- **Why a decision is required:** Canon(·) determines M, which determines c and t, which determine every digest in the system. Two implementations with different Canon(·) produce different roots for the same input. Reproduction and testing are impossible without it.
- **Known candidate choices (none authorized):** JSON Canonicalization Scheme (RFC 8785); sorted-key JSON with UTF-8; CBOR canonical form; a fixed-field delimited encoding.
- **Blocked requirement IDs:** R-CAN-01, R-CAN-02, R-CAN-03, R-EVAL-08

---

### Q7 — [GAP] AD field set and byte serialization (including timestamp serialization)

- **What the paper says:** "AD = (log_type, timestamp, . . .) the associated metadata bound to the ciphertext (but not encrypted) for tamper-evident indexing" (Sec. III.C Phase 1, Eq. (14), p. 5). "AD_i (associated data) binds metadata such as the record type and timestamps" (Sec. III.B, after Eq. (1), p. 4). Table 1 (p. 5): "AD — Associated metadata bound to ciphertext."
- **What the paper does not specify:** The complete AD field list (the paper writes an open-ended "…"); the byte serialization of AD; the timestamp representation (epoch seconds, epoch milliseconds, ISO-8601, integer width, endianness, timezone); the encoding of log_type.
- **Why a decision is required:** AD is an input to both the AEAD call (Eq. (14)) and the leaf digest d = H(0x00 ‖ c ‖ t ‖ AD) (Eq. (15)). Its exact byte form is load-bearing for every hash in the system.
- **Known candidate choices (none authorized):** fixed field list vs. extensible; JSON; length-prefixed binary; big-endian 64-bit integer timestamps; RFC 3339 strings.
- **Blocked requirement IDs:** R-ENC-03, R-MKT-01, R-EVAL-08

---

### Q8 — [GAP] Byte-level semantics of the concatenation operator "‖" in all hash inputs

- **What the paper says:** Uses "‖" throughout: d = H(0x00 ‖ c ‖ t ‖ AD) (Eq. (15)/(20)); N_j,k = H(0x01 ‖ N_{j+1,2k} ‖ N_{j+1,2k+1}) (Eq. (3)/(21)); H(0xA3 ‖ d) (Eq. (16)); R_agg = H(R_τ1 ‖ … ‖ R_τm) (Eq. (7)/(24)/(30)); R^(Δ)_τ = H(d_i1 ‖ … ‖ d_ik) (Eq. (28)); R^(e)_τ = H(R^(Δ1)_τ ‖ R^(Δ2)_τ ‖ …) (Eq. (29)). Domain-separation tags are defined: "tag_L = 0x00 for leaves and tag_I = 0x01 for internal nodes" (Sec. III.B, p. 4), plus 0xA3 for attestation (Eq. (16), p. 5).
- **What the paper does not specify:** Whether "‖" is raw byte concatenation or length-prefixed concatenation; whether digests are concatenated as raw bytes or as hex strings; the byte width of the tag constants 0x00, 0x01, 0xA3 (one byte each is implied by the notation but never stated); how variable-length fields (c, AD) are delimited inside d.
- **Why a decision is required:** d = H(0x00 ‖ c ‖ t ‖ AD) concatenates three variable/fixed-length fields with no stated delimiter. Different framings yield different digests and different roots. This is not a stylistic choice; it changes the output of the system.
- **Known candidate choices (none authorized):** raw byte concatenation with no delimiters; length-prefixed (e.g. 4-byte big-endian length) fields; hex-string concatenation (the behavior of the `merkletools` library named in Table 5 is relevant here but is not stated by the paper).
- **Blocked requirement IDs:** R-MKT-01, R-MKT-03, R-ATT-02, R-EPO-01, R-RTV-01, R-RTV-03

---

### Q9 — [GAP] Meaning of the dummy-leaf constant in H(tag_L ‖ 0)

- **What the paper says:** "If n is not a power of two, we pad to n′ = 2^⌈log2 n⌉ with dummies ℓ_i = H(tag_L ‖ 0) to obtain a full binary tree" (Sec. III.B, after Eq. (2), p. 4).
- **What the paper does not specify:** Whether "0" denotes a single zero byte 0x00, the ASCII character "0", a zero-valued digest of λ bits, or an empty/zero-length string. The paper writes "0" in bold-math style, distinct from the "0x00" hex notation used for tag_L in the same expression, and never defines it.
- **Why a decision is required:** Every padded tree contains these leaves; their value propagates into the per-type root and therefore into the on-chain anchor. With the default epoch size of 1,000 logs (Table 5, p. 10) padded to n′ = 1024, 24 of every 1024 leaves are dummies.
- **Known candidate choices (none authorized):** single byte 0x00; λ/8 zero bytes; empty string; ASCII "0".
- **Blocked requirement IDs:** R-MKT-02

---

### Q10 — [GAP] DHT protocol, library, node count, replication, routing, and topology

- **What the paper says:** "Intermediate proofs are distributed via a private Distributed Hash Table (DHT)" (Sec. III.A, p. 3). "Distributed Hash Table (DHT) provides an auditable repository for reusable intermediate proofs" storing Parent Node Records, Ancestor Certificates, and Root Records (Sec. III.A item 5, pp. 3–4). "published to a private DHT for low-latency retrieval" (Sec. III.B, before Eq. (6), p. 4). Table 3 (p. 9) defines cost symbol "DHT_ℓ — One DHT lookup at level ℓ for proof material". Table 4 (p. 10) gives single-record verify cost as (L−s)H + sH + DHT_ℓ and batch as n(L−s)H + gsH + (n+g)DHT_ℓ.
- **What the paper does not specify:** The DHT protocol (Kademlia, Chord, Pastry, …); the library; the number of DHT nodes; the replication factor; routing behavior; network topology; whether the DHT is a real network or an in-process simulation. **Table 5 "Experimental Environment and Simulation Parameters" (p. 10) lists no DHT library or DHT node count whatsoever**, despite listing the language, crypto library, data-structure libraries, benchmarking tools, and blockchain simulator.
- **Why a decision is required:** The DHT is one of the seven named system entities and appears in every verification cost expression. Its nature also bears directly on the reported single-record verification latency of 0.000035 s (Sec. V.C.1, Fig. 3, p. 9) — 35 µs total for (L−s)H + sH hashes **plus a DHT lookup** is only attainable if the lookup is in-process, but the paper never says so.
- **Known candidate choices (none authorized):** in-process map/simulated DHT; local multi-node Kademlia; real networked DHT across processes/hosts. Node count and replication factor: unspecified in all cases.
- **Go interaction (§0.7):** the paper names no DHT library at all, so no Python library is being displaced here — but the answer must now name a Go-usable option or an in-process construction. Note that §0.6 forbids introducing goroutines or concurrency for a DHT that the paper does not describe as concurrent.
- **Blocked requirement IDs:** R-DHT-01, R-DHT-02, R-DHT-07, R-NFR-01, R-NFR-02, R-EVAL-03

---

### Q11 — [GAP] DHT key derivation, and the formats of tree_id and ancestor_id

- **What the paper says:** DHT-stored objects are P_j,k := (treeID, j, k, N_j,k, seq); C_s,k := (ancestorID, treeID, s, k, A_s,k, upProof_s,k, [i_min, i_max], seq); R^(e)_τ := (treeID, seq, R^(e)_τ, leafCnt, τ) (Sec. III.B, Eq. (6), p. 4); and P_j,k = (tree_id, j, k, N_j,k, commit_seq) (Eq. (22), p. 6); C_s,k = (ancestor_id, tree_id, level, index, ancestor_hash, up_proof, [t_min, t_max], commit_seq) (Eq. (23), p. 6). Table 1 (p. 5) lists P_j,k as "Parent node record in DHT for (j, k)".
- **What the paper does not specify:** The DHT **key** under which each record is stored and looked up; the format/derivation of `tree_id` (the paper never says whether it encodes τ and e, nor its type); the format/derivation of `ancestor_id`; the DHT key for Root Records and for micro-roots.
- **Why a decision is required:** Verification requires retrieving {P_j,k} and C_s,k from the DHT (Phase 4 steps 2–3, p. 6). Without a key derivation rule, retrieval cannot be implemented.
- **Known candidate choices (none authorized):** key = H(tree_id ‖ j ‖ k); key = a structured string; key = a UUID recorded in an index. tree_id as (τ, e) pair, hash thereof, or opaque counter.
- **Blocked requirement IDs:** R-DHT-03, R-DHT-04, R-DHT-05, R-DHT-06, R-VER-03

---

### Q12 — [GAP] Serialization format of DHT records and of the on-chain payload

- **What the paper says:** The tuple contents of P_j,k, C_s,k, and R^(e)_τ are given (Eq. (6), (22), (23)); the on-chain commit is "BC.commit(R^(e)_agg, e, timestamp)" (Eq. (8), p. 4) / "BC.commit(R^(e)_agg, e)" (Eq. (25), p. 6). "All items can be locally rehashed and reconciled with the blockchain anchor" (Sec. III.A item 5, p. 4).
- **What the paper does not specify:** The wire/storage serialization of these tuples (JSON, MessagePack, Protobuf, ABI encoding, custom binary), field encodings, or integer widths.
- **Why a decision is required:** Records must be stored, retrieved, and rehashed; "locally rehashed" implies a deterministic byte representation that the paper does not define.
- **Known candidate choices (none authorized):** JSON; canonical JSON; MessagePack; Protobuf; custom binary.
- **Go interaction (§0.7):** Python pickle is no longer a candidate. Go's `encoding/json`, `encoding/gob`, and `encoding/binary` are possible vehicles but **none is selected** — the choice of Go serialization package is part of Q35/Q36 tooling approval, and the wire format itself remains this question. Note that Go struct field ordering and map iteration order affect determinism (R-EVAL-08), so the answer must yield a deterministic byte representation.
- **Blocked requirement IDs:** R-DHT-06, R-EPO-04

---

### Q13 — [GAP] Authorization token τ_A: serialization, `policy` semantics, validity, transport, and enforcement scope

- **What the paper says:** "τ_A = Sign_skCA(auditor_id, epoch, policy)"; "Upon request, DHT verifies: Ver_pkCA(τ_A) = 1"; "Only valid tokens permit access to proof data" (Sec. III.C Phase 5(a), p. 6). Sec. IV.A assumption (v): "only authorized auditors possessing valid CA-issued credentials may request and verify proofs, and enforcement of auditor access control is orthogonal to the cryptographic soundness analyzed herein" (p. 7). Table 2 (p. 9) lists our auditor/trust model as "Blockchain anchor + CA-issued tokens".
- **What the paper does not specify:** The serialization of the signed triple (auditor_id, epoch, policy); the type and semantics of `policy` (what policies exist, how they are evaluated, what they restrict); whether the token carries an expiry; how the token is transported to the DHT on each request; whether the DHT checks that the requested record's epoch matches the token's `epoch` field, or only that the signature verifies; what happens to a request that fails verification.
- **Why a decision is required:** Phase 5(a) is a required system function. As literally written, the DHT performs only a signature check (Ver_pkCA(τ_A) = 1) and no policy evaluation at all — whether that literal reading is intended must be decided by the user, not by an agent.
- **Known candidate choices (none authorized):** signature-check-only, exactly as Eq. in Phase 5(a) is written; signature check plus epoch match; signature check plus a defined policy grammar (e.g. permitted log types τ). Serialization: JSON, concatenated fields, ASN.1.
- **Blocked requirement IDs:** R-AUTH-02, R-AUTH-03, R-AUTH-04, R-AUTH-05

---

### Q14 — [GAP] Smart-contract interface and on-chain storage layout; and what exactly is anchored

- **What the paper says:** "only an epoch-level root-of-roots is anchored on a public blockchain" (Abstract, p. 1). "Each epoch's root-of-roots is appended to a smart contract, forming a tamper-evident chain of integrity states" (Sec. III.A item 7, p. 4). "The blockchain stores immutable (epoch, root) tuples, providing tamper-evidence and rollback detection" (Sec. III.B, after Eq. (8), p. 4). Table 5 (p. 10): "Blockchain Simulator — Hardhat v2.20 (EVM); Solidity 0.8.20". Table 2 (p. 9): on-chain anchor/records = "Root-of-roots per epoch".
- **What the paper does not specify:** The Solidity contract's function signatures, storage structures, events, or access control; how the client submits the commit; gas/transaction handling; how a verifier reads back the anchor.
- **Additional unresolved point (see also Q15):** Sec. V.C.3 (p. 10) states "only a compact root-of-roots **and a CA signature** are anchored", but neither Eq. (8) nor Eq. (25) includes any CA signature in the commit, and no CA signature over R_agg is defined anywhere in Sec. III. Whether a CA signature is part of the on-chain payload must be decided by the user.
- **Why a decision is required:** The anchoring component and the verifier's anchor lookup cannot be built without a contract interface, and the constant-on-chain-storage claim cannot be measured without knowing the anchored payload.
- **Known candidate choices (none authorized):** mapping(uint256 => bytes32) with a commit(epoch, root) function; an append-only array; event-log-only anchoring. With or without a CA signature field.
- **Blocked requirement IDs:** R-EPO-03, R-EPO-04, R-EPO-05, R-VER-05, R-EVAL-07

---

### Q15 — [PAPER AMBIGUITY — USER DECISION REQUIRED] Blockchain commit payload: with or without `timestamp`

- **Conflicting statements:**
  - Sec. III.B, Eq. (8), p. 4: "BC.commit(R^(e)_agg, e, timestamp)."
  - Sec. III.C Phase 3, Eq. (25), p. 6: "BC.commit(R^(e)_agg, e)."
  - Sec. III.B prose, p. 4: "The blockchain stores immutable (epoch, root) tuples" — a 2-tuple, agreeing with Eq. (25).
  - Sec. V.C.3, p. 10: "only a compact root-of-roots and a CA signature are anchored" — a third, different payload.
- **What the paper does not specify:** Which payload is authoritative; and if a timestamp is included, its source (block time vs. client clock) and serialization.
- **Why a decision is required:** The commit payload defines the smart-contract ABI and the measured constant on-chain storage per epoch.
- **Known candidate choices (none authorized):** (R_agg, e); (R_agg, e, timestamp); (R_agg, e, CA signature); (R_agg, e, timestamp, CA signature).
- **Blocked requirement IDs:** R-EPO-03, R-EPO-04, R-EVAL-07

---

### Q16 — [PAPER AMBIGUITY — USER DECISION REQUIRED] Two conflicting definitions of the per-type root R^(e)_τ

- **Conflicting statements:**
  - Sec. III.B, after Eq. (3), p. 4: "the per-type root is R^(e)_τ = N_0,0" — i.e. the root of the binary MK-Tree built by Eq. (2) and (3).
  - Sec. III.C Phase 5(b), Eq. (29), p. 7: "R^(e)_τ = H(R^(Δ1)_τ ‖ R^(Δ2)_τ ‖ · · ·)" — i.e. a flat hash over the sequence of per-window micro-roots, where each micro-root is itself a flat hash R^(Δ)_τ = H(d_i1 ‖ d_i2 ‖ · · · ‖ d_ik) (Eq. (28)).
- **Why these cannot both hold:** Eq. (29) is a two-level flat-concatenation digest; N_0,0 is the root of a domain-separated binary tree. For the same set of leaf digests they produce different values. Eq. (29) also has no binary-tree structure, so no inclusion proof, no ancestor checkpoint A_s,k, no up-proof, and no stride parameter s can be defined over it — yet Phase 4's verification (p. 6) and Theorem G.1 (p. 8) all require exactly those objects and terminate at R^(e)_τ.
- **Why a decision is required:** This determines whether the real-time micro-root path and the MK-Tree path are (a) the same structure, (b) two alternative modes, or (c) two independent artifacts both published, and it determines what the verifier's step 3–4 root comparison actually compares against. It is the single most consequential contradiction in the paper.
- **Known candidate choices (none authorized):** treat Eq. (29) as authoritative and the MK-Tree as a per-window sub-structure; treat N_0,0 as authoritative and Eq. (29) as an alternative real-time-only mode producing a separate value; implement both and anchor only one; some other reconciliation the authors intended.
- **Blocked requirement IDs:** R-MKT-04, R-RTV-02, R-RTV-03, R-EPO-01, R-VER-04

---

### Q17 — [PAPER AMBIGUITY — USER DECISION REQUIRED] Ancestor certificate range field: leaf-index range or time range

- **Conflicting statements:**
  - Sec. III.B, Eq. (5), p. 4: "C_s,k = (A_s,k, upProof_s,k, **[i_min, i_max]**, seq)", introduced by "Each A_s,k covers a contiguous leaf range [i_min, i_max]".
  - Sec. III.B, Eq. (6), p. 4: "C_s,k := (ancestorID, treeID, s, k, A_s,k, upProof_s,k, **[i_min, i_max]**, seq)".
  - Sec. III.C Phase 3, Eq. (23), p. 6: "C_s,k = (ancestor_id, tree_id, level, index, ancestor_hash, up_proof, **[t_min, t_max]**, commit_seq)".
- **What the paper does not specify:** Whether [t_min, t_max] in Eq. (23) is a typographic variant of the leaf-index range or a genuinely different time-window range; if it is a time range, its source and serialization (see Q7).
- **Why a decision is required:** The field is part of the certificate that the verifier retrieves and rehashes; its type changes the certificate's serialization and any range-based lookup.
- **Known candidate choices (none authorized):** leaf-index range only; timestamp range only; both fields present.
- **Blocked requirement IDs:** R-ANC-02, R-DHT-05

---

### Q18 — [PAPER AMBIGUITY — USER DECISION REQUIRED] Inclusion proof contents: sibling hashes S↓_i or parent node records {P_j,k}

- **Conflicting statements:**
  - Sec. III.B, Eq. (9), p. 4: "π_i = (ℓ_i, **S↓_i**, C_s,k, R^(e)_agg), where S↓_i are sibling hashes from ℓ_i up to its ancestor A_s,k."
  - Sec. III.C Phase 4, Eq. (26), p. 6: "π_i = (d, **{P_j,k}**, C_s,k, R^(e)_agg)."
  - Sec. III.B step 1, p. 4, mixes both: "derive A_s,k using S↓_i **and cached** P_j,k."
  - Sec. III.A item 6, p. 4: "recomputes leaf-to-ancestor paths using **parent node records**".
  - Sec. III.C Phase 4 step 2, p. 6: "Retrieve {P_j,k} from the DHT and recompute the path to A_s,k."
- **Why these differ materially:** P_j,k contains the node value N_j,k on the path (Eq. (22)), not the sibling value. Recomputing a Merkle path upward requires the **siblings**, not the path nodes themselves; using the stored path nodes instead would mean trusting DHT-supplied node values rather than recomputing them, which changes the security argument in Theorem F.1 (p. 8). The paper asserts both and never reconciles them.
- **Why a decision is required:** This defines the proof object, the DHT fetch pattern, the number of hashes performed, and what the soundness test actually tests.
- **Known candidate choices (none authorized):** proof carries sibling hashes only, with P_j,k used purely as an optional cache; proof carries the P_j,k records and siblings are derived from them; proof carries both.
- **Blocked requirement IDs:** R-VER-02, R-VER-03, R-DHT-04, R-ANC-03

---

### Q19 — [PAPER AMBIGUITY — USER DECISION REQUIRED] Who performs canonicalization and encryption: a separate Encryptor entity or the producer

- **Conflicting statements:**
  - Sec. III.A item 2, p. 3: "**Encryptor** performs trusted preprocessing. For each record, it: a) Canonicalizes the structure…; b) Applies AEAD…" — a distinct system entity, shown as a separate actor in Fig. 1 (p. 3).
  - Sec. III.C Phase 1 heading, p. 5: "**Producer-Side** Canonicalization, Encryption, and Public Attestation"; "Each producer … prepares its logs before submission."
- **What the paper does not specify:** Whether the Encryptor is a separate trusted process between producers and the lake, or a producer-side library; and consequently who holds K (see Q2) and where σ is generated relative to encryption.
- **Why a decision is required:** This determines the component boundaries, the trust boundary, and the key-distribution model.
- **Known candidate choices (none authorized):** separate Encryptor service; producer-embedded encryption library; both (per Fig. 1's separate icon with producer-side semantics).
- **Blocked requirement IDs:** R-SYS-02, R-SYS-03, R-ENC-01, R-ATT-01

---

### Q20 — [PAPER AMBIGUITY — USER DECISION REQUIRED] Who performs ingestion-time signature verification

- **Conflicting statements:**
  - Sec. III.C Phase 2, p. 6: "Upon ingestion, **the verifier** performs integrity and authenticity validation without decryption" — running Eq. (18)/(19) and discarding failures.
  - Sec. III.A item 6, p. 4: "**Verifier** checks log integrity in two modes: Inclusion Check … Batch Check" — i.e. the Verifier is the auditor-side entity, and neither mode is ingestion-time signature checking.
  - Sec. III.C Phase 4, p. 6: "**Auditors** may verify integrity of individual or batched records" — auditors, not the ingestion path.
- **What the paper does not specify:** Which component runs Phase 2. Fig. 1 (p. 3) shows no distinct ingestion-verification actor.
- **Why a decision is required:** Phase 2 is a mandatory gate ("Only verified tuples are accepted; failures are discarded"). Its owner determines the pipeline architecture.
- **Known candidate choices (none authorized):** the Partitioner performs Phase 2 before tree construction; a dedicated ingestion verifier component; the same Verifier component in a different mode.
- **Blocked requirement IDs:** R-SYS-06, R-ING-01, R-ING-04

---

### Q21 — [PAPER AMBIGUITY — USER DECISION REQUIRED] Window Δ: a time window or a fixed log count

- **Conflicting statements:**
  - Sec. III.C Phase 5(b), p. 7: "Logs are grouped within a **sliding time window Δ**."
  - Table 1, p. 5: "R^(Δ)_τ — Micro-root for **real-time window Δ**."
  - Sec. V.C.5, p. 11: "An **aggregation window of 1,024 logs** is used as the baseline configuration"; Fig. 7 (p. 11) legend gives W = 256, 512, 1024, 2048 — all counts, not durations.
- **What the paper does not specify:** Whether Δ is a duration or a record count; if a duration, its value and units (Table 5 lists no window duration); whether "sliding" means overlapping windows or consecutive tumbling windows — and if overlapping, whether a record contributes to more than one micro-root, which Eq. (29) does not account for.
- **Why a decision is required:** Δ determines micro-root membership, hence R^(Δ)_τ, hence (per Eq. (29)) possibly R^(e)_τ itself.
- **Known candidate choices (none authorized):** count-based tumbling windows of 1,024 logs; count-based sliding windows; time-based windows of an unspecified duration.
- **Blocked requirement IDs:** R-RTV-01, R-RTV-02, R-EVAL-06

---

### Q22 — [GAP] Relationship between the aggregation window, the micro-root window, and the MK-Tree; and micro-root DHT publication

- **What the paper says:** "Publish R^(Δ)_τ to the DHT for near real-time validation" (Phase 5(b) step 2, p. 7). Corollary G.1 (p. 8): "If h = O(1) via sliding-window micro-roots and batching ensures g = O(1)…". Sec. V.C.5 (p. 11) varies an "aggregation window" jointly with stride s.
- **What the paper does not specify:** Whether the "aggregation window" of Sec. V.C.5 is the same object as Δ; whether one MK-Tree is built per window or per epoch; the DHT record structure and key for a micro-root (Eq. (6) defines DHT record shapes for P, C, and R only — **not** for micro-roots); the ordering of d_i1…d_ik within Eq. (28); whether micro-roots are covered by ancestor certificates.
- **Why a decision is required:** Corollary G.1's h = O(1) claim depends on tree depth being bounded by the window, which implies per-window trees — but the paper defines the tree per (τ, e). This cannot be resolved by an agent.
- **Known candidate choices (none authorized):** one MK-Tree per epoch with windows as leaf sub-ranges; one MK-Tree per window with epoch-level consolidation per Eq. (29); windows used only for the real-time path and unrelated to the tree.
- **Blocked requirement IDs:** R-RTV-02, R-RTV-04, R-MKT-05, R-EVAL-06

---

### Q23 — [GAP] Epoch boundary definition, and `seq` / `commit_seq` semantics

- **What the paper says:** "At each epoch, all per-type Merkle roots are aggregated into a root-of-roots and immutably committed to the blockchain" (Sec. III.A item 4, p. 3). "seq is a monotonically increasing commit index" (Sec. III.B, after Eq. (5), p. 4). Fig. 2 (p. 5) shows a chain of "Seq 2 / Prev Hash / Root Hash", "Seq 3 …", "Seq 4 …" blocks feeding a "Commit". Table 5 (p. 10): "Epoch Sizes 1,000 (default), 10,000, 100,000 logs".
- **What the paper does not specify:** Whether an epoch closes on record count, elapsed time, or an explicit trigger; the scope of `seq` (per tree, per type, or global); its initial value and increment rule; what a "commit" is that `seq` indexes; and what the "Prev Hash" field shown in Fig. 2 is — **no equation in the paper defines a Prev Hash field**, and it appears in no DHT record tuple (Eq. (6), (22), (23)) nor in the on-chain payload (Eq. (8), (25)).
- **Why a decision is required:** `seq` appears in every DHT record and must be assigned deterministically. The Fig. 2 "Prev Hash" chaining is either an unstated required mechanism or an illustrative artifact; an agent must not decide which.
- **Known candidate choices (none authorized):** count-based epochs at the Table 5 sizes; time-based epochs; seq global vs. per-tree; Prev Hash implemented vs. omitted.
- **Blocked requirement IDs:** R-EPO-02, R-DHT-06, R-MKT-06, R-EVAL-05

---

### Q24 — [PAPER AMBIGUITY — USER DECISION REQUIRED] Incremental append vs. power-of-two padding

- **Conflicting statements:**
  - Sec. III.B, p. 4: "If n is not a power of two, we pad to n′ = 2^⌈log2 n⌉ with dummies … to obtain a full binary tree", with L = ⌈log2 n′⌉ and ancestor indices k ∈ {0, …, 2^s − 1} (Eq. (4)) — a fixed-size, fully-built tree.
  - Sec. III.A item 4, p. 3: "Leaf digests are computed from encrypted records and internal nodes are updated **incrementally**."
  - Sec. III.C Phase 3, Eq. (21) heading, p. 6: "Incremental Merkle Update — Only affected paths are recomputed."
- **Why these conflict:** Under true incremental append, n grows continuously, so n′ = 2^⌈log2 n⌉ doubles at each power-of-two boundary, L increases by one, and every ancestor index k and every previously published P_j,k, C_s,k become stale. The paper never states how already-published DHT records and certificates are handled across a tree-growth event, nor whether the tree is instead pre-sized to the known epoch size (1,000 → n′ = 1024).
- **Why a decision is required:** This determines the entire tree-construction and DHT-publication implementation, and whether previously issued ancestor certificates remain valid.
- **Known candidate choices (none authorized):** pre-size the tree to the configured epoch size and pad once; grow and re-pad dynamically, republishing affected records; append-only with a forest/rollover scheme.
- **Blocked requirement IDs:** R-MKT-02, R-MKT-05, R-MKT-07, R-DHT-04, R-ANC-04

---

### Q25 — [GAP + AMBIGUITY] Stride s: production default, and the inconsistent explanation in Sec. V.C.5

- **What the paper says:** "Fix a stride parameter s ∈ {1, …, L}. All nodes at level s act as ancestor checkpoints: A_s,k = N_s,k, k ∈ {0, …, 2^s − 1}" (Eq. (4), p. 4), with the root at level j = 0 and leaves at j = L (Sec. III.B, p. 4). "At a **configurable** stride s, ancestor checkpoints are produced and published to the DHT" (Sec. III.A item 4, p. 3). Theorem G.1 (p. 8): H(S, s) ≤ n(L − s) + gs. Experiments use s ∈ {1, 2, 4, 8} at n = 2^10 (Sec. V.C.5, p. 11).
- **The internal inconsistency:** Sec. V.C.5 (p. 11) explains Fig. 7 as "higher stride values generally reduce verification latency **by enabling multiple proofs to share common ancestor checkpoints**". With the paper's own indexing (root at j = 0), a higher s means a level nearer the leaves and therefore **more** checkpoints (2^s of them) and **less** sharing, not more. The measured trend in Fig. 7 is consistent with the formula H(S, s) ≤ n(L − s) + gs (the n(L − s) term shrinks as s grows), but the stated causal explanation contradicts the paper's own level indexing.
- **What the paper does not specify:** The default/production value of s (Table 5 lists epoch sizes and window sizes but **no default stride**).
- **Why a decision is required:** s is a required construction parameter; a default is needed to build and to reproduce the non-Fig-7 experiments. The explanation conflict must be adjudicated so the Tester knows which behavior is the expected one.
- **Known candidate choices (none authorized):** any s ∈ {1, …, L}; the values 1, 2, 4, 8 used in Fig. 7; a value derived from the window size.
- **Blocked requirement IDs:** R-ANC-01, R-EVAL-06

---

### Q26 — [PAPER AMBIGUITY — USER DECISION REQUIRED] How the verifier reconciles a single per-type root R^(e)_τ against an anchor that stores only R^(e)_agg

- **Conflicting / incomplete statements:**
  - Sec. III.C Phase 4 step 4, p. 6: "**Confirm R^(e)_τ against the blockchain anchor**."
  - Sec. III.B step 3, p. 4: "Use (30) and the on-chain anchor (8) to validate R^(e)_agg" — where Eq. (30) is R_agg = H(R_τ1 ‖ R_τ2 ‖ · · · ‖ R_τm), i.e. recomputation requires **all m per-type roots**, not just the one under audit.
  - The chain stores only (epoch, root-of-roots) (Sec. III.B, p. 4).
- **What the paper does not specify:** Where the verifier obtains the other m − 1 per-type roots; the canonical **ordering** of the types τ_1 … τ_m inside Eq. (7)/(24)/(30) (without a fixed order, R_agg is not reproducible); and whether the Root Records R^(e)_τ in the DHT (Eq. (6)) are the intended source, given that the DHT is explicitly adversary-controlled (Sec. IV.A, p. 7).
- **Why a decision is required:** Phase 4 step 4 is unimplementable as literally written. The type ordering is required for any anchor to be reproducible at all.
- **Known candidate choices (none authorized):** fetch all Root Records from the DHT and recompute R_agg; anchor per-type roots as well; fix a lexicographic or registration order for τ; include m and the type list in the on-chain payload.
- **Blocked requirement IDs:** R-VER-05, R-EPO-01, R-DHT-05

---

### Q27 — [GAP] Cloud data lake storage technology and stored record schema

- **What the paper says:** "**Cloud Data Lake** stores encrypted records **(c, t, AD)** at scale. The storage layer is untrusted" (Sec. III.A item 3, p. 3). "Verified tuples **(c, t, AD, d, σ, pk_P)** are stored in the cloud data lake" (Sec. III.C Phase 2, p. 6).
- **What the paper does not specify:** The storage technology (object store, filesystem, relational DB, key-value store); the schema; indexes; how a record is addressed for later audit. The two statements above also list **different stored field sets**, and the paper does not say which is authoritative.
- **Why a decision is required:** The verifier must retrieve (c, t, AD) for the record under audit (Phase 4 step 1, p. 6); the storage interface and record identity scheme are needed.
- **Known candidate choices (none authorized):** flat files; SQLite; PostgreSQL; an object store; in-memory. Field set (c,t,AD) vs. (c,t,AD,d,σ,pk_P).
- **Blocked requirement IDs:** R-ING-04, R-ING-05, R-SYS-04

---

### Q28 — [GAP] Component interfaces and process boundaries

- **What the paper says:** Names seven entities — Log Producers, Encryptor, Cloud Data Lake, Partitioner, DHT, Verifier, Blockchain (Sec. III.A, pp. 3–4) — and shows their interactions in Fig. 1 (p. 3).
- **What the paper does not specify:** Whether these are separate processes/services or in-process modules; any API structure; REST vs. gRPC vs. library calls vs. message queue; request/response schemas; ports; authentication between components (other than the auditor token of Q13).
- **Why a decision is required:** The interface style determines the entire code structure and is required before any component can be built or integration-tested.
- **Known candidate choices (none authorized):** single-process Python modules; REST/HTTP services; gRPC; message queue.
- **Blocked requirement IDs:** R-SYS-07, R-NFR-03

---

### Q29 — [GAP] Log type set, number of types m, and evaluation dataset

- **What the paper says:** Producers are "applications, API gateways, WAF, IDS, databases, etc." and "Each record is tagged with type metadata to support partitioning and per-type Merkle construction" (Sec. III.A item 1, p. 3). Table 5 (p. 10): "Log Size 256–512 bytes per record"; "Dataset Sizes 10^3, 10^4, 10^5 logs"; "Input Distributions Uniform, Zipf-heavy, Bursty". The Conclusion (p. 11) narrows the target to "append-only, high-frequency **web-server** logs".
- **What the paper does not specify:** The concrete set of log types τ used; the value of m; whether the dataset is real or synthetic and, if synthetic, the generation procedure; the field schema of a record; the parameters of the Zipf-heavy and Bursty distributions (e.g. Zipf exponent, burst size/interval).
- **Why a decision is required:** No experiment in Sec. V can be reproduced without the dataset definition, and R_agg's type count m follows from it.
- **Known candidate choices (none authorized):** synthetic generator with a fixed type list; a public web-server log corpus; user-supplied dataset.
- **Blocked requirement IDs:** R-EVAL-01, R-EVAL-02, R-EVAL-08, R-SYS-01

---

### Q30 — [GAP] Commit batch size B and the batch sizes used for verification experiments

- **What the paper says:** Table 3 (p. 9) defines "B — Logs per commit batch" and "n — Batch size used in verification experiments". Sec. V.C.3 (p. 10) defines commit time as "the total duration required to construct the authenticated data structure (ADS) for a new batch of logs and anchor the resulting integrity state on the blockchain", reporting 0.0053 s. Sec. V.C.5 (p. 11) fixes n = 2^10 for the stride experiment; Fig. 6 (p. 11) sweeps batch sizes 2^6 … 2^12.
- **What the paper does not specify:** The value of B used to obtain the 0.0053 s commit-time result, or the value of n used for the single-record and throughput results in Figs. 3–4.
- **Why a decision is required:** The commit-time and throughput figures are the paper's headline results; they cannot be reproduced or validated without the batch size that produced them.
- **Known candidate choices (none authorized):** B equal to the default epoch size (1,000); B equal to the aggregation window (1,024); some other value.
- **Blocked requirement IDs:** R-EVAL-04, R-EVAL-05

---

### Q31 — [GAP] Scope of baseline re-implementation (Schemes [8], [12], [16], [26])

- **What the paper says:** "This section compares the proposed scheme with four representative baselines: Scheme [8], Scheme [12], Scheme [16], and Scheme [26]. All baselines were **re-implemented** using identical hashing primitives, Merkle-based structures (when applicable), and the same dataset inputs" (Sec. V.C, p. 9). Numeric results are given for each in Figs. 3–5 and Tables 2 and 4.
- **What the paper does not specify:** Any construction detail of the four baselines — those live in the cited external papers, which this project is explicitly forbidden to consult. The paper gives only symbolic cost expressions (Table 4, p. 10) and result values.
- **Why a decision is required:** Reproducing Figs. 3, 4, and 5 requires the baselines. Building them from the cited papers would violate the paper-only rule; omitting them means the comparative results cannot be reproduced. The user must decide the scope.
- **Known candidate choices (none authorized):** implement the proposed scheme only and record the paper's baseline numbers as static reference values; implement baselines from the cited sources (requires lifting the paper-only restriction); omit comparative evaluation entirely.
- **Blocked requirement IDs:** R-EVAL-10, R-EVAL-11

---

### Q32 — [GAP] Caching, concurrency, retries, and timeouts

- **What the paper says:** Sec. III.B step 1 (p. 4) refers to deriving A_s,k "using S↓_i and **cached** P_j,k", and again "bottom-up recomputation from leaves to A_s,k using **cached** P_j,k" (Sec. III.B, after Eq. (6), p. 4).
- **What the paper does not specify:** What the cache is, where it lives (verifier-side, DHT-node-side), its eviction policy, its size, or its lifetime. Nor does the paper specify any concurrency model, parallel execution, retry policy, or timeout for DHT lookups or blockchain commits.
- **Why a decision is required:** The word "cached" appears in the verification procedure itself, so the reproduction must decide whether a cache is part of the specified behavior or merely descriptive — and caching materially affects the reported 0.000035 s latency and 34,482.76 logs/s throughput.
- **Known candidate choices (none authorized):** no cache (fetch every P_j,k from the DHT); unbounded verifier-side cache; bounded LRU cache. Concurrency: single-goroutine; multi-goroutine; multi-process.
- **Interaction with the global no-optimization rule (§0.6):** §0.6 prohibits all unstated optimization, explicitly naming caching, memoization, concurrency, goroutines, and parallel execution. The **only** reason this question exists at all is that the paper itself uses the word "cached" in its verification procedure — that single mention is the entire paper-stated surface. **Until Q32 is answered, the Developer implements no cache and no concurrency of any kind.** A Q32 answer may authorize a cache; it does not thereby authorize concurrency, which the paper never mentions and which §0.6 forbids absent an explicit separate approval.
- **Blocked requirement IDs:** R-VER-06, R-NFR-01, R-NFR-02, R-NFR-04

---

### Q33 — ~~[GAP] The "(modified)" merkletools library~~ — **SUPERSEDED BY Q37**

**Status: SUPERSEDED.** This question asked what the undocumented modification to the Python `merkletools` library was. Following the USER-APPROVED switch to Go (§0.7), `merkletools` and `bitarray` are Python-only and cannot be used at all, so the original question is moot.

Retained for audit trail: Table 5 (p. 10) specifies "Data Structures — `merkletools` (**modified**), `bitarray`", and never states what the modification was. The substantive concern behind this question — that a third-party Merkle library's built-in conventions (odd-node handling, hex vs. byte representation, absence of the paper's domain-separation tags) would silently override the paper's Eq. (2), (3) and padding rule — **carries over intact to Q37** and is amplified there, since the Go ecosystem's Merkle libraries have their own differing conventions.

**Superseded by:** Q37. **Blocked requirement IDs (now tracked under Q37):** R-ENV-03, R-MKT-08

---

### Q34 — [GAP] Are the paper's reported performance numbers pass/fail acceptance criteria?

- **What the paper says:** Reports single-record verification 0.000035 s (Sec. V.C.1, Fig. 3, p. 9); throughput 34,482.76 logs/s (Sec. V.C.2, Fig. 4, p. 10); commit time 0.0053 s (Sec. V.C.3, Fig. 5, p. 10); constant on-chain storage per epoch (Sec. V.C.3, p. 10); 50 repetitions with 95% confidence intervals (Sec. V.C, p. 9). Hardware: "AMD Ryzen 5 5600X CPU, NVIDIA RTX 3060 Ti GPU, and 16 GB RAM, running Windows 10 (64-bit)" (Sec. V.C, p. 9).
- **What the paper does not specify:** Any tolerance band for reproduction, and it gives no basis for expecting identical timings on different hardware. The paper also does not state what, if anything, the listed GPU is used for — no GPU-accelerated operation appears anywhere in Sec. III.
- **Why a decision is required:** The Tester Agent cannot mark a performance test pass or fail without knowing whether these are hard thresholds, relative-ordering checks, or informational reference values.
- **Known candidate choices (none authorized):** treat as hard thresholds; treat as thresholds within a stated tolerance; verify only the relative ordering versus baselines; treat as informational and assert only functional correctness.
- **Go interaction (§0.7) — materially strengthens this question:** every figure above was measured on a **Python 3.10** implementation using pycryptodome, on one specific machine. The reproduction is now in **Go**, a different language with a different performance profile, using a different (and as yet unselected — Q35/Q36) cryptographic stack. A Go implementation may plausibly beat, match, or miss these numbers, and any such difference would be attributable to the language change rather than to fidelity or infidelity of the reproduction. Treating the Python-measured figures as hard pass/fail thresholds for a Go implementation is therefore of questionable validity — but that is the user's call to make, not an agent's.
- **Interaction with §0.6:** if the Go implementation misses a reported figure, §0.6 forbids closing the gap by adding any optimization the paper does not state. The shortfall is reported, not engineered away. The Tester must not require an unstated optimization to reach any number here.
- **Blocked requirement IDs:** R-EVAL-03, R-EVAL-04, R-EVAL-05, R-EVAL-07, R-NFR-01, R-NFR-02

---

### Q35 — [LANGUAGE CONSEQUENCE] Go toolchain: language version and module identity

- **What the paper says:** Table 5 (p. 10) fixes the implementation toolchain as "Programming Language: Python 3.10". The paper states nothing about Go, because the paper's implementation is not in Go.
- **What the user has fixed:** The implementation language is Go (§0.7). Nothing further.
- **What is not specified by either:** The Go language/toolchain version; the module path; the target OS/architecture for the reproduction runs; whether CGO is permitted (which bears on which cryptographic libraries are even reachable, Q36); the repository/package layout.
- **Why a decision is required:** A Go module cannot be initialized without a version and module path, and the choice of Go version gates which standard-library and third-party APIs exist. The paper's Table 5 equivalent (Python 3.10) has no Go counterpart to translate.
- **Known candidate choices (none authorized):** any released Go version (the user must name one); module path under a chosen VCS root; CGO enabled vs. disabled (`CGO_ENABLED=0`).
- **Blocked requirement IDs:** R-ENV-01, R-ENV-07

### Q36 — [LANGUAGE CONSEQUENCE] Go cryptographic provider for H, AEAD, and signatures

- **What the paper says:** Table 5 (p. 10): "Crypto Library: **pycryptodome** (SHA-256, HMAC) [30]", with reference [30] pointing to the PyCryptodome PyPI package. Sec. III.B (p. 4) requires a collision-resistant H with λ ∈ {256, 384, …}; Sec. IV.A (p. 7) requires an IND-CPA + INT-CTXT AEAD and EUF-CMA signatures.
- **What the paper does not specify / what Go invalidates:** `pycryptodome` is a Python C-extension package and is unusable from Go. The paper names **no** Go cryptographic library, and cannot, since it predates this decision. Separately, the paper never named a concrete AEAD (Q1) or signature scheme (Q3, Q4) in *any* language — so those remain open independently of this question.
- **Why a decision is required:** Every hash, encryption, and signature operation in the system needs a concrete Go implementation. The SHA-256 **algorithm** is fixed by Table 5 (R-CRY-01) and is unaffected by the language change; the **provider** of that algorithm is not. Different providers also differ in API shape, which affects how AD is passed to the AEAD (Q7) and how signatures are encoded (Q3).
- **Known candidate choices (none authorized):** Go standard library (`crypto/sha256`, `crypto/aes` + `crypto/cipher` AEAD interfaces, `crypto/ecdsa`, `crypto/ed25519`, `crypto/rsa`); `golang.org/x/crypto` (e.g. `chacha20poly1305`); a third-party or CGO-bound library. **I have deliberately not selected among these, including the standard library** — §0.7 approved the language only, and the prohibited-assumptions list covers concrete AEAD ciphers, signature algorithms, and key sizes regardless of which language they are written in.
- **Relationship to existing questions:** Q1 (which AEAD), Q3 (which producer signature), Q4 (which CA signature) ask *what algorithm*; this question asks *which Go implementation provides it*. Both halves must be answered.
- **Blocked requirement IDs:** R-ENV-02, R-CRY-01 (provider only — the SHA-256 algorithm choice is unaffected), R-ENC-02, R-ATT-02, R-AUTH-03

### Q37 — [LANGUAGE CONSEQUENCE] Merkle-tree and bit-manipulation implementation in Go (supersedes Q33)

- **What the paper says:** Table 5 (p. 10): "Data Structures: `merkletools` (**modified**), `bitarray`". The paper never states what the modification to `merkletools` was.
- **What the paper does not specify / what Go invalidates:** Both named packages are Python-only. The paper names no Go equivalent. The original undocumented-modification problem (Q33) is now compounded: any third-party Go Merkle library carries its own conventions for node ordering, odd-node/duplicate-node handling, byte vs. hex digest representation, and — critically — **none of them implement the paper's domain-separation tags** tag_L = 0x00 and tag_I = 0x01 (Sec. III.B, p. 4). Silently adopting a library's conventions would override the paper's Eq. (2), (3) and its padding rule.
- **Why a decision is required:** The MK-Tree is the core artifact of the paper. Whether it is hand-implemented directly from Eq. (2)/(3) or built on a library determines whether the paper's stated hashing convention survives contact with the code. It is also unclear what role `bitarray` played in the original — the paper never explains it — so its Go counterpart, if any, cannot be inferred.
- **Known candidate choices (none authorized):** implement the MK-Tree directly from Eq. (2)/(3)/(4) with no third-party Merkle dependency; adopt a Go Merkle library and override its hashing to match the paper; some other approach. For `bitarray`: omit entirely (its purpose is unstated), or select a Go bitset package.
- **Note on §0.6:** whichever option is chosen, the implementation must follow the paper's stated procedure literally. Structural shortcuts that produce the same root faster (incremental recomputation beyond the paper's stated "only affected paths", precomputed subtree tables, etc.) are optimizations and are prohibited unless the paper states them.
- **Blocked requirement IDs:** R-ENV-03, R-MKT-08

### Q38 — [LANGUAGE CONSEQUENCE] Does the Hardhat / Solidity blockchain toolchain survive the language change, and how does Go reach it?

- **What the paper says:** Table 5 (p. 10): "Blockchain Simulator: **Hardhat v2.20 (EVM); Solidity 0.8.20**." Sec. III.A item 7 (p. 4): the root-of-roots is "appended to a smart contract".
- **Analysis of survival (stated, not decided):** Hardhat and Solidity are a **separate toolchain from the implementation language**. Hardhat is a Node.js-based EVM development environment; Solidity is the contract language. Neither is a Python library, so neither is invalidated by the Python→Go switch in the way `pycryptodome` and `merkletools` are. On that reasoning the Hardhat v2.20 / Solidity 0.8.20 specification **can** survive unchanged, and I have left R-ENV-05 classified PAPER-DERIVED.
- **What is genuinely unclear and must be decided:** How the **Go** application communicates with the Hardhat EVM node. The paper's Python implementation would have used a Python Ethereum client; no such client is named in Table 5, and no Go client is named anywhere. A JSON-RPC client, contract bindings, and transaction-signing keys are all required and none is specified. It is also unstated whether running a Node.js toolchain alongside a Go implementation is acceptable to the user, or whether an all-Go simulated chain is preferred instead.
- **Why a decision is required:** R-EPO-03/R-EPO-04 (blockchain commit and smart contract) cannot be implemented without a client mechanism, and R-VER-05 (anchor reconciliation) cannot read the anchor back.
- **Known candidate choices (none authorized):** keep Hardhat v2.20 + Solidity 0.8.20 per Table 5 and reach it from Go via a JSON-RPC client / generated contract bindings; keep the contract but substitute an all-Go EVM harness; replace the EVM simulation entirely with an in-process append-only anchor store. The first preserves Table 5 fidelity; the others deviate from it and would need the same explicit override that §0.7 gave the language.
- **Blocked requirement IDs:** R-ENV-05 (client mechanism only — the Hardhat/Solidity versions remain PAPER-DERIVED), R-EPO-03, R-EPO-04, R-VER-05

### Q39 — [LANGUAGE CONSEQUENCE] Benchmarking and statistics tooling in Go

- **What the paper says:** Table 5 (p. 10): "Benchmarking Tools: `time.perf_counter`, NumPy stats". Sec. V.C (p. 9): "Each experiment was repeated 50 times, and we report the mean values with 95% confidence intervals."
- **What the paper does not specify / what Go invalidates:** `time.perf_counter` is a Python standard-library function and NumPy is a Python package; neither exists in Go. The paper names no Go timing or statistics facility. The **methodology** — 50 repetitions, mean, 95% confidence interval — is paper-derived and unaffected; only the tooling is invalidated.
- **Why a decision is required:** The evaluation requirements (R-EVAL-02 through R-EVAL-06) mandate a specific measurement protocol that must be implemented somehow. Timer choice also affects measurement resolution, which matters at the paper's 35 µs single-record figure.
- **Known candidate choices (none authorized):** Go standard library `time` (monotonic clock) with hand-computed statistics; Go's built-in `testing.B` benchmark harness; a third-party statistics package. Note that `testing.B`'s adaptive iteration model differs from the paper's fixed 50-repetition protocol and would change the reported methodology.
- **Note on §0.6:** benchmark scaffolding must not introduce warm-up caching, buffer reuse, or parallelism into the measured code path; doing so would be an unstated optimization contaminating the measurement.
- **Blocked requirement IDs:** R-ENV-04, R-EVAL-02, R-EVAL-03, R-EVAL-04, R-EVAL-05, R-EVAL-06

---

## 2. SYSTEM SCOPE

### 2.1 In scope (per the paper)

- Producer-side canonicalization, AEAD encryption with metadata binding, and producer attestation (Sec. III.C Phase 1, p. 5).
- Ingestion-time stateless signature/integrity validation and storage of verified tuples (Sec. III.C Phase 2, p. 6).
- Per-log-type MK-Tree construction with stride-based ancestor checkpoints, DHT publication of parent node records / ancestor certificates / root records, and epoch-level root-of-roots anchoring on a blockchain (Sec. III.B, Sec. III.C Phase 3, pp. 4, 6).
- Single-record inclusion verification and batch verification with ancestor-certificate reuse (Sec. III.C Phase 4, p. 6).
- CA-issued auditor authorization tokens gating DHT proof access (Sec. III.C Phase 5(a), p. 6).
- Sliding-window micro-roots for near real-time verification (Sec. III.C Phase 5(b), p. 7).
- Experimental evaluation per Sec. V and Table 5 (pp. 9–11).

### 2.2 Out of scope (explicitly)

- **Anything not in the attached paper.** In particular, and per the user's standing instruction: HMF-Audit functionality, HMF hierarchy, proof-planner functionality, and Cassandra-specific functionality are **out of scope**. This SRS contains no requirement derived from them.
- Cross-lake reconciliation, adaptive stride selection, and privacy-preserving analytics — named by the paper itself as **future work**, not as part of this system (Sec. VI, pp. 11–12).
- Decryption during audit — the paper states audit does not decrypt (Table 4, p. 10: "— (audit does not decrypt)").
- Any additional security mechanism, additional validation, additional domain separator, alternate Merkle convention, alternate padding rule, alternate root aggregation, or alternate proof format beyond what the paper defines.

---

## 3. REQUIREMENTS

Notation follows Table 1 of the paper (p. 5).

### 3.1 System entities and architecture

**R-SYS-01 — Log Producers**
- **Class:** PAPER-DERIVED
- **Trace:** Sec. III.A item 1, p. 3; Fig. 1, p. 3
- **Requirement:** The system shall include Log Producers that generate heterogeneous logs (applications, API gateways, WAF, IDS, databases). Each record shall be tagged with type metadata to support partitioning and per-type Merkle construction.
- **Note:** The concrete type set and count m are blocked by **Q29**.

**R-SYS-02 — Encryptor entity**
- **Class:** BLOCKED — USER PERMISSION REQUIRED
- **Trace:** Sec. III.A item 2, p. 3; Fig. 1, p. 3; vs. Sec. III.C Phase 1, p. 5
- **Requirement:** The placement of canonicalization and encryption (separate Encryptor entity vs. producer-side) is contradicted between sections.
- **Blocked by:** Q19

**R-SYS-03 — Trusted preprocessing boundary**
- **Class:** BLOCKED — USER PERMISSION REQUIRED
- **Trace:** Sec. III.A item 2, p. 3 ("Encryptor performs **trusted** preprocessing")
- **Requirement:** The trust boundary between producer, Encryptor, and the untrusted lake must be fixed before component design.
- **Blocked by:** Q19, Q2

**R-SYS-04 — Cloud Data Lake, untrusted**
- **Class:** PAPER-DERIVED
- **Trace:** Sec. III.A item 3, p. 3; Sec. IV.A, p. 7
- **Requirement:** The system shall include a Cloud Data Lake storing encrypted records at scale. The storage layer shall be treated as untrusted; integrity shall be enforced externally through Merkle proofs and blockchain anchoring, enabling detection of tampering, insertion, or omission. The adversary model shall assume the adversary controls the untrusted storage layer, the DHT, and the network.
- **Note:** Storage technology and stored field set are blocked by **Q27**.

**R-SYS-05 — Partitioner**
- **Class:** PAPER-DERIVED
- **Trace:** Sec. III.A item 4, p. 3
- **Requirement:** The system shall include a Partitioner that maintains per-type Merkle trees, computes leaf digests from encrypted records, updates internal nodes incrementally, produces ancestor checkpoints at a configurable stride s and publishes them to the DHT, and at each epoch aggregates all per-type Merkle roots into a root-of-roots committed to the blockchain.

**R-SYS-06 — Verifier**
- **Class:** PAPER-DERIVED (entity existence and its two modes) / BLOCKED for Phase 2 ownership
- **Trace:** Sec. III.A item 6, p. 4; Sec. III.C Phase 2, p. 6
- **Requirement:** The system shall include a Verifier supporting exactly two modes: **Inclusion Check** (recompute leaf-to-ancestor paths using parent node records, verify ancestor-to-root using the up-proof, match the final root with the blockchain anchor) and **Batch Check** (validate all records in a partition or epoch, reusing shared proofs across subtrees to reduce hashing cost).
- **Blocked by (ownership of Phase 2 only):** Q20

**R-SYS-07 — Component interfaces**
- **Class:** BLOCKED — USER PERMISSION REQUIRED
- **Blocked by:** Q28

---

### 3.2 Cryptographic primitives

**R-CRY-01 — Hash function H**
- **Class:** PAPER-DERIVED
- **Trace:** Sec. III.B "Notation", p. 4 ("H : {0,1}* → {0,1}^λ … collision-resistant"); Table 5, p. 10 ("Crypto Library: pycryptodome (SHA-256, HMAC)"); Sec. IV.A assumption (iii), p. 7
- **Requirement:** The system shall use SHA-256 as the collision-resistant hash function H throughout (λ = 256), as fixed by Table 5.
- **Acceptance:** Every digest produced by the system (leaf, internal node, ancestor, per-type root, micro-root, root-of-roots, attestation pre-image) is 32 bytes.
- **Note on §0.7:** the **algorithm** (SHA-256) is unaffected by the switch to Go and remains PAPER-DERIVED. The **Go provider** of that algorithm is not selected and is blocked by **Q36**.

**R-CRY-02 — Domain-separation tags**
- **Class:** PAPER-DERIVED
- **Trace:** Sec. III.B, p. 4 ("tag_L = 0x00 for leaves and tag_I = 0x01 for internal nodes"); Eq. (16), p. 5 (0xA3)
- **Requirement:** The system shall use exactly three domain-separation tags and no others: 0x00 for leaf digests, 0x01 for internal nodes, 0xA3 for the producer attestation pre-image.
- **Acceptance:** No additional domain separator is introduced anywhere. Note that Eq. (7), (24), (28), (29), and (30) are specified **without** any tag; they shall be implemented without one, exactly as written.

**R-CRY-03 — Security assumptions to be preserved**
- **Class:** PAPER-DERIVED
- **Trace:** Sec. IV.A, p. 7
- **Requirement:** The implementation shall not violate the stated assumptions: (i) the AEAD scheme is IND-CPA and INT-CTXT secure; (ii) producer and CA signatures are EUF-CMA secure; (iii) H is collision resistant; (iv) blockchain commitments are immutable once finalized; (v) only authorized auditors possessing valid CA-issued credentials may request and verify proofs.

**R-KEY-01 — AEAD key management** — **Class:** BLOCKED — USER PERMISSION REQUIRED — **Blocked by:** Q2
**R-KEY-02 — Producer key pair generation, storage, distribution** — **Class:** BLOCKED — USER PERMISSION REQUIRED — **Blocked by:** Q3, Q5
**R-KEY-03 — CA key pair and pk_CA distribution** — **Class:** BLOCKED — USER PERMISSION REQUIRED — **Blocked by:** Q4

---

### 3.3 Phase 1 — Canonicalization, encryption, attestation

**R-CAN-01 — Canonicalization function** — **Class:** BLOCKED — USER PERMISSION REQUIRED
- **Trace:** Eq. (13), Sec. III.C Phase 1, p. 5
- **Paper-derived fragment:** M ← Canon(M_raw), where Canon(·) normalizes field order, encoding, and structure for consistent representation across heterogeneous sources.
- **Blocked by:** Q6

**R-CAN-02 — Raw log input format** — **Class:** BLOCKED — USER PERMISSION REQUIRED — **Blocked by:** Q6
**R-CAN-03 — Canonical output encoding** — **Class:** BLOCKED — USER PERMISSION REQUIRED — **Blocked by:** Q6

**R-ENC-01 — Encryption placement** — **Class:** BLOCKED — USER PERMISSION REQUIRED — **Blocked by:** Q19

**R-ENC-02 — AEAD encryption call**
- **Class:** PAPER-DERIVED for the interface / BLOCKED for the algorithm
- **Trace:** Eq. (14), Sec. III.C Phase 1, p. 5; Sec. III.A item 2b, p. 3; Eq. (1), Sec. III.B, p. 4
- **Requirement:** The canonicalized record M shall be encrypted under symmetric key K as (c, t) ← Enc_K(M, AD), producing ciphertext c and authentication tag t, with AD bound as associated data and **not** encrypted.
- **Blocked by (concrete AEAD):** Q1

**R-ENC-03 — Associated data AD** — **Class:** BLOCKED — USER PERMISSION REQUIRED
- **Paper-derived fragment:** AD = (log_type, timestamp, …), bound to the ciphertext but not encrypted, for tamper-evident indexing (Eq. (14), p. 5).
- **Blocked by:** Q7, Q8

**R-ENC-04 — AEAD nonce/IV handling** — **Class:** BLOCKED — USER PERMISSION REQUIRED
- **Note:** The paper's Enc_K(M, AD) signature (Eq. (14)) contains no nonce parameter, and no nonce appears in the emitted tuple (Eq. (17)) or in any stored record. Nonce generation, transport, and uniqueness are entirely unspecified, yet are mandatory for every candidate AEAD in Q1.
- **Blocked by:** Q1

**R-ENC-05 — Ciphertext/tag framing** — **Class:** BLOCKED — USER PERMISSION REQUIRED — **Blocked by:** Q1, Q8
**R-ENC-06 — Key scope** — **Class:** BLOCKED — USER PERMISSION REQUIRED — **Blocked by:** Q2
**R-ENC-07 — Key length** — **Class:** BLOCKED — USER PERMISSION REQUIRED — **Blocked by:** Q2

**R-ATT-01 — Leaf digest at producer side**
- **Class:** PAPER-DERIVED
- **Trace:** Eq. (15), Sec. III.C Phase 1, p. 5; Eq. (2), Sec. III.B, p. 4
- **Requirement:** A domain-separated leaf digest shall be computed as d = H(0x00 ‖ c ‖ t ‖ AD). This value serves as the Merkle leaf.
- **Blocked by (byte framing only):** Q8

**R-ATT-02 — Producer attestation signature**
- **Class:** PAPER-DERIVED for the construction / BLOCKED for the algorithm
- **Trace:** Eq. (16), Sec. III.C Phase 1, p. 5
- **Requirement:** The producer shall sign the digest as σ ← Sign_skP(H(0xA3 ‖ d)). The signature shall be computed over H(0xA3 ‖ d), not over d directly.
- **Blocked by:** Q3, Q8

**R-ATT-03 — Emitted tuple**
- **Class:** PAPER-DERIVED
- **Trace:** Eq. (17), Sec. III.C Phase 1, p. 6
- **Requirement:** Phase 1 shall emit exactly the tuple (c, t, AD, d, σ, pk_P).
- **Blocked by (serialization):** Q3, Q12

---

### 3.4 Phase 2 — Stateless verification and secure storage

**R-ING-01 — Stateless validation without decryption**
- **Class:** PAPER-DERIVED
- **Trace:** Sec. III.C Phase 2, p. 6
- **Requirement:** Upon ingestion, integrity and authenticity validation shall be performed **without decryption**.
- **Blocked by (which component performs it):** Q20

**R-ING-02 — Digest recomputation and signature check**
- **Class:** PAPER-DERIVED
- **Trace:** Eq. (18), (19), Sec. III.C Phase 2, p. 6
- **Requirement:** For each received record the system shall recompute d′ = H(0x00 ‖ c ‖ t ‖ AD) and check Ver_pkP(σ, H(0xA3 ‖ d′)) = 1.
- **Acceptance:** A record whose (c, t, AD) has been altered, or whose σ does not verify, fails this check.
- **Blocked by (signature algorithm):** Q3

**R-ING-03 — Accept/discard policy**
- **Class:** PAPER-DERIVED
- **Trace:** Sec. III.C Phase 2, p. 6 ("Only verified tuples are accepted; failures are discarded")
- **Requirement:** Only tuples passing R-ING-02 shall be accepted; failing tuples shall be discarded.
- **Acceptance:** A discarded record contributes no leaf to any MK-Tree and appears in no DHT record.
- **Note:** The paper specifies discard only — no logging, alerting, quarantine, or retry. None of those shall be added. Producer-key trust binding is blocked by **Q5**.

**R-ING-04 — Storage of verified tuples** — **Class:** BLOCKED — USER PERMISSION REQUIRED
- **Paper-derived fragment:** Verified tuples are stored in the cloud data lake (Sec. III.C Phase 2, p. 6), but the stored field set is stated inconsistently across Sec. III.A item 3 and Phase 2.
- **Blocked by:** Q27

**R-ING-05 — Record addressing/retrieval** — **Class:** BLOCKED — USER PERMISSION REQUIRED — **Blocked by:** Q27

**R-ING-06 — No plaintext exposure in integrity structures**
- **Class:** PAPER-DERIVED
- **Trace:** Sec. III.C Phase 2, p. 6
- **Requirement:** Integrity structures (Merkle leaves, ancestor checkpoints) shall be computed over digests {d_i} only — no plaintext exposure.
- **Acceptance:** No plaintext M or M_raw value appears in any MK-Tree node, DHT record, or on-chain payload.

---

### 3.5 MK-Tree construction

**R-MKT-01 — Leaf digests**
- **Class:** PAPER-DERIVED
- **Trace:** Eq. (2), Sec. III.B, p. 4; Eq. (20), Sec. III.C Phase 3, p. 6
- **Requirement:** Each verified record shall contribute a leaf digest ℓ_i = H(tag_L ‖ c_i ‖ t_i ‖ AD_i) with tag_L = 0x00, for i ∈ {1, …, n}.
- **Blocked by (byte framing, AD serialization):** Q7, Q8

**R-MKT-02 — Power-of-two padding**
- **Class:** PAPER-DERIVED for the rule / BLOCKED for the dummy value and for interaction with incremental append
- **Trace:** Sec. III.B, after Eq. (2), p. 4
- **Requirement:** If n is not a power of two, the leaf set shall be padded to n′ = 2^⌈log2 n⌉ with dummy leaves ℓ_i = H(tag_L ‖ 0), yielding a full binary tree.
- **Blocked by:** Q9 (meaning of "0"), Q24 (incremental growth)

**R-MKT-03 — Internal nodes**
- **Class:** PAPER-DERIVED
- **Trace:** Eq. (3), Sec. III.B, p. 4; Eq. (21), Sec. III.C Phase 3, p. 6
- **Requirement:** For j < L, N_j,k = H(tag_I ‖ N_{j+1,2k} ‖ N_{j+1,2k+1}) with tag_I = 0x01, where level j = 0 is the root, j = L is the leaf level, index k runs left-to-right, and L = ⌈log2 n′⌉.
- **Acceptance:** Left child is N_{j+1,2k}; right child is N_{j+1,2k+1}. This ordering shall not be altered.
- **Blocked by (byte framing):** Q8

**R-MKT-04 — Per-type root** — **Class:** BLOCKED — USER PERMISSION REQUIRED
- **Trace:** Sec. III.B, p. 4 (R^(e)_τ = N_0,0) vs. Eq. (29), Sec. III.C Phase 5(b), p. 7
- **Blocked by:** Q16

**R-MKT-05 — Tree scope (per type per epoch, or per window)** — **Class:** BLOCKED — USER PERMISSION REQUIRED
- **Paper-derived fragment:** "For each log type τ and epoch e, the system ingests a sequence of canonical, encrypted records L^(e)_τ = {(c_i, t_i, AD_i)}^n_{i=1}" (Eq. (1), Sec. III.B, p. 4); "records are grouped by log type τ per epoch e" (Sec. III.C Phase 3, p. 6).
- **Blocked by:** Q22, Q24

**R-MKT-06 — Leaf ordering**
- **Class:** LOGICALLY NECESSARY
- **Basis:** Eq. (1) defines L^(e)_τ as an indexed **sequence** {(c_i, t_i, AD_i)}^n_{i=1}, and Eq. (2) indexes leaves by the same i; Theorem E.1 (p. 7) makes **reordering** a detectable violation, which is only meaningful if leaf order is fixed.
- **Requirement:** Leaves shall be placed at index i in the order the records are ingested into the (τ, e) sequence, and that order shall not be permuted.
- **Note:** This introduces no design choice; it is entailed by the paper's own indexing and by Theorem E.1.

**R-MKT-07 — Incremental update**
- **Class:** PAPER-DERIVED for the statement / BLOCKED for the mechanism
- **Trace:** Sec. III.A item 4, p. 3; Sec. III.C Phase 3 "Incremental Merkle Update — Only affected paths are recomputed", p. 6
- **Blocked by:** Q24

**R-MKT-08 — Merkle implementation vehicle** — **Class:** BLOCKED — USER PERMISSION REQUIRED — **Blocked by:** Q37 *(formerly Q33; superseded by the switch to Go, §0.7)*

---

### 3.6 Stride-based ancestor checkpoints and certificates

**R-ANC-01 — Ancestor checkpoint definition**
- **Class:** PAPER-DERIVED for the definition / BLOCKED for the default value of s
- **Trace:** Eq. (4), Sec. III.B, p. 4; Sec. III.A item 4, p. 3
- **Requirement:** Given a stride parameter s ∈ {1, …, L}, all nodes at level s shall act as ancestor checkpoints: A_s,k = N_s,k for k ∈ {0, …, 2^s − 1}. The stride shall be configurable.
- **Blocked by:** Q25

**R-ANC-02 — Ancestor certificate contents** — **Class:** BLOCKED — USER PERMISSION REQUIRED
- **Trace:** Eq. (5), Sec. III.B, p. 4; Eq. (23), Sec. III.C Phase 3, p. 6
- **Paper-derived fragment:** A certificate binds the ancestor hash, an up-proof, a range, and a commit sequence, plus (per Eq. (6)/(23)) an ancestor id, tree id, level, and index.
- **Blocked by:** Q17 (range field type), Q12 (serialization)

**R-ANC-03 — Up-proof definition**
- **Class:** PAPER-DERIVED
- **Trace:** Sec. III.B, after Eq. (5), p. 4
- **Requirement:** upProof_s,k shall be the **minimal** set of sibling hashes required to recompute the path A_s,k ⇒ R^(e)_τ.
- **Acceptance:** For a tree of depth L and stride s, |upProof_s,k| = s sibling hashes.
- **Blocked by (ordering/serialization of the sibling list, and the S↓ vs. P conflict):** Q8, Q18

**R-ANC-04 — Certificate issuance timing**
- **Class:** PAPER-DERIVED for the trigger / BLOCKED for behavior under tree growth
- **Trace:** Sec. III.A item 4, p. 3 ("At a configurable stride s, ancestor checkpoints are produced and published to the DHT"); Sec. III.C Phase 3, p. 6 ("At stride level s, ancestor checkpoints are formed")
- **Blocked by:** Q24

**R-ANC-05 — Is the ancestor certificate cryptographically signed?**
- **Class:** BLOCKED — USER PERMISSION REQUIRED
- **What the paper says:** The paper repeatedly describes these objects as **certificates** and the checkpoints as "promoted to **certified**, reusable verification checkpoints" (Sec. I contributions, p. 2) and "elevates selected ancestor nodes to persistent verification checkpoints" (Sec. III.B, p. 4). Table 2 (p. 9) lists "MK-Tree + **ancestor certs** + DHT".
- **What the paper does not specify:** No signature field appears in C_s,k in **any** of its three definitions (Eq. (5), Eq. (6), Eq. (23)), and no signing operation over a certificate is defined anywhere. Table 4's ingestion cost for our scheme is "AEAD_enc + H + Sig_S" (p. 10) — a single signature, which Eq. (16) already accounts for as the producer attestation. Whether "certificate" denotes a signed object or merely a DHT-published record must be decided by the user.
- **Why it matters:** If certificates are unsigned, their authenticity rests entirely on recomputation against the on-chain anchor; if signed, a signer identity and key must be defined. Lemma G.1 (p. 8) relies on ancestors being "certified" without stating the mechanism.
- **Blocked by:** this question is raised as part of **Q4** (CA scope) and must be answered explicitly.

---

### 3.7 DHT

**R-DHT-01 — Private DHT** — **Class:** BLOCKED — USER PERMISSION REQUIRED
- **Paper-derived fragment:** A **private** DHT provides an auditable repository for reusable intermediate proofs, supporting low-latency retrieval (Sec. III.A item 5, pp. 3–4; Sec. III.B, p. 4).
- **Blocked by:** Q10

**R-DHT-02 — DHT topology, nodes, replication, routing** — **Class:** BLOCKED — USER PERMISSION REQUIRED — **Blocked by:** Q10

**R-DHT-03 — DHT key derivation** — **Class:** BLOCKED — USER PERMISSION REQUIRED — **Blocked by:** Q11

**R-DHT-04 — Parent node records**
- **Class:** PAPER-DERIVED for contents / BLOCKED for keys, serialization, and publication set
- **Trace:** Eq. (6), Sec. III.B, p. 4; Eq. (22), Sec. III.C Phase 3, p. 6
- **Requirement:** P_j,k = (tree_id, j, k, N_j,k, commit_seq). Per Sec. III.C Phase 3 (p. 6), "Each recomputed internal node **below the ancestor level** is recorded in the DHT."
- **Acceptance:** Nodes at levels j > s are published as P_j,k; nodes at levels j < s are not (they are covered by up-proofs).
- **Blocked by:** Q11, Q12, Q18, Q24

**R-DHT-05 — Root records**
- **Class:** PAPER-DERIVED for contents / BLOCKED for keys and serialization
- **Trace:** Eq. (6), Sec. III.B, p. 4
- **Requirement:** R^(e)_τ := (tree_id, seq, R^(e)_τ, leafCnt, τ) shall be published to the DHT.
- **Blocked by:** Q11, Q12, Q17, Q26

**R-DHT-06 — Local rehashing and reconciliation**
- **Class:** PAPER-DERIVED for the property / BLOCKED for serialization
- **Trace:** Sec. III.A item 5, p. 4 ("All items can be locally rehashed and reconciled with the blockchain anchor")
- **Blocked by:** Q12, Q23

**R-DHT-07 — DHT is adversary-controlled**
- **Class:** PAPER-DERIVED
- **Trace:** Sec. IV.A, p. 7 ("adversary A that controls the untrusted storage layer, DHT, and network")
- **Requirement:** The verification logic shall not assume DHT-returned data is authentic; correctness shall rest on recomputation against the blockchain anchor.
- **Acceptance:** A test that corrupts a DHT-stored P_j,k or C_s,k causes verification to fail.
- **Note:** This requirement interacts directly with **Q18** — if proofs carry P_j,k node values rather than siblings, satisfying this acceptance criterion is non-trivial.

---

### 3.8 Epoch aggregation and blockchain anchoring

**R-EPO-01 — Root-of-roots aggregation**
- **Class:** PAPER-DERIVED for the formula / BLOCKED for type ordering
- **Trace:** Eq. (7), Sec. III.B, p. 4; Eq. (24), Sec. III.C Phase 3, p. 6; Eq. (30), Sec. III.C Phase 5(b), p. 7
- **Requirement:** At epoch end, all per-type roots shall be aggregated as R^(e)_agg = H(R^(e)_τ1 ‖ R^(e)_τ2 ‖ · · · ‖ R^(e)_τm), with **no** domain-separation tag (as written).
- **Blocked by:** Q26 (canonical ordering of τ_1 … τ_m), Q8, Q16
- **Minor note (not a blocker):** Eq. (24) writes the type count as n (R_T1 … R_Tn) while Eq. (7) and (30) write m; these denote the same quantity, and n is used elsewhere for batch/leaf count.

**R-EPO-02 — Epoch boundary** — **Class:** BLOCKED — USER PERMISSION REQUIRED — **Blocked by:** Q23

**R-EPO-03 — Blockchain commit** — **Class:** BLOCKED — USER PERMISSION REQUIRED
- **Paper-derived fragment:** The aggregated root is immutably committed on-chain, and the chain stores immutable (epoch, root) tuples providing tamper-evidence and rollback detection (Eq. (8), p. 4; Eq. (25), p. 6; Sec. III.A item 7, p. 4).
- **Blocked by:** Q14, Q15

**R-EPO-04 — Smart contract** — **Class:** BLOCKED — USER PERMISSION REQUIRED
- **Paper-derived fragment:** "Each epoch's root-of-roots is appended to a smart contract, forming a tamper-evident chain of integrity states" (Sec. III.A item 7, p. 4). Table 5 (p. 10) fixes the toolchain: Hardhat v2.20 (EVM), Solidity 0.8.20.
- **Blocked by:** Q14, Q15

**R-EPO-05 — Constant on-chain storage per epoch**
- **Class:** PAPER-DERIVED for the property / BLOCKED for the measured payload
- **Trace:** Abstract, p. 1; Sec. V.C.3, p. 10; Table 2, p. 9; Table 3 symbol "R — On-chain root-of-roots record (constant size)", p. 9
- **Requirement:** On-chain storage per epoch shall be constant and independent of the number of records in the epoch.
- **Acceptance:** On-chain bytes written per epoch at epoch sizes 1,000 / 10,000 / 100,000 are identical.
- **Blocked by:** Q14, Q15

---

### 3.9 Phase 4 — Verification and audit

**R-VER-01 — Single-record digest recomputation**
- **Class:** PAPER-DERIVED
- **Trace:** Sec. III.C Phase 4 step 1, p. 6
- **Requirement:** Single-record verification shall begin by computing d = H(0x00 ‖ c ‖ t ‖ AD) from the retrieved record.

**R-VER-02 — Inclusion proof structure** — **Class:** BLOCKED — USER PERMISSION REQUIRED
- **Trace:** Eq. (9), Sec. III.B, p. 4 vs. Eq. (26), Sec. III.C Phase 4, p. 6
- **Blocked by:** Q18

**R-VER-03 — Bottom-up recomputation to the ancestor**
- **Class:** PAPER-DERIVED for the step / BLOCKED for the proof material
- **Trace:** Sec. III.C Phase 4 step 2, p. 6; Sec. III.B step 1, p. 4; Sec. III.A item 6, p. 4
- **Requirement:** The verifier shall retrieve proof material from the DHT and recompute the path from the leaf up to its ancestor checkpoint A_s,k.
- **Blocked by:** Q11, Q18

**R-VER-04 — Top-down validation to the per-type root**
- **Class:** PAPER-DERIVED for the step / BLOCKED by the root definition conflict
- **Trace:** Sec. III.C Phase 4 step 3, p. 6; Sec. III.B step 2, p. 4
- **Requirement:** The verifier shall retrieve C_s,k and validate the path A_s,k ⇒ R^(e)_τ using upProof_s,k.
- **Blocked by:** Q16

**R-VER-05 — Anchor reconciliation** — **Class:** BLOCKED — USER PERMISSION REQUIRED
- **Trace:** Sec. III.C Phase 4 step 4, p. 6 vs. Sec. III.B step 3, p. 4
- **Blocked by:** Q26, Q14

**R-VER-06 — Verification outcome**
- **Class:** LOGICALLY NECESSARY
- **Basis:** Theorem F.1 (p. 8) and Definition D.1 (p. 7) speak of an inclusion proof being "accepted" or not; an accept/reject outcome is entailed.
- **Requirement:** Verification shall yield a binary accept/reject result; it shall reject if any recomputation step fails to match.
- **Note:** No specific error taxonomy, message format, remediation, alerting, or retry behavior is specified by the paper and none shall be added. Caching is blocked by **Q32** and, pending that answer, prohibited by **§0.6**.

**R-VER-07 — Batch verification with certificate reuse**
- **Class:** PAPER-DERIVED
- **Trace:** Eq. (27), Sec. III.C Phase 4, p. 6; Sec. III.B, after Eq. (9), p. 4; Lemma G.1, p. 8
- **Requirement:** For a batch Π = {π_1, …, π_n}, proofs sharing the same ancestor A_s,k shall reuse the single certificate C_s,k, avoiding duplicate hashing along the shared ancestor-to-root path.
- **Acceptance:** For a batch of n leaves under g distinct level-s ancestors, the ancestor-to-root path is recomputed g times, not n times.
- **§0.6 status — EXPLICITLY PERMITTED:** ancestor-certificate reuse is an optimization that the paper *does* state, repeatedly and as its central contribution. It is therefore required, not prohibited. The same applies to R-MKT-07's "only affected paths are recomputed". §0.6 bars only optimizations the paper does **not** state.

**R-VER-08 — Verification cost bound**
- **Class:** PAPER-DERIVED
- **Trace:** Theorem G.1, Sec. IV.G, p. 8
- **Requirement:** Batch verification at level s shall require at most H(S, s) ≤ n(L − s) + gs hash recomputations, where g is the number of distinct level-s ancestors covering the batch.
- **Acceptance:** An instrumented hash counter over a batch of n leaves spanning g ancestors reports a count ≤ n(L − s) + gs.

**R-VER-09 — Audit does not decrypt**
- **Class:** PAPER-DERIVED
- **Trace:** Table 4, "Decrypt (Read)" column for "Ours": "— (audit does not decrypt)", p. 10; Sec. III.C Phase 2, p. 6; Sec. V.B, p. 10 ("our design audits ciphertext-only (no decryption)")
- **Requirement:** No verification or audit path shall perform AEAD decryption.
- **Acceptance:** No decrypt call occurs during any verification operation.

**R-VER-10 — Graceful degradation**
- **Class:** PAPER-DERIVED
- **Trace:** Sec. III.B, after Eq. (10), p. 5; Sec. V.C.5, p. 11
- **Requirement:** Under non-local or adversarial ingestion patterns that suppress proof reuse, and under conservative parameter choices (small s or small windows), the system shall degrade to classical Merkle-tree verification complexity while preserving correctness and soundness.
- **Acceptance:** Correctness results are identical for subtree-local and randomly scattered batches; only timing differs.

---

### 3.10 Phase 5(a) — Authorized auditor verification

**R-AUTH-01 — Auditor credential issuance** — **Class:** BLOCKED — USER PERMISSION REQUIRED
- **Paper-derived fragment:** "Each auditor A is issued a credential by the Certification Authority (CA)" (Sec. III.C Phase 5(a), p. 6).
- **Blocked by:** Q4

**R-AUTH-02 — Token issuance** — **Class:** BLOCKED — USER PERMISSION REQUIRED
- **Paper-derived fragment:** "For each audit request, CA issues a signed token: τ_A = Sign_skCA(auditor_id, epoch, policy)" (Sec. III.C Phase 5(a), p. 6).
- **Blocked by:** Q4, Q13

**R-AUTH-03 — Token verification at the DHT** — **Class:** BLOCKED — USER PERMISSION REQUIRED
- **Paper-derived fragment:** "Upon request, DHT verifies: Ver_pkCA(τ_A) = 1" (Sec. III.C Phase 5(a), p. 6).
- **Blocked by:** Q4, Q13

**R-AUTH-04 — Access gate**
- **Class:** PAPER-DERIVED for the rule / BLOCKED for enforcement detail
- **Trace:** Sec. III.C Phase 5(a), p. 6 ("Only valid tokens permit access to proof data")
- **Requirement:** Proof data shall be released from the DHT only on presentation of a valid token.
- **Acceptance:** A request with an absent, malformed, or invalid-signature token is denied proof data.
- **Blocked by:** Q13

**R-AUTH-05 — Token transport and per-request binding** — **Class:** BLOCKED — USER PERMISSION REQUIRED — **Blocked by:** Q13

---

### 3.11 Phase 5(b) — Real-time streaming verification

**R-RTV-01 — Micro-root computation**
- **Class:** PAPER-DERIVED for the formula / BLOCKED for window semantics
- **Trace:** Eq. (28), Sec. III.C Phase 5(b), p. 7
- **Requirement:** For each window, a micro-root shall be computed as R^(Δ)_τ = H(d_i1 ‖ d_i2 ‖ · · · ‖ d_ik), with **no** domain-separation tag (as written).
- **Blocked by:** Q21, Q22, Q8

**R-RTV-02 — Micro-root publication to the DHT** — **Class:** BLOCKED — USER PERMISSION REQUIRED
- **Paper-derived fragment:** "Publish R^(Δ)_τ to the DHT for near real-time validation" (Sec. III.C Phase 5(b) step 2, p. 7).
- **Note:** Eq. (6) defines DHT record structures for P, C, and R only — no micro-root record structure or key is defined anywhere.
- **Blocked by:** Q11, Q21, Q22

**R-RTV-03 — Epoch-end consolidation** — **Class:** BLOCKED — USER PERMISSION REQUIRED
- **Paper-derived fragment:** "At epoch end, consolidate: R^(e)_τ = H(R^(Δ1)_τ ‖ R^(Δ2)_τ ‖ · · ·)" (Eq. (29), Sec. III.C Phase 5(b), p. 7).
- **Blocked by:** Q16 — this equation directly contradicts R^(e)_τ = N_0,0.

**R-RTV-04 — Real-time responsiveness**
- **Class:** PAPER-DERIVED (claim) / BLOCKED (measurable criterion)
- **Trace:** Sec. III.C Phase 5(b), p. 7 ("This extension provides low-latency tamper detection while preserving end-of-epoch audit guarantees. Real-Time Responsiveness: Micro-roots enable near-instant log verification.")
- **Note:** The paper gives no numeric latency target for micro-root verification and no experiment measuring it. "Near-instant" is not testable as stated.
- **Blocked by:** Q22, Q34

---

### 3.12 Security properties to be demonstrated

These restate the paper's theorems as testable system properties. They are PAPER-DERIVED, but each test's concrete construction depends on the blocked primitives above.

**R-SEC-01 — Confidentiality** — PAPER-DERIVED — Theorem B.1, Sec. IV.B, p. 7. Canonicalized records are encrypted before any public processing.
**R-SEC-02 — Producer authenticity** — PAPER-DERIVED — Theorem C.1, Sec. IV.C, p. 7. Forging σ* on an unseen digest d* is negligible.
**R-SEC-03 — Ciphertext integrity** — PAPER-DERIVED — Theorem C.2, Sec. IV.C, p. 7. Modifying (c, t) for fixed (M, AD) without detection is negligible.
**R-SEC-04 — Record integrity** — PAPER-DERIVED — Definition D.1 and Theorem D.1, Sec. IV.D, p. 7. An adversary cannot produce (c′, t′, AD′, d′, σ′, π′) that passes both signature and inclusion checks for an uncommitted record.
**R-SEC-05 — Append-only freshness / no rollback or reorder** — PAPER-DERIVED — Theorem E.1, Sec. IV.E, pp. 7–8. Once R^(e)_agg is finalized on-chain, replacing, removing, or reordering any committed leaf up to epoch e is detected.
**R-SEC-06 — Inclusion proof soundness** — PAPER-DERIVED — Theorem F.1, Sec. IV.F, p. 8. No accepted inclusion proof for a non-existent record.
**R-SEC-07 — Batch-group soundness** — PAPER-DERIVED — Lemma G.1, Sec. IV.G, p. 8. Aggregating multiple leaves under a shared certificate is sound.

---

### 3.13 Environment and evaluation

> **Section note — USER-APPROVED deviation.** Table 5 (p. 10) specifies a Python toolchain. Per §0.7 the implementation language is **Go**, by explicit user override of the paper. Table 5's original values are preserved verbatim in every requirement below so the deviation stays auditable. The language approval extends to **nothing else**: every displaced Python dependency is now BLOCKED under Q35–Q39, and no Go substitute — including the Go standard library — has been selected.

**R-ENV-01 — Programming language**
- **Class:** **USER-APPROVED** (deviation from PAPER-DERIVED)
- **Paper value (superseded):** Table 5, p. 10 — "Programming Language: **Python 3.10**".
- **Authority for deviation:** explicit user instruction, §0.7.
- **Requirement:** The reproduction shall be implemented in **Go (golang)**.
- **Blocked by (toolchain version, module path, CGO):** Q35

**R-ENV-02 — Crypto library**
- **Class:** BLOCKED — USER PERMISSION REQUIRED *(was PAPER-DERIVED for the library; invalidated by §0.7)*
- **Paper value (unusable in Go):** Table 5, p. 10 — "Crypto Library: **pycryptodome** (SHA-256, HMAC) [30]". `pycryptodome` is a Python package and cannot be used from Go.
- **Unaffected by the language change:** the **SHA-256 algorithm** itself remains PAPER-DERIVED (see R-CRY-01). Only its provider is open.
- **Blocked by:** Q36 (Go provider), Q1 (no AEAD named), Q3/Q4 (no signature scheme named)

**R-ENV-03 — Data-structure libraries** — **Class:** BLOCKED — USER PERMISSION REQUIRED
- **Paper value (unusable in Go):** Table 5, p. 10 — "Data Structures: **merkletools (modified), bitarray**". Both are Python-only; the modification to `merkletools` was never documented by the paper.
- **Blocked by:** Q37 *(supersedes Q33)*

**R-ENV-04 — Benchmarking tools**
- **Class:** PAPER-DERIVED for the methodology / BLOCKED for the tooling
- **Paper value (tooling unusable in Go):** Table 5, p. 10 — "Benchmarking Tools: **`time.perf_counter`, NumPy stats**". Both are Python-only.
- **Requirement (methodology, unaffected):** Each experiment shall be repeated 50 times; results shall be reported as means with 95% confidence intervals (Sec. V.C, p. 9).
- **Blocked by:** Q39

**R-ENV-05 — Blockchain simulator**
- **Class:** PAPER-DERIVED for the toolchain / BLOCKED for the Go client mechanism
- **Trace:** Table 5, p. 10 ("Blockchain Simulator: Hardhat v2.20 (EVM); Solidity 0.8.20")
- **Requirement:** Anchoring shall target an EVM chain simulated with Hardhat v2.20, contract compiled with Solidity 0.8.20. **This survives the language change** — Hardhat (Node.js) and Solidity are a separate toolchain from the implementation language, not Python dependencies, so §0.7 does not displace them.
- **Blocked by:** Q38 — how the Go application communicates with that chain is unspecified by the paper in any language.

**R-ENV-07 — Repository and package layout** — **Class:** BLOCKED — USER PERMISSION REQUIRED — **Blocked by:** Q35

**R-ENV-06 — Reference hardware**
- **Class:** PAPER-DERIVED (recorded for context)
- **Trace:** Sec. V.C, p. 9 ("AMD Ryzen 5 5600X CPU, NVIDIA RTX 3060 Ti GPU, and 16 GB RAM, running Windows 10 (64-bit)")
- **Note:** No operation in Sec. III uses a GPU; the paper does not state what the listed GPU contributes. No GPU acceleration shall be implemented.

**R-EVAL-01 — Log record size**
- **Class:** PAPER-DERIVED
- **Trace:** Table 5, p. 10 ("Log Size: 256–512 bytes per record")
- **Blocked by (record content/schema):** Q29

**R-EVAL-02 — Dataset and epoch sizes**
- **Class:** PAPER-DERIVED for the sizes / BLOCKED for the data
- **Trace:** Table 5, p. 10 ("Dataset Sizes: 10^3, 10^4, 10^5 logs"; "Epoch Sizes: 1,000 (default), 10,000, 100,000 logs"; "Input Distributions: Uniform, Zipf-heavy, Bursty"; "Repetitions: 50 independent runs per experiment")
- **Requirement:** Experiments shall use the listed dataset sizes, epoch sizes, distributions, and 50 repetitions; results shall be reported as means with 95% confidence intervals (Sec. V.C, p. 9).
- **Blocked by:** Q29 (distribution parameters and dataset definition)

**R-EVAL-03 — Single-record verification latency**
- **Class:** PAPER-DERIVED (reference value) / BLOCKED (pass criterion)
- **Trace:** Sec. V.C.1 and Fig. 3, p. 9 — proposed scheme 0.000035 s; Scheme [12] 0.0016 s; Scheme [8] 0.001984 s; Scheme [26] 0.015607 s; Scheme [16] 0.2720 s; stated gains ≈45×, ≈56×, >7800×.
- **Blocked by:** Q34, Q10, Q31

**R-EVAL-04 — Verification throughput**
- **Class:** PAPER-DERIVED (reference value) / BLOCKED (pass criterion)
- **Trace:** Sec. V.C.2 and Fig. 4, p. 10 — ours 34,482.76 logs/s; [16] 21,052.63; [12] 6,410.25; [8] 1,579.07; [26] 64.07; gains ≈1.6×, 5.4×, 21.8×, >500×.
- **Blocked by:** Q34, Q30, Q31

**R-EVAL-05 — Commit time**
- **Class:** PAPER-DERIVED (reference value) / BLOCKED (pass criterion)
- **Trace:** Sec. V.C.3 and Fig. 5, p. 10 — ours 0.0053 s; [12] 0.0700 s; [26] 0.0906 s; [8] 0.3530 s; [16] 1.2800 s. Commit time = time to construct the ADS for a new batch **and** anchor the integrity state on-chain.
- **Blocked by:** Q34, Q30, Q23, Q31

**R-EVAL-06 — Input-distribution and stride/window experiments**
- **Class:** PAPER-DERIVED for the design / BLOCKED for parameters
- **Trace:** Sec. V.C.4 and Fig. 6, p. 11 (subtree-local vs. randomly scattered batches, batch sizes 2^6 … 2^12); Sec. V.C.5 and Fig. 7, p. 11 (n = 2^10 fixed, s ∈ {1, 2, 4, 8}, windows W = 256 / 512 / 1024 / 2048, baseline window 1,024)
- **Expected qualitative outcomes (paper-derived):** subtree-local verification time grows slowly, following O(n + log N); scattered batches grow more rapidly, following O(n(L − s) + gs); verification time stabilizes beyond a moderate window (~1,024 logs).
- **Blocked by:** Q21, Q22, Q25

**R-EVAL-07 — Constant on-chain storage measurement** — **Class:** BLOCKED — USER PERMISSION REQUIRED — **Blocked by:** Q14, Q15, Q34

**R-EVAL-08 — Deterministic reproducibility**
- **Class:** LOGICALLY NECESSARY
- **Basis:** The paper's entire integrity argument (Theorems D.1, E.1, F.1) requires that the same input sequence yields the same roots; without determinism no verification can succeed.
- **Requirement:** Given the same input records in the same order and the same parameters, the system shall produce identical leaf digests, internal nodes, ancestor certificates, per-type roots, and root-of-roots.
- **Blocked by (the byte-level rules that make this achievable):** Q6, Q7, Q8, and — for record/DHT serialization — Q12
- **Go-specific hazard (§0.7):** Go map iteration order is randomized by design. Any serialization or hashing path that iterates a map (AD fields, type list for R_agg per Q26, certificate fields) will produce non-deterministic bytes and silently break every root in the system. Deterministic ordering is required; the ordering rules themselves remain blocked by Q7, Q12, and Q26.

**R-EVAL-09 — Functional comparison table**
- **Class:** PAPER-DERIVED
- **Trace:** Table 2, p. 9
- **Requirement:** The reproduction's feature set shall match the "This work" row: real-time/windowed verify ✓ (micro-roots); log-specific ✓; trust model "Blockchain anchor + CA-issued tokens"; ADS "MK-Tree + ancestor certs + DHT"; proof reuse ✓ (ancestor reuse); on-chain "Root-of-roots per epoch".

**R-EVAL-10 — Baseline re-implementation** — **Class:** BLOCKED — USER PERMISSION REQUIRED — **Blocked by:** Q31
**R-EVAL-11 — Comparative figures (Figs. 3–5)** — **Class:** BLOCKED — USER PERMISSION REQUIRED — **Blocked by:** Q31, Q34

---

### 3.14 Non-functional

**R-NFR-01 — Verification latency** — **Class:** BLOCKED — USER PERMISSION REQUIRED — **Blocked by:** Q34, Q10, Q32
**R-NFR-02 — Throughput** — **Class:** BLOCKED — USER PERMISSION REQUIRED — **Blocked by:** Q34, Q10, Q32
**R-NFR-03 — Deployment topology / containerization** — **Class:** BLOCKED — USER PERMISSION REQUIRED — **Blocked by:** Q28
**R-NFR-04 — Concurrency, retries, timeouts** — **Class:** BLOCKED — USER PERMISSION REQUIRED — **Blocked by:** Q32
- **Standing prohibition pending that answer (§0.6):** the paper describes no concurrency, no retry policy, and no timeout anywhere. Until explicitly approved, the implementation shall be single-goroutine and shall use no Go concurrency primitives.

**R-NFR-06 — No unstated optimization**
- **Class:** USER-APPROVED (constraint, per §0.6)
- **Authority:** explicit user instruction — *"if there can be optimization I would like you to not do it if the paper have not stated"*.
- **Requirement:** The implementation shall contain no optimization that the paper does not explicitly specify, per the full text of §0.6. Where the paper's stated procedure is less efficient than a known alternative, the paper's procedure shall be implemented as stated.
- **Acceptance (Developer):** every deviation from a literal reading of the paper's stated procedure is either traceable to a paper statement or absent.
- **Acceptance (Tester):** the absence of an unstated optimization is never recorded as a defect; a performance shortfall is recorded as a measured result against R-EVAL-03/04/05, never as a trigger to add optimization.

**R-NFR-05 — Asymptotic verification complexity**
- **Class:** PAPER-DERIVED
- **Trace:** Eq. (10), Sec. III.B, p. 5; Corollary G.1, Sec. IV.G, p. 8; Sec. I contributions, p. 2
- **Requirement:** Batch verification shall exhibit T(n) = O(n) + O(log n) = O(n + log n) in the amortized sense under workloads with sufficient subtree locality or window-level aggregation, per Corollary G.1's conditions h = O(1) and g = O(1). Under worst-case non-local or adversarial ingestion the system shall degrade to classical Merkle-tree verification complexity (see R-VER-10).

---

## 4. TRACEABILITY INDEX

| Paper element | Page | Requirements |
|---|---|---|
| Sec. III.A entities 1–7, Fig. 1 | 3–4 | R-SYS-01 … R-SYS-07 |
| Eq. (1) sequence L^(e)_τ | 4 | R-MKT-05, R-MKT-06 |
| Eq. (2) leaf digest | 4 | R-MKT-01, R-ATT-01 |
| Padding rule | 4 | R-MKT-02 |
| Eq. (3) internal node | 4 | R-MKT-03 |
| Eq. (4) ancestor checkpoints | 4 | R-ANC-01 |
| Eq. (5) ancestor certificate | 4 | R-ANC-02, R-ANC-03 |
| Eq. (6) DHT records | 4 | R-DHT-04, R-DHT-05 |
| Eq. (7) root-of-roots | 4 | R-EPO-01 |
| Eq. (8) BC.commit(·, e, timestamp) | 4 | R-EPO-03, Q15 |
| Eq. (9) inclusion proof (siblings) | 4 | R-VER-02, Q18 |
| Eq. (10) complexity | 5 | R-NFR-05 |
| Eq. (11)/(12) interfaces | 5 | R-VER-02 |
| Fig. 2 hierarchy + commit sequencing | 5 | R-MKT-06, Q23 |
| Eq. (13) Canon | 5 | R-CAN-01 |
| Eq. (14) AEAD | 5 | R-ENC-02, R-ENC-03 |
| Eq. (15) leaf digest | 5 | R-ATT-01 |
| Table 1 notation | 5 | all |
| Eq. (16) attestation | 5 | R-ATT-02 |
| Eq. (17) emitted tuple | 6 | R-ATT-03 |
| Eq. (18)/(19) ingestion checks | 6 | R-ING-02, R-ING-03 |
| Eq. (20)/(21) leaf + internal | 6 | R-MKT-01, R-MKT-03 |
| Eq. (22) parent node record | 6 | R-DHT-04 |
| Eq. (23) ancestor certificate | 6 | R-ANC-02, Q17 |
| Eq. (24)/(25) aggregate + commit | 6 | R-EPO-01, R-EPO-03 |
| Eq. (26) proof structure (P records) | 6 | R-VER-02, Q18 |
| Eq. (27) batch | 6 | R-VER-07 |
| Phase 5(a) token | 6 | R-AUTH-01 … R-AUTH-05 |
| Eq. (28) micro-root | 7 | R-RTV-01 |
| Eq. (29) epoch consolidation | 7 | R-RTV-03, Q16 |
| Eq. (30) root-of-roots | 7 | R-EPO-01 |
| Sec. IV.A assumptions | 7 | R-CRY-03, R-DHT-07 |
| Theorems B.1–G.1, Lemma G.1, Def. D.1 | 7–8 | R-SEC-01 … R-SEC-07, R-VER-08 |
| Table 2 functional comparison | 9 | R-EVAL-09 |
| Table 3 symbols | 9 | R-EPO-05, R-EVAL-05 |
| Table 4 costs | 10 | R-VER-08, R-VER-09 |
| Table 5 environment | 10 | R-ENV-01 … R-ENV-05, R-EVAL-01, R-EVAL-02 — **language overridden to Go by user, §0.7; Python toolchain displaced, Q35–Q39** |
| Sec. V.C.1–V.C.5, Figs. 3–7 | 9–11 | R-EVAL-03 … R-EVAL-06 |
| Sec. VI future work | 11–12 | §2.2 out of scope |

---

## 5. SUMMARY OF READINESS

- **PAPER-DERIVED (fully or partly):** 44 requirements
- **LOGICALLY NECESSARY:** 3 requirements (R-MKT-06, R-VER-06, R-EVAL-08)
- **USER-APPROVED:** 0
- **BLOCKED — USER PERMISSION REQUIRED:** 45 requirements, gated on Q1–Q34

**Verdict: NOT READY — USER PERMISSION REQUIRED.**

No implementation may begin on any blocked requirement. Requirements marked PAPER-DERIVED or LOGICALLY NECESSARY that are additionally annotated "Blocked by: Qn" are implementable only in the part the paper fixes; the annotated portion must wait.

**Status:** draft
