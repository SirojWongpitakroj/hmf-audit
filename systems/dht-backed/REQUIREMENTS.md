# Software Requirements Specification (SRS)

## DHT-Backed Ancestor-Assisted Merkle Verification for Scalable and Real-Time Log Integrity in Cloud Data Lakes

**Reproduction target (sole authoritative source):**
Saengthong, V., Mantharngkul, T., Wongsuwan, C., Ritthaisong, K., Fugkeaw, S.,
"DHT-Backed Ancestor-Assisted Merkle Verification for Scalable and Real-Time Log Integrity in Cloud Data Lakes,"
IEEE Open Journal of the Computer Society, DOI 10.1109/OJCS.2026.3663463 (accepted author version, 12 pages).

Local copy: `paper/DHT-Backed_Ancestor-Assisted_Merkle_Verification_f.pdf` (the paper is not included in this repository)

**Document status:** **READY FOR DEV**
**Overall verdict:** **READY**
**Answered:** Q1–Q54 (Q33 superseded by Q37) — **53 decisions**, all USER-APPROVED. Rounds 1–2 (Q1–Q49) adopted by explicit user instruction on 2026-09-10; Round 3 (Q50–Q54) answered interactively by the user on 2026-09-11. See §0.8 for provenance, §1 / §1A / §1B for the decisions.
**Superseded:** Q45 revises the Q11 DHT key rule — `seq` is no longer part of any lookup key. See **§0.9**.
**Open questions:** **none.** No requirement is BLOCKED.
**Implementation language:** **Go (golang)** — USER-APPROVED deviation from Table 5's Python 3.10. See §0.7.
**Global constraint:** no unstated optimization may be implemented. See §0.6.
**Pass/fail line:** functional correctness is pass/fail; performance figures are informational. See §0.4.1.

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

### 0.4.1 Tester constraint — amendment adopted with Q34 (USER-APPROVED, 2026-09-10)

Per the Q34 decision, the pass/fail line is drawn as follows:

- **Functional correctness is pass/fail.** Valid proofs must accept; tampered records, forged proofs, corrupted DHT records, unregistered producers, and invalid or expired tokens must reject.
- **Performance figures are reported, not asserted.** The paper's 0.000035 s single-record latency, 34,482.76 logs/s throughput, and 0.0053 s commit time are **informational reference values**, measured on a Python implementation on different hardware. The Tester records the reproduction's own mean and 95% confidence interval and must **not** mark a test failed for missing any of those numbers.
- **The Tester must not require an unstated optimization** to reach any figure (§0.6), and must not treat the absence of one as a defect.
- **Baseline comparisons are not testable claims** (Q31): Schemes [8], [12], [16], [26] are not implemented, and their published values are recorded as unreproduced author-reported figures.
- **Two paper statements are deliberately not reproduced and must not be tested as expected behaviour:** Sec. V.C.5's causal explanation of the stride trend (see Q25 — test Theorem G.1's formula instead), and Fig. 2's `Prev Hash` field (see Q23).
- **Provisional mid-epoch results are not anchored verifications (Q52).** A micro-root check against an open epoch must **not** be recorded as satisfying R-SEC-04, R-SEC-05 or R-SEC-06, all of which terminate at the blockchain anchor. Full Phase 4 inclusion/batch verification against an **open** epoch must be refused — testing that refusal is itself a required test.
- **One micro-root per epoch at default settings is expected (Q54).** With epoch 1,000 and W = 1,024 there is exactly one window, `windowID = 0`, which never fills, and its provisional micro-root is republished on every insert. This is the specified outcome and must not be reported as a defect. Multi-window behaviour is exercised at W = 256 / 512 and at epoch sizes 10⁴ / 10⁵.

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

**NAMED EXCEPTION 1 — one database constraint, and only this one (USER-APPROVED via Q43).**

> §0.6 otherwise forbids indexes as unstated optimizations. Exactly one is carved out:
>
> **`UNIQUE (epoch, log_type, leaf_index)`** on the PostgreSQL record table — **and nothing else.**
>
> This is a **named, bounded exception**. It is **not** general license to add indexes. No other secondary index, covering index, partial index, materialized view, or query-plan hint may be added unless separately and explicitly approved. In particular, the `(epoch, log_type)` index considered under Q43 was **not** approved and must not be created.
>
> **Its primary purpose is correctness, not speed:** it is the enforcement mechanism for Q44's leaf-position invariant — that a leaf index is unique within a `(epoch, log_type)` tree and never reassigned. Its retrieval benefit is incidental.
>
> **Tester:** this constraint is **expected**. Its absence is a defect. A duplicate `(epoch, log_type, leaf_index)` insert must be rejected by the database.

**Boundary case — caching.** The paper's own verification procedure refers to "cached P_j,k" (Sec. III.B, p. 4, twice). Caching is therefore *partly paper-mentioned but wholly undefined*: the paper names a cache but specifies no location, scope, eviction policy, size, or lifetime. This rule does **not** authorize implementing a cache. The permissible scope of that one paper-mentioned cache remains **BLOCKED under Q32**. All caching not covered by a resolved Q32 answer falls under the prohibition above.

### 0.7 Implementation language — USER-APPROVED DEVIATION FROM THE PAPER

> **The implementation language is Go (golang).** Classification: **USER-APPROVED**.
> Authority: explicit user instruction — *"before that I would like you to insert that this should be done in golang. Moreover if there can be optimization I would like you to not do it if the paper have not stated"*.

**This deviates from the paper and does so deliberately.** Table 5 (p. 10) specifies **Python 3.10**. Under the Conflict Rule the paper normally wins; that rule's sole exception — "unless the user explicitly asks you to do so" — has been exercised by the user. The original Table 5 values are preserved throughout this document so the deviation remains visible and auditable.

**Scope of this approval is exactly one item: the implementation language.** It does not authorize any other decision. In particular it does **not** by inference approve any Go library, package, module, toolchain version, standard-library facility, serialization mechanism, or concurrency use. Every such consequence that the paper cannot supply is raised as a new numbered question (Q35–Q39) and remains BLOCKED.

**Known consequence:** Table 5's toolchain is Python-specific and is now partly unusable — `pycryptodome`, `merkletools (modified)`, `bitarray`, `time.perf_counter`, and NumPy have no automatic Go counterparts. Substitutes are recorded under Q35–Q39.

### 0.8 Provenance of the adopted decisions (Q1–Q39)

The decisions recorded beneath questions Q1–Q39 were **drafted as assistant recommendations** in `DHT_Open_Questions_Answered.md`, whose own header describes them as "assistant-recommended implementation decisions ... still pending explicit user approval."

They were **adopted by explicit user instruction on 2026-09-10**: *"The answer to your question is in @DHT_Open_Questions_Answered.md Call the requirements-analyst agent to modify @REQUIREMENTS.md"*.

That instruction is the approval. Each such decision is therefore classified **USER-APPROVED** on that basis — **not** PAPER-DERIVED, because the paper specifies none of them.

This section records where the decisions came from. It is **not** a delegation of authority: it does not authorize any agent to answer future questions, and it creates no new classification category. The four categories in §0.2 stand unchanged; "delegated" is not a category. Any gap discovered from here on is raised as a new numbered question and returns to the user.

**Round 2 (Q40–Q49) — same provenance.** The decisions recorded beneath questions Q40–Q49 were **drafted as assistant proposals** in `DHT_Open_Questions_Round2_Answered.md`, whose header states "10 assistant-proposed answers inserted. Review/approve them before reclassifying." They were **adopted by explicit user instruction on 2026-09-10**: *"The answer to your question is in @DHT_Open_Questions_Round2_Answered.md Call the requirements-analyst agent to modify @REQUIREMENTS.md"*. That instruction is the approval, and all ten are classified **USER-APPROVED** on that basis. Everything said above about non-delegation applies identically to Round 2.

**One Round 2 decision revises a Round 1 decision.** Q45 supersedes the DHT key-derivation rule adopted under Q11. The superseded rule is struck through in the Q11 record and must not be implemented. See §0.9.

**Round 3 (Q50–Q54) — DIFFERENT PROVENANCE, recorded distinctly.** The decisions beneath Q50–Q54 were **answered interactively by the user on 2026-09-11**, via direct multiple-choice prompts in session. There was **no intermediate answers file and no assistant drafting** — these are the user's own selections, not adopted recommendations. They are classified **USER-APPROVED** on that basis, dated **2026-09-11**.

This distinction is preserved deliberately: Rounds 1 and 2 were assistant-drafted proposals subsequently adopted by user instruction; Round 3 was answered by the user directly. Both are valid approvals under §0.2, but the audit trail should not conflate them.

### 0.9 SUPERSESSION NOTICE — DHT key derivation (Q11 → Q45)

> **The Round 1 Q11 key rule is SUPERSEDED and must not be implemented.**
>
> **Superseded (Q11, Round 1) — DO NOT USE:**
> `Key_P = H("P" ‖ treeID ‖ j ‖ k ‖ seq)` · `Key_C = H("C" ‖ treeID ‖ s ‖ k ‖ seq)` · `Key_R = H("R" ‖ treeID ‖ seq)`
>
> **In force (Q45, Round 2) — USE THIS:**
> ```
> Key_P = H("P" ‖ treeID ‖ j ‖ k)
> Key_C = H("C" ‖ treeID ‖ s ‖ k)
> Key_R = H("R" ‖ treeID)
> Key_M = H("M" ‖ treeID ‖ windowID)      // unchanged from Q11
> ```
> `treeID = H("TREE" ‖ epoch ‖ logType)` and `ancestorID = H("ANC" ‖ treeID ‖ s ‖ k)` are **unchanged**. All numeric values remain unsigned 64-bit big-endian before hashing.
>
> **`seq` is no longer part of any lookup key.** It remains version metadata *inside* `P`, `C`, `R`, and micro-root records. A newer write to the same logical object **overwrites the previous value at the same key**.

This notice governs over any older text elsewhere in this document.

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
- **Originally blocked requirement IDs:** R-ENC-02, R-ENC-03, R-ENC-04, R-ENC-05, R-EVAL-09

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** **AES-256-GCM.** 256-bit key; 12-byte nonce from `crypto/rand`; standard 16-byte GCM authentication tag. To preserve the paper's conceptual tuple `(c, t)`, encode **`c = nonce ‖ ciphertext`** and keep `t` as the 16-byte tag. The leaf digest remains `d = H(0x00 ‖ c ‖ t ‖ AD)`.
- **Fidelity note:** the paper's `c` denotes ciphertext alone (Eq. (14)); under this decision `c` additionally carries a 12-byte nonce prefix. This is a recorded, deliberate deviation adopted to give the nonce a home, since the paper's tuple has no nonce field. It changes every leaf digest relative to a nonce-less framing and must be implemented exactly as stated.
- **Resolves:** R-ENC-02 (algorithm), R-ENC-04 (nonce), R-ENC-05 (framing) → USER-APPROVED.

---

### Q2 — [GAP] AEAD key K: generation, storage, scope, and distribution

- **What the paper says:** "The canonicalized record is encrypted with symmetric key K" (Sec. III.C Phase 1, Eq. (14), p. 5). Table 1 (p. 5): "K — Symmetric secret key used in AEAD encryption."
- **What the paper does not specify:** How K is generated; its length; where it is stored; whether there is one K system-wide, one per producer, one per log type τ, or one per epoch e; how it reaches the Encryptor; whether it is ever rotated. The paper also never states who holds K (auditors explicitly do not decrypt — Table 4, "audit does not decrypt", p. 10).
- **Why a decision is required:** Key scope determines the encryption module's interface and the system's data model. Key length is an input to the AEAD instantiation. Neither can be inferred.
- **Known candidate choices (none authorized):** single global key; per-producer key; per-log-type key; per-epoch key. Key length: 128-bit / 192-bit / 256-bit. Storage: in-memory only; key file; environment variable; KMS.
- **Originally blocked requirement IDs:** R-ENC-06, R-ENC-07, R-KEY-01

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** **One 256-bit system-wide AEAD key.** Generated once with `crypto/rand.Reader`; loaded from configuration or an environment variable at startup; never hard-coded in the repository. **No key rotation** is implemented in the reproduction. Fixed test keys may be used only in unit tests, for deterministic vectors.
- **Resolves:** R-ENC-06 (scope: global), R-ENC-07 (length: 256-bit), R-KEY-01 → USER-APPROVED.
- **~~Residual gap~~ — RESOLVED BY Q46:** the key is supplied via **`DHT_AEAD_KEY_HEX`** (exactly 32 bytes as 64 hex characters); the program fails fast on an invalid length and must never silently generate a replacement.

---

### Q3 — [GAP] Producer signature algorithm, key generation, and key sizes

- **What the paper says:** "σ ← Sign_skP(H(0xA3 ‖ d))" (Sec. III.C Phase 1, Eq. (16), p. 5). Table 1 (p. 5): "pk_P, sk_P — Public/Private key pair of the producer." Verification: "Ver_pkP(σ, H(0xA3 ‖ d')) =? 1" (Eq. (19), p. 6). Security assumption (ii): "producer and CA signatures are EUF-CMA secure" (Sec. IV.A, p. 7). Table 4 lists `Sig_S` in our scheme's ingestion column (p. 10).
- **What the paper does not specify:** The signature algorithm, curve or modulus, key size, key generation procedure, key storage, or signature encoding. Table 5 (p. 10) lists no signature library.
- **Why a decision is required:** Producer attestation is a mandatory ingestion gate (Eq. (19): "Only verified tuples are accepted; failures are discarded"). It cannot be implemented or tested without a concrete scheme.
- **Known candidate choices (none authorized):** RSA-PSS; RSA-PKCS#1 v1.5; ECDSA (P-256 / P-384 / secp256k1); Ed25519; Ed448.
- **Originally blocked requirement IDs:** R-ATT-02, R-ATT-03, R-ING-02, R-KEY-02

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** **Ed25519**, one key pair per producer. Compute `m_P = SHA256(0xA3 ‖ d)` and then `σ = Ed25519.Sign(sk_P, m_P)`. Raw **32-byte** public keys and **64-byte** signatures. Producer key pairs generated once with secure randomness.
- **Fidelity note:** this matches Eq. (16) literally — the signature is over `H(0xA3 ‖ d)`, not over `d`. Ed25519's own internal hashing is additional to, not a substitute for, that pre-hash.
- **Resolves:** R-ATT-02, R-ATT-03, R-ING-02, R-KEY-02 (algorithm and encoding) → USER-APPROVED.

---

### Q4 — [GAP] CA signature algorithm, CA implementation, and credential/certificate format

- **What the paper says:** "Each auditor A is issued a credential by the Certification Authority (CA)" and "τ_A = Sign_skCA(auditor_id, epoch, policy)"; "DHT verifies: Ver_pkCA(τ_A) = 1" (Sec. III.C Phase 5(a), p. 6). Table 1 (p. 5): "τ_A — Authorization token issued by CA." Table 4 lists setup for our scheme as "CA + producers + DHT/epoch" (p. 10).
- **What the paper does not specify:** The CA signature algorithm and key size; how the CA is implemented (service, library, offline script); the format of the auditor "credential" issued in step 1 and whether it is distinct from the token τ_A in step 2; how pk_CA is distributed to the DHT; any certificate format.
- **Why a decision is required:** Auditor authorization is a required system function (Phase 5(a)); the DHT must verify the token before releasing proof material.
- **Known candidate choices (none authorized):** same algorithm as the producer signature or a different one; X.509 certificates; raw public keys; JWT-style tokens; custom binary credential.
- **Originally blocked requirement IDs:** R-AUTH-01, R-AUTH-02, R-AUTH-03, R-AUTH-05, R-KEY-03, R-ANC-05

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** **Ed25519 for the CA** as well. The CA is an **in-process module** for this reproduction. The auditor **enrollment credential is kept distinct** from the per-epoch authorization token `τ_A`. The CA public key is **configured into the DHT verifier before execution**. Encoding is **deterministic binary**, not X.509 and not JWT.
- **Ancestor certificates are NOT signed (resolves R-ANC-05):** no digital-signature field is added to `C_s,k`. "Ancestor certificate" is treated as the paper's name for an authenticated verification record, not an X.509-style signed certificate. Its authenticity rests entirely on recomputation against the on-chain anchor, consistent with R-DHT-07 (the DHT is adversary-controlled).
- **Resolves:** R-AUTH-01, R-AUTH-02, R-AUTH-03, R-AUTH-05, R-KEY-03, R-ANC-05 → USER-APPROVED.

---

### Q5 — [GAP] Trust binding for producer public keys

- **What the paper says:** The emitted tuple is "(c, t, AD, d, σ, pk_P)" (Eq. (17), p. 6) — the producer's public key travels with the record. Verification uses pk_P from that tuple (Eq. (19), p. 6).
- **What the paper does not specify:** How the verifier establishes that pk_P legitimately belongs to a trusted producer. As written, an adversary who controls the ingestion path could substitute its own (d, σ, pk_P). The paper's Theorem C.1 (p. 7) assumes a fixed pk_P but the construction transmits it in-band. The paper does not state that the CA certifies producer keys.
- **Why a decision is required:** Whether a producer key registry / allow-list exists changes the ingestion component's data model and its failure behavior, and determines whether Theorem C.1 is testable at all.
- **Known candidate choices (none authorized):** trust pk_P as transmitted, exactly as Eq. (17)/(19) are written; maintain a pre-registered producer public-key registry; have the CA issue producer certificates.
- **Originally blocked requirement IDs:** R-ING-03, R-KEY-02

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** Maintain a **pre-registered producer public-key registry**, `ProducerID → pk_P`. On ingestion, verify that the public key carried with the record **equals the registered key** before checking the producer signature. An arbitrary in-band public key is **not** accepted as sufficient identity binding.
- **Consequence for testing:** Theorem C.1 (Sec. IV.C, p. 7) now *is* testable as an identity-binding claim — a record bearing a valid self-consistent signature under an unregistered key must be rejected at ingestion.
- **Resolves:** R-ING-03, R-KEY-02 (trust binding) → USER-APPROVED.
- **~~Residual gap~~ — RESOLVED BY Q47/Q46:** five fixed producers (`producer-access`, `producer-error`, `producer-auth`, `producer-api`, `producer-security`), each with one persistent Ed25519 key pair, loaded into the registry from `DHT_PRODUCER_REGISTRY_PATH` before ingestion.

---

### Q6 — [GAP] Concrete canonicalization function Canon(·) and raw log input format

- **What the paper says:** "M ← Canon(M_raw)" where "Canon(·) normalizes field order, encoding, and structure to ensure consistent representation across heterogeneous log sources" (Sec. III.C Phase 1, Eq. (13), p. 5). "Canonicalizes the structure for uniformity across sources" (Sec. III.A item 2a, p. 3). Table 1 (p. 5): "M_raw — Raw log record emitted by producer before preprocessing; M — Canonicalized log record after normalization."
- **What the paper does not specify:** The input log format (JSON, syslog, CEF, key-value, CSV, ...); the canonical output format; the field-ordering rule; the character encoding; number/timestamp normalization; whitespace handling; the field set of a log record.
- **Why a decision is required:** Canon(·) determines M, which determines c and t, which determine every digest in the system. Two implementations with different Canon(·) produce different roots for the same input. Reproduction and testing are impossible without it.
- **Known candidate choices (none authorized):** JSON Canonicalization Scheme (RFC 8785); sorted-key JSON with UTF-8; CBOR canonical form; a fixed-field delimited encoding.
- **Originally blocked requirement IDs:** R-CAN-01, R-CAN-02, R-CAN-03, R-EVAL-08

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** **JSON input with RFC 8785-style canonical JSON semantics.** Fixed logical record schema: **`record_id`, `producer_id`, `log_type`, `timestamp`, `message`, `attributes`.** Timestamps normalized to UTC; UTF-8 encoding; insignificant whitespace removed; deterministic object-key ordering. Synthetic evaluation logs must use this same schema.
- **Resolves:** R-CAN-01, R-CAN-02, R-CAN-03 → USER-APPROVED. R-EVAL-08 determinism is supported by this plus Q7, Q8, Q12.
- **~~Residual gap~~ — PARTLY RESOLVED BY Q48:** `attributes` is a fixed flat object with keys `host, source_ip, status, request_id, padding`. **Still open:** the canonical string form of the `timestamp` field (**Q51**) and `record_id` generation (**Q50**), both of which sit inside `M`.

---

### Q7 — [GAP] AD field set and byte serialization (including timestamp serialization)

- **What the paper says:** "AD = (log_type, timestamp, . . .) the associated metadata bound to the ciphertext (but not encrypted) for tamper-evident indexing" (Sec. III.C Phase 1, Eq. (14), p. 5). "AD_i (associated data) binds metadata such as the record type and timestamps" (Sec. III.B, after Eq. (1), p. 4). Table 1 (p. 5): "AD — Associated metadata bound to ciphertext."
- **What the paper does not specify:** The complete AD field list (the paper writes an open-ended "…"); the byte serialization of AD; the timestamp representation (epoch seconds, epoch milliseconds, ISO-8601, integer width, endianness, timezone); the encoding of log_type.
- **Why a decision is required:** AD is an input to both the AEAD call (Eq. (14)) and the leaf digest d = H(0x00 ‖ c ‖ t ‖ AD) (Eq. (15)). Its exact byte form is load-bearing for every hash in the system.
- **Known candidate choices (none authorized):** fixed field list vs. extensible; JSON; length-prefixed binary; big-endian 64-bit integer timestamps; RFC 3339 strings.
- **Originally blocked requirement IDs:** R-ENC-03, R-MKT-01, R-EVAL-08

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** **`AD = (producerID, logType, timestamp)`**, serialized in exactly that order using deterministic binary encoding: length-prefixed UTF-8 `producerID`, length-prefixed UTF-8 `logType`, then a **big-endian 64-bit Unix timestamp in nanoseconds**. This identical byte representation is used **both** as AEAD associated data (Eq. (14)) **and** inside the leaf digest (Eq. (15)).

**Integrity amendment (2026-10-31):** The implementation appends a length-prefixed `recordID` to AD, so a requested data-lake key is bound to the anchored leaf. This extends the paper's open-ended metadata tuple and supersedes the three-field byte layout above. Previously stored tuples require reingestion.
- **Consistency check (passed):** all three AD fields are drawn from the Q6 canonical record schema (`producer_id`, `log_type`, `timestamp`). The paper's open-ended "AD = (log_type, timestamp, …)" is closed to exactly these three fields.
- **Resolves:** R-ENC-03, R-MKT-01 (AD serialization) → USER-APPROVED.

---

### Q8 — [GAP] Byte-level semantics of the concatenation operator "‖" in all hash inputs

- **What the paper says:** Uses "‖" throughout: d = H(0x00 ‖ c ‖ t ‖ AD) (Eq. (15)/(20)); N_j,k = H(0x01 ‖ N_{j+1,2k} ‖ N_{j+1,2k+1}) (Eq. (3)/(21)); H(0xA3 ‖ d) (Eq. (16)); R_agg = H(R_τ1 ‖ … ‖ R_τm) (Eq. (7)/(24)/(30)); R^(Δ)_τ = H(d_i1 ‖ … ‖ d_ik) (Eq. (28)); R^(e)_τ = H(R^(Δ1)_τ ‖ R^(Δ2)_τ ‖ …) (Eq. (29)). Domain-separation tags are defined: "tag_L = 0x00 for leaves and tag_I = 0x01 for internal nodes" (Sec. III.B, p. 4), plus 0xA3 for attestation (Eq. (16), p. 5).
- **What the paper does not specify:** Whether "‖" is raw byte concatenation or length-prefixed concatenation; whether digests are concatenated as raw bytes or as hex strings; the byte width of the tag constants 0x00, 0x01, 0xA3 (one byte each is implied by the notation but never stated); how variable-length fields (c, AD) are delimited inside d.
- **Why a decision is required:** d = H(0x00 ‖ c ‖ t ‖ AD) concatenates three variable/fixed-length fields with no stated delimiter. Different framings yield different digests and different roots. This is not a stylistic choice; it changes the output of the system.
- **Known candidate choices (none authorized):** raw byte concatenation with no delimiters; length-prefixed (e.g. 4-byte big-endian length) fields; hex-string concatenation (the behavior of the `merkletools` library named in Table 5 is relevant here but is not stated by the paper).
- **Originally blocked requirement IDs:** R-MKT-01, R-MKT-03, R-ATT-02, R-EPO-01, R-RTV-01, R-RTV-03

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** **`‖` is raw byte concatenation.** `0x00`, `0x01`, and `0xA3` are **exactly one byte each**. SHA-256 digests are concatenated as **raw 32-byte values, never as hexadecimal strings**. No hidden separators are introduced into expressions such as `H(0x01 ‖ left ‖ right)`. Variable-length fields must already carry deterministic framing in their own representation (Q7 for AD; Q1 for `c = nonce ‖ ciphertext` at a fixed 12-byte nonce; `t` fixed at 16 bytes).
- **Determinism check (passed):** in `d = H(0x00 ‖ c ‖ t ‖ AD)` the fields are unambiguously parseable because `t` is fixed-length (16 B) and `AD` is internally length-prefixed, so raw concatenation is injective here.
- **Resolves:** R-MKT-01, R-MKT-03, R-ATT-02, R-EPO-01, R-RTV-01, R-RTV-03 (byte framing) → USER-APPROVED.

---

### Q9 — [GAP] Meaning of the dummy-leaf constant in H(tag_L ‖ 0)

- **What the paper says:** "If n is not a power of two, we pad to n′ = 2^⌈log2 n⌉ with dummies ℓ_i = H(tag_L ‖ 0) to obtain a full binary tree" (Sec. III.B, after Eq. (2), p. 4).
- **What the paper does not specify:** Whether "0" denotes a single zero byte 0x00, the ASCII character "0", a zero-valued digest of λ bits, or an empty/zero-length string. The paper writes "0" in bold-math style, distinct from the "0x00" hex notation used for tag_L in the same expression, and never defines it.
- **Why a decision is required:** Every padded tree contains these leaves; their value propagates into the per-type root and therefore into the on-chain anchor. With the default epoch size of 1,000 logs (Table 5, p. 10) padded to n′ = 1024, 24 of every 1024 leaves are dummies.
- **Known candidate choices (none authorized):** single byte 0x00; λ/8 zero bytes; empty string; ASCII "0".
- **Originally blocked requirement IDs:** R-MKT-02

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** The paper's dummy value `0` is **one zero byte `0x00`**. The dummy leaf is therefore **`SHA256(0x00 ‖ 0x00)`** — a two-byte pre-image consisting of the leaf tag followed by the dummy byte. This identical constant is used everywhere padding is required.
- **Resolves:** R-MKT-02 (dummy value) → USER-APPROVED.

---

### Q10 — [GAP] DHT protocol, library, node count, replication, routing, and topology

- **What the paper says:** "Intermediate proofs are distributed via a private Distributed Hash Table (DHT)" (Sec. III.A, p. 3). "Distributed Hash Table (DHT) provides an auditable repository for reusable intermediate proofs" storing Parent Node Records, Ancestor Certificates, and Root Records (Sec. III.A item 5, pp. 3–4). "published to a private DHT for low-latency retrieval" (Sec. III.B, before Eq. (6), p. 4). Table 3 (p. 9) defines cost symbol "DHT_ℓ — One DHT lookup at level ℓ for proof material". Table 4 (p. 10) gives single-record verify cost as (L−s)H + sH + DHT_ℓ and batch as n(L−s)H + gsH + (n+g)DHT_ℓ.
- **What the paper does not specify:** The DHT protocol (Kademlia, Chord, Pastry, …); the library; the number of DHT nodes; the replication factor; routing behavior; network topology; whether the DHT is a real network or an in-process simulation. **Table 5 "Experimental Environment and Simulation Parameters" (p. 10) lists no DHT library or DHT node count whatsoever**, despite listing the language, crypto library, data-structure libraries, benchmarking tools, and blockchain simulator.
- **Why a decision is required:** The DHT is one of the seven named system entities and appears in every verification cost expression. Its nature also bears directly on the reported single-record verification latency of 0.000035 s (Sec. V.C.1, Fig. 3, p. 9) — 35 µs total for (L−s)H + sH hashes **plus a DHT lookup** is only attainable if the lookup is in-process, but the paper never says so.
- **Known candidate choices (none authorized):** in-process map/simulated DHT; local multi-node Kademlia; real networked DHT across processes/hosts. Node count and replication factor: unspecified in all cases.
- **Go interaction (§0.7):** the paper names no DHT library at all, so no Python library is being displaced here — but the answer must now name a Go-usable option or an in-process construction. Note that §0.6 forbids introducing goroutines or concurrency for a DHT that the paper does not describe as concurrent.
- **Originally blocked requirement IDs:** R-DHT-01, R-DHT-02, R-DHT-07, R-NFR-01, R-NFR-02, R-EVAL-03

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** A **deterministic in-process simulated private DHT**, not a real networked Kademlia deployment. **16 logical DHT nodes, replication factor 1.** A key is assigned to a logical node by taking the **first eight bytes of `SHA256(key)` as an unsigned integer reduced modulo 16**. Lookups are **synchronous**, and **no routing-delay simulation** is included in the fidelity benchmark.
- **§0.6 coupling:** the simulated DHT is single-goroutine and synchronous. No goroutines, no parallel lookups, no background replication.
- **Consequence for R-EVAL-03:** the reported 0.000035 s single-record latency becomes plausible precisely because lookups are in-process. Any comparison to the paper's figure must state this.
- **OUT OF SCOPE — do not implement:** the answer's reference to applying "controlled RTT ... separately and equally to comparable remote operations" pertains to **later HMF-vs-DHT experiments**, which are not part of this reproduction. See §2.3.
- **Resolves:** R-DHT-01, R-DHT-02 → USER-APPROVED. R-DHT-07 remains PAPER-DERIVED and still applies: the simulated DHT is treated as adversary-controlled for correctness purposes.

---

### Q11 — [GAP] DHT key derivation, and the formats of tree_id and ancestor_id

- **What the paper says:** DHT-stored objects are P_j,k := (treeID, j, k, N_j,k, seq); C_s,k := (ancestorID, treeID, s, k, A_s,k, upProof_s,k, [i_min, i_max], seq); R^(e)_τ := (treeID, seq, R^(e)_τ, leafCnt, τ) (Sec. III.B, Eq. (6), p. 4); and P_j,k = (tree_id, j, k, N_j,k, commit_seq) (Eq. (22), p. 6); C_s,k = (ancestor_id, tree_id, level, index, ancestor_hash, up_proof, [t_min, t_max], commit_seq) (Eq. (23), p. 6). Table 1 (p. 5) lists P_j,k as "Parent node record in DHT for (j, k)".
- **What the paper does not specify:** The DHT **key** under which each record is stored and looked up; the format/derivation of `tree_id` (the paper never says whether it encodes τ and e, nor its type); the format/derivation of `ancestor_id`; the DHT key for Root Records and for micro-roots.
- **Why a decision is required:** Verification requires retrieving {P_j,k} and C_s,k from the DHT (Phase 4 steps 2–3, p. 6). Without a key derivation rule, retrieval cannot be implemented.
- **Known candidate choices (none authorized):** key = H(tree_id ‖ j ‖ k); key = a structured string; key = a UUID recorded in an index. tree_id as (τ, e) pair, hash thereof, or opaque counter.
- **Originally blocked requirement IDs:** R-DHT-03, R-DHT-04, R-DHT-05, R-DHT-06, R-VER-03

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8) — ⚠️ KEY RULE PARTLY SUPERSEDED BY Q45:** **Deterministic domain-separated SHA-256 identifiers.**

**Still in force:**
```
treeID     = H("TREE" ‖ epoch ‖ logType)
ancestorID = H("ANC"  ‖ treeID ‖ s ‖ k)
```
All numeric indices and sequence values are encoded as **unsigned 64-bit big-endian integers** before hashing.

**SUPERSEDED BY Q45 — DO NOT IMPLEMENT THE FOLLOWING:**
```
~~Key_P = H("P" ‖ treeID ‖ j ‖ k ‖ seq)~~     SUPERSEDED
~~Key_C = H("C" ‖ treeID ‖ s ‖ k ‖ seq)~~     SUPERSEDED
~~Key_R = H("R" ‖ treeID ‖ seq)~~             SUPERSEDED
```
**The rule in force is Q45's** (see §0.9 and the Q45 decision in §1A):
```
Key_P = H("P" ‖ treeID ‖ j ‖ k)
Key_C = H("C" ‖ treeID ‖ s ‖ k)
Key_R = H("R" ‖ treeID)
Key_M = H("M" ‖ treeID ‖ windowID)     // unchanged from this decision
```
- **Why superseded:** including `seq` in the key made the key unpredictable to a verifier, because `seq` (Q23) advances on every insert and (Q24) every insert rewrites a whole root-to-leaf path. Q45 removes `seq` from all lookup keys and retains it as in-record version metadata only.
- **Resolves:** R-DHT-03, R-DHT-04, R-DHT-05 (`treeID`, `ancestorID`, and — as amended by Q45 — key derivation) → USER-APPROVED.
- **`windowID`** is now defined by **Q49** as a zero-based `uint64` per `(epoch, log_type)`, `windowID = ⌊i / W⌋`.

---

### Q12 — [GAP] Serialization format of DHT records and of the on-chain payload

- **What the paper says:** The tuple contents of P_j,k, C_s,k, and R^(e)_τ are given (Eq. (6), (22), (23)); the on-chain commit is "BC.commit(R^(e)_agg, e, timestamp)" (Eq. (8), p. 4) / "BC.commit(R^(e)_agg, e)" (Eq. (25), p. 6). "All items can be locally rehashed and reconciled with the blockchain anchor" (Sec. III.A item 5, p. 4).
- **What the paper does not specify:** The wire/storage serialization of these tuples (JSON, MessagePack, Protobuf, ABI encoding, custom binary), field encodings, or integer widths.
- **Why a decision is required:** Records must be stored, retrieved, and rehashed; "locally rehashed" implies a deterministic byte representation that the paper does not define.
- **Known candidate choices (none authorized):** JSON; canonical JSON; MessagePack; Protobuf; custom binary.
- **Go interaction (§0.7):** Python pickle is no longer a candidate. Go's `encoding/json`, `encoding/gob`, and `encoding/binary` are possible vehicles but **none is selected** — the choice of Go serialization package is part of Q35/Q36 tooling approval, and the wire format itself remains this question. Note that Go struct field ordering and map iteration order affect determinism (R-EVAL-08), so the answer must yield a deterministic byte representation.
- **Originally blocked requirement IDs:** R-DHT-06, R-EPO-04

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** **Custom deterministic binary serialization for DHT records.** `uint64` as 8-byte big-endian; hashes as raw 32-byte values; strings and variable byte arrays as a 4-byte big-endian length followed by the bytes. **Fixed field order** and a **one-byte record-type/version prefix**. **Go maps are never serialized.** For blockchain calls, **Solidity ABI encoding** is used instead of this DHT format.
- **Encoding-boundary confirmation (checked, non-conflicting).** The system uses **four distinct encodings at four distinct layers**, and they do not overlap or compete:
  1. **RFC 8785-style canonical JSON** (Q6) — produces `M`, the AEAD *plaintext* only.
  2. **Deterministic length-prefixed binary** (Q7) — produces `AD`, used as AEAD associated data and inside `d`.
  3. **Custom deterministic binary** (this question) — DHT record storage and retrieval only.
  4. **Solidity ABI** (Q14/Q15) — on-chain `commit`/`getRoot` calls only.
  No byte sequence is produced by more than one of these, so no reconciliation is required. The Developer must not substitute one for another.
- **Resolves:** R-DHT-06, R-EPO-04 (serialization) → USER-APPROVED.

---

### Q13 — [GAP] Authorization token τ_A: serialization, `policy` semantics, validity, transport, and enforcement scope

- **What the paper says:** "τ_A = Sign_skCA(auditor_id, epoch, policy)"; "Upon request, DHT verifies: Ver_pkCA(τ_A) = 1"; "Only valid tokens permit access to proof data" (Sec. III.C Phase 5(a), p. 6). Sec. IV.A assumption (v): "only authorized auditors possessing valid CA-issued credentials may request and verify proofs, and enforcement of auditor access control is orthogonal to the cryptographic soundness analyzed herein" (p. 7). Table 2 (p. 9) lists our auditor/trust model as "Blockchain anchor + CA-issued tokens".
- **What the paper does not specify:** The serialization of the signed triple (auditor_id, epoch, policy); the type and semantics of `policy` (what policies exist, how they are evaluated, what they restrict); whether the token carries an expiry; how the token is transported to the DHT on each request; whether the DHT checks that the requested record's epoch matches the token's `epoch` field, or only that the signature verifies; what happens to a request that fails verification.
- **Why a decision is required:** Phase 5(a) is a required system function. As literally written, the DHT performs only a signature check (Ver_pkCA(τ_A) = 1) and no policy evaluation at all — whether that literal reading is intended must be decided by the user, not by an agent.
- **Known candidate choices (none authorized):** signature-check-only, exactly as Eq. in Phase 5(a) is written; signature check plus epoch match; signature check plus a defined policy grammar (e.g. permitted log types τ). Serialization: JSON, concatenated fields, ASN.1.
- **Originally blocked requirement IDs:** R-AUTH-02, R-AUTH-03, R-AUTH-04, R-AUTH-05

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** Token body = **`(version, auditorID, epoch, allowedTypes, expiresAt)`**, deterministically serialized. **`τ_A = Ed25519.Sign(sk_CA, SHA256(TokenBody))`**. **`policy` is interpreted as the set of permitted log types** (`allowedTypes`). The DHT must verify **all four** of: (i) the CA signature; (ii) the requested **epoch matches** the token epoch; (iii) the requested **type is permitted**; (iv) **`now < expiresAt`**. The token is sent **with every DHT proof request**. On failure, return an authorization failure and **no proof data**.
- **Deviation note:** this goes beyond the paper's literal Phase 5(a), which specifies only `Ver_pkCA(τ_A) = 1` — a signature check with no policy evaluation, no epoch match, and no expiry. The additional three checks are a deliberate USER-APPROVED strengthening, recorded here so the Tester tests four conditions rather than the paper's one.
- **Resolves:** R-AUTH-02, R-AUTH-04, R-AUTH-05 → USER-APPROVED.
- **~~UNRESOLVED CONSEQUENCE~~ — RESOLVED BY Q41.** DHT keys are opaque SHA-256 digests, and `treeID` is itself a hash, so a DHT node handed only a key cannot recover the epoch or log type and could not perform checks (ii) or (iii). **Q41 resolves this** with a typed request envelope — `(epoch, logType, …, key)` — from which the DHT recomputes `treeID` and the expected key (under the **Q45** rule) and rejects any mismatch. See the Q41 decision in §1A.
- **~~Residual gap~~ — RESOLVED BY Q40:** token validity is **1 hour** from issuance, `expiresAt` serialized as Unix seconds, system UTC clock in normal execution and an injectable `Clock` in tests, with no skew grace period.

---

### Q14 — [GAP] Smart-contract interface and on-chain storage layout; and what exactly is anchored

- **What the paper says:** "only an epoch-level root-of-roots is anchored on a public blockchain" (Abstract, p. 1). "Each epoch's root-of-roots is appended to a smart contract, forming a tamper-evident chain of integrity states" (Sec. III.A item 7, p. 4). "The blockchain stores immutable (epoch, root) tuples, providing tamper-evidence and rollback detection" (Sec. III.B, after Eq. (8), p. 4). Table 5 (p. 10): "Blockchain Simulator — Hardhat v2.20 (EVM); Solidity 0.8.20". Table 2 (p. 9): on-chain anchor/records = "Root-of-roots per epoch".
- **What the paper does not specify:** The Solidity contract's function signatures, storage structures, events, or access control; how the client submits the commit; gas/transaction handling; how a verifier reads back the anchor.
- **Additional unresolved point (see also Q15):** Sec. V.C.3 (p. 10) states "only a compact root-of-roots **and a CA signature** are anchored", but neither Eq. (8) nor Eq. (25) includes any CA signature in the commit, and no CA signature over R_agg is defined anywhere in Sec. III. Whether a CA signature is part of the on-chain payload must be decided by the user.
- **Why a decision is required:** The anchoring component and the verifier's anchor lookup cannot be built without a contract interface, and the constant-on-chain-storage claim cannot be measured without knowing the anchored payload.
- **Known candidate choices (none authorized):** mapping(uint256 => bytes32) with a commit(epoch, root) function; an append-only array; event-log-only anchoring. With or without a CA signature field.
- **Originally blocked requirement IDs:** R-EPO-03, R-EPO-04, R-EPO-05, R-VER-05, R-EVAL-07

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** A **minimal Solidity contract**: an append-only `mapping(uint256 => bytes32) public epochRoots`; a `commit(uint256 epoch, bytes32 root)` function; a `getRoot(uint256 epoch) external view returns (bytes32)` function. **`commit` must reject overwriting an existing epoch.** One **configured authorized committer**. Emits `RootCommitted(uint256 epoch, bytes32 root)`.
- **Supports:** R-EPO-05 (constant on-chain storage) — one `bytes32` per epoch regardless of record count; and Theorem E.1's rollback resistance, since overwrite is rejected on-chain.
- **Resolves:** R-EPO-03, R-EPO-04, R-EPO-05, R-VER-05 (contract interface) → USER-APPROVED.
- **~~Residual gap~~ — RESOLVED BY Q42/Q46:** committer = the first Hardhat development account, key via `DHT_EVM_PRIVATE_KEY_HEX`; chain ID `31337` (verified at startup); contract deployed by a Hardhat script before the Go process starts, address via `DHT_CONTRACT_ADDRESS`.

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
- **Originally blocked requirement IDs:** R-EPO-03, R-EPO-04, R-EVAL-07

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** The authoritative on-chain payload is **`(epoch, R_agg^(e))` only**. **No client timestamp** and **no CA signature**.
- **Adjudication basis recorded:** this follows Eq. (25) and the Sec. III.B prose "immutable (epoch, root) tuples", and rejects both Eq. (8)'s `timestamp` argument and Sec. V.C.3's "and a CA signature" phrasing. The latter is rejected because no CA signature over `R_agg` is defined anywhere in Sec. III, so adopting it would require inventing an operation the paper never specifies.
- **Resolves:** R-EPO-03, R-EPO-04, R-EVAL-07 (commit payload) → USER-APPROVED.

---

### Q16 — [PAPER AMBIGUITY — USER DECISION REQUIRED] Two conflicting definitions of the per-type root R^(e)_τ

- **Conflicting statements:**
  - Sec. III.B, after Eq. (3), p. 4: "the per-type root is R^(e)_τ = N_0,0" — i.e. the root of the binary MK-Tree built by Eq. (2) and (3).
  - Sec. III.C Phase 5(b), Eq. (29), p. 7: "R^(e)_τ = H(R^(Δ1)_τ ‖ R^(Δ2)_τ ‖ · · ·)" — i.e. a flat hash over the sequence of per-window micro-roots, where each micro-root is itself a flat hash R^(Δ)_τ = H(d_i1 ‖ d_i2 ‖ · · · ‖ d_ik) (Eq. (28)).
- **Why these cannot both hold:** Eq. (29) is a two-level flat-concatenation digest; N_0,0 is the root of a domain-separated binary tree. For the same set of leaf digests they produce different values. Eq. (29) also has no binary-tree structure, so no inclusion proof, no ancestor checkpoint A_s,k, no up-proof, and no stride parameter s can be defined over it — yet Phase 4's verification (p. 6) and Theorem G.1 (p. 8) all require exactly those objects and terminate at R^(e)_τ.
- **Why a decision is required:** This determines whether the real-time micro-root path and the MK-Tree path are (a) the same structure, (b) two alternative modes, or (c) two independent artifacts both published, and it determines what the verifier's step 3–4 root comparison actually compares against. It is the single most consequential contradiction in the paper.
- **Known candidate choices (none authorized):** treat Eq. (29) as authoritative and the MK-Tree as a per-window sub-structure; treat N_0,0 as authoritative and Eq. (29) as an alternative real-time-only mode producing a separate value; implement both and anchor only one; some other reconciliation the authors intended.
- **Originally blocked requirement IDs:** R-MKT-04, R-RTV-02, R-RTV-03, R-EPO-01, R-VER-04

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** The **binary MK-Tree root `R^(e)_τ = N_0,0` is authoritative.** It is used for inclusion proofs, ancestor checkpoints, Root Records, `R^(e)_agg`, and blockchain verification. **Eq. (29)'s flat micro-root aggregation does not redefine this symbol.** The micro-root path is a **separate auxiliary provisional real-time state**, denoted **`R^(e)_τ,RT`**, and is *not* the authoritative per-type MK-Tree root.
- **Adjudication basis recorded:** Sec. III.B's `N_0,0` is retained because Phase 4 verification, ancestor checkpoints `A_s,k`, up-proofs, the stride parameter `s`, and Theorem G.1 all require a binary-tree structure that Eq. (29) cannot provide. Eq. (29) is reinterpreted as defining a distinct auxiliary value under a distinct symbol.
- **Consequence:** the paper's Eq. (29) is **not reproduced as written** — it no longer defines `R^(e)_τ`. The Tester must test `R^(e)_τ = N_0,0` and must not assert Eq. (29) as a definition of the per-type root.
- **Resolves:** R-MKT-04, R-RTV-03, R-EPO-01, R-VER-04 → USER-APPROVED. The two follow-ups this question flagged are both settled: the anchored artifact is the MK-Tree root, and Phase 4 terminates at the same artifact, so Q26's reconciliation is well-defined.

---

### Q17 — [PAPER AMBIGUITY — USER DECISION REQUIRED] Ancestor certificate range field: leaf-index range or time range

- **Conflicting statements:**
  - Sec. III.B, Eq. (5), p. 4: "C_s,k = (A_s,k, upProof_s,k, **[i_min, i_max]**, seq)", introduced by "Each A_s,k covers a contiguous leaf range [i_min, i_max]".
  - Sec. III.B, Eq. (6), p. 4: "C_s,k := (ancestorID, treeID, s, k, A_s,k, upProof_s,k, **[i_min, i_max]**, seq)".
  - Sec. III.C Phase 3, Eq. (23), p. 6: "C_s,k = (ancestor_id, tree_id, level, index, ancestor_hash, up_proof, **[t_min, t_max]**, commit_seq)".
- **What the paper does not specify:** Whether [t_min, t_max] in Eq. (23) is a typographic variant of the leaf-index range or a genuinely different time-window range; if it is a time range, its source and serialization (see Q7).
- **Why a decision is required:** The field is part of the certificate that the verifier retrieves and rehashes; its type changes the certificate's serialization and any range-based lookup.
- **Known candidate choices (none authorized):** leaf-index range only; timestamp range only; both fields present.
- **Originally blocked requirement IDs:** R-ANC-02, R-DHT-05

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** Use the **leaf-index range `[i_min, i_max]`** in each ancestor certificate. An ancestor represents a contiguous structural subtree of the MK-Tree, so leaf indices are the appropriate range semantics. **Timestamps are not substituted into this field.**
- **Adjudication basis recorded:** Eq. (5) and Eq. (6) are followed; Eq. (23)'s `[t_min, t_max]` is treated as a typographic variant, not a distinct time-range field.
- **Resolves:** R-ANC-02, R-DHT-05 (range field) → USER-APPROVED.

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
- **Originally blocked requirement IDs:** R-VER-02, R-VER-03, R-DHT-04, R-ANC-03

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** The **logical Merkle proof uses sibling hashes.** DHT `P_j,k` records are **storage records from which the verifier retrieves the sibling node** needed at each level — for a path position `k`, retrieve the corresponding sibling position (`k xor 1` where valid). The retrieved value becomes an element of `S↓_i`, and **the verifier recomputes each parent itself**. An on-path parent value returned by the adversarial DHT is **never trusted in place of recomputation**.
- **Adjudication basis recorded:** Eq. (9) governs the logical proof; Eq. (26)'s `{P_j,k}` is reinterpreted as the *retrieval mechanism*, not as trusted path values. This preserves Theorem F.1's soundness argument (Sec. IV.F, p. 8) and is consistent with R-DHT-07.
- **Note under Q24:** because the tree is pre-sized and fully padded, every sibling position always exists, so the "where valid" qualifier is always satisfied within an epoch tree.
- **Resolves:** R-VER-02, R-VER-03, R-DHT-04, R-ANC-03 → USER-APPROVED.

---

### Q19 — [PAPER AMBIGUITY — USER DECISION REQUIRED] Who performs canonicalization and encryption: a separate Encryptor entity or the producer

- **Conflicting statements:**
  - Sec. III.A item 2, p. 3: "**Encryptor** performs trusted preprocessing. For each record, it: a) Canonicalizes the structure…; b) Applies AEAD…" — a distinct system entity, shown as a separate actor in Fig. 1 (p. 3).
  - Sec. III.C Phase 1 heading, p. 5: "**Producer-Side** Canonicalization, Encryption, and Public Attestation"; "Each producer … prepares its logs before submission."
- **What the paper does not specify:** Whether the Encryptor is a separate trusted process between producers and the lake, or a producer-side library; and consequently who holds K (see Q2) and where σ is generated relative to encryption.
- **Why a decision is required:** This determines the component boundaries, the trust boundary, and the key-distribution model.
- **Known candidate choices (none authorized):** separate Encryptor service; producer-embedded encryption library; both (per Fig. 1's separate icon with producer-side semantics).
- **Originally blocked requirement IDs:** R-SYS-02, R-SYS-03, R-ENC-01, R-ATT-01

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** The Encryptor is a **producer-side in-process module/library**. Although the paper's Fig. 1 draws `Producer → Encryptor` as separate actors, **no separate network service is introduced**. The producer invokes canonicalization, AEAD encryption, digest construction, and attestation through this module.
- **Adjudication basis recorded:** Phase 1's "Producer-Side" heading governs; Sec. III.A item 2's separate-entity depiction is treated as a logical role, not a deployment boundary. Consistent with Q2's single system-wide key, which the producer-side module holds.
- **Resolves:** R-SYS-02, R-SYS-03, R-ENC-01, R-ATT-01 → USER-APPROVED.

---

### Q20 — [PAPER AMBIGUITY — USER DECISION REQUIRED] Who performs ingestion-time signature verification

- **Conflicting statements:**
  - Sec. III.C Phase 2, p. 6: "Upon ingestion, **the verifier** performs integrity and authenticity validation without decryption" — running Eq. (18)/(19) and discarding failures.
  - Sec. III.A item 6, p. 4: "**Verifier** checks log integrity in two modes: Inclusion Check … Batch Check" — i.e. the Verifier is the auditor-side entity, and neither mode is ingestion-time signature checking.
  - Sec. III.C Phase 4, p. 6: "**Auditors** may verify integrity of individual or batched records" — auditors, not the ingestion path.
- **What the paper does not specify:** Which component runs Phase 2. Fig. 1 (p. 3) shows no distinct ingestion-verification actor.
- **Why a decision is required:** Phase 2 is a mandatory gate ("Only verified tuples are accepted; failures are discarded"). Its owner determines the pipeline architecture.
- **Known candidate choices (none authorized):** the Partitioner performs Phase 2 before tree construction; a dedicated ingestion verifier component; the same Verifier component in a different mode.
- **Originally blocked requirement IDs:** R-SYS-06, R-ING-01, R-ING-04

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** A **dedicated ingestion-verifier module** runs before partitioning / MK-Tree insertion. The pipeline is **`Producer → Encryptor/Attestation → Ingestion Verifier → accepted storage + Partitioner`**. This is **separate** from the auditor-side verifier used for inclusion and batch audit verification.
- **Adjudication basis recorded:** Phase 2's "the verifier performs" is assigned to a distinct ingestion component, preserving Sec. III.A item 6's statement that the auditor-side Verifier has exactly **two** modes (Inclusion Check, Batch Check) — the ingestion gate is not a third mode of that entity.
- **Resolves:** R-SYS-06, R-ING-01 → USER-APPROVED.

---

### Q21 — [PAPER AMBIGUITY — USER DECISION REQUIRED] Window Δ: a time window or a fixed log count

- **Conflicting statements:**
  - Sec. III.C Phase 5(b), p. 7: "Logs are grouped within a **sliding time window Δ**."
  - Table 1, p. 5: "R^(Δ)_τ — Micro-root for **real-time window Δ**."
  - Sec. V.C.5, p. 11: "An **aggregation window of 1,024 logs** is used as the baseline configuration"; Fig. 7 (p. 11) legend gives W = 256, 512, 1024, 2048 — all counts, not durations.
- **What the paper does not specify:** Whether Δ is a duration or a record count; if a duration, its value and units (Table 5 lists no window duration); whether "sliding" means overlapping windows or consecutive tumbling windows — and if overlapping, whether a record contributes to more than one micro-root, which Eq. (29) does not account for.
- **Why a decision is required:** Δ determines micro-root membership, hence R^(Δ)_τ, hence (per Eq. (29)) possibly R^(e)_τ itself.
- **Known candidate choices (none authorized):** count-based tumbling windows of 1,024 logs; count-based sliding windows; time-based windows of an unspecified duration.
- **Originally blocked requirement IDs:** R-RTV-01, R-RTV-02, R-EVAL-06

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** **Count-based, non-overlapping tumbling windows.** Default aggregation window **1,024 logs**; sensitivity evaluated at **256, 512, 1,024, 2,048** logs. **Overlapping sliding windows are not implemented.**
- **Adjudication basis recorded:** Sec. V.C.5 and Fig. 7's log-count windows govern; Phase 5(b)'s "sliding time window" is not reproduced as a time-based or overlapping construct. Tumbling windows also remove the Eq. (29) double-counting problem that overlapping windows would create.
- **Consequence:** the paper's word "sliding" is **not** reproduced. The Tester must test tumbling-window membership, not overlapping membership.
- **Resolves:** R-RTV-01 (window semantics), R-EVAL-06 (window sizes) → USER-APPROVED.

---

### Q22 — [GAP] Relationship between the aggregation window, the micro-root window, and the MK-Tree; and micro-root DHT publication

- **What the paper says:** "Publish R^(Δ)_τ to the DHT for near real-time validation" (Phase 5(b) step 2, p. 7). Corollary G.1 (p. 8): "If h = O(1) via sliding-window micro-roots and batching ensures g = O(1)…". Sec. V.C.5 (p. 11) varies an "aggregation window" jointly with stride s.
- **What the paper does not specify:** Whether the "aggregation window" of Sec. V.C.5 is the same object as Δ; whether one MK-Tree is built per window or per epoch; the DHT record structure and key for a micro-root (Eq. (6) defines DHT record shapes for P, C, and R only — **not** for micro-roots); the ordering of d_i1…d_ik within Eq. (28); whether micro-roots are covered by ancestor certificates.
- **Why a decision is required:** Corollary G.1's h = O(1) claim depends on tree depth being bounded by the window, which implies per-window trees — but the paper defines the tree per (τ, e). This cannot be resolved by an agent.
- **Known candidate choices (none authorized):** one MK-Tree per epoch with windows as leaf sub-ranges; one MK-Tree per window with epoch-level consolidation per Eq. (29); windows used only for the real-time path and unrelated to the tree.
- **Originally blocked requirement IDs:** R-RTV-02, R-RTV-04, R-MKT-05, R-EVAL-06

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** Micro-roots are an **independent provisional real-time verification path**, not the authoritative MK-Tree structure.
- **Main path:** `d_i → MKTree_τ^(e) → R^(e)_τ → R^(e)_agg → blockchain`.
- **Auxiliary path:** `d_i → R^(Δ)_τ → DHT`.
- **Micro-root record:** `(treeID, windowID, i_min, i_max, R^(Δ)_τ, seq)`, stored under `Key_M` (Q11).
- A DHT-only micro-root is **provisional** until the corresponding records are incorporated into an epoch MK-Tree that is externally anchored.
- **Resolves:** R-RTV-02 (micro-root record structure — the paper defined none), R-MKT-05 (one tree per epoch, windows as leaf sub-ranges) → USER-APPROVED.
- **Consequence for R-RTV-04:** "near-instant" verification against a micro-root is explicitly **provisional** and carries no anchored guarantee until epoch close. The Tester must not assert micro-root verification as equivalent to anchored verification.
- **~~Residual gap~~ — RESOLVED BY Q49:** `windowID` is a zero-based `uint64` per `(epoch, log_type)`, `windowID = ⌊leaf_index / W⌋`, encoded big-endian. **Still open:** partial-window handling and dummy participation in Eq. (28) — **Q53**.

---

### Q23 — [GAP] Epoch boundary definition, and `seq` / `commit_seq` semantics

- **What the paper says:** "At each epoch, all per-type Merkle roots are aggregated into a root-of-roots and immutably committed to the blockchain" (Sec. III.A item 4, p. 3). "seq is a monotonically increasing commit index" (Sec. III.B, after Eq. (5), p. 4). Fig. 2 (p. 5) shows a chain of "Seq 2 / Prev Hash / Root Hash", "Seq 3 …", "Seq 4 …" blocks feeding a "Commit". Table 5 (p. 10): "Epoch Sizes 1,000 (default), 10,000, 100,000 logs".
- **What the paper does not specify:** Whether an epoch closes on record count, elapsed time, or an explicit trigger; the scope of `seq` (per tree, per type, or global); its initial value and increment rule; what a "commit" is that `seq` indexes; and what the "Prev Hash" field shown in Fig. 2 is — **no equation in the paper defines a Prev Hash field**, and it appears in no DHT record tuple (Eq. (6), (22), (23)) nor in the on-chain payload (Eq. (8), (25)).
- **Why a decision is required:** `seq` appears in every DHT record and must be assigned deterministically. The Fig. 2 "Prev Hash" chaining is either an unstated required mechanism or an illustrative artifact; an agent must not decide which.
- **Known candidate choices (none authorized):** count-based epochs at the Table 5 sizes; time-based epochs; seq global vs. per-tree; Prev Hash implemented vs. omitted.
- **Originally blocked requirement IDs:** R-EPO-02, R-DHT-06, R-MKT-06, R-EVAL-05

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** **Count-based epochs**, default **1,000 logs**, with **10,000** and **100,000** as evaluation alternatives. A **global monotonically increasing `seq` beginning at 1**, incremented **once per accepted log-state update**; all DHT objects emitted by that update share the same sequence value. Root Records at finalization carry the latest sequence. **Fig. 2's unexplained `Prev Hash` field is NOT implemented.**
- **Consequence:** the paper's Fig. 2 chaining is treated as an illustrative artifact and is deliberately **not reproduced**. The Tester must not test for a `Prev Hash` field.
- **Resolves:** R-EPO-02 (epoch boundary), R-DHT-06 (`seq` assignment) → USER-APPROVED.
- **~~UNRESOLVED CONSEQUENCE~~ — RESOLVED BY Q45:** `seq` remains global and per-update exactly as decided here, but it is **removed from all DHT lookup keys** (§0.9). Keys are now stable and a newer write overwrites at the same key, so a verifier can compute them without knowing `seq`. `seq` survives as in-record version metadata.

---

### Q24 — [PAPER AMBIGUITY — USER DECISION REQUIRED] Incremental append vs. power-of-two padding

- **Conflicting statements:**
  - Sec. III.B, p. 4: "If n is not a power of two, we pad to n′ = 2^⌈log2 n⌉ with dummies … to obtain a full binary tree", with L = ⌈log2 n′⌉ and ancestor indices k ∈ {0, …, 2^s − 1} (Eq. (4)) — a fixed-size, fully-built tree.
  - Sec. III.A item 4, p. 3: "Leaf digests are computed from encrypted records and internal nodes are updated **incrementally**."
  - Sec. III.C Phase 3, Eq. (21) heading, p. 6: "Incremental Merkle Update — Only affected paths are recomputed."
- **Why these conflict:** Under true incremental append, n grows continuously, so n′ = 2^⌈log2 n⌉ doubles at each power-of-two boundary, L increases by one, and every ancestor index k and every previously published P_j,k, C_s,k become stale. The paper never states how already-published DHT records and certificates are handled across a tree-growth event, nor whether the tree is instead pre-sized to the known epoch size (1,000 → n′ = 1024).
- **Why a decision is required:** This determines the entire tree-construction and DHT-publication implementation, and whether previously issued ancestor certificates remain valid.
- **Known candidate choices (none authorized):** pre-size the tree to the configured epoch size and pad once; grow and re-pad dynamically, republishing affected records; append-only with a forest/rollover scheme.
- **Originally blocked requirement IDs:** R-MKT-02, R-MKT-05, R-MKT-07, R-DHT-04, R-ANC-04

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** **Pre-size each MK-Tree to the configured epoch capacity and pad it from the beginning.** For capacity `N_e`, use `N′_e = 2^⌈log2 N_e⌉`; for the default 1,000-log epoch, allocate **1,024 leaf positions**. Those positions initially hold dummy leaves; **each arriving real digest replaces the next dummy position** and **only the affected path is recomputed**. This preserves stable tree height, node coordinates, stride levels, ancestor IDs, and already-published addressing semantics within the epoch.
- **Adjudication basis recorded:** this reconciles the padding rule with "incremental Merkle update" by fixing `n′` up front, so `L` never changes mid-epoch and no previously published `P_j,k` or `C_s,k` is invalidated by tree growth.
- **§0.6 note:** "only the affected path is recomputed" is the paper's own stated behavior (Sec. III.A item 4; Phase 3, Eq. (21)) and is therefore required, not a prohibited optimization. Additional speedups beyond it remain prohibited.
- **Resolves:** R-MKT-02 (padding), R-MKT-05 (tree scope: one tree per (τ, e)), R-MKT-07, R-ANC-04 → USER-APPROVED.
- **~~UNRESOLVED CONSEQUENCE~~ — RESOLVED BY Q45:** rewrites along a path now **overwrite at stable keys** rather than creating a new key per `seq` (§0.9). **Still open:** whether verification may run against an epoch that is still open, given that these overwrites continue until finalization — **Q52**.

---

### Q25 — [GAP + AMBIGUITY] Stride s: production default, and the inconsistent explanation in Sec. V.C.5

- **What the paper says:** "Fix a stride parameter s ∈ {1, …, L}. All nodes at level s act as ancestor checkpoints: A_s,k = N_s,k, k ∈ {0, …, 2^s − 1}" (Eq. (4), p. 4), with the root at level j = 0 and leaves at j = L (Sec. III.B, p. 4). "At a **configurable** stride s, ancestor checkpoints are produced and published to the DHT" (Sec. III.A item 4, p. 3). Theorem G.1 (p. 8): H(S, s) ≤ n(L − s) + gs. Experiments use s ∈ {1, 2, 4, 8} at n = 2^10 (Sec. V.C.5, p. 11).
- **The internal inconsistency:** Sec. V.C.5 (p. 11) explains Fig. 7 as "higher stride values generally reduce verification latency **by enabling multiple proofs to share common ancestor checkpoints**". With the paper's own indexing (root at j = 0), a higher s means a level nearer the leaves and therefore **more** checkpoints (2^s of them) and **less** sharing, not more. The measured trend in Fig. 7 is consistent with the formula H(S, s) ≤ n(L − s) + gs (the n(L − s) term shrinks as s grows), but the stated causal explanation contradicts the paper's own level indexing.
- **What the paper does not specify:** The default/production value of s (Table 5 lists epoch sizes and window sizes but **no default stride**).
- **Why a decision is required:** s is a required construction parameter; a default is needed to build and to reproduce the non-Fig-7 experiments. The explanation conflict must be adjudicated so the Tester knows which behavior is the expected one.
- **Known candidate choices (none authorized):** any s ∈ {1, …, L}; the values 1, 2, 4, 8 used in Fig. 7; a value derived from the window size.
- **Originally blocked requirement IDs:** R-ANC-01, R-EVAL-06

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** **`s = 8` is the default** for the default padded tree of 1,024 leaves (`L = 10`). Evaluate `s ∈ {1, 2, 4, 8}` in the stride-sensitivity experiment. **Levels are interpreted exactly as the paper formally defines them: root at `j = 0`, leaves at `j = L`.** Therefore a **larger `s` places checkpoints closer to the leaves and increases the number of checkpoints**; the real tradeoff is a **shorter leaf-to-checkpoint path versus more checkpoint groups**.
- **PAPER'S OWN EXPLANATION IS NOT REPRODUCED — Tester read this.** Sec. V.C.5 (p. 11) attributes Fig. 7's downward trend to higher `s` "enabling multiple proofs to share common ancestor checkpoints". Under the paper's own indexing that is backwards: higher `s` yields `2^s` checkpoints, i.e. **less** sharing. The measured trend is instead explained by **Theorem G.1's formula `H(S, s) ≤ n(L − s) + gs`**, whose dominant `n(L − s)` term shrinks as `s` grows.
  **The Tester shall test the formula (R-VER-08), not the prose.** The absence of the paper's stated causal mechanism is not a defect.
- **Resolves:** R-ANC-01 (default stride and level indexing), R-EVAL-06 (stride sweep) → USER-APPROVED.

---

### Q26 — [PAPER AMBIGUITY — USER DECISION REQUIRED] How the verifier reconciles a single per-type root R^(e)_τ against an anchor that stores only R^(e)_agg

- **Conflicting / incomplete statements:**
  - Sec. III.C Phase 4 step 4, p. 6: "**Confirm R^(e)_τ against the blockchain anchor**."
  - Sec. III.B step 3, p. 4: "Use (30) and the on-chain anchor (8) to validate R^(e)_agg" — where Eq. (30) is R_agg = H(R_τ1 ‖ R_τ2 ‖ · · · ‖ R_τm), i.e. recomputation requires **all m per-type roots**, not just the one under audit.
  - The chain stores only (epoch, root-of-roots) (Sec. III.B, p. 4).
- **What the paper does not specify:** Where the verifier obtains the other m − 1 per-type roots; the canonical **ordering** of the types τ_1 … τ_m inside Eq. (7)/(24)/(30) (without a fixed order, R_agg is not reproducible); and whether the Root Records R^(e)_τ in the DHT (Eq. (6)) are the intended source, given that the DHT is explicitly adversary-controlled (Sec. IV.A, p. 7).
- **Why a decision is required:** Phase 4 step 4 is unimplementable as literally written. The type ordering is required for any anchor to be reproducible at all.
- **Known candidate choices (none authorized):** fetch all Root Records from the DHT and recompute R_agg; anchor per-type roots as well; fix a lexicographic or registration order for τ; include m and the type list in the on-chain payload.
- **Originally blocked requirement IDs:** R-VER-05, R-EPO-01, R-DHT-05

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** To verify one per-type root against the aggregate anchor, **retrieve the Root Records for all log types in epoch `e`**, **sort log types lexicographically by their canonical UTF-8 `log_type` value**, and compute `R^(e)_agg = H(R^(e)_τ1 ‖ … ‖ R^(e)_τm)`. Compare that aggregate with the blockchain value for the epoch. A forged DHT Root Record is therefore **detected**, because the recomputed aggregate will not match the externally anchored root.
- **Resolves:** R-VER-05, R-EPO-01 (canonical type ordering — the missing ingredient without which `R_agg` was not reproducible at all), R-DHT-05 → USER-APPROVED.
- **Consistency with Q29:** the five types `access`, `api`, `auth`, `error`, `security` sort lexicographically in that order, giving `m = 5`.

---

### Q27 — [GAP] Cloud data lake storage technology and stored record schema

- **What the paper says:** "**Cloud Data Lake** stores encrypted records **(c, t, AD)** at scale. The storage layer is untrusted" (Sec. III.A item 3, p. 3). "Verified tuples **(c, t, AD, d, σ, pk_P)** are stored in the cloud data lake" (Sec. III.C Phase 2, p. 6).
- **What the paper does not specify:** The storage technology (object store, filesystem, relational DB, key-value store); the schema; indexes; how a record is addressed for later audit. The two statements above also list **different stored field sets**, and the paper does not say which is authoritative.
- **Why a decision is required:** The verifier must retrieve (c, t, AD) for the record under audit (Phase 4 step 1, p. 6); the storage interface and record identity scheme are needed.
- **Known candidate choices (none authorized):** flat files; SQLite; PostgreSQL; an object store; in-memory. Field set (c,t,AD) vs. (c,t,AD,d,σ,pk_P).
- **Originally blocked requirement IDs:** R-ING-04, R-ING-05, R-SYS-04

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** **PostgreSQL** as the reproduced Cloud Data Lake. Store the complete verified tuple: **`record_id`, `epoch`, `producer_id`, `log_type`, `timestamp`, `c`, `t`, `AD`, `d`, `sigma`, `producer_public_key`**. **`record_id` is the primary key.** The DHT stores proof/authentication state; PostgreSQL stores encrypted log records. `c` includes `nonce ‖ ciphertext` per Q1.
- **Adjudication basis recorded:** Phase 2's larger field set `(c, t, AD, d, σ, pk_P)` governs over Sec. III.A item 3's `(c, t, AD)`.
- **Note:** `producer_id`, `log_type`, and `timestamp` are stored both as queryable columns and inside the serialized `AD` blob. These must agree; `AD` remains the authoritative bytes for hashing and AEAD.
- **Resolves:** R-ING-04 (field set), R-SYS-04 (storage technology) → USER-APPROVED.
- **~~Residual gaps~~ — RESOLVED BY Q43/Q44:** PostgreSQL 17 with `pgx/v5`, full column types, and one authorized constraint `UNIQUE (epoch, log_type, leaf_index)` (§0.6 Named Exception 1); a persisted `leaf_index` column supplies the record→leaf-position mapping. **The Q43 schema supersedes this field list**, adding `leaf_index` and fixing the names `timestamp_ns` and `ad`.

---

### Q28 — [GAP] Component interfaces and process boundaries

- **What the paper says:** Names seven entities — Log Producers, Encryptor, Cloud Data Lake, Partitioner, DHT, Verifier, Blockchain (Sec. III.A, pp. 3–4) — and shows their interactions in Fig. 1 (p. 3).
- **What the paper does not specify:** Whether these are separate processes/services or in-process modules; any API structure; REST vs. gRPC vs. library calls vs. message queue; request/response schemas; ports; authentication between components (other than the auditor token of Q13).
- **Why a decision is required:** The interface style determines the entire code structure and is required before any component can be built or integration-tested.
- **Known candidate choices (none authorized):** single-process Python modules; REST/HTTP services; gRPC; message queue.
- **Originally blocked requirement IDs:** R-SYS-07, R-NFR-03

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** **One Go process with explicit packages/interfaces**, with **Hardhat remaining an external process**. Packages: `producer`, `encryptor`, `ingestverifier`, `storage`, `mktree`, `dht`, `auth`, `verifier`, `blockchain`, `benchmark`. Communication is by **ordinary in-process function/interface calls**. **No REST, gRPC, message queues, or microservice boundaries** are introduced.
- **§0.6 coupling:** in-process calls only; no concurrency between packages.
- **Resolves:** R-SYS-07, R-NFR-03 → USER-APPROVED.
- **Note:** PostgreSQL (Q27) and Hardhat (Q38) remain external processes, so connection configuration is still required — see **Q46**.

---

### Q29 — [GAP] Log type set, number of types m, and evaluation dataset

- **What the paper says:** Producers are "applications, API gateways, WAF, IDS, databases, etc." and "Each record is tagged with type metadata to support partitioning and per-type Merkle construction" (Sec. III.A item 1, p. 3). Table 5 (p. 10): "Log Size 256–512 bytes per record"; "Dataset Sizes 10^3, 10^4, 10^5 logs"; "Input Distributions Uniform, Zipf-heavy, Bursty". The Conclusion (p. 11) narrows the target to "append-only, high-frequency **web-server** logs".
- **What the paper does not specify:** The concrete set of log types τ used; the value of m; whether the dataset is real or synthetic and, if synthetic, the generation procedure; the field schema of a record; the parameters of the Zipf-heavy and Bursty distributions (e.g. Zipf exponent, burst size/interval).
- **Why a decision is required:** No experiment in Sec. V can be reproduced without the dataset definition, and R_agg's type count m follows from it.
- **Known candidate choices (none authorized):** synthetic generator with a fixed type list; a public web-server log corpus; user-supplied dataset.
- **Originally blocked requirement IDs:** R-EVAL-01, R-EVAL-02, R-EVAL-08, R-SYS-01

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** A **deterministic synthetic web/cloud-log dataset** with **five log types: `access`, `error`, `auth`, `api`, `security`** (so **`m = 5`**). Records of **256–512 bytes**; dataset sizes **10³, 10⁴, 10⁵** logs. **PRNG seed 42.** Distributions: **Uniform** — each type with probability 1/5; **Zipf-heavy** — exponent **1.2** over the five ranked types; **Bursty** — **1,024-record windows**, one hot type per window, **80%** from that type and the remaining **20%** uniform across the other four.
- **Consistency check against Q6 (passed):** generated records use the Q6 canonical schema (`record_id`, `producer_id`, `log_type`, `timestamp`, `message`, `attributes`), with `log_type` drawn from the five types above. Q7's `AD = (producerID, logType, timestamp)` draws its three fields from that same schema. No divergence found.
- **Consistency check against Q26 (passed):** lexicographic ordering of these five types is `access, api, auth, error, security`, which is the order used to compute `R^(e)_agg`.
- **Resolves:** R-EVAL-01, R-EVAL-02 (distribution parameters), R-SYS-01 (log type set) → USER-APPROVED.
- **~~Residual gaps~~ — RESOLVED BY Q47/Q48:** five type-aligned producers; per-type message prefixes; a fixed flat `attributes` object; target size drawn uniformly in [256, 512] and measured on the **canonicalized plaintext `M`**, reached by deterministic ASCII padding. **Still open:** `record_id` generation (**Q50**) and the `timestamp` canonical form (**Q51**), both inside `M`.

---

### Q30 — [GAP] Commit batch size B and the batch sizes used for verification experiments

- **What the paper says:** Table 3 (p. 9) defines "B — Logs per commit batch" and "n — Batch size used in verification experiments". Sec. V.C.3 (p. 10) defines commit time as "the total duration required to construct the authenticated data structure (ADS) for a new batch of logs and anchor the resulting integrity state on the blockchain", reporting 0.0053 s. Sec. V.C.5 (p. 11) fixes n = 2^10 for the stride experiment; Fig. 6 (p. 11) sweeps batch sizes 2^6 … 2^12.
- **What the paper does not specify:** The value of B used to obtain the 0.0053 s commit-time result, or the value of n used for the single-record and throughput results in Figs. 3–4.
- **Why a decision is required:** The commit-time and throughput figures are the paper's headline results; they cannot be reproduced or validated without the batch size that produced them.
- **Known candidate choices (none authorized):** B equal to the default epoch size (1,000); B equal to the aggregation window (1,024); some other value.
- **Originally blocked requirement IDs:** R-EVAL-04, R-EVAL-05

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** **`B = 1000` logs** for the default commit/epoch benchmark, matching the default epoch size. **`n = 1`** for the single-record experiment. Headline throughput workload uses **batches of 1,024 verification requests**. Locality/batch scaling uses **`n ∈ {64, 128, 256, 512, 1024, 2048, 4096}`**. The stride/window experiment uses **`n = 1024`**. These values are documented explicitly **because the paper does not specify them**, and any reported comparison must say so.
- **Resolves:** R-EVAL-04, R-EVAL-05 (batch sizes) → USER-APPROVED.

---

### Q31 — [GAP] Scope of baseline re-implementation (Schemes [8], [12], [16], [26])

- **What the paper says:** "This section compares the proposed scheme with four representative baselines: Scheme [8], Scheme [12], Scheme [16], and Scheme [26]. All baselines were **re-implemented** using identical hashing primitives, Merkle-based structures (when applicable), and the same dataset inputs" (Sec. V.C, p. 9). Numeric results are given for each in Figs. 3–5 and Tables 2 and 4.
- **What the paper does not specify:** Any construction detail of the four baselines — those live in the cited external papers, which this project is explicitly forbidden to consult. The paper gives only symbolic cost expressions (Table 4, p. 10) and result values.
- **Why a decision is required:** Reproducing Figs. 3, 4, and 5 requires the baselines. Building them from the cited papers would violate the paper-only rule; omitting them means the comparative results cannot be reproduced. The user must decide the scope.
- **Known candidate choices (none authorized):** implement the proposed scheme only and record the paper's baseline numbers as static reference values; implement baselines from the cited sources (requires lifting the paper-only restriction); omit comparative evaluation entirely.
- **Originally blocked requirement IDs:** R-EVAL-10, R-EVAL-11

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** Implement **only the DHT-backed proposed system**. **Do not reconstruct Schemes [8], [12], [16], or [26]** from the incomplete descriptions inside this paper. Published baseline values may be recorded **only as static values explicitly labelled as reported by the original authors and NOT reproduced**.
- **Consequence:** Figs. 3, 4 and 5 are **not reproduced as comparative results**. Only the "Ours" data point in each is measured. Every table or chart carrying baseline numbers must label them as unreproduced author-reported values. The Tester must not treat a baseline comparison as a testable claim.
- **Preserves the paper-only rule:** reconstructing baselines would require reading the cited external papers, which this project is forbidden to consult.
- **OUT OF SCOPE — do not implement:** the answer's reference to implementing **DELIA and BLAS from their own primary papers** pertains to a **later HMF-Audit comparison**, not to this reproduction. See §2.3.
- **Resolves:** R-EVAL-10, R-EVAL-11 (scope) → USER-APPROVED.

---

### Q32 — [GAP] Caching, concurrency, retries, and timeouts

- **What the paper says:** Sec. III.B step 1 (p. 4) refers to deriving A_s,k "using S↓_i and **cached** P_j,k", and again "bottom-up recomputation from leaves to A_s,k using **cached** P_j,k" (Sec. III.B, after Eq. (6), p. 4).
- **What the paper does not specify:** What the cache is, where it lives (verifier-side, DHT-node-side), its eviction policy, its size, or its lifetime. Nor does the paper specify any concurrency model, parallel execution, retry policy, or timeout for DHT lookups or blockchain commits.
- **Why a decision is required:** The word "cached" appears in the verification procedure itself, so the reproduction must decide whether a cache is part of the specified behavior or merely descriptive — and caching materially affects the reported 0.000035 s latency and 34,482.76 logs/s throughput.
- **Known candidate choices (none authorized):** no cache (fetch every P_j,k from the DHT); unbounded verifier-side cache; bounded LRU cache. Concurrency: single-goroutine; multi-goroutine; multi-process.
- **Interaction with the global no-optimization rule (§0.6):** §0.6 prohibits all unstated optimization, explicitly naming caching, memoization, concurrency, goroutines, and parallel execution. The **only** reason this question exists at all is that the paper itself uses the word "cached" in its verification procedure — that single mention is the entire paper-stated surface. **Until Q32 is answered, the Developer implements no cache and no concurrency of any kind.** A Q32 answer may authorize a cache; it does not thereby authorize concurrency, which the paper never mentions and which §0.6 forbids absent an explicit separate approval.
- **Originally blocked requirement IDs:** R-VER-06, R-NFR-01, R-NFR-02, R-NFR-04

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** A **request-local verifier cache only.** Within a single audit request, if the same `P_j,k` record is needed more than once it may be reused from a map; **that map is cleared when the request ends.** **No persistent cross-query LRU cache.** **A single goroutine** for the reproduction logic and **no parallel DHT lookups.** **No automatic DHT retry** (the DHT is in-process). For external Hardhat JSON-RPC calls: **5-second request timeout, no automatic retry during benchmark measurement**, and failures recorded explicitly.
- **§0.6 boundary — this is the exact and only authorized exception.** The paper's two uses of "cached `P_j,k`" (Sec. III.B, p. 4) are honoured by a request-local map and nothing more. Every other form of caching, memoization, precomputation, buffer reuse, goroutine use, and parallelism remains **prohibited** by §0.6. **This approval does NOT authorize concurrency**, which the paper never mentions.
- **Resolves:** R-VER-06 (cache scope), R-NFR-04 (concurrency: single-goroutine; retries: none; timeout: 5 s for JSON-RPC) → USER-APPROVED.

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
- **Originally blocked requirement IDs:** R-EVAL-03, R-EVAL-04, R-EVAL-05, R-EVAL-07, R-NFR-01, R-NFR-02

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** The paper's numeric performance figures are **informational reference values, NOT pass/fail criteria.**
- **Functional correctness is pass/fail** — e.g. valid proofs must accept and tampered proofs must reject.
- **Performance is reported, not asserted** — record the reproduction's own mean, 95% confidence interval, latency, throughput, and relative trends.
- The Go implementation is **not required** to reproduce the Python implementation's exact timing values.
- **§0.6 coupling:** if a figure is missed, the shortfall is **reported**, never engineered away with an unstated optimization.
- **Resolves:** R-EVAL-03, R-EVAL-04, R-EVAL-05, R-EVAL-07, R-NFR-01, R-NFR-02 (pass/fail semantics) → USER-APPROVED. See the amended Tester constraint in §0.4.1.

---

### Q35 — [LANGUAGE CONSEQUENCE] Go toolchain: language version and module identity

- **What the paper says:** Table 5 (p. 10) fixes the implementation toolchain as "Programming Language: Python 3.10". The paper states nothing about Go, because the paper's implementation is not in Go.
- **What the user has fixed:** The implementation language is Go (§0.7). Nothing further.
- **What is not specified by either:** The Go language/toolchain version; the module path; the target OS/architecture for the reproduction runs; whether CGO is permitted (which bears on which cryptographic libraries are even reachable, Q36); the repository/package layout.
- **Why a decision is required:** A Go module cannot be initialized without a version and module path, and the choice of Go version gates which standard-library and third-party APIs exist. The paper's Table 5 equivalent (Python 3.10) has no Go counterpart to translate.
- **Known candidate choices (none authorized):** any released Go version (the user must name one); module path under a chosen VCS root; CGO enabled vs. disabled (`CGO_ENABLED=0`).
- **Originally blocked requirement IDs:** R-ENV-01, R-ENV-07

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** Pin **Go 1.26** with **`CGO_ENABLED=0`**. Module path **`dhtbacked`**, until/unless the code is placed in a public repository with a canonical import path. Keep the code **cross-platform**, but **record the actual OS and architecture used for benchmark runs** in the evaluation report.
- **Coupling:** `CGO_ENABLED=0` forecloses CGO-bound cryptographic libraries, which is consistent with Q36's standard-library-only decision.
- **Resolves:** R-ENV-01 (toolchain version), R-ENV-07 (module identity) → USER-APPROVED.

### Q36 — [LANGUAGE CONSEQUENCE] Go cryptographic provider for H, AEAD, and signatures

- **What the paper says:** Table 5 (p. 10): "Crypto Library: **pycryptodome** (SHA-256, HMAC) [30]", with reference [30] pointing to the PyCryptodome PyPI package. Sec. III.B (p. 4) requires a collision-resistant H with λ ∈ {256, 384, …}; Sec. IV.A (p. 7) requires an IND-CPA + INT-CTXT AEAD and EUF-CMA signatures.
- **What the paper does not specify / what Go invalidates:** `pycryptodome` is a Python C-extension package and is unusable from Go. The paper names **no** Go cryptographic library, and cannot, since it predates this decision. Separately, the paper never named a concrete AEAD (Q1) or signature scheme (Q3, Q4) in *any* language — so those remain open independently of this question.
- **Why a decision is required:** Every hash, encryption, and signature operation in the system needs a concrete Go implementation. The SHA-256 **algorithm** is fixed by Table 5 (R-CRY-01) and is unaffected by the language change; the **provider** of that algorithm is not. Different providers also differ in API shape, which affects how AD is passed to the AEAD (Q7) and how signatures are encoded (Q3).
- **Known candidate choices (none authorized):** Go standard library (`crypto/sha256`, `crypto/aes` + `crypto/cipher` AEAD interfaces, `crypto/ecdsa`, `crypto/ed25519`, `crypto/rsa`); `golang.org/x/crypto` (e.g. `chacha20poly1305`); a third-party or CGO-bound library. **I have deliberately not selected among these, including the standard library** — §0.7 approved the language only, and the prohibited-assumptions list covers concrete AEAD ciphers, signature algorithms, and key sizes regardless of which language they are written in.
- **Relationship to existing questions:** Q1 (which AEAD), Q3 (which producer signature), Q4 (which CA signature) ask *what algorithm*; this question asks *which Go implementation provides it*. Both halves must be answered.
- **Originally blocked requirement IDs:** R-ENV-02, R-CRY-01 (provider only — the SHA-256 algorithm choice is unaffected), R-ENC-02, R-ATT-02, R-AUTH-03

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** Use the **Go standard library** for all core cryptography: **`crypto/sha256`, `crypto/aes`, `crypto/cipher`, `crypto/ed25519`, `crypto/rand`**. Thus **H = SHA-256**, **AEAD = AES-256-GCM**, and **both producer and CA signatures are Ed25519**. **No CGO cryptographic dependency.**
- **Consistency check (passed):** aligns with Q1 (AES-256-GCM), Q3 (producer Ed25519), Q4 (CA Ed25519), Q35 (`CGO_ENABLED=0`), and R-CRY-01 (SHA-256 algorithm, already PAPER-DERIVED from Table 5).
- **Resolves:** R-ENV-02, R-CRY-01 (provider), R-ENC-02, R-ATT-02, R-AUTH-03 → USER-APPROVED.

### Q37 — [LANGUAGE CONSEQUENCE] Merkle-tree and bit-manipulation implementation in Go (supersedes Q33)

- **What the paper says:** Table 5 (p. 10): "Data Structures: `merkletools` (**modified**), `bitarray`". The paper never states what the modification to `merkletools` was.
- **What the paper does not specify / what Go invalidates:** Both named packages are Python-only. The paper names no Go equivalent. The original undocumented-modification problem (Q33) is now compounded: any third-party Go Merkle library carries its own conventions for node ordering, odd-node/duplicate-node handling, byte vs. hex digest representation, and — critically — **none of them implement the paper's domain-separation tags** tag_L = 0x00 and tag_I = 0x01 (Sec. III.B, p. 4). Silently adopting a library's conventions would override the paper's Eq. (2), (3) and its padding rule.
- **Why a decision is required:** The MK-Tree is the core artifact of the paper. Whether it is hand-implemented directly from Eq. (2)/(3) or built on a library determines whether the paper's stated hashing convention survives contact with the code. It is also unclear what role `bitarray` played in the original — the paper never explains it — so its Go counterpart, if any, cannot be inferred.
- **Known candidate choices (none authorized):** implement the MK-Tree directly from Eq. (2)/(3)/(4) with no third-party Merkle dependency; adopt a Go Merkle library and override its hashing to match the paper; some other approach. For `bitarray`: omit entirely (its purpose is unstated), or select a Go bitset package.
- **Note on §0.6:** whichever option is chosen, the implementation must follow the paper's stated procedure literally. Structural shortcuts that produce the same root faster (incremental recomputation beyond the paper's stated "only affected paths", precomputed subtree tables, etc.) are optimizations and are prohibited unless the paper states them.
- **Originally blocked requirement IDs:** R-ENV-03, R-MKT-08

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** **Implement the MK-Tree directly in Go from the paper's equations**, with **no third-party Merkle library**. Implement the domain-separated leaf/internal hashes, the padding rule, node indexing, and ancestor checkpoint selection explicitly. **No Go bitset dependency**, because the paper never explains what `bitarray` contributed.
- **Why this is the fidelity-preserving choice:** it removes the risk that a library's own conventions (odd-node handling, hex vs. byte digests, absent domain-separation tags) silently override Eq. (2), (3) and the padding rule.
- **§0.6 coupling:** the direct implementation must follow the paper's stated procedure literally. Precomputed subtree tables, structural shortcuts, or incremental recomputation beyond the paper's stated "only affected paths" are prohibited.
- **Resolves:** R-ENV-03, R-MKT-08 → USER-APPROVED.

### Q38 — [LANGUAGE CONSEQUENCE] Does the Hardhat / Solidity blockchain toolchain survive the language change, and how does Go reach it?

- **What the paper says:** Table 5 (p. 10): "Blockchain Simulator: **Hardhat v2.20 (EVM); Solidity 0.8.20**." Sec. III.A item 7 (p. 4): the root-of-roots is "appended to a smart contract".
- **Analysis of survival (stated, not decided):** Hardhat and Solidity are a **separate toolchain from the implementation language**. Hardhat is a Node.js-based EVM development environment; Solidity is the contract language. Neither is a Python library, so neither is invalidated by the Python→Go switch in the way `pycryptodome` and `merkletools` are. On that reasoning the Hardhat v2.20 / Solidity 0.8.20 specification **can** survive unchanged, and I have left R-ENV-05 classified PAPER-DERIVED.
- **What is genuinely unclear and must be decided:** How the **Go** application communicates with the Hardhat EVM node. The paper's Python implementation would have used a Python Ethereum client; no such client is named in Table 5, and no Go client is named anywhere. A JSON-RPC client, contract bindings, and transaction-signing keys are all required and none is specified. It is also unstated whether running a Node.js toolchain alongside a Go implementation is acceptable to the user, or whether an all-Go simulated chain is preferred instead.
- **Why a decision is required:** R-EPO-03/R-EPO-04 (blockchain commit and smart contract) cannot be implemented without a client mechanism, and R-VER-05 (anchor reconciliation) cannot read the anchor back.
- **Known candidate choices (none authorized):** keep Hardhat v2.20 + Solidity 0.8.20 per Table 5 and reach it from Go via a JSON-RPC client / generated contract bindings; keep the contract but substitute an all-Go EVM harness; replace the EVM simulation entirely with an in-process append-only anchor store. The first preserves Table 5 fidelity; the others deviate from it and would need the same explicit override that §0.7 gave the language.
- **Originally blocked requirement IDs:** R-ENV-05 (client mechanism only — the Hardhat/Solidity versions remain PAPER-DERIVED), R-EPO-03, R-EPO-04, R-VER-05

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** **Keep Hardhat v2.20 and Solidity 0.8.20** as specified by Table 5. Run **Hardhat as the local EVM process**. The Go application connects **over local JSON-RPC** using **a Go Ethereum client and ABI/generated contract bindings** for `commit(epoch, root)` and `getRoot(epoch)`. **Do not replace the blockchain with an in-memory Go map** in the fidelity reproduction.
- **Confirms:** R-ENV-05 stays PAPER-DERIVED — Table 5's blockchain toolchain survives the Go switch intact.
- **Resolves:** the transport mechanism (local JSON-RPC) → USER-APPROVED.
- **~~Residual gap~~ — RESOLVED BY Q42:** `github.com/ethereum/go-ethereum` with `ethclient` and `abigen`-generated bindings; RPC `http://127.0.0.1:8545`; chain ID `31337` verified at startup; committer key and contract address supplied by environment (Q46); normal gas estimation.

### Q39 — [LANGUAGE CONSEQUENCE] Benchmarking and statistics tooling in Go

- **What the paper says:** Table 5 (p. 10): "Benchmarking Tools: `time.perf_counter`, NumPy stats". Sec. V.C (p. 9): "Each experiment was repeated 50 times, and we report the mean values with 95% confidence intervals."
- **What the paper does not specify / what Go invalidates:** `time.perf_counter` is a Python standard-library function and NumPy is a Python package; neither exists in Go. The paper names no Go timing or statistics facility. The **methodology** — 50 repetitions, mean, 95% confidence interval — is paper-derived and unaffected; only the tooling is invalidated.
- **Why a decision is required:** The evaluation requirements (R-EVAL-02 through R-EVAL-06) mandate a specific measurement protocol that must be implemented somehow. Timer choice also affects measurement resolution, which matters at the paper's 35 µs single-record figure.
- **Known candidate choices (none authorized):** Go standard library `time` (monotonic clock) with hand-computed statistics; Go's built-in `testing.B` benchmark harness; a third-party statistics package. Note that `testing.B`'s adaptive iteration model differs from the paper's fixed 50-repetition protocol and would change the reported methodology.
- **Note on §0.6:** benchmark scaffolding must not introduce warm-up caching, buffer reuse, or parallelism into the measured code path; doing so would be an unstated optimization contaminating the measurement.
- **Originally blocked requirement IDs:** R-ENV-04, R-EVAL-02, R-EVAL-03, R-EVAL-04, R-EVAL-05, R-EVAL-06

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** Use Go's **monotonic clock via `time.Now()` / `time.Since()`** for benchmark timing. **Do not use `testing.B` as the primary reproduction benchmark**, because its adaptive iteration model differs from the paper's fixed protocol. Use **exactly 50 independent repetitions**, reporting the **mean with a 95% confidence interval using the Student-t critical value for 49 degrees of freedom**. Compute statistics in **ordinary Go code**.
- **§0.6 coupling (explicit):** benchmark scaffolding must **not** introduce warm-up caching, buffer reuse, goroutine pools, or parallelism into the measured code path.
- **Preserves the paper's methodology:** 50 runs and 95% CI (Sec. V.C, p. 9) are unchanged; only the tooling is substituted.
- **Resolves:** R-ENV-04 (tooling), R-EVAL-02 → USER-APPROVED.

---

## 1A. NEW OPEN QUESTIONS — RESIDUAL GAPS (Q40–Q49)

**These are gaps that the Q1–Q39 answers imply but do not settle.** Several arise from interactions *between* answers that were individually complete. None may be resolved by any agent. Each blocks the requirement IDs listed. **These are why the verdict is still NOT READY.**

---

### Q40 — [GAP] Authorization token validity duration and clock source

- **What the adopted decisions say:** Q13 defines the token body as `(version, auditorID, epoch, allowedTypes, expiresAt)` and requires the DHT to enforce `now < expiresAt`.
- **What is not specified:** **How long a token is valid** — no duration, no default, no rule for computing `expiresAt` at issuance. Also unspecified: the clock source for `now` (system wall clock vs. injected clock), and behaviour on clock skew. The paper itself never mentions expiry at all, so it offers nothing here.
- **Why a decision is required:** The CA cannot issue a token without setting `expiresAt`, and the DHT check `now < expiresAt` is untestable without a known validity window. A deterministic/injectable clock is also needed for the Tester to exercise the expiry path at all.
- **Known candidate choices (none authorized):** a fixed duration (minutes / hours / one epoch's wall-clock span); expiry tied to epoch close rather than wall time; an injectable clock for tests vs. system clock in production.
- **Originally blocked requirement IDs:** R-AUTH-02, R-AUTH-03

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** **Fixed token validity of 1 hour from issuance.** At issuance, `expiresAt = clock.Now().UTC() + 1h`, serialized as **Unix seconds (`int64`)** in the signed token body. Normal execution uses the **system UTC wall clock**; tests use an **injectable `Clock` interface** so expiry is testable deterministically without sleeping or altering the system clock. **No clock-skew grace period**: valid only while `now < expiresAt`; rejected at or after `expiresAt`. Epoch and allowed-type checks remain mandatory.
- **Note on §0.6:** the injectable `Clock` is a testability seam, not an optimization, and is permitted.
- **Resolves:** R-AUTH-02, R-AUTH-03 (validity duration and clock source) → USER-APPROVED.

### Q41 — [CONFLICT BETWEEN ADOPTED DECISIONS] Q13's policy enforcement is not implementable against Q11's opaque DHT keys

- **What the adopted decisions say:** Q11 makes every DHT key an opaque SHA-256 digest — `Key_P = H("P" ‖ treeID ‖ j ‖ k ‖ seq)`, where `treeID = H("TREE" ‖ epoch ‖ logType)` is itself a hash. Q13 requires the DHT, on every proof request, to "require the requested **epoch** to match the token epoch" and "require the requested **type** to be permitted".
- **The conflict:** A DHT node handed only an opaque key **cannot recover the epoch or the log type from it** — that is the point of a hash. As specified, checks (ii) and (iii) of Q13 cannot be performed. The two answers are individually coherent and jointly unimplementable.
- **What is not specified:** Any **request envelope** carrying `epoch` and `logType` (and possibly `treeID`) alongside the key, and — critically — how the DHT verifies that a claimed envelope actually corresponds to the requested key rather than being spoofed by the caller to pass the policy check.
- **Why a decision is required:** Auditor authorization (Phase 5(a), a required paper function) cannot be built or tested until this is settled. Note that a naive envelope is trivially forgeable: a caller could claim a permitted `logType` while requesting a key belonging to a forbidden one.
- **Known candidate choices (none authorized):** request envelope `(epoch, logType, key)` with the DHT recomputing `treeID` and rejecting keys that do not derive from the claimed pair (requires storing `j, k, seq` in the envelope too); storing epoch/type as plaintext metadata beside each DHT record and checking that; abandoning checks (ii)/(iii) and reverting to the paper's literal signature-only check.
- **Originally blocked requirement IDs:** R-AUTH-03, R-AUTH-04, R-DHT-03

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** **A typed request envelope, with the DHT recomputing the expected key before serving.** Each proof request carries the authorization token plus the clear structural fields needed to derive the requested key:

| Object | Envelope |
|---|---|
| Parent node | `(epoch, logType, j, k, key)` |
| Ancestor certificate | `(epoch, logType, s, k, key)` |
| Root record | `(epoch, logType, key)` |
| Micro-root | `(epoch, logType, windowID, key)` |

The DHT then: verifies the CA signature; checks request epoch **equals** token epoch; checks `logType` ∈ `allowedTypes`; recomputes `treeID = H("TREE" ‖ epoch ‖ logType)`; **recomputes the expected object key using the Q45 rule**; and **rejects if the supplied key does not match**. This closes the spoofing hole — a caller cannot claim an authorized type while requesting a key belonging to another type.
- **CONSISTENCY CHECK vs. Q45 (passed).** Key derivation here uses the **Q45** rule, not the superseded Q11 rule. Each envelope is **sufficient** to recompute its key: `(epoch, logType)` → `treeID`; then `j, k` → `Key_P`; `s, k` → `Key_C`; nothing further → `Key_R`; `windowID` → `Key_M`. **No envelope carries `seq`, and none needs to** — `seq` is no longer in any key. Had the Q11 rule remained, every envelope would additionally have required `seq`, which the verifier could not know.
- **Resolves:** R-AUTH-03, R-AUTH-04, R-DHT-03 → USER-APPROVED.

### Q42 — [GAP] Concrete Go Ethereum client, binding generator, and EVM account configuration

- **What the adopted decisions say:** Q38 keeps Hardhat v2.20 / Solidity 0.8.20 and connects "over local JSON-RPC using **a Go Ethereum client and ABI/generated contract bindings**". Q14 specifies "one **configured authorized committer**".
- **What is not specified:** The concrete client library; the binding generator; the committer's EVM account and private key; the chain ID; the RPC endpoint; the contract deployment procedure and how the deployed address reaches the Go application; gas configuration.
- **Why a decision is required:** R-EPO-03/R-EPO-04 cannot commit and R-VER-05 cannot read the anchor back without these. The prohibited-assumptions list covers library choice and key material regardless of language.
- **Known candidate choices (none authorized):** `github.com/ethereum/go-ethereum` (`ethclient`) with `abigen`-generated bindings; a hand-written JSON-RPC client with manual ABI encoding; another Go Ethereum library. Account: a Hardhat default development account vs. a generated key.
- **Originally blocked requirement IDs:** R-ENV-05, R-EPO-03, R-EPO-04, R-VER-05

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** Keep **Hardhat v2.20 / Solidity 0.8.20**. Use **`github.com/ethereum/go-ethereum`**, **`ethclient`** for JSON-RPC, and **`abigen`-generated Go bindings** for the anchoring contract. Local Hardhat development chain at **`http://127.0.0.1:8545`**, **chain ID `31337`**, committer = **the first configured Hardhat development account**. The private key is **not hard-coded**; it is supplied via **`DHT_EVM_PRIVATE_KEY_HEX`**. The contract is deployed by a **Hardhat deployment script before the Go application starts**, and its address supplied via **`DHT_CONTRACT_ADDRESS`**. The Go process **verifies chain ID 31337**, loads the generated binding, and calls `commit(epoch, root)`. **Normal client gas estimation**, not fixed gas values. Verification reads use `getRoot(epoch)`.
- **CONSISTENCY CHECK vs. Q46 (passed).** Chain ID `31337`, RPC `http://127.0.0.1:8545`, `DHT_EVM_PRIVATE_KEY_HEX`, and `DHT_CONTRACT_ADDRESS` are stated identically in both answers. Q46 additionally makes the RPC URL and chain ID overridable via `DHT_ETH_RPC_URL` and `DHT_CHAIN_ID` with exactly these values as defaults — consistent, not conflicting.
- **Resolves:** R-ENV-05 (client mechanism), R-EPO-03, R-EPO-04, R-VER-05 → USER-APPROVED.

### Q43 — [GAP] PostgreSQL version, column types, and indexes

- **What the adopted decisions say:** Q27 selects PostgreSQL, lists eleven columns, and makes `record_id` the primary key.
- **What is not specified:** The PostgreSQL version; the SQL column types (notably whether `c`, `t`, `AD`, `d`, `sigma`, `producer_public_key` are `bytea` or encoded text, and the type of `timestamp` and `record_id`); any index beyond the primary key; the Go database driver.
- **Why a decision is required:** Batch verification (R-VER-07) and epoch-level operations retrieve records **by epoch and log type**, which under a PK-only schema is a full table scan at 10⁵ records — a materially different performance profile from the paper's. Adding an index is an optimization and is therefore forbidden by §0.6 unless explicitly approved here. Column types also affect byte fidelity: a text encoding of `c` would not round-trip raw AEAD output.
- **Known candidate choices (none authorized):** a named PostgreSQL version; `bytea` vs. `text`/`bytea` mixes; index on `(epoch, log_type)` vs. no secondary index; `pgx` vs. `lib/pq` vs. `database/sql` with either.
- **Originally blocked requirement IDs:** R-ING-04, R-ING-05, R-SYS-04, R-VER-07

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** **PostgreSQL 17**, Go driver **`github.com/jackc/pgx/v5`**, using **one ordinary `pgx.Conn`** — no pool, no concurrent database access. Column types:

```sql
record_id            TEXT   PRIMARY KEY
epoch                BIGINT NOT NULL
log_type             TEXT   NOT NULL
leaf_index           BIGINT NOT NULL
producer_id          TEXT   NOT NULL
timestamp_ns         BIGINT NOT NULL
c                    BYTEA  NOT NULL
t                    BYTEA  NOT NULL
ad                   BYTEA  NOT NULL
d                    BYTEA  NOT NULL
sigma                BYTEA  NOT NULL
producer_public_key  BYTEA  NOT NULL
UNIQUE (epoch, log_type, leaf_index)
```
Exactly **one** additional B-tree constraint is authorized — `UNIQUE (epoch, log_type, leaf_index)` — enforcing the Q44 leaf-position invariant and supporting retrieval by tree and leaf position. **No other secondary indexes.** Cryptographic byte strings remain raw `BYTEA`, never hex or base64 text.
- **§0.6:** this index is a **named, bounded carve-out** — see §0.6 "NAMED EXCEPTION 1". It is not license to add further indexes; the `(epoch, log_type)` index discussed in the question was **not** approved.
- **Supersedes part of Q27:** this schema extends Q27's eleven fields with `leaf_index`, and fixes the names `timestamp_ns` and `ad`. Where the two differ, **Q43 governs**.
- **CONSISTENCY CHECK vs. Q44 (passed):** `leaf_index BIGINT NOT NULL` is present and is the column Q44 requires.
- **Type note (recorded, not blocking):** Q44 specifies `leaf_index` as `uint64`; PostgreSQL `BIGINT` is signed `int64`. At the evaluated scale (≤ 10⁵ leaves) the ranges are trivially compatible. The Go representation is `uint64` and is converted to `int64` at the database boundary.
- **§0.6 note on `pgx.Conn`:** a single connection with no pooling is required; adding a pool would be an unstated optimization.
- **Resolves:** R-ING-04, R-SYS-04, R-VER-07 → USER-APPROVED.

### Q44 — [GAP] How a record is resolved to its MK-Tree leaf index and treeID

- **What the adopted decisions say:** Q24 assigns leaf positions implicitly — "each arriving real digest replaces the next dummy" position. Q27's PostgreSQL schema stores `record_id`, `epoch`, `log_type` and the record bytes, but **no leaf index**. Q11 derives `treeID` from `(epoch, logType)`.
- **What is not specified:** Where the mapping **`record_id → leaf index i`** is stored, and how a verifier obtains it. Phase 4 verification (p. 6) begins from a specific record and must recompute the path from **its** leaf position; without the index, the verifier does not know which siblings to fetch or which ancestor `A_s,k` covers it. Nothing in any answer records this value.
- **Why a decision is required:** Single-record verification (R-VER-01 … R-VER-05) is unimplementable without it. It also determines whether `[i_min, i_max]` in an ancestor certificate (Q17) can be used for lookup at all.
- **Known candidate choices (none authorized):** add a `leaf_index` column to the PostgreSQL schema; derive the index from an ingestion-order sequence per `(epoch, log_type)`; maintain a separate index structure; include the leaf index in the proof request.
- **Originally blocked requirement IDs:** R-ING-05, R-VER-01, R-VER-02, R-VER-03, R-MKT-06

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** **Add a `leaf_index` field to the PostgreSQL record schema.** For every `(epoch, log_type)` tree, assign leaf positions with a **zero-based `uint64` counter in accepted-ingestion order** (`0, 1, 2, …`). **Persist it with the record and never change it after admission.** The verifier resolves a record by `record_id`, reads `(epoch, log_type, leaf_index)`, derives `treeID = H("TREE" ‖ epoch ‖ logType)`, and uses `leaf_index` to determine sibling positions and the covering ancestor checkpoint. **Uniqueness of `(epoch, log_type, leaf_index)` is enforced in PostgreSQL.** No separate locator structure is needed.
- **Confirms R-MKT-06 (LOGICALLY NECESSARY):** "accepted-ingestion order" is exactly the leaf ordering entailed by Eq. (1)'s indexed sequence and Theorem E.1's reorder-detection claim. The two now agree explicitly.
- **Consistency with Q24 (passed):** "the next dummy position" is now given a concrete, persisted identity — the counter value.
- **Consistency with Q49 (passed):** `windowID = ⌊leaf_index / W⌋` uses this same zero-based index.
- **Resolves:** R-ING-05, R-VER-01, R-VER-02, R-VER-03, R-MKT-06 (leaf resolution) → USER-APPROVED.

### Q45 — [CONFLICT BETWEEN ADOPTED DECISIONS] `seq` is part of the DHT key but changes on every update

- **What the adopted decisions say:** Q11 puts `seq` inside the lookup key — `Key_P = H("P" ‖ treeID ‖ j ‖ k ‖ seq)`, `Key_C = H("C" ‖ treeID ‖ s ‖ k ‖ seq)`, `Key_R = H("R" ‖ treeID ‖ seq)`. Q23 makes `seq` a **global** counter incremented **once per accepted log-state update**. Q24 has each insert rewrite every node along one root-to-leaf path.
- **The conflict:** Within a single epoch tree of 1,024 leaves, a node near the root is rewritten on essentially every insert, each time under a **different `seq`** and therefore a **different DHT key**. A verifier that wants the current `P_j,k` cannot compute its key, because it does not know which `seq` last touched that node. The DHT also accumulates ~`L` records per insert rather than one per node, which changes storage behaviour and the `DHT_ℓ` lookup-cost model in Table 4.
- **What is not specified:** Whether keys should omit `seq`; whether `seq` should be per-tree and only advanced at finalization; whether the verifier looks up a "latest" pointer; or whether only the final post-epoch state is published to the DHT.
- **Why a decision is required:** DHT retrieval — the core of Phase 4 — is unimplementable as currently specified. This is the most serious residual gap.
- **Known candidate choices (none authorized):** drop `seq` from `Key_P`/`Key_C` and let a write overwrite the previous version; publish DHT records only once per epoch at finalization, with a single `seq`; keep `seq` in the key and add a separate "current seq" index; make `seq` per-tree and increment once per epoch.
- **Originally blocked requirement IDs:** R-DHT-03, R-DHT-04, R-DHT-05, R-DHT-06, R-VER-03, R-EPO-02

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8) — ⚠️ THIS SUPERSEDES PART OF Q11:** **Stable DHT lookup keys; `seq` becomes version metadata only.** The Round 1 key rule is revised to:
```
Key_P = H("P" ‖ treeID ‖ j ‖ k)
Key_C = H("C" ‖ treeID ‖ s ‖ k)
Key_R = H("R" ‖ treeID)
Key_M = H("M" ‖ treeID ‖ windowID)
```
`seq` remains a **global monotonically increasing commit/update number stored inside** `P`, `C`, `R`, and micro-root records, but is **not part of any lookup key**. A newer write to the same logical object **overwrites the previous value at the same DHT key**. Because `treeID = H("TREE" ‖ epoch ‖ logType)`, records from different epochs or log types still occupy distinct keys. **After an epoch is finalized, its DHT records are no longer modified.**
- **Effect:** Phase 4 lookup becomes deterministic without a separate latest-sequence index, while `seq` is preserved for freshness/version checks. The DHT now holds one record per logical object rather than accumulating ~`L` records per insert, which also restores Table 4's `DHT_ℓ` single-lookup cost model.
- **Supersession is recorded in three places:** §0.9 (notice), the Q11 decision (struck through), and here. **The Q11 key rule must not be implemented.**
- **Retained from Q11 unchanged:** `treeID`, `ancestorID`, `Key_M`, and the uint64-big-endian encoding rule.
- **Retained from Q23 unchanged:** `seq` is still global, still starts at 1, still increments once per accepted log-state update. Only its role in key construction is removed.
- **Resolves:** R-DHT-03, R-DHT-04, R-DHT-05, R-DHT-06, R-VER-03, R-EPO-02 → USER-APPROVED.
- **Residual gap:** the decision fixes immutability only *after* finalization; mid-epoch overwrite behaviour and whether verification against an unfinalized tree is in scope are not settled — see **Q52**.

### Q46 — [GAP] Configuration and deployment surface

- **What the adopted decisions say:** Q2 loads the AEAD key "from configuration or an environment variable". Q27 uses an external PostgreSQL. Q38 uses an external Hardhat process. Q4 configures `pk_CA` into the DHT verifier "before execution". Q28 keeps everything else in one Go process.
- **What is not specified:** The configuration mechanism (file format, env var names, flags); the AEAD key variable name and encoding; the PostgreSQL connection string; the Hardhat RPC endpoint; where CA and producer key material lives on disk; how the producer registry (Q5) is loaded; startup order and failure behaviour when a dependency is unavailable.
- **Why a decision is required:** The system has three external dependencies and several pieces of key material, none of which has a defined provisioning path. Nothing can be run end-to-end without this.
- **Known candidate choices (none authorized):** environment variables; a YAML/TOML/JSON config file; command-line flags; a mixture.
- **Originally blocked requirement IDs:** R-KEY-01, R-KEY-02, R-KEY-03, R-SYS-04, R-ENV-05

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** **Environment variables as the primary configuration surface**, with key/registry files referenced by filesystem path. Required variables:

| Variable | Meaning |
|---|---|
| `DHT_AEAD_KEY_HEX` | exactly 32 bytes as 64 hex characters |
| `DHT_POSTGRES_DSN` | PostgreSQL connection string |
| `DHT_ETH_RPC_URL` | default `http://127.0.0.1:8545` |
| `DHT_CHAIN_ID` | default `31337` |
| `DHT_CONTRACT_ADDRESS` | deployed anchoring contract |
| `DHT_EVM_PRIVATE_KEY_HEX` | committer key |
| `DHT_CA_PRIVATE_KEY_PATH` | CA signing key |
| `DHT_CA_PUBLIC_KEY_PATH` | `pk_CA` for the DHT verifier |
| `DHT_PRODUCER_REGISTRY_PATH` | producer registry file |
| `DHT_PRODUCER_KEY_DIR` | producer key material |

The producer registry is **deterministic JSON** mapping each `producer_id` to its registered Ed25519 public key or public-key file. **Private key files must not be committed.**
**Startup order:** PostgreSQL → Hardhat node → contract deployment → set contract address → load CA/producer keys and registry → start Go process.
**Fail fast** on missing or invalid configuration, unavailable PostgreSQL or Hardhat, wrong chain ID, invalid key lengths, missing registry, or invalid contract address. **The program must never silently generate replacement secrets.**
- **CONSISTENCY CHECK vs. Q42 (passed):** RPC endpoint, chain ID, and both EVM variable names match Q42 exactly; Q46 adds override variables with those values as defaults.
- **Consistency vs. Q2 (passed):** `DHT_AEAD_KEY_HEX` at 32 bytes matches Q2's single system-wide 256-bit key.
- **Consistency vs. Q5/Q47 (passed):** `DHT_PRODUCER_REGISTRY_PATH` is the provisioning path for the Q5 registry that Q47 populates.
- **Resolves:** R-KEY-01, R-KEY-02, R-KEY-03, R-SYS-04, R-ENV-05 (provisioning) → USER-APPROVED.

### Q47 — [GAP] Producer identities and registry population for the evaluation dataset

- **What the adopted decisions say:** Q5 requires a pre-registered `ProducerID → pk_P` registry and rejection of unregistered keys. Q6 makes `producer_id` a canonical record field. Q7 makes `producerID` the first field of `AD`. Q29 specifies five **log types** and the record schema.
- **What is not specified:** **How many producers exist**, their identifier values, how their Ed25519 key pairs are generated and registered for the evaluation runs, and the relationship (if any) between producer identity and log type — the paper's Sec. III.A item 1 lists producer *kinds* (applications, gateways, WAF, IDS, databases) that resemble but do not match Q29's five log types.
- **Why a decision is required:** `producerID` is hashed into `AD` and therefore into every leaf digest, so the producer set changes every root. The ingestion gate rejects unregistered producers, so an unpopulated registry rejects the entire dataset.
- **Known candidate choices (none authorized):** one producer per log type (5); a fixed pool of N producers assigned randomly under seed 42; a single producer for all records.
- **Originally blocked requirement IDs:** R-SYS-01, R-ING-03, R-KEY-02, R-EVAL-01, R-EVAL-08

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** **Five fixed producers, one per evaluation log type:** `producer-access`, `producer-error`, `producer-auth`, `producer-api`, `producer-security`. Each has **one persistent Ed25519 key pair**, generated once during test-environment setup with `crypto/rand`, persisted in the configured key directory (`DHT_PRODUCER_KEY_DIR`), and reused across runs. Public keys are loaded into the pre-registered `ProducerID → pk_P` registry **before ingestion**. The synthetic generator **deterministically assigns each log to the producer corresponding to its `log_type`**. These are **development/evaluation keys only**; private keys must not be committed.
- **Consistency with Q29 (passed):** the five producers map one-to-one onto the five log types, so the Q29 type distributions (Uniform, Zipf-heavy, Bursty) also determine the producer distribution.
- **Observation (recorded, not a conflict):** because `producer_id` is a bijection of `log_type`, the `producerID` field in `AD` (Q7) is functionally redundant with `logType`. Both are retained exactly as specified; no field is dropped.
- **Consistency with the paper (noted):** Sec. III.A item 1's producer *kinds* (applications, gateways, WAF, IDS, databases) are **not** reproduced as such. The five type-aligned producers are a USER-APPROVED substitute.
- **Resolves:** R-SYS-01, R-ING-03, R-KEY-02 (registry population), R-EVAL-01 → USER-APPROVED.

### Q48 — [GAP] Synthetic record body content and how the 256–512 byte size is reached

- **What the adopted decisions say:** Q6 includes `message` and `attributes` in the canonical schema. Q29 specifies 256–512 byte records, seed 42, and the type distributions.
- **What is not specified:** What `message` and `attributes` actually contain; how record size is driven into the 256–512 byte band; whether `attributes` is a flat or nested object, and what keys it holds; whether size is measured before or after canonicalization.
- **Why a decision is required:** R-EVAL-08 requires byte-for-byte determinism, and R-EVAL-01 requires the size band to be met. Record content determines plaintext length, hence ciphertext length, hence `d`, hence every root. Two generators with different `message` content produce entirely different anchors from the same seed.
- **Known candidate choices (none authorized):** fixed per-type message templates with seeded substitutions; random printable padding to a seeded target length; a fixed `attributes` key set with seeded values.
- **Originally blocked requirement IDs:** R-EVAL-01, R-EVAL-02, R-EVAL-08, R-CAN-02

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** Deterministic synthetic JSON records. **The 256–512 byte band is measured on the canonicalized plaintext `M`, before AEAD encryption.** Fixed schema `record_id, producer_id, log_type, timestamp, message, attributes`. **Per-type message prefixes:** `access event`, `error event`, `authentication event`, `api event`, `security event`. **Fixed flat `attributes` object** with keys `host, source_ip, status, request_id, padding`. All non-padding values are generated deterministically from **PRNG seed 42**. For each record, a deterministic target size is chosen uniformly in **[256, 512]** inclusive; the record is canonicalized, the byte deficit computed, and `attributes.padding` filled with **deterministic ASCII alphanumeric bytes** until the RFC 8785-style canonical UTF-8 representation is **exactly** the target size. Re-canonicalize and **verify the final length before encryption**. **Fail generation** if a base record already exceeds its selected target — never silently truncate fields.
- **TERMINATION CHECK (passed — the padding loop is well-defined).** ASCII alphanumeric characters require no JSON escaping and JSON strings carry no length prefix, so **each appended padding character increases the canonical byte length by exactly 1**. The deficit therefore decreases by exactly 1 per character and reaches 0 without overshoot. Key ordering is unaffected by padding length (`host, padding, request_id, source_ip, status` under RFC 8785 sorting), so re-canonicalization cannot change the size relationship. The only non-convergent case — a negative deficit — is explicitly handled by failing generation.
- **Resolves:** R-CAN-02, R-EVAL-01 (size band and measurement point) → USER-APPROVED.
- **~~Residual gaps~~ — RESOLVED BY Q50/Q51, and the resolution simplifies this decision.** `record_id` is a zero-padded **12-character** decimal counter (Q50); `timestamp` is a fixed 9-digit-fraction RFC 3339 string, **always exactly 30 characters** (Q51).
- **INTERACTION — both new values are FIXED-WIDTH, which makes the padding deficit uniform.** `record_id` and `timestamp` now contribute a **constant number of bytes to the canonical record length for every record in the dataset**. The earlier termination analysis had to reason about these as variable-length contributors; that variable is now removed. The per-record deficit therefore varies only with `message` and the non-padding `attributes` values, and the deficit computation is uniform across the dataset.
- **Padding is still required.** Fixed-width identifiers do not make records uniform in length — `message` and the seeded `attributes` values still vary — so `attributes.padding` remains the mechanism that reaches the selected target size. The termination argument above is unaffected and still holds.

### Q49 — [GAP] `windowID` definition and encoding

- **What the adopted decisions say:** Q11 defines `Key_M = H("M" ‖ treeID ‖ windowID)` and requires numeric indices to be encoded as unsigned 64-bit big-endian. Q22 puts `windowID` in the micro-root record `(treeID, windowID, i_min, i_max, R^(Δ)_τ, seq)`.
- **What is not specified:** What `windowID` **is** — a zero-based counter within the epoch, a global counter, a timestamp-derived value, or a string. Its type is never stated, so the Q11 numeric-encoding rule cannot be applied with confidence, and the micro-root record cannot be serialized deterministically (Q12).
- **Why a decision is required:** `Key_M` is a hash over `windowID`; an undefined type makes the key undefined, and micro-root publication (R-RTV-02) unimplementable.
- **Known candidate choices (none authorized):** zero-based `uint64` counter per `(epoch, log_type)`; global `uint64` counter; derived from `i_min / windowSize`.
- **Originally blocked requirement IDs:** R-RTV-01, R-RTV-02, R-DHT-03

► **ADOPTED DECISION — USER-APPROVED (2026-09-10, §0.8):** **`windowID` is a zero-based `uint64` counter local to each `(epoch, log_type)` tree.** For tumbling window size `W` and zero-based `leaf_index i`, **`windowID = ⌊i / W⌋`**. Leaves `[0, W−1]` belong to window 0, `[W, 2W−1]` to window 1, and so on. `windowID` is encoded as an **unsigned 64-bit big-endian integer** everywhere it appears in DHT keys or record serialization.
- **CONSISTENCY CHECK vs. Q21 and Q22 (passed):** `⌊i / W⌋` is precisely count-based, non-overlapping tumbling-window membership — each leaf belongs to exactly one window, so Eq. (29)'s consolidation has no double-counting. It also makes Q22's micro-root record fields consistent: `i_min = windowID · W` and `i_max = i_min + W − 1`, both derivable from `windowID`, and it satisfies Q12's determinism requirement via the fixed uint64 encoding.
- **CONSISTENCY CHECK vs. Q44 (passed):** the `i` used here is exactly Q44's persisted zero-based `leaf_index`.
- **Resolves:** R-DHT-03 (`Key_M`), `windowID` typing → USER-APPROVED.
- **Residual gap:** at the **default** configuration (epoch 1,000 padded to 1,024; `W` = 1,024) there is exactly **one window per epoch**, and the final window of any epoch is generally **incomplete**. Neither the handling of a partial window nor whether dummy padding positions participate in Eq. (28) is settled — see **Q53**.

---

## 1B. NEW OPEN QUESTIONS — ROUND 3 RESIDUAL GAPS (Q50–Q53)

**Gaps that the Round 2 answers imply, expose, or leave open.** As before, no agent may resolve these. Each blocks the requirement IDs listed. **These are why the verdict is still NOT READY.**

---

### Q50 — [GAP] How is `record_id` generated?

- **What the adopted decisions say:** Q6 and Q48 place `record_id` in the canonical record schema. Q43 types it `TEXT PRIMARY KEY`. Q44 resolves records by `record_id`.
- **What is not specified:** **How the value is produced.** No answer defines a format, generator, or uniqueness scope.
- **Why a decision is required:** `record_id` is a field of the canonical record `M`, so it is inside the AEAD plaintext and therefore determines `c`, then `d`, then every Merkle node, then the on-chain anchor. Two generators producing different `record_id` values yield **entirely different roots from the same seed**, which defeats R-EVAL-08 determinism and makes the dataset non-reproducible. A random UUID would make every run produce a different anchor.
- **Known candidate choices (none authorized):** deterministic from seed 42 and the record ordinal (e.g. `rec-<epoch>-<logtype>-<n>`); a seeded UUIDv4 stream; a hash of the record's other fields; a globally increasing counter.
- **Originally blocked requirement IDs:** R-CAN-02, R-EVAL-01, R-EVAL-02, R-EVAL-08

► **ADOPTED DECISION — USER-APPROVED (answered interactively by the user on 2026-09-11, §0.8):** `record_id` is a **global increasing counter across the whole run**, independent of epoch and log type, rendered as a **zero-padded 12-digit decimal string, zero-based**. The first record is `"000000000000"`, the second `"000000000001"`, and so on. **Every record's `record_id` is exactly 12 characters wide.**
- **Requirement — deterministic ingestion order.** Reproducibility of the dataset depends on the ingestion order being **deterministic and fixed** across runs. The generator must emit records in a fixed order under seed 42, and the counter must advance in that order. A run that reorders generation produces different `record_id` values, hence different `M`, hence different roots. This is a binding requirement, not a note.
- **Zero-based is deliberate**, matching `leaf_index` (Q44) and `windowID` (Q49). All three index families are zero-based; none is 1-based.
- **`record_id` is NOT `leaf_index`.** `record_id` is **global** across the run; `leaf_index` is **per `(epoch, log_type)`**. They coincide only in the degenerate single-type single-epoch case and must never be conflated or derived from one another.
- **Satisfies Q43:** globally unique, so `record_id TEXT PRIMARY KEY` holds without further qualification.
- **Resolves:** R-CAN-02, R-EVAL-01, R-EVAL-02, R-EVAL-08 (`record_id`) → USER-APPROVED.

### Q51 — [GAP] Canonical string form of `timestamp` inside `M`, and the conversion rule between the three representations

- **What the adopted decisions say:** The same instant is represented **three ways**:
  1. **Q6** — a `timestamp` field in the canonical JSON record `M`. Q6 says only "normalize timestamps to UTC". **No string format is stated.**
  2. **Q7** — inside `AD`, as a **big-endian 64-bit Unix timestamp in nanoseconds**.
  3. **Q43** — in PostgreSQL, as **`timestamp_ns BIGINT`**.
- **Consistency status:** representations **2 and 3 agree** (both signed/unsigned 64-bit nanosecond counts; see the Q43 type note). Representation **1 is undefined** — RFC 3339, RFC 3339 with nanosecond precision, epoch nanoseconds as a JSON number, and epoch seconds as a JSON number are all consistent with "normalize to UTC" and all produce **different canonical bytes**.
- **Why a decision is required:** the `timestamp` field is inside `M`, hence inside the AEAD plaintext, hence determines every digest and every root. Additionally, no answer states the **conversion rule** between form 1 and forms 2/3, so a round-trip could lose precision (e.g. RFC 3339 seconds precision vs. nanosecond storage) and cause `AD` and `M` to disagree about the same instant.
- **Known candidate choices (none authorized):** RFC 3339 with nanosecond precision (`RFC3339Nano`) and a fixed trailing-zero policy; RFC 3339 with fixed 9-digit fractional seconds; a JSON number of epoch nanoseconds; a JSON number of epoch seconds. Plus an explicit statement that forms 1, 2 and 3 are derived from one canonical nanosecond value.
- **Originally blocked requirement IDs:** R-CAN-01, R-CAN-02, R-EVAL-08

► **ADOPTED DECISION — USER-APPROVED (answered interactively by the user on 2026-09-11, §0.8):** The `timestamp` field inside `M` is **RFC 3339 with a fixed 9-digit fractional-second field**, UTC, `Z` suffix, **trailing zeros preserved** — e.g. `"2026-09-11T04:30:00.500000000Z"`. **Always exactly 30 characters.**
- **Explicitly NOT `time.RFC3339Nano`.** Go's `time.RFC3339Nano` **suppresses trailing zeros**, so the same instant would serialize to different byte lengths depending on its value — which would change `M`, hence `c`, hence `d`, hence every root. The implementation must format with a **fixed 9-digit fraction**, i.e. a layout of the form `2006-01-02T15:04:05.000000000Z` (Go's `.000000000` preserves trailing zeros; `.999999999` suppresses them and must not be used).
- **CONVERSION RULE (user-endorsed, binding):** **all three representations derive from one canonical `int64` nanosecond value.**

| Form | Where | Representation |
|---|---|---|
| 1 | `M` (canonical JSON) | this fixed-width RFC 3339 string, UTC, 9-digit fraction |
| 2 | `AD` (Q7) | big-endian `uint64` Unix nanoseconds |
| 3 | PostgreSQL (Q43) | `timestamp_ns BIGINT` |

  Forms 1, 2 and 3 are **derived from the same canonical nanosecond integer**, never from each other by re-parsing. **No conversion can lose precision, and `M` and `AD` can never disagree about an instant.** The 9-digit fraction is exactly nanosecond resolution, so form 1 is lossless with respect to the canonical value.
- **CONSISTENCY CHECK (now passes):** the timestamp check that previously failed is resolved. Forms 2 and 3 already agreed; form 1 is now pinned and the derivation rule closes the gap.
- **Resolves:** R-CAN-01, R-CAN-02, R-EVAL-08 (timestamp canonical form and conversion) → USER-APPROVED.

### Q52 — [GAP] Is verification against an unfinalized (open) epoch in scope, and what is the mid-epoch DHT consistency rule?

- **What the adopted decisions say:** Q45 states that "after an epoch is finalized, its DHT records are no longer modified", and that a newer write **overwrites** at the same key. Q24 has every insert rewrite a full root-to-leaf path. Q22 says a DHT-only micro-root is "provisional until the corresponding records are incorporated into an epoch MK-Tree that is externally anchored".
- **What is not specified:** Whether a verifier may audit a record in an **open** epoch, and if so, what consistency guarantee applies. Mid-epoch, `P_j,k` values along the current path are overwritten continuously, and the per-type root `R^(e)_τ` is not yet anchored, so there is no on-chain value for Phase 4 step 4 to reconcile against. A verifier reading `P_j,k` records mid-epoch could observe a **torn state** — some nodes from before an insert, some after — and fail verification for a record that is in fact correctly stored.
- **Why a decision is required:** it determines whether Phase 4 has a precondition ("epoch must be finalized") and whether the Tester must exercise mid-epoch verification at all. Left open, the Developer would have to invent a consistency rule — precisely what is forbidden.
- **Known candidate choices (none authorized):** restrict verification to finalized epochs and make that an explicit precondition; permit mid-epoch verification against micro-roots only, per Q22's provisional semantics; define a snapshot/versioning rule using the in-record `seq`; publish DHT records only at finalization.
- **Originally blocked requirement IDs:** R-RTV-04, and a scope precondition on R-VER-01 … R-VER-05

► **ADOPTED DECISION — USER-APPROVED (answered interactively by the user on 2026-09-11, §0.8):** **Micro-roots only, mid-epoch.**
- **Explicit precondition on R-VER-01 … R-VER-05:** full MK-Tree **inclusion and batch verification require a finalized, anchored epoch**. A verifier must reject or refuse a Phase 4 verification request against an epoch that is still open. This is a **stated precondition**, not an implementation detail.
- **During an open epoch**, a record may be checked **only against its micro-root**, per Q22's "provisional until anchored" semantics.
- **A provisional mid-epoch result is explicitly NOT backed by the blockchain anchor.** The Tester must **not** treat it as equivalent to an anchored verification, and must not report it as satisfying R-SEC-04/R-SEC-05/R-SEC-06, all of which terminate at the anchor.
- **Torn-read hazard is foreclosed, not merely unlikely.** The DHT is in-process and synchronous with a single goroutine (Q10, Q32), and §0.6 forbids introducing concurrency. Ingestion and verification therefore cannot interleave, so a verifier cannot observe a partially-updated tree or a mid-update micro-root.
- **Resolves:** R-RTV-04, and converts the Q52 scope caveat on R-VER-01 … R-VER-05 into an explicit, testable precondition → USER-APPROVED.

### Q53 — [GAP] Incomplete final micro-root window, and whether dummy padding positions participate

- **What the adopted decisions say:** Q21 sets count-based tumbling windows, default `W` = 1,024, evaluated at 256/512/1,024/2,048. Q49 defines `windowID = ⌊i / W⌋`. Q24 pre-sizes the tree with dummy leaves that real digests progressively replace. Eq. (28) defines `R^(Δ)_τ = H(d_i1 ‖ … ‖ d_ik)` over record digests.
- **What is not specified:**
  1. **Partial windows.** An epoch rarely fills its final window — at the **default** configuration (epoch 1,000, `W` = 1,024) there is exactly **one window and it is never full**. Whether a micro-root is emitted for a partial window, deferred until the window fills, or emitted at epoch close over `k < W` digests is undefined.
  2. **Dummy participation.** Whether the `H(0x00 ‖ 0x00)` dummy-leaf digests occupying unfilled positions are included in Eq. (28)'s concatenation, or only real record digests are.
- **Why a decision is required:** these change the value of `R^(Δ)_τ` and therefore what `Key_M` addresses. At default settings the real-time path is degenerate — one window covering the whole epoch — which also means the "near real-time" behaviour of Phase 5(b) cannot be demonstrated without deciding this.
- **Known candidate choices (none authorized):** emit a micro-root only when a window is full and none for a trailing partial window; emit at epoch close over the real digests present; include dummies so every window has exactly `W` inputs; make the default `W` smaller than the default epoch so multiple windows exist.
- **Originally blocked requirement IDs:** R-RTV-01, R-RTV-02, R-EVAL-06 (window-sensitivity experiment only)

► **ADOPTED DECISION (Q53a — partial windows) — USER-APPROVED (answered interactively by the user on 2026-09-11, §0.8):** **Emit and update as the window fills.** A partial window emits a **provisional micro-root**, which is **republished at the same `Key_M`** each time the window grows, under Q45's overwrite semantics. **The micro-root value for a window therefore changes over time until the window closes.** Once the window is full (or the epoch finalizes), its value is final.

► **ADOPTED DECISION (Q53b — dummy participation) — USER-APPROVED (answered interactively by the user on 2026-09-11, §0.8):** **Real digests only.** Eq. (28) concatenates **only digests of actual records**. The `H(0x00 ‖ 0x00)` dummy padding leaves (Q9, Q24) **never participate in a micro-root**. A window's micro-root covers **k real records, k ≤ W**. **The micro-root value does not depend on tree padding.**
- **Consequence:** the micro-root path and the MK-Tree path treat padding differently — the MK-Tree includes dummies (Q24 pre-sizing), micro-roots exclude them. This is intentional and follows Eq. (28)'s wording, which concatenates record digests `d_i`, not leaf positions. The two paths were already separate artifacts under Q16/Q22.

**INTERACTION ANALYSIS — Q52 + Q53a (checked; no residual gap, no Q55).**
The concern is that mid-epoch verification targets a micro-root that is itself republished on every insert. Three failure modes were examined:
1. **Verifier reads a micro-root mid-update.** *Foreclosed.* The DHT is in-process, synchronous, single-goroutine (Q10, Q32), and §0.6 bars adding concurrency. Ingestion and verification cannot interleave, so no torn read is possible.
2. **Verifier fetches a micro-root that does not yet cover the record under audit.** *Cannot arise.* Insertion and republication occur within the same synchronous ingestion step, so any record readable from PostgreSQL (i.e. already accepted) is already covered by the current micro-root at its `Key_M`. There is no window in which an accepted record is uncovered.
3. **Verifier cannot tell which k digests the current value covers.** *Resolved by the record itself.* Q22's micro-root record carries `[i_min, i_max]`. Under Q53a these are **`i_min = windowID · W`** (fixed for the window) and **`i_max = i_min + k − 1`** (advancing with each republication). The verifier reads `i_max` and knows exactly which real leaf indices the current value covers, and can confirm its record's `leaf_index` falls within that range before accepting the result.
- **`i_max` advances on each republication — LOGICALLY NECESSARY.** This introduces no design choice: a record that states a range it does not cover would simply be false, and R-DHT-06 requires every DHT item to be locally rehashable and reconcilable. `i_max` must equal the range actually hashed into `R^(Δ)_τ`. Recorded as entailed, not chosen.

**INTERACTION ANALYSIS — Q53b + Q12/Q22 (checked; serialization remains deterministic).**
The micro-root record `(treeID, windowID, i_min, i_max, R^(Δ)_τ, seq)` is composed entirely of **fixed-width fields** under Q12 — a 32-byte `treeID`, three `uint64` big-endian values, a 32-byte digest, and a `uint64` `seq`. **`k` is not a serialized field**; it is implied by `i_max − i_min + 1`. Varying `k` therefore changes field *values* but never field *widths* or ordering, so the record serializes deterministically for every `k`, satisfying Q12 and R-EVAL-08. Eq. (28) itself is a raw concatenation of `k` 32-byte digests in leaf-index order, which is deterministic given the ordered digest list.

- **Resolves:** R-RTV-01, R-RTV-02, R-EVAL-06 (window handling) → USER-APPROVED.

---

### Q54 — [GAP] Default aggregation window size, given Q53a and the default epoch size

- **Context:** Q21 set the default aggregation window to **W = 1,024** and the sensitivity sweep to 256 / 512 / 1,024 / 2,048. Q23 set the default epoch to **1,000 logs**, padded to 1,024 positions. Q49 defines `windowID = ⌊i / W⌋`. Q53a makes partial windows emit continuously-updated provisional micro-roots.
- **The issue raised:** at the default configuration the epoch holds 1,000 real records and W = 1,024, so there is **exactly one window per epoch (`windowID` always 0), and it never fills.**
- **Why a decision was required:** whether to keep W = 1,024 (accepting a single perpetually-partial window at defaults) or reduce it so multi-window behaviour is exercised by default.

► **ADOPTED DECISION — USER-APPROVED (answered interactively by the user on 2026-09-11, §0.8):** **Keep W = 1,024** as the Q21 default. The user selected this **knowing the consequence**.
- **Recorded consequence — this is expected behaviour, NOT a defect.** At the default configuration (epoch 1,000, W = 1,024), the system produces **exactly one window per epoch, `windowID = 0`, which never fills**, and therefore — combined with Q53a — **one continuously-updated provisional micro-root per epoch**, republished at the same `Key_M` on every insert and finalized at epoch close with `k = 1,000`, `i_min = 0`, `i_max = 999`.
- **The Tester must not report this as a fault.** A single micro-root per epoch at default settings is the specified outcome.
- **Multi-window behaviour remains observable**, and should be exercised, in:
  - the **sensitivity runs at W = 256 and W = 512** (giving 4 and 2 windows respectively over a 1,000-record epoch), and
  - the **larger epoch sizes 10⁴ and 10⁵** (Table 5), where W = 1,024 yields ~10 and ~98 windows.
- **Resolves:** R-RTV-01, R-RTV-02, R-EVAL-06 (default window size) → USER-APPROVED.

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

### 2.3 Explicitly out of scope — future work referenced by the adopted answers

Two of the Q1–Q39 answers mention work that lies **beyond this reproduction**. Recorded here so the Developer does not build them:

1. **Baselines for a later HMF-Audit comparison (from Q31).** The answer states that "DELIA and BLAS should be implemented separately from their own primary papers" for a later HMF-Audit comparison. **That is not part of this reproduction.** This project implements only the DHT-backed proposed system. Building DELIA or BLAS here would also require reading external papers, which the paper-only rule forbids.
2. **HMF-vs-DHT experiments with controlled RTT (from Q10).** The answer notes that "in later HMF-vs-DHT experiments, controlled RTT can be applied separately and equally to comparable remote operations." **That is not part of this reproduction.** The DHT here is in-process and synchronous with **no routing-delay simulation** (Q10). No RTT injection, network emulation, or latency modelling is to be implemented.

Neither item has any requirement ID in this SRS. If either is wanted later it needs its own specification and its own scope decision.

---

## 3. REQUIREMENTS

Notation follows Table 1 of the paper (p. 5).

### 3.0 CLASSIFICATION UPDATE — THIS TABLE GOVERNS

The individual entries in §3.1–§3.14 were written **before any question was answered**, and most still carry stale markers such as "BLOCKED — USER PERMISSION REQUIRED" and "Blocked by: Qn". **Those markers are historical. This table governs.** The §3 entries are retained for their requirement text and paper traceability, which are unchanged and remain authoritative for *what* each requirement says.

> ⚠️ **DEVELOPER — READ THIS BEFORE §3.1.** As of the Round 3 answers (2026-09-11) **no requirement is blocked**. If a §3.1–§3.14 entry says "BLOCKED" or "Blocked by: Qn", that text is superseded by the row for that ID in the table below. Every question Q1–Q54 has been answered. Do not skip a requirement because its old entry header still says BLOCKED.

Statuses: **PD** = PAPER-DERIVED · **LN** = LOGICALLY NECESSARY · **UA** = USER-APPROVED · **BLK** = BLOCKED — USER PERMISSION REQUIRED.

| ID | Status | Basis / still blocked by |
|---|---|---|
**Round 2 update (Q40–Q49 adopted):** every requirement previously blocked on Q40–Q49 is now **UA**, except where a new Q50–Q53 gap applies. Rows below are current as of this revision.

| R-SYS-01 | PD + **UA** | Q29, Q47 |
| R-SYS-02, R-SYS-03 | **UA** | Q19 |
| R-SYS-04 | PD + **UA** | Q27, Q43, Q46 |
| R-SYS-05 | PD | unchanged |
| R-SYS-06 | PD + **UA** | Q20 |
| R-SYS-07 | **UA** | Q28 |
| R-CRY-01 | PD + **UA** | SHA-256 PD (Table 5); Go provider UA via Q36 |
| R-CRY-02, R-CRY-03 | PD | unchanged |
| R-KEY-01 | **UA** | Q2, Q46 |
| R-KEY-02 | **UA** | Q3, Q5, Q46, Q47 |
| R-KEY-03 | **UA** | Q4, Q46 |
| R-CAN-01 | **UA** | Q6, Q51 |
| R-CAN-03 | **UA** | Q6 |
| R-CAN-02 | **UA** | Q6, Q48, Q50, Q51 |
| R-ENC-01 | **UA** | Q19 |
| R-ENC-02, R-ENC-04, R-ENC-05 | **UA** | Q1, Q36 |
| R-ENC-03 | **UA** | Q7 |
| R-ENC-06, R-ENC-07 | **UA** | Q2 |
| R-ATT-01 | PD + **UA** | Q8, Q19 |
| R-ATT-02, R-ATT-03 | PD + **UA** | Q3, Q8, Q36 |
| R-ING-01 | PD + **UA** | Q20 |
| R-ING-02 | PD + **UA** | Q3 |
| R-ING-03 | PD + **UA** | Q5, Q47 |
| R-ING-04 | **UA** | Q27, Q43 |
| R-ING-05 | **UA** | Q43, Q44 |
| R-ING-06 | PD | unchanged |
| R-MKT-01 | PD + **UA** | Q7, Q8 |
| R-MKT-02 | PD + **UA** | Q9, Q24 |
| R-MKT-03 | PD + **UA** | Q8 |
| R-MKT-04 | **UA** | Q16 — `R^(e)_τ = N_0,0` authoritative |
| R-MKT-05 | **UA** | Q22, Q24 |
| R-MKT-06 | LN + **UA** | leaf ordering LN; leaf index persisted per Q44 |
| R-MKT-07 | PD + **UA** | Q24 |
| R-MKT-08 | **UA** | Q37 |
| R-ANC-01 | PD + **UA** | Q25 — `s = 8` default, root at `j = 0` |
| R-ANC-02 | **UA** | Q17 — `[i_min, i_max]` |
| R-ANC-03 | PD + **UA** | Q18 |
| R-ANC-04 | PD + **UA** | Q24 |
| R-ANC-05 | **UA** | Q4 — certificates are **not** signed |
| R-DHT-01, R-DHT-02 | **UA** | Q10 |
| R-DHT-03 | **UA** | Q11 (as amended), **Q45 key rule**, Q41, Q49 |
| R-DHT-04 | PD + **UA** | Q18, Q24, **Q45 key rule** |
| R-DHT-05 | PD + **UA** | Q17, Q26, **Q45 key rule** |
| R-DHT-06 | PD + **UA** | Q12, Q23, **Q45** (`seq` = in-record metadata) |
| R-DHT-07 | PD | unchanged — still binding under the simulated DHT |
| R-EPO-01 | PD + **UA** | Q8, Q26 — lexicographic type order |
| R-EPO-02 | **UA** | Q23, Q45 |
| R-EPO-03, R-EPO-04 | **UA** | Q14, Q15, Q38, Q42 |
| R-EPO-05 | PD + **UA** | Q14, Q15 |
| R-VER-01 | PD + **UA** | Q44, Q52. **Precondition: finalized, anchored epoch.** |
| R-VER-02, R-VER-03 | **UA** | Q18, Q44, Q45, Q52. **Same precondition.** |
| R-VER-04 | PD + **UA** | Q16, Q52. **Same precondition.** |
| R-VER-05 | PD + **UA** | Q26, Q42, Q52. **Same precondition.** |
| R-VER-06 | LN + **UA** | Q32 — request-local cache only |
| R-VER-07 | PD + **UA** | reuse PD; retrieval via Q43's unique constraint |
| R-VER-08, R-VER-09, R-VER-10 | PD | unchanged |
| R-AUTH-01 | **UA** | Q4 |
| R-AUTH-02 | **UA** | Q13, Q40 |
| R-AUTH-03 | **UA** | Q13, Q36, Q40, Q41 |
| R-AUTH-04 | PD + **UA** | Q13, Q41 |
| R-AUTH-05 | **UA** | Q13 |
| R-RTV-01 | PD + **UA** | Q8, Q21, Q49, Q53a, Q53b, Q54 |
| R-RTV-02 | **UA** | Q22, Q49, Q53a, Q53b, Q54 |
| R-RTV-03 | **UA** | Q16 — Eq. (29) not reproduced as the per-type root |
| R-RTV-04 | PD + **UA** | Q22, Q34, Q52 — provisional, not anchor-backed |
| R-SEC-01 … R-SEC-07 | PD | unchanged; now testable given the Q1–Q39 primitives |
| R-ENV-01 | **UA** | §0.7 (Go), Q35 |
| R-ENV-02 | **UA** | Q36 |
| R-ENV-03 | **UA** | Q37 |
| R-ENV-04 | PD + **UA** | methodology PD; tooling UA via Q39 |
| R-ENV-05 | PD + **UA** | Hardhat/Solidity PD; transport UA via Q38, Q42, Q46 |
| R-ENV-06 | PD | unchanged; no GPU use |
| R-ENV-07 | **UA** | Q35 |
| R-EVAL-01 | PD + **UA** | Q29, Q47, Q48, Q50 |
| R-EVAL-02 | PD + **UA** | Q29, Q39, Q48, Q50 |
| R-EVAL-03, R-EVAL-04, R-EVAL-05, R-EVAL-07 | PD + **UA** | Q34 — informational, not pass/fail; batch sizes Q30 |
| R-EVAL-06 | PD + **UA** | Q21, Q25, Q53a, Q53b, Q54 |
| R-EVAL-08 | LN + **UA** | Q6, Q7, Q8, Q12, Q47, Q48, Q50, Q51 |
| R-EVAL-09 | PD + **UA** | Q1 |
| R-EVAL-10, R-EVAL-11 | **UA** | Q31 — proposed system only; baselines not reproduced |
| R-NFR-01, R-NFR-02 | **UA** | Q34 — reported, not asserted |
| R-NFR-03 | **UA** | Q28 |
| R-NFR-04 | **UA** | Q32 — single-goroutine, no retry, 5 s JSON-RPC timeout |
| R-NFR-05 | PD | unchanged |
| R-NFR-06 | **UA** | §0.6 |

**Round 3 update (Q50–Q54 answered interactively, 2026-09-11): NO ROW IN THIS TABLE IS BLOCKED.** All 93 requirements are now PAPER-DERIVED, LOGICALLY NECESSARY, USER-APPROVED, or a combination. The Developer is cleared to implement the whole specification.

**Developer instruction:** the rule that a **BLK** requirement must not be implemented still stands as policy — there are simply no BLK rows left. If development reveals a new unspecified decision, stop work on that portion and return the question to the user (§0.3). Do not resolve it, and do not treat this table's completeness as licence to fill a newly discovered gap.

**R-VER-01 … R-VER-05 carry an explicit precondition (Q52):** full MK-Tree inclusion and batch verification require a **finalized, anchored** epoch. Verification against an open epoch must be refused; mid-epoch checks are micro-root-only and provisional.

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

Counts are over the 93 distinct requirement IDs, per the §3.0 table. A requirement carrying more than one status is counted once in each applicable line, so the figures sum to more than 93.

- **PAPER-DERIVED (fully or partly):** 42
- **LOGICALLY NECESSARY:** 4 (R-MKT-06, R-VER-06, R-EVAL-08, and the `i_max` advancement rule recorded under Q53a)
- **USER-APPROVED (fully or partly):** **80** — the 53 decisions of Q1–Q54 (§0.8), plus R-ENV-01 (Go, §0.7) and R-NFR-06 (no unstated optimization, §0.6)
- **BLOCKED — USER PERMISSION REQUIRED:** **0**
- **Implementable now:** **93 of 93**

**Change log for this revision (Round 2 application)**
- Extended §0.8 to cover Round 2 provenance: the Q40–Q49 decisions were drafted as proposals in `DHT_Open_Questions_Round2_Answered.md` and adopted by explicit user instruction on 2026-09-10. All ten are USER-APPROVED. No new category.
- **Added §0.9 supersession notice.** Q45 revises the Q11 DHT key rule: `seq` is removed from `Key_P`, `Key_C`, `Key_R`. The superseded rule is struck through in the Q11 record itself so it cannot be implemented from §1, and is cross-referenced in both directions. R-DHT-03/04/05/06 rows in §3.0 now cite the Q45 rule.
- **Added §0.6 NAMED EXCEPTION 1:** `UNIQUE (epoch, log_type, leaf_index)` is the single authorized database constraint, bounded so it cannot be read as general license to add indexes, and flagged to the Tester as expected. It doubles as the enforcement mechanism for Q44's leaf-position invariant.
- Recorded all ten Round 2 decisions beneath their original question text in §1A.
- Added §1B with four new questions Q50–Q53.
- Updated §3.0 for every requirement previously blocked on Q40–Q49.

**Deliberate non-reproductions, recorded so they are not mistaken for defects**
- Eq. (29) no longer defines `R^(e)_τ` (Q16); the micro-root path is auxiliary and provisional.
- Sec. V.C.5's causal explanation of the stride trend is not reproduced (Q25); Theorem G.1's formula is.
- Fig. 2's `Prev Hash` field is not implemented (Q23).
- Phase 5(b)'s "sliding" window is implemented as tumbling (Q21).
- Phase 5(a)'s signature-only token check is strengthened to four checks (Q13).
- `c` carries a 12-byte nonce prefix that the paper's `c` does not (Q1).
- Baseline Schemes [8], [12], [16], [26] are not implemented (Q31).

**Change log — Round 3 (2026-09-11)**
- Extended §0.8 with Round 3 provenance, recorded **distinctly**: Q50–Q54 were answered **interactively by the user**, not drafted by an assistant and subsequently adopted. Both routes are valid approvals; the audit trail keeps them separate.
- Recorded the Q50–Q54 decisions in §1B, including Q53a/Q53b and the new Q54.
- Recorded two interaction analyses in full: **Q52 + Q53a** (three failure modes examined, all foreclosed — no Q55 raised) and **Q53b + Q12/Q22** (micro-root serialization remains deterministic for every `k`).
- Recorded the **Q50 + Q51 + Q48** fixed-width interaction: `record_id` (12 chars) and `timestamp` (30 chars) now contribute constant length, making the padding deficit uniform.
- Added the Q52 finalized-epoch precondition to R-VER-01…R-VER-05 and to the Tester constraint (§0.4.1), along with the Q54 single-window expectation.
- §3.0 updated: **no BLOCKED rows remain.**

---

## VERDICT: **READY**

Every implementation-affecting decision is now covered by one of exactly three things: **the paper**, **strict logical necessity**, or **an explicit user approval**. No requirement is BLOCKED. No open question remains.

The specification passes the readiness test stated at the outset — the acceptance criteria are specific enough that someone who was not part of these three rounds could implement and test against them without further questions. In particular, the byte-level determinism chain is now closed end to end: canonicalization (Q6, Q51), `record_id` (Q50), `AD` serialization (Q7), concatenation semantics (Q8), dummy leaves (Q9), DHT record encoding (Q12), and DHT key derivation (Q45) are all pinned, so two independent implementations must produce identical roots from the same seed.

**What remains true and binding regardless of READY status:**
- §0.6 — no unstated optimization, with exactly two named exceptions: the request-local verifier cache (Q32) and the `UNIQUE (epoch, log_type, leaf_index)` constraint (Q43).
- §0.3 — if development reveals a new unspecified decision, the Developer **stops** work on that portion and returns the question to the user. READY is not licence to fill a newly discovered gap.
- §2.2 / §2.3 — HMF-Audit, the proof planner, Cassandra, DELIA/BLAS baselines, and HMF-vs-DHT RTT experiments are all out of scope.
- The deliberate non-reproductions listed above must not be tested as expected paper behaviour.

**Status:** ready for dev — 93 of 93 requirements implementable; 0 blocked
