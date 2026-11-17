# BLAS Reproduction in Go: Requirements Specification

**Document status:** DECISIONS APPLIED (round 2), 2026-09-12. All 52 gaps ruled and all 5 addenda settled by the project owner; the rulings are recorded in **`DECISIONS.md`**, which is normative for everything the paper leaves unspecified. §16 below retains each gap's analysis (why it exists, what it affects) and carries the ruling in §16.0. Three residual details (R-1, R-2, R-3 in `DECISIONS.md`) sit inside already-decided rulings and need one line of specification each before the affected component is coded. Four knowing deviations from the paper's printed text (D-1 … D-4) are listed in `DECISIONS.md` and must be disclosed in any write-up.
**Source of truth:** `BLAS_A_Blockchain-Enabled_Efficient_and_Verifiable_Log_Audit_System_With_Hybrid_Storage.pdf` (this directory).
**Target language:** Go. Only the implementation language changes; the paper's implementation is C++ (Sec. VIII, "Implementation").

**Version check (done against the PDF header, page 1):**
- Header: "IEEE TRANSACTIONS ON DEPENDABLE AND SECURE COMPUTING, VOL. 23, NO. 4, JULY/AUGUST 2026", pages 8231–8248.
- "Received 28 January 2026; revised 11 March 2026; accepted 30 March 2026. Date of publication 6 April 2026; date of current version 9 July 2026." DOI 10.1109/TDSC.2026.3681195.
- This is the published issue version (final volume/issue/page numbers), not an early-access version or a preprint. It matches the brief (IEEE TDSC, Vol. 23, No. 4, 2026).

**How the PDF was read:** all 18 pages were loaded (text layer plus page images): body text, Algorithms 1–5, Figs. 1–7, Tables I–VI, the references, and the author biographies. The PDF contains no appendix or supplementary material. Content that exists only in figures (Fig. 2 example values, Fig. 3 field layout, Fig. 4 message labels, Fig. 5 leaf contents, Fig. 7 legend) was read from the page images and is cited as such.

**Classification tags used on every requirement:**
- `[MANDATED]` = PAPER-MANDATED: explicitly stated by the paper.
- `[PERMITS]` = PAPER-PERMITS: the paper explicitly allows alternatives.
- `[UNSPECIFIED]` = IMPLEMENTATION-NECESSARY-BUT-UNSPECIFIED: needs a decision, so it always points to a GAP.
- `[EXPERIMENT]` = EXPERIMENT-ONLY: an evaluation setting, never a protocol rule.

Where a requirement is tagged `[MANDATED]` but depends on a still-open detail, the GAP is cited inline ("see GAP-n").

## 1. PAPER SCOPE SUMMARY

### 1.1 What must be reproduced

BLAS is a log-audit system on a hybrid-storage blockchain. Raw logs are stored off-chain at a storage service provider (SP). Log-file metadata and index commitments are stored on-chain at full nodes. A light-node log auditor issues Boolean and time-range queries and verifies the soundness and completeness of the returned log entries (Sec. II-A, II-D, V-A). The reproduction consists of:

1. **IOMF (Index-Object Merkle Forest, Sec. IV, Fig. 2).** Three authenticated structures over a set of objects:
   - **OMT** (Object Merkle Tree): binds oid to object.
   - **IIMT** (Inverted Index Merkle Tree): fixed-fanout, fully balanced; one bitmap per keyword.
   - **RIMBT** (Range Index Merkle B-Tree): fixed fanout; one cumulative "value <= v" bitmap per distinct value.
   
   Also covered: the update procedures (Alg. 1, and the IV-C prose that stands in for Alg. 2), query processing (Alg. 3, 4, 5), and verification (IV-E).
2. **ALIS (Authenticated Layered Index Structure, Sec. V-C).** IOMF instantiated at three levels:
   - **Block level** (on-chain, global): log-generator IIMT, file-keyword IIMT, FileTsMin RIMBT, FileTsMax RIMBT.
   - **Transaction level** (on-chain, one per block): OMT, file-keyword IIMT, ts_min RIMBT, ts_max RIMBT.
   - **Entry level** (off-chain, one per log file): OMT combined with B+ Tree temporal information, and an entry-keyword IIMT.
3. **Three-stage verifiable query** Q = <lg, [ts, te], FK, EK>, split into Q_on and Q_off (Sec. V-D, Fig. 4), followed by three-stage verification (Sec. V-E).
4. **Both design optimizations (Sec. VI):**
   - block-level index partition (segments, plus header fields PastTsMax, BlockTsMax, BlockTsMin, plus a backward header scan by the auditor);
   - on-chain index aggregation (per-segment MHTs over same-keyword bitmaps, and over same-lid transactions with leaves BlockHeight||tsn||H(tx)).
5. **Hashing:** BLAKE2b for 256-bit hashes (Sec. VIII).
6. **Threat model and security definitions:** Sec. II-C and VII (Definitions 1 and 2, Theorems 1 and 2). These determine the accept/reject behaviour of the verifier.
7. **The Section VIII evaluation.** Kept strictly separate from the protocol requirements (Section 12 of this document).

### 1.2 What is explicitly out of scope (the paper does not require it)

- The deployment example in Sec. V-F (logger plugin, ELK-style stack, campus network). The paper calls it an example ("For example, consider..."), so it is non-normative.
- Confidentiality or encryption of log payloads, and index privacy. Sec. X ("Data privacy") treats these as orthogonal.
- Bitmap compression. Sec. IV-D and Sec. X permit it; the complexity analysis assumes uncompressed bitmaps. It is not required (see GAP-3).
- Any concrete blockchain platform, consensus implementation, network protocol, RPC/REST API, database, or container tooling. The paper specifies none of these (see GAP-23, GAP-39).
- Baseline systems (vChain+, MSTDB, MART). Whether they are in scope is GAP-44.

### 1.3 Reading of the paper's own terminology

- "IOMF maintainer" (Sec. IV) is whichever party builds and serves a given IOMF instance in ALIS:
  - entry level: built by the log generator, stored and served by the SP (V-A, V-C); see GAP-36;
  - transaction level: built by the "block generator" (V-C);
  - block level: built by the full node / "block generator" (V-C).
- "Block generator" (V-C) is not a fifth party. Sec. II-A defines exactly four parties. Treating the block generator as the full node that produces a block is an interpretation, tagged `[UNSPECIFIED]`; see GAP-23.

### 1.4 Differences between the brief's summary and the PDF (the PDF is followed)

| # | Brief says | PDF says | Handling |
|---|---|---|---|
| D-1 | Hybrid storage: "Off-chain SP: raw log files; entry keyword sets; entry-level indexes" and "On-chain: log metadata; keyword-set information; block data; root hash sets; block-level indexes; transaction-level indexes". Entry keyword sets are listed only off-chain. | V-A: "The SP manages the off-chain storage which includes the raw log data and the corresponding entry keyword sets ... The on-chain storage contains log metadata, the keyword set file, and **the entry keyword sets**." Entry keyword sets appear on both sides. V-B also puts "log file metadata" inside each off-chain log file. | PDF followed (R-FUNC-006). What "entry keyword sets" on-chain means is GAP-26. |
| D-2 | "Algorithm 2: RIMBT Construction / Update ... Algorithm 2 behavior" assumes Alg. 2 describes RIMBT insertion. | The published Alg. 2 box ("RIMBT Construction") is a verbatim copy of the Alg. 1 box: input "IIMT T"; "if ln.w ∈ o.keywords then ln.bm[oid] ← 1". The RIMBT insertion procedure is given only in the IV-C prose. | GAP-8. The prose is transcribed in R-IOMF-018..021 pending a decision. |
| D-3 | Lists "scalability configuration: block-level index size = 1024; fanout of all trees = 4" as general experiment configuration. | VIII-B, "Log generation Time" paragraph (Table IV): "For BLAS, we set the block-level index size to 1024 and the fanout of all trees to 4." The paper does not say whether these values also apply to Tables II/III/V/VI or Figs. 6/7. | Recorded as the Table IV setting (R-EVAL-013). Applicability to other experiments is GAP-42. |
| D-4 | "hash: BLAKE2b-256", "BLAKE2b with 256-bit output". | VIII: "Blake2b is used for 256-bit hash computations." The paper does not state the parameterisation (digest-length parameter 32 vs. a truncated 64-byte digest, keyed or unkeyed). | GAP-1. |
| D-5 | Ablation variants "All, No-aggre, No-multi-win". | Text (VIII-C) uses All / No-aggre / No-multi-win. The Fig. 7 legend reads "all", "no part", "no aggr". | Text naming used; the mismatch is recorded in GAP-45. |
| D-6 | Stage 3: "auditor sends lg/fids and Q_off to SP". | Text (V-D): "log auditor sends SP fids of those log files with Q_off. SP locates log files based on lg and fids." Fig. 4 labels the message "(4) lids, Q_off". | Matches the text; the text-vs-figure conflict is GAP-33. |
| D-7 | Data model m_i = <lid, fid, [ts_min, ts_max], FW, bm_EW, RH> presented as the only metadata form. | II-B gives exactly that. The experiment (VIII, "Dataset") represents metadata as <lid, [timestamp_min, timestamp_max], {log file keywords}, bitmap, IIMT root hash, OMT root hash>, with no fid. The experiment's per-entry representation <timestamp, {log entry keywords}, content> has no esn. | GAP-37. |
| D-8 | "Bloom filters unless the paper explicitly uses them for the relevant structure." | II-B: bm_EW "denotes the keyword usage record (e.g., a bitmap or Bloom filter)". The paper explicitly permits a Bloom filter for bm_EW; the experiment uses "bitmap". | Tagged `[PERMITS]` (R-FUNC-011); choice is GAP-16. |
| D-9 | "Optimization 1 ... independent block-level index per segment." | VI detailed text agrees ("set an independent block-level index for each segment"). The VI introduction says "building a separate RIMBT for each partition". | Internal inconsistency, GAP-27. |

All other brief items checked against the PDF (e_i fields, m_i fields, IIMT/RIMBT hash formulas, the V separator-set formula, the cumulative bitmap semantics, tsn from 0, lid-then-fid sorting, the interval-intersection transformation, Q/Q_on/Q_off, header fields, aggregation leaf BlockHeight||tsn||H(tx), 4096 generators, 2048 entries per file, 256 files per block, 10 random queries, the seven query types, 10–50% range widths, 2–10 keywords at 1024 blocks) match the PDF wording.

## 2. ACTORS AND TRUST MODEL

Exactly four parties (II-A, Fig. 1). No other actor may be introduced.

| Actor | Responsibilities | Stored State | Trust Assumption | Paper Source |
|---|---|---|---|---|
| Log Generator (LG, identified by lid) | Generates raw log entries. Assigns esn in chronological order of generation. Batches entries into immutable log files. Builds the per-file entry-level index (OMT over esn and entries; entry-keyword IIMT; B+ Tree temporal information combined with the OMT). Maintains its own entry-keyword set and extends it when new keywords appear. Computes the log-file metadata m_i including RH. Sends raw log data to the SP and the digest/metadata (transaction) to full nodes. | Its maintained entry-keyword set (per generator). The paper states no other persistent state. | Trusted "for recording and packaging log data, generating off-chain ADS from log-entry attributes, and sending data to the SP and full nodes". Sources may still be compromised hosts; BLAS does not address semantic truthfulness. | II-A, II-B, II-C, V-C (Entry-Level Index) |
| Blockchain Full Node | Maintains on-chain components. Validates and stores all transactions (log digests/metadata). As "block generator": sorts transactions per block by lid then fid, assigns tsn from 0, builds the transaction-level index. Maintains the block-level index (global, or per segment under Opt. 1). Fills the block header fields. Answers Q_on with transactions plus block-level and transaction-level VOs. | Full blocks (headers and bodies). The keyword set file. Log metadata. Entry keyword sets (V-A, see GAP-26). Block-level and transaction-level indexes. Root hash sets in headers. | The blockchain satisfies its security assumptions, e.g. "the proportion of malicious nodes under BFT consensus is no more than one-third". II-C does not name the full node as an untrusted query responder, but its responses are verified (V-E, VI); see GAP-35. | II-A, II-C, V-A, V-B, V-C, V-D, VI |
| Off-chain Storage Service Provider (SP) | Stores raw log data fully. Stores entry keyword sets, the entry-level index, and log-file metadata per file. Answers Q_off for the fids/lg sent by the auditor. Returns log entries plus the entry-level VO. May be centralized (e.g. cloud) or decentralized (e.g. IPFS). | Log files: entries, off-chain index, log file metadata (V-B). Entry keyword sets (V-A). | **Untrusted**: "may intentionally or unintentionally add, delete, or tamper with query results". | II-A, II-C, V-A, V-B, V-D |
| Log Auditor | A blockchain light node. Stores only block headers. Sends Q_on to full nodes and Q_off (plus selected fids) to the SP. Filters verified file metadata by bm_EW. Runs the three-stage verification. Accepts log entries "iff all verification outputs are true" (Fig. 4). Under Opt. 1, scans segment-final headers backward. | Block header information only (all headers, via light-node sync). | **Trusted**: "will faithfully follow the protocol in the query verification procedure". | II-A, II-C, V-D, V-E, VI, Fig. 4 |

### 2.1 On-chain vs. off-chain placement `[MANDATED]` (V-A, V-B, V-C, Fig. 3)

- **On-chain (full nodes):**
  - blocks (header plus body);
  - transactions carrying log-file metadata and signatures;
  - log metadata;
  - the keyword set file (records the maintained keyword universe; incrementally extensible);
  - entry keyword sets (see GAP-26);
  - block-level index and transaction-level index. Their root hash sets are "stored in the block header" (V-C).
- **Off-chain (SP):**
  - log files: entries, the entry-level index, log file metadata;
  - raw log data;
  - entry keyword sets.
  
  The entry-level root hash set "is recorded in the corresponding on-chain log file metadata" (V-C), i.e. in the RH field of m_i inside the transaction.
- **Auditor local state:** block headers only (II-A).

### 2.2 Blockchain security assumption `[MANDATED]` (II-C, VII)

- Root hash sets are "committed on-chain after consensus" (proof of Theorem 1).
- A successful attack that relies on changing committed data, or on hiding "some of the latest blocks", contradicts the blockchain security assumption (proofs of Theorems 1 and 2).
- No consensus protocol is specified. BFT with at most one-third malicious nodes is given only as an example. See GAP-23.

### 2.3 What BLAS does and does not protect `[MANDATED]` (II-C, II-D, VII, X)

- **Protects:**
  - integrity and verifiability of committed logs;
  - soundness: every returned entry satisfies Q and originates from the target log generators;
  - completeness: every valid entry is returned;
  - security against forging authenticated information (Definition 1) and against unsound or incomplete results (Definition 2), under hash collision resistance and blockchain security.
- **Does not protect:**
  - semantic truthfulness of logs produced falsely before commitment by a compromised host;
  - confidentiality of payloads or indexes (Sec. X);
  - precision of temporal semantics across generators with clock skew (II-B: "strict global clock synchronization is not required for verifiability, although clock skew may affect the semantic precision").

## 3. DATA STRUCTURES

No field widths, byte encodings, or serialization formats are defined here, because the paper defines none. Every hash input encoding is open (GAP-2, GAP-3, GAP-4).

### 3.1 LogEntry

- **Fields** `[MANDATED]` (II-B): e_i = <esn, ts, EW, V>.
  - `esn`: entry sequence number within a log file (Table I). Assigned by the log generator "in chronological order of generation" (V-C). The start value is not stated (GAP-21).
  - `ts`: timestamp. Representation and resolution are not stated (GAP-22).
  - `EW`: set of associated (entry) keywords. Finite, drawn from the generator's keyword set, which is "predefined by each log generator at a given time" and extensible (II-B).
  - `V`: log payload.
- **Invariants:**
  - `[MANDATED]` (V-C): recorded timestamps within a log file are "locally consistent with the entry generation order". The esn order is consistent with the timestamp order.
  - `[UNSPECIFIED]`: handling of equal timestamps (GAP-22).
- **Ordering:** by esn, ascending (OMT leaves are "sorted in ascending order of oid", IV-A, with oid = esn at entry level, V-C).
- **Hash:** the entry's object hash is the leaf-bound value in the entry-level OMT (IV-A). The serialization of e_i before hashing is not stated (GAP-4). The experiment's representation <timestamp, {log entry keywords}, content> omits esn (GAP-37).
- **Mutability:** immutable once packed into a log file ("immutable log files", II-B).
- **Joined query view** `[MANDATED]` (V-D): for query semantics an entry is viewed as e = <lid, t, FW, EW>, i.e. the entry together with its file's lid and FW.

### 3.2 LogFile

- **Contents** `[MANDATED]` (II-B, V-B): a batch of log entries produced by one generator, plus "the corresponding off-chain index and log file metadata".
- **Size:** unspecified by the protocol. The experiment uses 2048 entries per file (R-EVAL-003).
- **Invariants:**
  - `[MANDATED]` (II-B): immutable.
  - `[MANDATED]` (V-C): its entry-level index is independent of other files.
- **Ordering:** entries in esn order.
- **Hash:** no whole-file hash is defined. Integrity comes through the entry-level OMT/IIMT roots in RH.
- **Mutability:** immutable. Older files are never rebuilt when the keyword set grows (V-C).

### 3.3 LogFileMetadata (m_i)

- **Fields** `[MANDATED]` (II-B): m_i = <lid, fid, [ts_min, ts_max], FW, bm_EW, RH>.
  - `lid`: log generator identifier. `fid`: log file identifier. Types and uniqueness scope are not stated (GAP-20).
  - `[ts_min, ts_max]`: the interval covering the file's entry timestamps.
  - `FW`: file-level keyword set. How it is derived is not stated; in the experiment it is "determined by the log generators" (GAP-46).
  - `bm_EW`: "keyword usage record (e.g., a bitmap or Bloom filter) for entries within the file" (II-B); Table I: "Entry keywords in a log file". `[PERMITS]`: bitmap or Bloom filter. Encoding and semantics: GAP-16.
  - `RH`: the set of index root hashes of the file's entry-level index (II-B, V-C). The experiment lists "IIMT root hash, OMT root hash" (VIII).
- **Invariants:** `[MANDATED]` (V-C): the entry-level root hash set is recorded in the on-chain log file metadata.
- **Ordering inside a block:** transactions are sorted by lid, then fid (V-C). Sort direction and comparison are open (GAP-20).
- **Hash:** it is hashed as part of the transaction, H(tx). The field order and encoding are not stated (GAP-4).
- **Mutability:** immutable once committed in a block.

### 3.4 Transaction

- **Fields** (Fig. 3 image, transaction row): `TSN | Sig | LID | FID | Ts_min | Ts_max | FW | BM_EW | RH`.
  - `[MANDATED]` (V-B text): a block body "contains several transactions recording the log file metadata and corresponding signatures".
  - `[MANDATED]` (V-C): `tsn` is assigned by the block generator, starting from 0, after sorting by lid then fid.
- **Invariants:** exactly one log file's metadata per transaction. This is implied by the text: "transactions (i.e., log file metadata)" (V-C).
- **Ordering:** by tsn = position after the lid-then-fid sort.
- **Hash:**
  - `[UNSPECIFIED]`: H(tx) is used by the aggregation leaf BlockHeight||tsn||H(tx) (VI, Fig. 5) and as the object hash in the transaction-level OMT. Which fields enter H(tx) (whether TSN and Sig are included), their order, and their encoding are not stated (GAP-4).
  - Signature algorithm, signer, and keys: GAP-25.
- **Mutability:** immutable after inclusion in a block.

### 3.5 Block

- **Fields** `[MANDATED]` (V-B): a block header plus a block body. The body contains transactions: log file metadata with signatures.
- **Invariants:**
  - `[MANDATED]` (V-C): the transaction-level index of each block is independent.
  - `[EXPERIMENT]` (VIII): 256 log-file metadata per block.
- **Ordering:** blocks ordered by BlockHeight. Transactions inside the body are ordered by tsn.
- **Hash:** "PrevBlockHash records the hash value of the previous block" (V-B). The construction of a block's hash is not stated (GAP-24).
- **Mutability:** immutable after commitment.

### 3.6 BlockHeader

- **Base fields** `[MANDATED]` (V-B text; Fig. 3):
  - `BlockHeight`: "the block sequence number in the blockchain". Start value is open (GAP-21).
  - `PrevBlockHash`: hash of the previous block.
  - `FileTsMin`: minimum ts_min over all log file metadata in this block.
  - `FileTsMax`: maximum ts_max over all log file metadata in this block.
  - `TxNum`: number of transactions in this block.
  - `RH`: "the root hash set of on-chain index". This includes the transaction-level index roots of this block ("its root hash set is also stored in the block header", V-C) and the block-level index roots ("its root hash set is stored in the block header", V-C). Which roots, their order, and in which header the block-level roots appear are open (GAP-24, GAP-28).
- **Opt. 1 additional fields** `[MANDATED]` (VI, Block-Level Index Partition):
  - `PastTsMax`: "the maximum value of FileTsMax of all blocks up to the current block".
  - `BlockTsMax`: "the maximum value of FileTsMax of the blocks in the current segment".
  - `BlockTsMin`: "the minimum value of FileTsMin of the blocks in the current segment".
  - For a block that is not the last in its segment, "of the blocks in the current segment" is not defined as "so far" vs. "whole segment" (GAP-28).
- **Invariants:** FileTsMin <= FileTsMax (follows from the definitions); BlockTsMax <= PastTsMax.
- **Serialization:** not stated (GAP-24).
- **Mutability:** immutable.

### 3.7 OMT (Object Merkle Tree)

- **Fields** `[MANDATED]` (IV-A, Fig. 2): leaf nodes "store object IDs (i.e., oid) and the corresponding object hash values, sorted in ascending order of oid". Fig. 2 labels leaves "1||h1" ... "8||h8", internal nodes h9..h14, root h15.
- **Invariants:** a "modified MHT" (IV-A). Sec. III defines an MHT as a binary tree. Whether the OMT follows the binary MHT or the experiment's "fanout of all trees = 4" is GAP-5.
- **Ordering:** ascending oid.
  - Entry level: oid = esn (V-C).
  - Transaction level: oid = tsn (V-C).
- **Hash:** object hash h_i = H(o_i) (implied by "corresponding object hash values"; the serialization is GAP-4). The leaf-node hash formula and the internal-node formula are not given (GAP-5).
- **Proof:** for oid = 5 the maintainer returns 5, o5 and the path {6||h6, h12, h13}. The user reconstructs h'15 and compares it with h15 (IV-A). Non-existence can be proven "using adjacent nodes" when leaves are sorted (III).
- **Mutability:** the entry-level OMT is built per immutable file and the transaction-level OMT per immutable block. The paper describes no incremental OMT update.

### 3.8 IIMT (Inverted Index Merkle Tree)

- **Fields** `[MANDATED]` (IV-B, Fig. 2):
  - leaf = (keyword w, inverted bitmap bm, hash h = H(w||H(bm)));
  - non-leaf = hash h = H(h_c1||...||h_cF), where c1..cF are its children.
- **Invariants** `[MANDATED]`:
  - "modified, fully balanced MHT with a fixed fanout" F;
  - kid assigned "in a monotonically increasing order";
  - "the i-th bit indicates whether object i contains that keyword";
  - bitmap length = N, the number of data objects (IV-D complexity analysis: "N ... (i.e., the length of each bitmap)").
- **Ordering:** leaves in kid order ("array-based layout of leaf nodes"; Alg. 4 "n ← leaf_nodes[kid]"). Padding and incomplete-node handling: GAP-6.
- **Fig. 2 example (F = 2):** Error 11001001, Warning 00100110, PUT 10011000, GET 01000001 over o1..o8. Printed with the leftmost bit = object 1. This is a depiction, not a serialization (GAP-3).
- **Mutability:** incremental. Alg. 1 sets bit oid on the leaves whose keyword is in o.keywords, updates their hashes, and recomputes all non-leaf hashes bottom-up.

### 3.9 RIMBT (Range Index Merkle B-Tree)

- **Fields** `[MANDATED]` (IV-C, Fig. 2):
  - leaf = (numerical value v, range bitmap bm, h = H(v||H(bm)));
  - non-leaf = (V = {min(V_c2), ..., min(V_cF)}, h = H(V||h_c1||...||h_cF)).
- **Semantics** `[MANDATED]`: "The range index list maps each numerical value to the objects with an attribute value less than or equal to it". Bit i of bm(v) = 1 iff value(object i) <= v.
- **Invariants:**
  - leaves are in ascending v. This is stated through the IV-C suffix-update wording ("from leaf_nodes[x] to the last leaf so that every range threshold v >= o.attr"), the Alg. 5 neighbour definitions, and Fig. 2 (12, 15, 97, 99).
  - one leaf per distinct value.
  - fixed fanout F, "balanced index based on a B-tree structure" (IV-D).
  - The meaning of V_ci is GAP-7. Split, occupancy, and padding are GAP-6.
- **Fig. 2 example (F = 2):**
  - leaves 12: 00110010; 15: 00111010; 97: 10111110; 99: 11111111;
  - internal V labels 15, 99, root 97.
- **Mutability:** incremental insertion (IV-C prose; see GAP-8).

### 3.10 IOMF (Index-Object Merkle Forest)

- **Fields** `[MANDATED]` (IV, Fig. 2): over one object set:
  - one OMT;
  - one IIMT for keywords;
  - one RIMBT per numerical attribute (Fig. 2: "RIMBT 1 ... RIMBT k").
- **Invariants:**
  - all trees index the same objects;
  - bitmaps in the IIMT and the RIMBTs share the same oid-to-bit mapping (bit i = object i, IV-B, IV-C);
  - the committed root hashes of all trees form RH (IV-E).
- **Hashes:** see 3.7–3.9.
- **Mutability:** incremental via Alg. 1 and the RIMBT insertion procedure.

### 3.11 ALIS (Authenticated Layered Index Structure)

- **Fields** `[MANDATED]` (V-C, Fig. 3, Fig. 4):

| Level | Scope | Location | Trees | Objects (bit index) | Root hash set stored in |
|---|---|---|---|---|---|
| Block level | global (or per segment under Opt. 1) | on-chain, full node | LG IIMT (lid → BlockHeights); FK IIMT (file keyword → BlockHeights); FileTsMin RIMBT; FileTsMax RIMBT | blocks (BlockHeight) | block header (V-C). Which header: GAP-24/GAP-28 |
| Transaction level | one per block, independent | on-chain, full node | OMT (tsn, transaction); FK IIMT (file keyword → tsns); ts_min RIMBT; ts_max RIMBT | transactions (tsn) | block header of that block (V-C) |
| Entry level | one per log file, independent | off-chain, SP | OMT (esn, entry) combined with B+ Tree temporal information; entry-keyword IIMT | entries (esn) | RH of the on-chain log file metadata (V-C) |

- **Invariants:**
  - `[MANDATED]` (V-C): entry-level IIMT leaves are "always one-to-one with the log generator entry keywords"; an absent keyword has an all-zero bitmap, and its leaf is not removed.
  - `[MANDATED]` (VI): with Opt. 2, the block-level FK IIMT leaves and LG IIMT leaves additionally carry aggregated MHT roots (Fig. 5; see 3.12 and GAP-31).
- **Mutability:**
  - block level: grows with each block;
  - transaction level: fixed per block;
  - entry level: fixed per file.

### 3.12 Aggregated MHTs (Opt. 2)

- **Fields** `[MANDATED]` (VI, Fig. 5):
  - **Keyword aggregation:** per segment and per file keyword, an MHT whose leaves are "the bitmaps of the same keyword" from the blocks of the segment. Fig. 5 leaves show "bm_k | h_k". The root is "deposited into the corresponding leaf node of the file keyword IIMT in the block-level index". Fig. 5 shows that leaf as "W_i | h_i / bm_i | RH_i".
  - **Log-generator aggregation:** per segment and per lid, an MHT over that generator's transactions, "whose leaf node is BlockHeight||tsn||H(tx)". The root is deposited into the corresponding leaf of the block-level LG IIMT. Fig. 5 shows that leaf as "LG_i | h_i / bm_i | RH_i".
- **Not specified:** arity, leaf order, the leaf hash, which blocks/transactions are included, and how the root enters the IIMT leaf hash (GAP-30, GAP-31).

### 3.13 VerificationObject (VO)

- **IOMF-level VO** `[MANDATED]`:
  - IV-E: VO = <BM_K, π_K, BM_R, π_R, QR_O, π_O>.
  - Alg. 3 builds it as: (BM_K, π_K) added at line 8; (BM_SR, π_SR) added for each range sub-query SR at line 11; (QR_O, π_O) added at line 15.
  - BM_K = bitmaps bm_w of the keywords in K. π_K = the merged IIMT proof.
  - BM_SR = "the bitmaps used in the evaluation of SR" (the two boundary bitmaps, Alg. 5). π_SR = the merged RIMBT proof.
  - QR_O = final oid set. π_O = OMT proof for the returned objects.
- **ALIS VOs** `[MANDATED]` (V-D):
  - block-level VO: integrates the VOs of the LG, FK and time-range sub-queries;
  - transaction-level VO: per candidate block; integrates the LG sub-query proof (OMT, including the left and right neighbour leaves), the FK sub-query proof and the time-range sub-query proof;
  - entry-level VO: per selected file; integrates the entry-keyword and time-range sub-query VOs, including the boundary OMT leaves for ts and te.
- **Result sets:** QR (objects), QR_O (oid set), QR_K, QR_R (sub-results), QR_W, QR_SR (intermediate per-keyword and per-range results).
- **Wire encoding:** not stated (GAP-38).
- **Mutability:** produced per query. Never trusted by the verifier; every component is re-checked (IV-E).

## 4. FUNCTIONAL REQUIREMENTS

Each requirement uses the format: ID, tag, statement, source.

- **R-FUNC-001** `[MANDATED]` The system shall consist of exactly four party roles: log generators, blockchain full nodes, off-chain SP(s), and log auditors. The hybrid-storage blockchain consists of the full nodes and the SP. — II-A, Fig. 1
- **R-FUNC-002** `[MANDATED]` A log generator shall generate raw log entries e_i = <esn, ts, EW, V> and assign esn to each entry of a log file in chronological order of generation. — II-B, V-C
- **R-FUNC-003** `[MANDATED]` A log generator shall batch entries into immutable log files. For each file it shall build that file's independent entry-level index (R-ALIS-002..009) and compute m_i = <lid, fid, [ts_min, ts_max], FW, bm_EW, RH>, where RH holds the entry-level root hashes. — II-B, V-C
- **R-FUNC-004** `[MANDATED]` A log generator shall send raw log data to the SP and the digest (log file metadata, as a transaction) to the full nodes. — II-A, II-C. What exactly is sent to the SP (raw entries only, or entries plus the built index): GAP-36.
- **R-FUNC-005** `[MANDATED]` A log generator shall keep a finite, predefined entry-keyword set. When a new entry keyword appears, the generator appends it and indexes it "in subsequently generated log files". Previously committed files are not rebuilt. — II-B, V-C. Timing detail: GAP-14.
- **R-FUNC-006** `[MANDATED]` Storage placement:
  - Off-chain (SP): raw log data, entry keyword sets, the entry-level index, per-file log file metadata.
  - On-chain (full nodes): log metadata, the keyword set file, entry keyword sets, the block-level and transaction-level indexes.
  - Source: V-A, V-B, V-C. The content of the on-chain "entry keyword sets" and of the keyword set file: GAP-26.
- **R-FUNC-007** `[MANDATED]` A full node shall validate and store all transactions (containing log digests) from log generators. — II-A. What "validate" checks is not stated (GAP-25).
- **R-FUNC-008** `[MANDATED]` For each block, the block generator shall:
  - sort the block's transactions by lid, then by fid;
  - assign tsn to each, starting from 0;
  - build the block's independent transaction-level index (R-ALIS-010..017).
  
  Source: V-C. Sort direction and comparison: GAP-20. Block formation, timing, and consensus: GAP-23.
- **R-FUNC-009** `[MANDATED]` Each block header shall contain BlockHeight, PrevBlockHash, FileTsMin, FileTsMax, TxNum and RH, with the V-B definitions (see 3.6). With Opt. 1 it shall also contain PastTsMax, BlockTsMax and BlockTsMin (VI). — V-B, VI, Fig. 3. Header serialization and block hash: GAP-24.
- **R-FUNC-010** `[MANDATED]` Each block body shall contain transactions recording log file metadata and the corresponding signatures. The transaction fields are those shown in Fig. 3: TSN, Sig, LID, FID, Ts_min, Ts_max, FW, BM_EW, RH. — V-B, Fig. 3. Signature scheme: GAP-25.
- **R-FUNC-011** `[PERMITS]` bm_EW may be a bitmap or a Bloom filter (II-B: "e.g., a bitmap or Bloom filter"). `[EXPERIMENT]` The evaluation uses "bitmap" (VIII). The choice and encoding for the reproduction: GAP-16.
- **R-FUNC-012** `[MANDATED]` Full nodes shall maintain the block-level index: global without Opt. 1, per segment with Opt. 1 (VI). They shall answer Q_on with the resulting transactions plus the block-level and transaction-level VOs. — II-A, V-C, V-D
- **R-FUNC-013** `[MANDATED]` The SP shall fully store raw log data and answer Q_off for the files named by the auditor, returning the result log entries and the entry-level VO. — II-A, V-D
- **R-FUNC-014** `[PERMITS]` The SP may be centralized (e.g. cloud storage) or decentralized (e.g. IPFS). — II-A. The reproduction's SP storage backend: GAP-39.
- **R-FUNC-015** `[MANDATED]` The log auditor shall store only block header information. It shall issue Boolean and range queries, and verify the soundness and completeness of returned log entries against block headers and verified on-chain metadata. — II-A, V-E
- **R-FUNC-016** `[MANDATED]` The system shall support Boolean queries on keywords (EW, FW) and on identifiers, and range queries on timestamps (ts), combined with ∧, ∨, ¬. — II-B, II-D. The ALIS query form carries a single lg and no fid predicate (V-D); see GAP-34.
- **R-FUNC-017** `[MANDATED]` Time-range predicates shall be evaluated over the timestamps and time intervals committed in the authenticated indexes. No global clock synchronization mechanism shall be required or added. — II-B
- **R-FUNC-018** `[MANDATED]` Query processing shall follow three stages: block level (full node), transaction level (full node), entry level (SP). Verification shall follow three corresponding stages at the auditor. — V-A, V-D, V-E, Fig. 4
- **R-FUNC-019** `[PROJECT-RULE]` The implementation shall not require a specific blockchain platform, and none is selected. — II-A, V-F name no platform; the *prohibition* on selecting one comes from the project's no-extra-feature rule, not from the paper (re-tagged per addendum A-3). Resolved by GAP-23 → deterministic single-process emulator (`DECISIONS.md` E1).
- **R-FUNC-020** `[PROJECT-RULE]` No confidentiality mechanism shall be part of the reproduction. Sec. X states confidentiality is not a primary design goal and can be layered independently — which is weaker than a prohibition; the prohibition is a project rule (re-tagged per addendum A-3). — X
- **R-FUNC-021** `[MANDATED]` The deployment example of V-F (logger-side plugin, ELK-style stack, server-side plugin acting as a consensus node) is illustrative. It shall not be implemented as a requirement. — V-F ("For example")

## 5. CRYPTOGRAPHIC REQUIREMENTS

- **R-CRYPTO-001** `[MANDATED]` The cryptographic hash H(·) shall be BLAKE2b producing 256-bit digests. VIII: "Blake2b is used for 256-bit hash computations". No other hash algorithm (e.g. SHA-256) may be substituted. Exact parameterisation (digest-length parameter vs. truncation, key, salt, personalization): GAP-1.
- **R-CRYPTO-002** `[MANDATED]` H(·) is the single hash function for all authenticated structures: OMT, IIMT, RIMBT, the aggregated MHTs, and object/transaction hashes. The paper names only one H(·) (IV-B, IV-C, VI). — IV-B, IV-C, VI, VIII
- **R-CRYPTO-003** `[MANDATED]` IIMT leaf hash: h = H(w || H(bm)). Order: the keyword w first, then the digest of the bitmap. — IV-B. Encodings of w, bm, and the nested digest: GAP-2, GAP-3.
- **R-CRYPTO-004** `[MANDATED]` IIMT non-leaf hash: h = H(h_c1 || ... || h_cF), with children in order c1..cF. — IV-B. Incomplete nodes and padding: GAP-6.
- **R-CRYPTO-005** `[MANDATED]` RIMBT leaf hash: h = H(v || H(bm)). — IV-C. Encoding of v: GAP-2, GAP-22.
- **R-CRYPTO-006** `[MANDATED]` RIMBT non-leaf hash: h = H(V || h_c1 || ... || h_cF), where V = {min(V_c2), ..., min(V_cF)}. — IV-C. Meaning of V_ci and the encoding of the set V: GAP-7, GAP-2.
- **R-CRYPTO-007** `[MANDATED]` An OMT leaf binds oid to the corresponding object hash value. Leaves are sorted by ascending oid. — IV-A. `[UNSPECIFIED]` The leaf-node hash formula, the internal-node formula, arity, and odd-node handling: GAP-5. Object serialization: GAP-4.
- **R-CRYPTO-008** `[MANDATED]` The Opt. 2 log-generator aggregation MHT leaf is BlockHeight || tsn || H(tx). — VI, Fig. 5. Whether the leaf hash is H(BlockHeight||tsn||H(tx)) or the raw concatenation feeds its parent: GAP-31.
- **R-CRYPTO-009** `[MANDATED]` Child order inside every hash input shall be the tree's left-to-right order:
  - IIMT: kid order;
  - RIMBT: ascending v;
  - OMT: ascending oid.
  
  Source: IV-A, IV-B, IV-C, Alg. 4 ("leaf_nodes[kid]").
- **R-CRYPTO-010** `[UNSPECIFIED]` The byte encodings of every hash input are not defined by the paper and shall not be invented:
  - integers (oid/esn/tsn/BlockHeight/kid/lid/fid), timestamps, keyword strings, numeric values v;
  - value sets V;
  - bitmaps;
  - nested digests (raw 32 bytes vs. any textual form);
  - field framing for "||".
  
  See GAP-2, GAP-3.
- **R-CRYPTO-011** `[MANDATED]` The paper specifies no domain-separation tags, keys, or salts in any hash formula. None shall be added unless adopted through GAP-1 or GAP-2. — IV-B, IV-C (formulas as given)
- **R-CRYPTO-012** `[MANDATED]` Transactions carry signatures ("Sig", Fig. 3; "corresponding signatures", V-B). `[PROJECT-RULE]` No other signatures shall be added — the paper mentions signatures but does not forbid others; the prohibition is a project rule (re-tagged per addendum A-3). **Resolved (GAP-25 → `DECISIONS.md` E3):** the signature is an opaque byte field; the reproduction neither generates nor verifies signatures, and no algorithm is selected. Consequence accepted: R-FUNC-007 is untestable. See residual R-2 — the placeholder bytes must be fixed deterministically, since A5 puts all object fields inside `H(tx)`.
- **R-CRYPTO-013** `[MANDATED]` Each block header records PrevBlockHash, the hash of the previous block. — V-B. `[UNSPECIFIED]` Block-hash input: GAP-24.
- **R-CRYPTO-014** `[MANDATED]` The security claims rely on H being collision-resistant (Theorems 1, 2). The implementation shall use H exactly as selected under R-CRYPTO-001 for every node and leaf. Truncating digests below 256 bits is not allowed. — VII
- **R-CRYPTO-015** `[MANDATED]` No encryption of log contents and no TLS/transport-security requirements are part of BLAS. — X ("Data privacy"); the paper defines no transport security

## 6. IOMF REQUIREMENTS

### 6.1 Object Merkle Tree (OMT)

- **R-IOMF-001** `[MANDATED]` The OMT is a modified MHT. Its leaf nodes store oid and the corresponding object hash value, sorted in ascending order of oid. — IV-A, Fig. 2
- **R-IOMF-002** `[MANDATED]` The OMT guarantees object integrity and the ID-object mapping: a verified proof shows that the object was not tampered with and is bound to its oid. — IV-A
- **R-IOMF-003** `[MANDATED]` Proof generation for an oid shall return the oid, the object o_oid, and the OMT verification path (the sibling information from leaf to root). Worked example with Fig. 2 (8 leaves, binary): for oid = 5 the path is {6||h6, h12, h13}. — IV-A
- **R-IOMF-004** `[MANDATED]` Proof verification: the user reconstructs the OMT root hash h' from the received data and compares it with the valid root h. Accept iff equal. — IV-A
- **R-IOMF-005** `[MANDATED]` Because OMT leaves are sorted, non-existence and boundary evidence shall be given with adjacent leaves and their Merkle proofs. This is used at the transaction level (left/right neighbours of lg's transactions) and at the entry level (boundary leaves for ts, te). — III, V-D
- **R-IOMF-006** `[UNSPECIFIED]` Leaf hash formula, internal-node formula, arity (binary MHT vs. F = 4), odd-leaf handling, and the positional information in proofs: GAP-5. Merging of several OMT paths into π_O: GAP-10. The paper describes no incremental OMT insertion. The OMT is built over a complete, immutable object set (one file or one block).

### 6.2 Inverted Index Merkle Tree (IIMT)

- **R-IOMF-007** `[MANDATED]` The IIMT is a modified, fully balanced MHT with fixed fanout F. Leaf nodes store keywords and their inverted bitmaps. — IV-B
- **R-IOMF-008** `[MANDATED]` The IOMF maintainer determines the keyword set and assigns each keyword a kid in monotonically increasing order. Leaves are laid out as an array indexed by kid ("leaf_nodes[kid]"; "array-based layout of leaf nodes"). — IV-B, IV-D, Alg. 4. Start value of kid: GAP-13. Fig. 2 uses kid 1..4.
- **R-IOMF-009** `[MANDATED]` One bitmap per keyword. Bit i = 1 iff object i contains the keyword, else 0. Bitmap length = N, the number of data objects. — III, IV-B, IV-D. Bit numbering origin and byte layout: GAP-3.
- **R-IOMF-010** `[MANDATED]` Leaf: stores w, bm, and h = H(w||H(bm)). Non-leaf: stores h = H(h_c1||...||h_cF). — IV-B
- **R-IOMF-011** `[MANDATED]` Fanout F is a fixed configuration parameter of the tree. The paper fixes no protocol value. Fig. 2 illustrates F = 2. `[EXPERIMENT]` F = 4 ("fanout of all trees to 4", VIII-B). The IIMT shall not be hard-coded as binary. — IV-B, IV-D, VIII-B
- **R-IOMF-012** `[MANDATED]` **Algorithm 1 (IIMT Construction/Update), normative form:**
  - *Inputs:* data object o (with o.keywords), data object ID oid, IIMT T.
  - *Output:* updated T (new root hash).
  - *Preconditions:* T's leaves exist for the keywords of the maintained keyword set. New-keyword leaf creation is not part of Alg. 1 (GAP-14).
  - *Steps:*
    1. For every leaf ln in T (the prose says "traverses all IIMT leaf nodes"; the box writes `leaf_nodes[1:−1]`): if ln.w ∈ o.keywords, set ln.bm[oid] ← 1 and update ln's hash.
    2. For every non-leaf node nn, bottom-up, update nn's hash. This "ultimately updates the IIMT root hash".
  - *State mutated:* bitmaps and hashes of matching leaves; all non-leaf hashes.
  - *Proof output:* none.
  - *Failure conditions:* none stated by the paper.
  - *Complexity:* none claimed.
  - *Note:* Alg. 1 updates only matching leaves' hashes. Bitmaps of non-matching leaves gain object oid as a 0 bit without their hash being recomputed. How that squares with a length-N bitmap serialization is GAP-3.
  
  Source: IV-B, Alg. 1.
- **R-IOMF-013** `[MANDATED]` **Algorithm 4 (IIMT Query Processing), normative form:**
  - *Inputs:* IIMT T_IIMT, keyword w.
  - *Outputs:* query result QR (the set of oids), bitmap bm, Merkle proof π.
  - *Steps:*
    1. QR, bm, π ← ∅.
    2. Retrieve w's kid; n ← leaf_nodes[kid].
    3. bm ← n.bm; QR ← {i | bm[i] = 1}.
    4. While n is not the root: π ← π ∪ {n.sibling.hash}; n ← n.parent.
    5. Return <QR, bm, π>.
  - *State mutated:* none.
  - *Proof components:* the sibling hashes along the leaf-to-root path. What "n.sibling.hash" means for F > 2 (F−1 siblings and the node's position among them): GAP-9.
  - *Failure conditions:* none stated. Keyword not present: GAP-13.
  - *Complexity (claimed):* time O(N + h_I) = O(N + log_F M_I), with M_I = number of IIMT leaves and h_I = height. Leaf lookup O(1) under the array layout. Proof size O(h_I). Assumes uncompressed bitmaps counted at bitmap-entry granularity.
  
  Source: IV-D, Alg. 4.
- **R-IOMF-014** `[MANDATED]` When several keywords are queried, the per-keyword proofs are merged into π_K to reduce proof size. Fig. 2 example: for ¬Error ∧ PUT ∨ GET (keywords Error, PUT, GET), π_K = {h2}. — Alg. 3 line 5, IV-D example. The exact merge rule: GAP-10.
- **R-IOMF-015** `[PERMITS]` Compressed bitmaps are permitted ("if compressed bitmaps are used ..."; Sec. X "can benefit from mature bitmap compression techniques"). They are not required. The complexity bounds are stated for dense uncompressed bitmaps. — IV-D, X. Choice: GAP-3.

### 6.3 Range Index Merkle B-Tree (RIMBT)

- **R-IOMF-016** `[MANDATED]` The RIMBT is a modified Merkle B-tree whose leaves store numerical values and range bitmaps. It is "a balanced index based on a B-tree structure with fixed fanout F". — IV-C, IV-D. It shall not be replaced by a conventional B+ tree whose node contents or hashes differ from IV-C.
- **R-IOMF-017** `[MANDATED]` The IOMF maintainer determines the value set of the numerical attribute. For each distinct value v there is one leaf. Bit i of bm(v) = 1 iff the attribute value of object i is <= v. Leaves are in ascending v. — IV-C, Fig. 2, Alg. 5
- **R-IOMF-018** `[MANDATED]` Leaf: (v, bm, h = H(v||H(bm))). Non-leaf: (V = {min(V_c2), ..., min(V_cF)}, h = H(V||h_c1||...||h_cF)). — IV-C. Meaning of V_ci: GAP-7. Fig. 2 (F = 2): the parent of leaves 12 and 15 has V = {15}; the parent of 97 and 99 has V = {99}; the root has V = {97}.
- **R-IOMF-019** `[MANDATED]` **RIMBT insertion (IV-C prose; the Alg. 2 box is defective, see GAP-8), normative form:**
  - *Inputs:* data object o, oid, numerical attribute o.attr, RIMBT T.
  - *Output:* updated T (new root).
  - *Steps:*
    1. Search T for a leaf whose v = o.attr.
    2. If it exists at position x: set bit oid to 1 in every bitmap from leaf_nodes[x] to the last leaf ("so that every range threshold v >= o.attr records the presence of object oid").
    3. Otherwise: create a new leaf with v = o.attr and insert it into T at position x (its ascending-order position). Initialize its bitmap by copying the bitmap of its predecessor leaf_nodes[x−1]. Then apply the same suffix update (set bit oid in leaf_nodes[x] .. last).
    4. Update the numerical value sets V. Recompute the hashes of all affected non-leaf nodes bottom-up, which refreshes the root hash.
  - *State mutated:* the leaf set (on a new value), bitmaps of the suffix leaves, their leaf hashes, the affected V sets and non-leaf hashes.
  - *Proof output:* none.
  - *Failure conditions:* none stated.
  - *Complexity:* none claimed.
  - *Unspecified:* no predecessor exists when o.attr is smaller than every existing value (GAP-8). Node split, occupancy, and rebalancing when a leaf is inserted (GAP-6). Recomputing the hashes of suffix leaves is implied by "update ... hashes of all affected" nodes.
  
  Source: IV-C.
- **R-IOMF-020** `[MANDATED]` **Algorithm 5 (RIMBT Query Processing), normative form:**
  - *Inputs:* RIMBT T_RIMBT (the box says "IIMT T_RIMBT", a typo), range [α, β].
  - *Outputs:* QR (oid set), bitmap set BM, Merkle proof π.
  - *Steps:*
    1. QR, BM, π ← ∅.
    2. Locate α: leaves ln_x, ln_{x+1} with ln_x.v < α and ln_{x+1}.v >= α.
    3. Locate β: leaves ln_y, ln_{y+1} with ln_y.v < β and ln_{y+1}.v >= β.
    4. For each n in {ln_x, ln_{x+1}, ln_y, ln_{y+1}}: π_ln ← ∅; while n is not the root:
       - if n is a leaf: π_ln ← π_ln ∪ {<n.sibling.v, n.sibling.hash>};
       - else: π_ln ← π_ln ∪ {<n.V, n.sibling.hash>};
       - n ← n.parent.
       
       Then merge π_ln into π.
    5. If ln_{y+1}.v = β: add ln_x.bm and ln_{y+1}.bm to BM; QR ← {i | ¬ln_x.bm[i] ∧ ln_{y+1}.bm[i] = 1}.
    6. Else: add ln_x.bm and ln_y.bm to BM; QR ← {i | ¬ln_x.bm[i] ∧ ln_y.bm[i] = 1}.
    7. Return <QR, BM, π>.
  - *State mutated:* none.
  - *Proof components:* per boundary leaf, sibling (v, hash) at the leaf level and (V, sibling hash) at internal levels; merged. Fig. 2 example for SR = [13, 98]: π_SR = {<15>, <99>, <15, h2>, <99, h4>}, BM = {bm1 (leaf 12), bm3 (leaf 97)}, result ¬bm1 ∧ bm3 = 10001100.
  - *Unspecified:*
    - F > 2 sibling content: GAP-9;
    - the root's V is never added by the loop: GAP-9;
    - whether the values v of the bitmap-carrying leaves are shipped: GAP-11;
    - missing ln_x (α <= min v) or missing ln_{y+1} (β > max v), and α > β: GAP-11;
    - the merge rule: GAP-10.
  - *Complexity (claimed):* O(N + h_R) = O(N + log_F M_R). At most four boundary leaves; proof size O(h_R); a constant number of length-N bitmaps.
  
  Source: IV-D, Alg. 5.
- **R-IOMF-021** `[MANDATED]` One RIMBT per numerical attribute ("RIMBT 1 ... RIMBT k", Fig. 2). Each range sub-query is processed on the RIMBT of its attribute. — IV-D, Fig. 2

### 6.4 IOMF query processing (Algorithm 3)

- **R-IOMF-022** `[MANDATED]` Query form Q = <K, R>, where K is a Boolean keyword expression and R a range expression. The result is {o_i = <W_i, V_i> | K(W_i) = 1 ∧ R(V_i) = 1}. Q may contain only K or only R. R may involve multiple numerical attributes. — IV-D. When only one part is present, the result is defined by the same set expression with the absent conjunct omitted. The mechanism (skipping the line-13 intersection) is `[UNSPECIFIED]` but has no degrees of freedom affecting results.
- **R-IOMF-023** `[MANDATED]` **Algorithm 3 (Verifiable Boolean Range Query Processing), normative form:**
  - *Inputs:* Q = <K, R>.
  - *Outputs:* QR and VO.
  - *Steps:*
    1. QR, VO ← ∅.
    2. <BSQ = <K>, RSQ = {SR}> ← DivideQuery(Q). RSQ contains one range sub-query per range condition, covering all numerical attributes in R. Example: Q = <(¬w1 ∧ w3 ∨ w4), ([a1, a2] ∧ [b1, b2])> gives BSQ = (¬w1 ∧ w3 ∨ w4) and RSQ = {[a1, a2], [b1, b2]}.
    3. For each keyword w appearing in K:
       - <qr_w, bm_w, π_w> ← QueryIIMT(T_IIMT, w) (Alg. 4);
       - merge π_w into π_K;
       - add bm_w to BM_K and qr_w to QR_W.
    4. QR_K ← LogicalOperation on QR_W according to K.
    5. Add <BM_K, π_K> to VO.
    6. For each SR in RSQ:
       - <qr_SR, BM_SR, π_SR> ← QueryRIMBT(T_RIMBT, SR) (Alg. 5) on the RIMBT of SR's attribute;
       - add qr_SR to QR_SR;
       - add <BM_SR, π_SR> to VO.
    7. QR_R ← LogicalOperation on QR_SR according to R.
    8. QR_O ← QR_K ∩ QR_R. The paper realizes this as a bitwise AND of the result bitmaps (line 13).
    9. <QR, π_O> ← retrieve QR_O in the OMT.
    10. Add QR_O and π_O to VO.
    11. Return <QR, VO>.
  - *State mutated:* none.
  - *Failure conditions:* none stated.
  - *Complexity:* none stated for Alg. 3 as a whole.
  
  Source: IV-D, Alg. 3.
- **R-IOMF-024** `[MANDATED]` Boolean evaluation shall use bitwise NOT, AND, OR on bitmaps of length N. Worked example (IV-D, Fig. 2): ¬Error ∧ PUT ∨ GET is evaluated as ((¬bm_Error) ∧ bm_PUT) ∨ bm_GET = 01010001. Range results use ¬bm_left ∧ bm_right (Alg. 5). — I (intro example), III, IV-D
- **R-IOMF-025** `[MANDATED]` The VO produced by Alg. 3 contains (BM_K, π_K), every (BM_SR, π_SR), QR_O and π_O, "enabling the client to verify both the index-level evaluation and the object-level correctness". — IV-D. IV-E names the parsed form <BM_K, π_K, BM_R, π_R, QR_O, π_O>.

## 7. ALIS REQUIREMENTS

- **R-ALIS-001** `[MANDATED]` ALIS shall have exactly three levels: block level, transaction level, entry level. **Qualified per addendum A-4 and GAP-51 (`DECISIONS.md` D8):** the block and transaction levels are full IOMF instances (OMT + IIMT + RIMBT); the **entry level is a reduced IOMF** — OMT + IIMT, with B+ Tree temporal routing replacing the RIMBT for the single temporal attribute. The entry level is therefore not an IOMF instance under §3.10's definition, and R-ALIS-007's prohibition on substituting the B+ Tree *for* a RIMBT stands.
  - The entry level is stored off-chain; the block and transaction levels on-chain.
  - Block-level and transaction-level root hash sets are stored in the block header. The entry-level root hash set is recorded in the on-chain log file metadata.
  
  Source: V-A, V-C, Fig. 3.

### 7.1 Entry level (off-chain, per log file)

- **R-ALIS-002** `[MANDATED]` Each log file has its own independent entry-level index. It consists of a modified OMT and an IIMT and supports keyword Boolean queries, time-range queries, and result entry queries. — V-C
- **R-ALIS-003** `[MANDATED]` The log generator assigns each entry of the file an esn in chronological order of generation. It builds an OMT over the esns and the log entries (oid = esn). — V-C. esn origin: GAP-21.
- **R-ALIS-004** `[MANDATED]` The log generator keeps, per file, an inverted index list of entry keywords in bitmap form. Each bitmap is over the file's entries, bit index = esn. It builds an IIMT from that list. — V-C
- **R-ALIS-005** `[MANDATED]` The entry-level IIMT leaves are one-to-one with the log generator's entry keywords. If an entry keyword does not occur in the current file, its bitmap is all zeros and the leaf is kept, not removed. — V-C
- **R-ALIS-006** `[MANDATED]` A new entry keyword is appended to the generator's maintained keyword set and indexed in subsequently generated log files. Previously committed files are not rebuilt. For older files, the new keyword's bitmap "can be naturally treated as an all-zero bitmap". — V-C. How the verifier authenticates that treatment: GAP-13. Whether the current, not-yet-closed file counts as "subsequent": GAP-14.
- **R-ALIS-007** `[MANDATED]` Time-range queries at the entry level combine the OMT with a B+ Tree "whose nodes store temporal retrieval information". Superimposing the B+ Tree does not change the order of the OMT leaves, because esn order equals timestamp order (assumption: timestamps within a file are locally consistent with generation order). — V-C. The node contents, the hashing of the temporal information, and the B+ Tree fanout: GAP-17. This combination shall not be replaced by a RIMBT.
- **R-ALIS-008** `[MANDATED]` The entry-level root hash set (RH of m_i) is recorded in the on-chain log file metadata. `[EXPERIMENT]` It contains the "IIMT root hash, OMT root hash" (VIII). — V-C, VIII
- **R-ALIS-009** `[MANDATED]` Construction is attributed to the log generator (V-C: "the log generator assigns ... and constructs an OMT"). Maintenance and storage belong to the SP (V-A: "maintains the entry-level index"). — V-A, V-C. Table IV labels the off-chain construction cost "SP": GAP-36.

### 7.2 Transaction level (on-chain, per block)

- **R-ALIS-010** `[MANDATED]` Each block has its own independent transaction-level index: an OMT for result transaction queries, an IIMT for file-keyword Boolean queries, and two RIMBTs for time-range queries. — V-C
- **R-ALIS-011** `[MANDATED]` The block generator sorts the block's transactions by lid, then by fid, and assigns tsn starting from 0. — V-C. Direction and comparison semantics: GAP-20.
- **R-ALIS-012** `[MANDATED]` The block generator builds the OMT over tsns and transactions (oid = tsn). This ensures transaction integrity and lets lg's transactions, which are contiguous in tsn because of the sort, be proven complete with neighbour leaves. — V-C, V-D
- **R-ALIS-013** `[MANDATED]` The block generator keeps an inverted index list mapping each file keyword to the tsns of the block (bitmap over tsn) and builds an IIMT from it. — V-C. Which keywords get leaves (block-local vs. the keyword-set-file universe): GAP-13.
- **R-ALIS-014** `[MANDATED]` Two RIMBTs are built over the block's transactions: one on ts_min, one on ts_max. — V-C
- **R-ALIS-015** `[MANDATED]` Interval-intersection transformation: [ts, te] ∩ [ts_min, ts_max] ≠ ∅ iff ts <= ts_max and te >= ts_min. The time-range query shall:
  1. obtain the transactions with ts_min <= te from the ts_min RIMBT;
  2. obtain the transactions with ts_max >= ts from the ts_max RIMBT;
  3. intersect the two sets.
  
  Source: V-C. Leaf-selection details for these one-sided queries: GAP-12.
- **R-ALIS-016** `[MANDATED]` The transaction-level root hash set is stored in the block header (RH). — V-B, V-C. Which roots and their order: GAP-24.
- **R-ALIS-017** `[MANDATED]` Transaction-level indexes of different blocks are independent. This is the stated basis for parallel transaction-level query execution (R-QUERY-013). — V-C, V-D

### 7.3 Block level (on-chain, global; per segment under Opt. 1)

- **R-ALIS-018** `[MANDATED]` Without Opt. 1 the block-level index is global over all blocks of the blockchain: bitmap length and RIMBT leaf counts grow with the number of blocks. — V-C, VI
- **R-ALIS-019** `[MANDATED]` The full node keeps an inverted index list of log generators, recording in which blocks each generator's transactions are located (lid → bitmap over BlockHeights). It builds the log-generator (LG) IIMT from it. — V-C. Leaf set and leaf order for lids: GAP-13.
- **R-ALIS-020** `[MANDATED]` The block generator keeps an inverted index list mapping each file keyword to BlockHeights and builds the file-keyword (FK) IIMT from it. — V-C
- **R-ALIS-021** `[MANDATED]` The full node builds two RIMBTs from the block header fields FileTsMin and FileTsMax of all blocks. The leaves are distinct FileTsMin (resp. FileTsMax) values with cumulative bitmaps over BlockHeights, "similar to the design in the transaction-level index". — V-C
- **R-ALIS-022** `[MANDATED]` The block-level index is a coarse filter that identifies blocks that may contain satisfying transactions. Its root hash set is stored in the block header. — V-C. In which header, and for which state of the global index: GAP-24.
- **R-ALIS-023** `[MANDATED]` FileTsMin, FileTsMax and TxNum in each header are computed from that block's transactions (min ts_min, max ts_max, count). — V-B
- **R-ALIS-024** `[UNSPECIFIED]` Mapping of BlockHeight to bit position (absolute BlockHeight, or offset within a segment under Opt. 1): GAP-21.

## 8. QUERY REQUIREMENTS

### 8.1 Query model

- **R-QUERY-001** `[MANDATED]` General log-entry query: Q = <lg, [ts, te], FK, EK>.
  - Result: all log entries generated by lg within [ts, te] whose file keywords FW satisfy FK and whose entry keywords EW satisfy EK.
  - Formally: {e = <lid, t, FW, EW> | lid = lg ∧ t ∈ [ts, te] ∧ FK(FW) = 1 ∧ EK(EW) = 1}.
  
  Source: V-D.
- **R-QUERY-002** `[MANDATED]` Partition: Q_on = <lg, [ts, te], FK> (on-chain log file metadata query, processed by the full node with the block-level and transaction-level indexes) and Q_off = <lg, [ts, te], EK> (off-chain log entry query, processed by the SP with the entry-level index). — V-A, V-D, Fig. 4
- **R-QUERY-003** `[MANDATED]` Boolean operators are exactly ∧ (AND), ∨ (OR), ¬ (NOT), evaluated as bitwise operations on bitmaps. No other operators or predicate kinds (e.g. SQL-like predicates, LIKE, regex, numeric comparison on non-time attributes at ALIS level) shall be added. — II-B, II-D, IV-D
- **R-QUERY-004** `[MANDATED]` Evaluation order shall reproduce the paper's worked example. ¬a ∧ b ∨ c is evaluated as ((¬a) ∧ b) ∨ c (IV-D example with Error/PUT/GET). The experiment's complex query has the same shape (VIII-A). — IV-D, VIII-A. General precedence and parenthesisation for other expression shapes: GAP-34.
- **R-QUERY-005** `[MANDATED]` Range predicates are closed intervals [ts, te] on timestamps. At the entry level: t ∈ [ts, te]. At the transaction and block levels: interval intersection via ts_max >= ts and ts_min <= te (resp. FileTsMax >= ts and FileTsMin <= te). — V-C, V-D
- **R-QUERY-006** `[MANDATED]` At IOMF level, a query is divided into one Boolean sub-query and a set of range sub-queries (one per range condition). Results are merged by intersection: QR_O = QR_K ∩ QR_R. — IV-D, Alg. 3. At ALIS levels, each stage divides its query into LG, keyword, and time-range sub-queries and intersects their results (V-D).
- **R-QUERY-007** `[UNSPECIFIED]` Handling of queries without a time range (the experiment's pure keyword queries: AND, OR, NOT-AND), of queries without lg, and of Boolean predicates over lid/fid (claimed in II-B, not provided by the V-D query form): GAP-34.
- **R-QUERY-008** `[UNSPECIFIED]` NOT inside FK at the block-level coarse filter, and inside EK at the auditor's bm_EW file filter: GAP-15.

### 8.2 Stage 1: block-level query (full node)

- **R-QUERY-009** `[MANDATED]` The full node divides Q_on into an LG sub-query, a file-keyword sub-query, and a time-range sub-query. — V-D
- **R-QUERY-010** `[MANDATED]` LG sub-query: obtain the bitmap and Merkle proof for lg from the block-level LG IIMT (Alg. 4). — V-D
- **R-QUERY-011** `[MANDATED]` File-keyword sub-query: obtain the bitmap and Merkle proof for each keyword in FK from the block-level FK IIMT, then apply the logical operations of FK to these bitmaps. — V-D. NOT semantics: GAP-15.
- **R-QUERY-012** `[MANDATED]` Time-range sub-query:
  - In the FileTsMax RIMBT, locate neighbouring leaves ln1, ln2 with t_ln1 < ts and t_ln2 >= ts, together with the rightmost leaf lnr. Blocks with FileTsMax >= ts are ¬bm_ln1 ∧ bm_lnr.
  - In the FileTsMin RIMBT, locate neighbouring leaves ln3, ln4 with t_ln3 <= te and t_ln4 > te. Blocks with FileTsMin <= te are bm_ln3.
  - Intersect the two BlockHeight sets.
  
  Source: V-D. Relation to Alg. 5, proof content for lnr, and missing-neighbour cases: GAP-12.
- **R-QUERY-013** `[MANDATED]` Intersect the three sub-query results to obtain the candidate BlockHeights. Integrate the three sub-query VOs into the block-level VO. — V-D. With Opt. 1, restrict to the intersecting segments (R-OPT-002). With Opt. 2, the proofs change (R-OPT-010..013).

### 8.3 Stage 2: transaction-level query (full node)

- **R-QUERY-014** `[MANDATED]` For each candidate BlockHeight, locate the block and divide Q_on into LG, file-keyword, and time-range sub-queries. The file-keyword and time-range sub-queries are processed as at the block level but over tsns (transaction-level FK IIMT; ts_min and ts_max RIMBTs). — V-D
- **R-QUERY-015** `[MANDATED]` LG sub-query at transaction level:
  - retrieve all of lg's transactions in the block with their Merkle proof through the transaction-level OMT;
  - for completeness, also provide the left neighbour leaf of lg's transaction with the smallest tsn and the right neighbour leaf of lg's transaction with the largest tsn, with their Merkle proofs.
  
  Source: V-D. Neighbour content and missing-neighbour cases: GAP-19.
- **R-QUERY-016** `[MANDATED]` Intersect the three sub-query results to obtain the block's matching transactions. Integrate the sub-query VOs into the transaction-level VO. — V-D
- **R-QUERY-017** `[PERMITS]` Transaction-level queries of different blocks may be executed in parallel, because the per-block indexes are independent. — V-D. Whether and how the reproduction parallelises: GAP-40.
- **R-QUERY-018** `[MANDATED]` After Stages 1 and 2 the full node sends the auditor the resulting transactions together with the block-level and transaction-level VOs. — V-D, Fig. 4 step (4)

### 8.4 Stage 3: entry-level query (auditor, then SP)

- **R-QUERY-019** `[MANDATED]` On receiving transactions and VOs, the auditor identifies and extracts the log file metadata whose bm_EW satisfies the conditions of Q_off. — V-D. Evaluation of EK over bm_EW: GAP-15, GAP-16. Whether verification of Stages 1–2 happens before this step: GAP-33.
- **R-QUERY-020** `[MANDATED]` The auditor sends the SP the fids of those log files with Q_off. The SP locates the files by lg and fids. — V-D. Fig. 4 labels the message "lids, Q_off": GAP-33.
- **R-QUERY-021** `[MANDATED]` Per file, the SP divides Q_off into an entry-keyword sub-query and a time-range sub-query.
  - Entry keywords: obtain the bitmap and Merkle proof for each keyword in EK from the entry-keyword IIMT, then apply the logical operations of EK.
  - Time range: retrieve all log entries with timestamp in [ts, te] with Merkle proofs through the entry-level OMT. For completeness, also provide "the OMT leaf nodes that contain the matching and boundary values of ts and te" with their Merkle proofs.
  
  Source: V-D. Structure of the temporal information and boundary evidence: GAP-17.
- **R-QUERY-022** `[MANDATED]` The SP intersects the two sub-query results to obtain the file's result entries. It integrates the sub-query VOs into the entry-level VO. — V-D. Exact VO contents and returned entries: GAP-18.
- **R-QUERY-023** `[PERMITS]` Entry-level queries of different files may be executed in parallel (independent per-file indexes). — V-D. See GAP-40.
- **R-QUERY-024** `[MANDATED]` The SP sends the result log entries and the entry-level VO to the auditor. — V-D, Fig. 4 step (5)
- **R-QUERY-025** `[UNSPECIFIED]` Ordering of returned results. The paper treats all results as sets (QR, QR_O, transaction sets, BlockHeight sets); no output order is defined. See GAP-38. Verification shall compare sets (R-VER-004).

## 9. VERIFICATION REQUIREMENTS

### 9.1 IOMF-level verification (IV-E)

- **R-VER-001** `[MANDATED]` Verifier inputs: query Q, returned result QR, VO, and the committed root hashes RH. The verifier first parses VO = <BM_K, π_K, BM_R, π_R, QR_O, π_O>. — IV-E
- **R-VER-002** `[MANDATED]` Integrity checking: reconstruct the roots of all index and data structures from the Merkle proofs in VO.
  - Verify BM_K against the IIMT root using π_K.
  - Verify BM_R against each relevant RIMBT root using π_R.
  - Verify the result objects QR and the oid set QR_O against the OMT root using π_O.
  - Integrity is confirmed only if every reconstructed root equals the corresponding entry of RH.
  
  Source: IV-E.
- **R-VER-003** `[MANDATED]` Correctness checking:
  - From BM_K and BM_R, recompute the oid set QR'_O satisfying Q by applying the same logical operations as the original query processing (Boolean expression K; range combination R; Alg. 5 boundary rule ¬bm_left ∧ bm_right; final AND).
  - Check QR'_O = QR_O.
  - Check that the set of object identifiers derived from QR coincides with QR_O.
  
  Source: IV-E, VII (proof of Theorem 2).
- **R-VER-004** `[MANDATED]` The query result is accepted iff integrity checking and correctness checking both pass. All comparisons of result sets are set equalities. — IV-E
- **R-VER-005** `[MANDATED]` The verifier shall compute QR'_O itself from the authenticated bitmaps. It shall not take any result bitmap or result set from the server as the expected answer. — IV-E ("independently computed set")
- **R-VER-006** `[MANDATED]` For range sub-queries, the verifier shall use the two boundary bitmaps and π_SR to reconstruct the RIMBT root (IV-D: "The client can verify this result using the two bitmaps bm1 and bm3 together with the Merkle proof π_SR"). — IV-D. The check that the boundary leaves are adjacent and straddle α and β is not written out in the paper. It is necessary for completeness (Theorem 2, case 3), but its exact form depends on the proof content (GAP-9, GAP-11, GAP-12).

### 9.2 Three-stage ALIS verification (V-E, Fig. 4)

- **R-VER-007** `[MANDATED]` Verification is divided into three stages that correspond to the three query stages and are executed in this order:
  1. block-level;
  2. transaction-level;
  3. entry-level.
  
  Source: V-E.
- **R-VER-008** `[MANDATED]` Stage 1 (block level):
  - verify the integrity of the bitmaps in the block-level VO with the Merkle proofs, against the block-level roots in the block header(s) held by the auditor;
  - check that the result blocks match the result of the logical operations (LG ∧ FK-expression ∧ time-range).
  
  Source: V-E. Which header's roots are the reference: GAP-24, GAP-28. With Opt. 1: R-OPT-004..008.
- **R-VER-009** `[MANDATED]` Stage 2 (transaction level): for each candidate block,
  - verify the integrity of the bitmaps and transactions with the transaction-level VO against that block's transaction-level roots in its header;
  - check that the resulting transactions are consistent with the logical operations;
  - check completeness of lg's transactions with the left and right neighbour leaves (R-QUERY-015).
  
  Source: V-D, V-E. The neighbour check (neighbour leaves are adjacent to lg's run and do not belong to lg) is implied by "to guarantee the completeness". Its exact form: GAP-19.
- **R-VER-010** `[MANDATED]` Stage 3 (entry level): for each returned file,
  - verify the integrity of the bitmaps and log entries with the entry-level VO against the entry-level root hash set RH stored in the (Stage-2-verified) transaction ("comparing it with the one stored in the transaction", Theorem 2, case 1);
  - check that the result entries are consistent with the logical operations (EK ∧ time range);
  - check time-range completeness with the boundary OMT leaves for ts and te.
  
  Source: V-D, V-E, VII. Exact form: GAP-17, GAP-18.
- **R-VER-011** `[MANDATED]` The auditor accepts the log entries iff all verification outputs are true. If every stage passes, soundness and completeness of the log query result are guaranteed. — V-E, Fig. 4 ("Accept log entries iff. all verification outputs are true")
- **R-VER-012** `[MANDATED]` The verifier output is a single accept/reject decision. The paper defines no error taxonomy, and none shall be invented. Diagnostic detail for tests is permitted only if it does not change the accept/reject outcome. — V-E, Fig. 4
- **R-VER-013** `[MANDATED]` Trust anchors: the auditor verifies against block header information it stores as a light node. Entry-level roots come from transactions verified in Stage 2. — II-A, V-C, VII
- **R-VER-014** `[UNSPECIFIED]` Verification of the auditor's own bm_EW file filtering is not described. The auditor applies it to Stage-2-verified metadata. Whether a file that the SP fails to answer causes rejection: GAP-33.

## 10. OPTIMIZATION REQUIREMENTS

Both optimizations are part of BLAS (VI) and are enabled in the reference configuration ("All", VIII-C).

### 10.1 Optimization 1: block-level index partition

- **R-OPT-001** `[MANDATED]` The block-level index shall be generated periodically instead of over all blocks: "we group a certain number of blocks into a segment and set an independent block-level index for each segment". — VI. The VI introduction says instead "building a separate RIMBT for each partition": GAP-27.
- **R-OPT-002** `[MANDATED]` For a time-range query the full node searches only the segments that intersect the target time range, using the corresponding block-level index without accessing the other blocks. — VI
- **R-OPT-003** `[MANDATED]` Three header fields are added:
  - PastTsMax = max FileTsMax of all blocks up to the current block;
  - BlockTsMax = max FileTsMax of the blocks in the current segment;
  - BlockTsMin = min FileTsMin of the blocks in the current segment.
  
  Source: VI. Values in non-final blocks and in the still-open segment: GAP-28.
- **R-OPT-004** `[MANDATED]` The log auditor obtains, from its local storage, the block header of the last block of each segment, "from back to front" (newest segment first). — VI
- **R-OPT-005** `[MANDATED]` If BlockTsMin < te and BlockTsMax > ts, the segment's time interval intersects the query range, and the segment must be among those searched. — VI. Equality cases: GAP-29.
- **R-OPT-006** `[MANDATED]` If BlockTsMax < ts or BlockTsMin > te, the intersection is empty "and the lookup should continue forward". — VI. The direction "forward" within a back-to-front scan: GAP-29.
- **R-OPT-007** `[MANDATED]` If PastTsMax < ts, no earlier block intersects the query range, and the lookup terminates. — VI
- **R-OPT-008** `[MANDATED]` After the scan, the log auditor checks the consistency of the block segments actually queried by the full node with the block-level VO. This verifies that the full node's block-level query covered all intersecting segments. — VI. The concrete comparison (e.g. set equality of segment identifiers) is not written out: GAP-29.
- **R-OPT-009** `[UNSPECIFIED]` The segment size is "a certain number of blocks" (VI). The paper fixes no protocol value. `[EXPERIMENT]` "block-level index size to 1024" (VIII-B, Table IV paragraph). Segment boundaries, where segment roots are stored, and queries without a time range under partitioning: GAP-28, GAP-34.

### 10.2 Optimization 2: on-chain index aggregation

- **R-OPT-010** `[MANDATED]` Purpose: transaction-level Merkle proofs from different blocks cannot be aggregated, so VO size grows with the number of hit blocks. Aggregation "replace[s] many individual Merkle paths with a shorter aggregated proof". — VI
- **R-OPT-011** `[MANDATED]` File-keyword aggregation:
  - categorize "the file keyword bitmaps of all blocks within a segment" by keyword;
  - build one MHT per keyword over the bitmaps of that keyword;
  - deposit each MHT root into the corresponding leaf of the block-level FK IIMT (Fig. 5 leaf: W_i, h_i, bm_i, RH_i).
  
  Source: VI, Fig. 5. Leaf contents, order, arity, which blocks are included, and how RH_i enters the leaf hash: GAP-31. Conflict with the VI introduction ("hashing the roots of these transaction-level indices"): GAP-30.
- **R-OPT-012** `[MANDATED]` Log-generator aggregation:
  - categorize all transactions within a segment by lid;
  - build one MHT per lid over that generator's transactions, with leaf node BlockHeight||tsn||H(tx);
  - deposit each MHT root into the corresponding leaf of the block-level LG IIMT (Fig. 5 leaf: LG_i, h_i, bm_i, RH_i).
  
  Source: VI, Fig. 5. Details: GAP-31.
- **R-OPT-013** `[MANDATED]` Proof consequences, only as far as the paper states them:
  - aggregation reduces the number of hash values in the VO, and with it communication and verification overhead;
  - the cost is extra precomputation during index maintenance;
  - the paper states that aggregation reduces VO size "without changing the asymptotic structure of the VO" (VIII-C).
  
  Source: VI, VIII-C. `[UNSPECIFIED]` Which Stage-2 proofs are replaced by aggregated-MHT proofs, and how the verifier checks them: GAP-32.
- **R-OPT-014** `[MANDATED]` Aggregation is defined "within a segment", so it is defined on top of the Opt. 1 segments. — VI. Behaviour with aggregation enabled and partitioning disabled (the "No-multi-win" ablation variant): GAP-32.
- **R-OPT-015** `[EXPERIMENT]` Each optimization shall be independently switchable to reproduce the ablation variants All, No-aggre (aggregation disabled) and No-multi-win (block-level partitioning with multi-window processing disabled). — VIII-C, Fig. 7

## 11. SECURITY REQUIREMENTS

- **R-SEC-001** `[MANDATED]` The SP is untrusted. It may intentionally or unintentionally add, delete, or tamper with query results. All SP responses shall be verified. — II-C
- **R-SEC-002** `[MANDATED]` The blockchain satisfies its security assumptions, e.g. at most one-third malicious nodes under BFT consensus. Committed data (headers, transactions, roots) cannot be altered or hidden without violating this assumption. — II-C, VII
- **R-SEC-003** `[MANDATED]` Log generators are trusted for recording and packaging log data, generating the off-chain ADS from entry attributes, and sending data to the SP and full nodes. — II-C
- **R-SEC-004** `[MANDATED]` Log auditors are trusted and follow the verification protocol faithfully. — II-C
- **R-SEC-005** `[MANDATED]` Scope limit: BLAS guarantees integrity and verifiability of committed logs, i.e. consistency of results with committed data. It does not guarantee the semantic truthfulness of logs falsely produced before commitment. — II-C
- **R-SEC-006** `[MANDATED]` Soundness: all returned log entries satisfy the query condition and originate from the target log generators. — II-D
- **R-SEC-007** `[MANDATED]` Completeness: all valid log entries are returned with respect to the query conditions. — II-D
- **R-SEC-008** `[MANDATED]` Definition 1 (BLAS Maintenance Security): for any PPT adversary that picks two different datasets LD_real and LD_forged, the probability that the maintenance algorithm yields RH_real = RH_forged is negligible. Theorem 1 claims this holds under collision resistance of H and blockchain security. — VII
- **R-SEC-009** `[MANDATED]` Definition 2 (BLAS Query Processing Security): an adversary given all of LD outputs (Q, R, VO). It succeeds if (Q, R, VO, RH) passes verification and any of the following holds:
  - (a) some e ∈ R with e ∉ LD;
  - (b) some e ∈ R that does not satisfy Q;
  - (c) some e ∈ LD that satisfies Q with e ∉ R.
  
  The success probability must be negligible. (a) and (b) are soundness violations; (c) is a completeness violation. — VII
- **R-SEC-010** `[MANDATED]` Case (a), forged entry: the verifier shall reject any returned entry whose entry-level OMT proof does not reconstruct the OMT root stored in the transaction. — VII, Theorem 2 case 1
- **R-SEC-011** `[MANDATED]` Case (b), non-satisfying entry:
  - if oid(e) ∉ QR_O, reject on the mismatch between the oids derived from QR and QR_O;
  - if oid(e) ∈ QR_O, reject because the recomputed QR'_O from the authenticated bitmaps differs, or because the bitmap/proof integrity check fails.
  
  Source: VII, Theorem 2 case 2.
- **R-SEC-012** `[MANDATED]` Case (c), omission:
  - if oid(e) ∈ QR_O but e ∉ R, reject on the mismatch between the oids derived from QR and QR_O;
  - if oid(e) is omitted from QR_O, reject because the recomputation from authenticated bitmaps includes it, or because manipulated bitmaps/proofs fail integrity;
  - omission through hiding the latest blocks is excluded by the blockchain assumption (the auditor holds the headers).
  
  Source: VII, Theorem 2 case 3.
- **R-SEC-013** `[MANDATED]` The security argument requires that each leaf and internal-node hash input determines the node contents uniquely ("two different leaf contents with the same hash value or two different internal-node inputs that lead to the same parent hash" must imply a collision, proof of Theorem 1). — VII. `[UNSPECIFIED]` Any encoding chosen under GAP-2/GAP-3 must keep this property. The paper gives no encoding, so this is a constraint on the GAP decision, not an invented mechanism.
- **R-SEC-014** `[MANDATED]` No security property beyond those in II-D and VII shall be claimed or implemented: no freshness mechanism beyond light-node header sync, no confidentiality, no access control, no additional signatures. — II-C, II-D, VII, X
- **R-SEC-015** `[UNSPECIFIED]` Whether a full node answering Q_on is modelled as a potential adversary. II-C names only the SP as untrusted, but V-E and VI verify full-node responses, and Definition 2 speaks of "a malicious log audit system": GAP-35.

## 12. EXPERIMENTAL REPRODUCTION REQUIREMENTS

Every item in this section is `[EXPERIMENT]`. None of it may be hard-coded into the protocol packages. Every value shall be an explicit experiment configuration input.

### 12.1 Setup

- **R-EVAL-001** Datasets:
  - "Android-v1" from LogHub [12] (collected from Android smartphones with instrumented modules);
  - Apache Spark logs from LogHub 2.0 [13] (collected from a Spark deployment at CUHK on 32 machines).
  
  Source: VIII "Dataset". Exact file versions, sizes, and the volume needed for 8192 blocks: GAP-41.
- **R-EVAL-002** Emulate 4096 log generators per dataset "by randomly and uniformly distributing the raw log data among them". Each generator's assigned portion is treated as the logs it generated. — VIII. Distribution unit, ordering, RNG, and seed: GAP-41.
- **R-EVAL-003** Pack every 2048 log entries into one log file. Each entry is represented as <timestamp, {log entry keywords}, content>, where the entry keywords are extracted from each entry. — VIII. Extraction method: GAP-46. Missing esn in the representation: GAP-37.
- **R-EVAL-004** Pack the metadata of every 256 log files into one block. Each metadata record is <lid, [timestamp_min, timestamp_max], {log file keywords}, bitmap, IIMT root hash, OMT root hash>, where the file keywords are "determined by the log generators". — VIII. Missing fid: GAP-37. File keyword determination: GAP-46. File-to-block packing order and remainders: GAP-41.
- **R-EVAL-005** Hash: BLAKE2b for 256-bit hash computations. — VIII (see R-CRYPTO-001, GAP-1)
- **R-EVAL-006** Implementation language: the paper's C++ implementation is replaced by Go. This is the only intended deviation. — VIII "Implementation"; brief
- **R-EVAL-007** Reference platform (for context only; not reproducible from the paper alone): Ubuntu 22.04 VM, Intel Xeon Gold 6230R 2.1 GHz, 78 GiB memory. — VIII. The reproduction shall record its own hardware and OS alongside results.
- **R-EVAL-008** Tree configuration for the Table IV measurement: "block-level index size to 1024 and the fanout of all trees to 4". — VIII-B. Whether this also applies to Tables II, III, V, VI and Figs. 6, 7, and whether "all trees" includes the OMT and the aggregated MHTs: GAP-42, GAP-5.
- **R-EVAL-009** Experimental roadmap, three sets:
  1. performance versus block count (query time and VO size);
  2. scalability (ADS construction; range width; keyword count);
  3. ablation (optimizations).
  
  Source: VIII "Experimental roadmap".

### 12.2 Workloads

- **R-EVAL-010** For each experiment, generate 10 random queries and report the average. — VIII-A. Query generation method, RNG, and seed: GAP-42.
- **R-EVAL-011** Seven query types (VIII-A):
  1. AND: (lid, fw1 ∧ fw2, ew1 ∧ ew2)
  2. OR: (lid, fw1 ∨ fw2, ew1 ∨ ew2)
  3. NOT-AND: (lid, ¬fw1 ∧ ¬fw2, ¬ew1 ∧ ¬ew2)
  4. AND + range: (lid, [ts, te], fw1 ∧ fw2, ew1 ∧ ew2)
  5. OR + range: (lid, [ts, te], fw1 ∨ fw2, ew1 ∨ ew2)
  6. NOT-AND + range: (lid, [ts, te], ¬fw1 ∧ ¬fw2, ¬ew1 ∧ ¬ew2)
  7. Complex: (lid, [ts, te], ¬fw1 ∧ fw2 ∨ fw3, ¬ew1 ∧ ew2 ∨ ew3)
  
  In Tables II/III the range variants and Complex use "RANGE 20%".
- **R-EVAL-012** Block counts for Tables II and III and Fig. 7: 128, 512, 1024, 4096, 8192. — Tables II, III; Fig. 7
- **R-EVAL-013** ADS construction (Table IV):
  - full node: construction time T (s/block) and ADS size S (KB/block);
  - SP: T (s/file) and S (KB/file);
  - settings as in R-EVAL-008.
  
  Source: VIII-B, Table IV. Accounting rules: GAP-43. Who constructs the off-chain ADS: GAP-36.
- **R-EVAL-014** Range scalability (Tables V, VI): time-range width 10%, 20%, 30%, 40%, 50% "of the entire dataset" / "of the log timeline"; both datasets; query time and VO size. — VIII-B. Query type, block count, and window placement are not stated: GAP-42.
- **R-EVAL-015** Keyword scalability (Fig. 6): number of query keywords 2 to 10, with block count fixed at 1024. Fig. 6(a) uses multi-keyword AND queries, Fig. 6(b) multi-keyword OR queries. Metrics: VO size (KB) and CPU time (ms). — VIII-B, Fig. 6. Dataset, keyword kinds (file, entry, or both), and presence of lid or range: GAP-42.
- **R-EVAL-016** Ablation (Fig. 7):
  - query: two-keyword AND Boolean range queries, range selectivity 0.2;
  - variants: All (both optimizations), No-aggre (on-chain index aggregation disabled), No-multi-win (block-level partitioning with multi-window processing disabled);
  - blocks: 128, 512, 1024, 4096, 8192;
  - metrics: query time (ms, log scale) and VO size (KB, log scale).
  
  Source: VIII-C, Fig. 7. Dataset not stated: GAP-42.

### 12.3 Metrics and baselines

- **R-EVAL-017** Metrics (VIII-A):
  - CPU time consumed by the full node and SP for query processing;
  - CPU time consumed by the log auditor for result verification;
  - size of the VO transmitted from the full node and SP to the auditor.
  
  Table IV adds construction time and ADS size. Composition of "query time", CPU vs. wall clock, and the KB unit: GAP-43.
- **R-EVAL-018** Baselines: vChain+ [11], MSTDB [8], MART [9], adapted with an extra off-chain ADS layer. Paper parameters:
  - vChain+: ID-tree fanout 4, B+-tree fanout 4, maximum ID number 4095, 4 verification threads; for Table IV, sliding windows {2, 4, 8} and ObjReg/SWA-B+-tree fanout 4;
  - MART: fanouts {4, 16, 48, 256}, MAX_PREFIX_LEN = 10;
  - MSTDB and MART are omitted for NOT and range queries.
  
  In scope only if GAP-44 says so. — VIII

### 12.4 Reported BLAS values (reference targets, transcribed from the PDF)

- **R-EVAL-019** The reproduction report shall present its results in the same layout as Tables II–VI and Figs. 6–7, next to the values below. What counts as "reproduced" (a numeric tolerance, or trend agreement only) is not defined by the paper: GAP-43.

**Table II, BLAS query time (ms).** Blocks: 128 / 512 / 1024 / 4096 / 8192.

| Scenario | Android | Spark |
|---|---|---|
| Boolean AND | 1.17 / 2.66 / 7.31 / 29.06 / 60.65 | 0.634 / 1.41 / 3.88 / 14.08 / 28.93 |
| AND + RANGE 20% | 1.84 / 3.28 / 4.77 / 12.45 / 18.93 | 1.01 / 1.2 / 1.8 / 5.67 / 8.6 |
| Boolean OR | 12.79 / 49.32 / 97.22 / 385.37 / 965.87 | 12.1 / 35.24 / 63.24 / 242.05 / 475.68 |
| OR + RANGE 20% | 12.26 / 24.73 / 27.17 / 118.48 / 190.66 | 12.67 / 18.62 / 31.9 / 99.05 / 182.87 |
| Boolean NOT-AND | 50.77 / 235.16 / 432.27 / 1694 / 3863 | 11.59 / 46.68 / 93.87 / 375.27 / 757.36 |
| NOT-AND + RANGE 20% | 34.97 / 54.48 / 95.60 / 386.50 / 894.89 | 6.93 / 14.87 / 24.67 / 85.82 / 168.18 |
| Complex + RANGE 20% | 5.90 / 25.31 / 47.68 / 185.51 / 374.96 | 4.65 / 4.72 / 4.59 / 15.35 / 26.39 |

**Table III, BLAS VO size (KB).** Blocks: 128 / 512 / 1024 / 4096 / 8192.

| Scenario | Android | Spark |
|---|---|---|
| Boolean AND | 4.87 / 14.89 / 32.85 / 135.12 / 268.59 | 4.17 / 12.3 / 23.71 / 93.3 / 186.08 |
| AND + RANGE 20% | 10.52 / 17.22 / 27.38 / 56.29 / 128.85 | 9.21 / 14.43 / 20.87 / 42.75 / 77.9 |
| Boolean OR | 8.11 / 29.68 / 59.06 / 236.23 / 472.46 | 4.51 / 13.789 / 27.13 / 107.81 / 215.68 |
| OR + RANGE 20% | 17.48 / 33.54 / 52.91 / 156.36 / 305.50 | 14.01 / 21.69 / 32.97 / 81.28 / 150.15 |
| Boolean NOT-AND | 5.90 / 25.31 / 47.68 / 185.51 / 374.96 | 5.67 / 20.27 / 40.09 / 160.93 / 321.85 |
| NOT-AND + RANGE 20% | 0.46 / 21.80 / 36.61 / 121.53 / 224.17 | 14.01 / 28.78 / 44.63 / 135.07 / 252.99 |
| Complex + RANGE 20% | 19.16 / 37.36 / 59.22 / 177.99 / 348.30 | 32.81 / 32.81 / 32.81 / 79.83 / 149.88 |

**Table IV, BLAS construction.**
- Full node: 0.79 s/block, 124 KB/block.
- SP: 0.05 s/file, 420 KB/file.

**Tables V and VI, BLAS range scalability.** Widths: 10% / 20% / 30% / 40% / 50%.
- Query time (ms): Android 86.69 / 135.29 / 212.82 / 268.87 / 331.56; Spark 63.936 / 106.94 / 152.98 / 197.07 / 242.05.
- VO size (KB): Android 27.95 / 38.90 / 51.59 / 62.57 / 73.51; Spark 63.936 / 106.94 / 152.98 / 197.079 / 242.051. The Spark VO row equals the Spark time row: GAP-45.

**Fig. 6, keyword scalability at 1024 blocks (text values).**
- AND: 3.45 ms → 15.00 ms and 23.05 KB → 111.92 KB, for 2 → 10 keywords. About +1.44 ms and +11.11 KB per added keyword.
- OR: 57.58 ms → 321.25 ms and 32.85 KB → 173.42 KB. From 2 to 3 keywords: 57.58 → 133.01 ms and 32.85 → 85.13 KB.

**Fig. 7, ablation (text values).**
- Query time, No-aggre vs. All: 1.33 vs. 1.25 ms at 128 blocks; 32.85 vs. 17.92 ms at 8192.
- Query time, No-multi-win vs. All: 9.85 vs. 7.98 ms at 4096; 23.61 vs. 17.92 ms at 8192.
- VO size, No-aggre vs. All: 11.22 vs. 10.52 KB at 128; 149.99 vs. 128.85 KB at 8192.
- VO size, No-multi-win vs. All: 71.56 vs. 56.29 KB at 4096; 155.67 vs. 128.85 KB at 8192.
- The All VO values (10.52, 56.29, 128.85) coincide with the Table III Android AND + RANGE 20% row. This suggests, but does not state, the dataset (GAP-42).

- **R-EVAL-020** Qualitative claims to check (VIII):
  - query time and VO size grow with block count;
  - on-chain aggregation contributes most to query-time reduction;
  - multi-window partitioning contributes most to VO compactness at scale;
  - OR queries cost substantially more than AND queries as the keyword count grows;
  - BLAS query time scales roughly linearly with range width.
  
  Comparative claims against baselines apply only if GAP-44 brings the baselines into scope.
- **R-EVAL-021** Randomness: the paper defines no RNG, no seed, and no reproducibility procedure for either the generator assignment or the query generation. The reproduction shall make every random choice seedable and record the seeds, once GAP-41 and GAP-42 are decided.

## 13. GO IMPLEMENTATION CONSTRAINTS

Go is the implementation language. Nothing in this section may change BLAS protocol semantics. Every rule below either (a) restates a protocol rule already fixed in §1–§12 so that a Go implementation cannot silently drift from it, or (b) names a point where a Go default would diverge from the paper and routes it to the governing requirement or to a GAP.

**Tag meaning in this section.** The classification tag on an `R-GO-*` item describes the status of the *underlying protocol statement*, not the status of the Go idiom. `[MANDATED]` means the paper fixes the behaviour and Go must conform; `[UNSPECIFIED]` means the paper fixes nothing and the item points at a GAP; `[EXPERIMENT]` means it only constrains the evaluation harness.

**Non-normative content is marked explicitly.** Only §13.6 (package boundaries) is non-normative.

### 13.1 Language, toolchain, dependencies

- **R-GO-001** `[MANDATED]` Only the implementation language changes from the paper. The Go implementation shall reproduce the BLAS protocol, authenticated data structures, hashing behaviour, query semantics, verification logic, optimizations and experiment design exactly as specified in §1–§12. No Go idiom, library convention or performance concern justifies a semantic deviation. — VIII "Implementation"; R-EVAL-006
- **R-GO-002** `[MANDATED]` H(·) shall be BLAKE2b with a 256-bit output (R-CRYPTO-001). The Go standard library does **not** provide BLAKE2b. The implementation shall use the extended-standard-library package `golang.org/x/crypto/blake2b`. SHA-256, SHA-3, `hash/fnv`, `crypto/sha256` or any other digest shall not be substituted, and BLAKE2s shall not be substituted for BLAKE2b. The exact parameterisation (digest-length parameter 32 vs. truncation of a 64-byte digest; keyed vs. unkeyed; salt/personalization) is **not** settled by this section — see GAP-1. Adoption of the dependency itself requires user approval — see GAP-52.
- **R-GO-003** `[MANDATED]` **Dependency rule.** An external Go module may be added only if (1) BLAS as specified requires the functionality, **and** (2) the Go standard library cannot provide it, **or** (3) the user has explicitly approved it. A dependency that merely makes implementation more convenient is prohibited. Every candidate is a *proposal requiring approval*, never a decision taken in this document. — STRICT NO-EXTRA-FEATURE RULE; R-FUNC-019, R-FUNC-020
- **R-GO-004** `[UNSPECIFIED]` **Candidate dependency list (proposals only, all pending approval).**

  | Candidate | Needed for | Stdlib alternative | Status |
  |---|---|---|---|
  | `golang.org/x/crypto/blake2b` | R-CRYPTO-001 BLAKE2b-256; stdlib has no BLAKE2b | none (only writing BLAKE2b from scratch) | **Proposed**, pending GAP-52 |
  | any bitmap library (e.g. roaring) | R-IOMF-009 bitmaps | `[]byte` / `math/big` in stdlib suffice; compression is optional (R-IOMF-015) | **Not proposed** — rejected under R-GO-003(2) unless GAP-3 selects compression |
  | any Merkle-tree library | OMT/IIMT/RIMBT | hash formulas are BLAS-specific (R-CRYPTO-003..008); no library implements them | **Not proposed** — would risk substituting an alternative construction |
  | any blockchain/consensus framework | GAP-23 | — | **Not proposed** — barred by the no-extra-feature rule; see GAP-23 |
  | any database / KV store | GAP-39 | — | **Not proposed** — see GAP-39 |
  | any serialization framework (protobuf, gob, JSON, CBOR) | GAP-2, GAP-24, GAP-38 | — | **Not proposed** — hash-input encoding is a GAP decision, not a library choice; see R-GO-021 |
  | a signature library | R-CRYPTO-012 | `crypto/ed25519`, `crypto/ecdsa` are in the stdlib | **Not proposed** — the algorithm itself is GAP-25 |

- **R-GO-005** `[EXPERIMENT]` The Go toolchain version, GOOS/GOARCH, and the exact module versions used shall be recorded alongside every reported measurement, in addition to the hardware and OS required by R-EVAL-007. The paper's reference platform (VIII) is context only.

### 13.2 Determinism of hashed state

Every rule here exists because a root hash, a proof, or an accept/reject decision depends on it.

- **R-GO-006** `[MANDATED]` No hashed or serialized structure shall be built by iterating a Go `map`. Go map iteration order is randomized per run. Ordering is fixed by the protocol: IIMT leaves in kid order (R-IOMF-008, R-CRYPTO-009), RIMBT leaves in ascending v (R-IOMF-017), OMT leaves in ascending oid (R-IOMF-001), transactions by lid then fid (R-ALIS-011), children in left-to-right order (R-CRYPTO-009). Maps may be used only as lookup accelerators whose iteration is never observed. — R-CRYPTO-009
- **R-GO-007** `[MANDATED]` Any sort that feeds a hashed structure shall implement a **total order**. `sort.Slice` is not stable; `sort.SliceStable` preserves input order for equal keys, which is itself an input-order dependency. The lid-then-fid sort of R-ALIS-011 shall therefore use a comparator that is total over (lid, fid). Whether (lid, fid) is unique, and whether the comparison is numeric or lexicographic and ascending or descending, is GAP-20. Until GAP-20 is decided, a tie in (lid, fid) is an unspecified condition, not a case for Go's default behaviour.
- **R-GO-008** `[MANDATED]` Integers that enter a hash input (oid, esn, tsn, kid, BlockHeight, lid, fid, numeric value v, timestamps) shall be encoded by an explicit, fixed-width, fixed-endianness rule. Go offers no default: `binary.BigEndian`, `binary.LittleEndian`, `binary.PutVarint`, `strconv.Itoa` and `fmt.Sprintf("%d")` all produce different bytes and therefore different roots. The rule is **not chosen here** — see GAP-2. `encoding/binary` with an implicit `int` width, and any text formatting of integers, are prohibited until GAP-2 is decided.
- **R-GO-009** `[MANDATED]` Go's `int`/`uint` are platform-dependent in width. No value that is hashed, serialized, or compared as a protocol quantity shall be typed `int`/`uint`; fixed-width types shall be used. Purely internal indices (slice offsets) may be `int` provided they never reach a hash input and never bound a protocol quantity. Note that tsn, esn and kid are protocol values, not slice offsets (R-GO-016). — R-CRYPTO-010, GAP-2
- **R-GO-010** `[MANDATED]` The `||` concatenation in R-CRYPTO-003, R-CRYPTO-004, R-CRYPTO-005, R-CRYPTO-006 and R-CRYPTO-008 is not defined by the paper as a framed encoding. In Go, `string + string`, `append([]byte, ...)`, `bytes.Join` and `strings.Builder` all yield unframed concatenation, which is ambiguous for variable-length fields (keywords, value sets V, bitmaps). Unframed concatenation shall not be adopted by default; the framing rule is GAP-2, constrained by R-SEC-013 (the encoding must make leaf and internal-node contents uniquely recoverable, otherwise the Theorem 1 collision argument fails).
- **R-GO-011** `[MANDATED]` Nested digests (the inner `H(bm)` of R-CRYPTO-003 and R-CRYPTO-005, the child hashes of R-CRYPTO-004 and R-CRYPTO-006) shall be fed to the outer hash in exactly one representation. Go's `hex.EncodeToString`, `%x` formatting and raw `[]byte` are three different inputs. The choice is GAP-2. No implicit conversion through `string(h)` shall be relied on.
- **R-GO-012** `[MANDATED]` Keyword strings shall be hashed byte-for-byte as stored. Go applies no Unicode normalization, but the implementation shall not add any (`strings.ToLower`, NFC/NFD normalization, trimming) — that would alter both leaf hashes and query matching. Any normalization would be part of keyword extraction, which is GAP-46, not a Go-level decision.
- **R-GO-013** `[MANDATED]` Floating-point types shall never hold a value that is hashed or range-compared: timestamps, ts_min/ts_max, FileTsMin/FileTsMax and RIMBT leaf values v are exact protocol quantities. Their representation is GAP-22. `time.Time` shall not be hashed directly — its internal representation is not a specified wire format (GAP-2, GAP-22).
- **R-GO-014** `[MANDATED]` Reflection-based or schema-driven encoders (`encoding/json`, `encoding/gob`, `encoding/xml`, struct tags) shall not be used to produce any hash input or any committed serialization (object hashes, transactions, block headers, VO components). Field order, optional-field omission and numeric formatting in those encoders are not protocol-defined. See GAP-2, GAP-4, GAP-24, GAP-38.

### 13.3 Structure-specific Go constraints

- **R-GO-015** `[MANDATED]` **Bitmap bit numbering is a protocol property, not a Go one.** R-IOMF-009 fixes "bit i indicates object i"; Fig. 2 depicts the leftmost printed bit as object 1. Go's `[]byte`, `[]uint64` and `math/big.Int` each impose a different natural bit order (and `math/big` is little-endian by word with no fixed length). The internal representation is free; the *serialized* bit order and the origin of i are fixed by GAP-3 and must be implemented from that decision, not from the chosen container's convenience.
- **R-GO-016** `[MANDATED]` Bitmap length shall be an explicit stored quantity, never inferred from `len(slice)*8` or from slice capacity, because padding to a byte or word boundary changes the serialized bytes and therefore the leaf hash. The length is N, the number of objects (R-IOMF-009). How N is authenticated to the verifier — necessary for ¬ to be well defined — is GAP-50; its serialization is GAP-3.
- **R-GO-017** `[MANDATED]` oid, esn, tsn and kid shall be stored explicitly on the node/record, not represented implicitly by a slice index. tsn is assigned after the lid-then-fid sort starting at 0 (R-FUNC-008, R-ALIS-011); esn is assigned in chronological generation order with an unstated origin (R-ALIS-003, GAP-21); kid is monotonically increasing with an unstated origin (R-IOMF-008, GAP-13). Equating any of them with a 0-based Go index is an assumption that GAP-13/GAP-21 must authorise.
- **R-GO-018** `[MANDATED]` Fanout F shall be a construction-time parameter of IIMT, RIMBT and (subject to GAP-5) OMT. It shall not be hard-coded to 2, and shall not be hard-coded to the experiment value 4 (R-IOMF-011; R-EVAL-008 is `[EXPERIMENT]` only). Node children shall be held in an ordered slice indexed c1..cF; the slice index order **is** the protocol child order of R-CRYPTO-009. Handling of a node with fewer than F children (padding, promotion, omission) is GAP-6 and shall not be decided by whatever `append` happens to produce.
- **R-GO-019** `[MANDATED]` Merkle proofs shall be represented as ordered sequences with explicit position information, not as Go `map`s or unordered sets. The paper writes proof accumulation with set union (`π ← π ∪ {...}`, Alg. 4, Alg. 5), but verification requires knowing each sibling's position among its F−1 siblings and each level's order. That positional content is GAP-9; the merge rule for multiple paths is GAP-10. Go must not resolve either by picking a convenient container.
- **R-GO-020** `[MANDATED]` Alg. 1's update behaviour shall be implemented literally (R-IOMF-012): set bit oid on leaves whose keyword is in `o.keywords`, update *those* leaves' hashes, then recompute every non-leaf hash bottom-up. Dirty-flag, copy-on-write, lazy-recompute or incremental-root optimizations are permitted only if they provably produce byte-identical node hashes and the identical set of recomputed leaf hashes. The interaction between "only matching leaves' hashes are updated" and a length-N bitmap serialization is GAP-3 and shall not be resolved by an optimization.
- **R-GO-021** `[MANDATED]` The RIMBT shall be implemented as the paper's Merkle B-tree (R-IOMF-016) with the IV-C node contents and hash formulas. A Go B+-tree library, `sort.Search` over a flat array, or any structure whose internal nodes carry contents other than `(V, h)` may be used only if the committed node contents and hashes are identical to R-IOMF-018. Split, minimum/maximum occupancy and rebalancing are GAP-6; insertion of a value below the current minimum is GAP-8.
- **R-GO-022** `[MANDATED]` The entry-level structure shall be the OMT combined with B+ Tree temporal information (R-ALIS-007). It shall not be replaced by a RIMBT, a Go time-index, or a sorted-slice binary search whose committed contents differ. The node contents and their hashing are GAP-17.
- **R-GO-023** `[MANDATED]` Query results are sets (R-QUERY-025) and verification compares sets (R-VER-004). Go set comparison shall be order-independent and shall not depend on map iteration. Any *emitted* order (wire order of QR, QR_O, VO components) affects VO bytes and VO size and is GAP-38; it shall be fixed by that decision, not by whatever order a loop produces.

### 13.4 Concurrency, errors, configuration

- **R-GO-024** `[MANDATED]` Concurrency shall not change any result, root hash, proof, or accept/reject outcome. The paper permits parallel transaction-level execution across blocks and parallel entry-level execution across files (R-QUERY-017, R-QUERY-023) because those indexes are independent. No other parallelism is authorised by the paper. Goroutine scheduling shall never influence ordering of hashed or serialized data (R-GO-006). Whether the reproduction parallelises at all, and with what degree of parallelism, is GAP-40.
- **R-GO-025** `[MANDATED]` Verification shall expose a single accept/reject outcome (R-VER-011, R-VER-012). Go `error` values and wrapped errors may carry diagnostics for tests, but the implementation shall not define an error taxonomy that the protocol does not have, and no caller behaviour may branch on an error kind in a way that changes accept/reject. A verification failure of any kind is a reject.
- **R-GO-026** `[MANDATED]` Panics shall not be used as protocol control flow. A malformed or adversarial VO from the SP (R-SEC-001) is an expected input and shall produce reject, not a panic; slice indexing driven by attacker-supplied lengths/indices shall be bounds-checked explicitly.
- **R-GO-027** `[EXPERIMENT]` No §12 value shall appear as a constant in protocol packages: 4096 generators, 2048 entries/file, 256 files/block, block counts {128, 512, 1024, 4096, 8192}, fanout 4, block-level index size 1024, range widths 10–50%, keyword counts 2–10, 10 queries per point. All are configuration inputs to the evaluation harness (R-EVAL-002/003/004/008/010/012/014/015/016).
- **R-GO-028** `[EXPERIMENT]` All randomness (generator assignment, query generation) shall use an explicitly seeded, recorded RNG; Go's global `math/rand` auto-seeding and `maps`/goroutine nondeterminism shall not be relied on. `crypto/rand` shall not be used where reproducibility is required. The generation procedures themselves are GAP-41 and GAP-42; R-GO-028 only fixes seedability (R-EVAL-021).

### 13.5 Go-default divergence checklist

Each row is a point where a Go default would silently differ from the paper. Column 3 says whether the point is already governed or is open.

| # | Go default that would diverge | Governed by / open as |
|---|---|---|
| 1 | `map` iteration order is randomized | Governed: R-CRYPTO-009, R-IOMF-008, R-IOMF-017, R-ALIS-011 via R-GO-006 |
| 2 | `sort.Slice` is not stable; equal keys reorder | Governed: R-ALIS-011 via R-GO-007. Tie semantics **open**: GAP-20 |
| 3 | `int`/`uint` width varies by platform | Governed: R-CRYPTO-010 via R-GO-009. Encoding **open**: GAP-2 |
| 4 | No default endianness for integers in a hash input | **Open**: GAP-2 |
| 5 | `[]byte` vs `string` concatenation; unframed `||` | **Open**: GAP-2 (constrained by R-SEC-013) |
| 6 | Nested digest as raw bytes vs. hex text | **Open**: GAP-2 |
| 7 | Bit order inside `[]byte` / `[]uint64` / `math/big.Int` | **Open**: GAP-3 |
| 8 | Bitmap padded to byte/word boundary | **Open**: GAP-3; length handling governed by R-GO-016, authentication of N **open**: GAP-50 |
| 9 | `encoding/json`/`gob` field order and omission | Governed (prohibited): R-GO-014. Actual encoding **open**: GAP-2, GAP-4, GAP-24, GAP-38 |
| 10 | `time.Time` marshalling as the timestamp wire form | **Open**: GAP-22 |
| 11 | Slice index used as oid/esn/tsn/kid | Governed: R-GO-017. Origins **open**: GAP-13, GAP-21 |
| 12 | `append` producing a short last child group | **Open**: GAP-6 |
| 13 | Binary tree assumed for OMT because it is the common Go Merkle idiom | **Open**: GAP-5 |
| 14 | Odd leaf duplicated/promoted (common Merkle library behaviour) | **Open**: GAP-5, GAP-6 |
| 15 | Proof as `map[level]hash`, losing sibling position | Governed: R-GO-019. Proof content **open**: GAP-9, GAP-10 |
| 16 | Result sets as `map[uint64]struct{}` compared by iteration | Governed: R-VER-004, R-QUERY-025 via R-GO-023. Wire order **open**: GAP-38 |
| 17 | Goroutine completion order affecting VO assembly order | Governed: R-GO-024. Degree of parallelism **open**: GAP-40 |
| 18 | Rich Go error taxonomy leaking into accept/reject | Governed: R-VER-012 via R-GO-025 |
| 19 | `crypto/sha256` chosen because BLAKE2b is not in stdlib | Governed (prohibited): R-CRYPTO-001 via R-GO-002. Parameterisation **open**: GAP-1; dependency approval **open**: GAP-52 |
| 20 | `blake2b.Sum512` truncated to 32 bytes instead of `blake2b.New256` | **Open**: GAP-1 (these give different digests) |
| 21 | Unicode normalization / case folding of keywords | Governed (prohibited): R-GO-012. Extraction rules **open**: GAP-46 |
| 22 | Float timestamps or `float64` seconds | Governed (prohibited): R-GO-013. Representation **open**: GAP-22 |

### 13.6 Package boundaries (NON-NORMATIVE)

The following module layout is a **recommendation only**. It is a mapping onto protocol requirements that are already fixed; it creates no requirement, and any other layout that satisfies §1–§12 and §13.1–§13.5 is equally acceptable. Nothing in this subsection may be cited as a source for behaviour.

| Package (suggested) | Maps onto | Notes |
|---|---|---|
| `hash` | R-CRYPTO-001/002/010/011 | Single entry point for H(·) and for the encoding rules once GAP-1/GAP-2 are decided |
| `bitmap` | R-IOMF-009, R-IOMF-024 | Bit order and serialization from GAP-3; NOT requires an explicit N (GAP-50) |
| `omt` | R-IOMF-001..006 | Built over a complete immutable object set |
| `iimt` | R-IOMF-007..015, Alg. 1, Alg. 4 | Incremental update |
| `rimbt` | R-IOMF-016..021, Alg. 5 | Incremental insertion (GAP-8) |
| `iomf` | R-IOMF-022..025, Alg. 3 | Query division, VO assembly |
| `alis` | R-ALIS-001..024 | Three levels, each an IOMF instantiation |
| `chain` | R-FUNC-007..010, R-ALIS-011 | Blocks, headers, transactions; representation of the chain is GAP-23 |
| `query` | R-QUERY-001..025 | Q, Q_on, Q_off, three stages |
| `verify` | R-VER-001..014, R-SEC-006..012 | Single accept/reject |
| `optimize` | R-OPT-001..015 | Both optimizations, independently switchable (R-OPT-015) |
| `eval` | §12 entirely | The only package permitted to hold experiment constants (R-GO-027) |

## 14. ACCEPTANCE TEST MATRIX

**How to read this matrix.**

- A test marked **DETERMINISTIC** can be written and run today: its expected result follows from §1–§12 without any open decision.
- A test marked **BLOCKED(GAP-n)** cannot have a fully determined expected result until GAP-n is decided. Where part of the test is still checkable (structure, ordering, accept/reject behaviour) that part is stated and the blocked part is named. No missing detail is invented.
- "Fig. 2 fixture" means the eight-object example of Fig. 2 as transcribed in §3.8/§3.9: objects o1..o8; IIMT (F = 2) leaves Error 11001001, Warning 00100110, PUT 10011000, GET 01000001; RIMBT (F = 2) leaves 12: 00110010, 15: 00111010, 97: 10111110, 99: 11111111; OMT leaves 1||h1 .. 8||h8, internal h9..h14, root h15. Bit-string orientation in the fixture follows the printed figure (leftmost bit = object 1) and is *depiction only*; the serialized orientation is GAP-3.
- Expected *digest values* are never asserted as literals anywhere in this matrix, because no digest is computable until GAP-1 and GAP-2 are decided. Tests assert structure, ordering, set equality, root-equality-after-recomputation, and accept/reject.

| Test ID | Requirement ID(s) | Test scenario | Input | Expected result | Paper source | Status |
|---|---|---|---|---|---|---|
| T-001 | R-IOMF-001, R-CRYPTO-007, R-ALIS-003, R-FUNC-002 | OMT leaves are ordered by ascending oid | Objects supplied in shuffled oid order | Leaf sequence is strictly ascending in oid; root is identical to the root built from the same objects supplied in ascending order | IV-A, Fig. 2 | DETERMINISTIC |
| T-002 | R-IOMF-003, R-IOMF-004, R-CRYPTO-007 | OMT membership verification (worked example) | Fig. 2 fixture, oid = 5; proof = (5, o5, {6\|\|h6, h12, h13}) | Verifier reconstructs h'15 and h'15 == h15; accept | IV-A ("the user can reconstruct h'15 and compare it with h15") | BLOCKED(GAP-5): leaf/internal hash formula and arity unfixed; path *shape* {sibling leaf, h12, h13} is assertable today |
| T-003 | R-IOMF-002, R-IOMF-004, R-SEC-010 | Tampered-object rejection | Fig. 2 fixture; proof for oid = 5 but o5 replaced by o5' ≠ o5, path unchanged | h'15 ≠ h15; reject | IV-A; VII Theorem 2 case 1 | BLOCKED(GAP-4, GAP-5) for the digest; the reject outcome is assertable once any encoding is in place |
| T-004 | R-IOMF-002, R-CRYPTO-007 | oid–object binding cannot be swapped | Fig. 2 fixture; proof presents o5 under oid = 6 | Reject (reconstructed root ≠ h15) | IV-A ("guarantees the ID-object mapping") | BLOCKED(GAP-5): depends on whether/how oid enters the leaf hash |
| T-005 | R-IOMF-005 | Non-existence / boundary evidence via adjacent leaves | Sorted OMT; query for an absent oid | Proof returns the two adjacent leaves with their paths; verifier confirms adjacency and that neither equals the queried oid | III, V-D | BLOCKED(GAP-5, GAP-19): adjacency-check form and missing-neighbour cases open |
| T-006 | R-CRYPTO-003, R-IOMF-010 | IIMT leaf hash formula | Leaf (w, bm) | Implementation computes H(w \|\| H(bm)) — bitmap digested first, keyword first in the outer input; not H(w \|\| bm), not H(H(bm) \|\| w) | IV-B | DETERMINISTIC as a formula-shape test; byte value BLOCKED(GAP-2, GAP-3) |
| T-007 | R-CRYPTO-004, R-IOMF-007, R-IOMF-010 | IIMT non-leaf hash formula and child order | Node with children c1..cF | Computes H(h_c1 \|\| ... \|\| h_cF) in left-to-right order; reordering children changes the hash | IV-B | DETERMINISTIC as shape; incomplete node BLOCKED(GAP-6) |
| T-008 | R-IOMF-013, R-IOMF-009, R-ALIS-004 | IIMT keyword lookup (Alg. 4) | Fig. 2 fixture; w = "PUT" | Returns bm = 10011000, QR = {1, 4, 5}, and a proof π consisting of the sibling hashes on the leaf-to-root path | IV-D, Alg. 4 | DETERMINISTIC for QR/bm under the fixture's stated bit orientation; π content BLOCKED(GAP-9) for F > 2 |
| T-009 | R-IOMF-008 | Array-based leaf lookup by kid | IIMT with keyword→kid mapping | `leaf_nodes[kid]` returns the leaf for that keyword in O(1); leaf order equals kid order | IV-B, IV-D, Alg. 4 | BLOCKED(GAP-13): kid origin and leaf universe open |
| T-010 | R-IOMF-024, R-QUERY-003, R-QUERY-004, R-FUNC-016 | AND/OR/NOT bitmap evaluation (worked example) | Fig. 2 fixture; expression ¬Error ∧ PUT ∨ GET | Evaluated as ((¬bm_Error) ∧ bm_PUT) ∨ bm_GET = 01010001 | IV-D worked example | DETERMINISTIC (bit values as printed in Fig. 2); ¬ over a length-N bitmap requires N — BLOCKED(GAP-50) when N is not the fixture's 8 |
| T-011 | R-IOMF-013, R-VER-002 | IIMT proof verification and tampered-bitmap rejection | Fig. 2 fixture; (a) genuine (bm_PUT, π); (b) bm_PUT with one bit flipped, π unchanged | (a) reconstructed IIMT root == committed root, accept; (b) root mismatch, reject | IV-B, IV-E | BLOCKED(GAP-2, GAP-3) for digests; accept/reject behaviour assertable |
| T-012 | R-IOMF-014 | Merged multi-keyword IIMT proof | Fig. 2 fixture; keywords {Error, PUT, GET} | π_K = {h2}: shared internal hashes appear once, and every queried leaf still verifies to the root from π_K alone | IV-D example, Alg. 3 line 5 | BLOCKED(GAP-10): merge rule not stated. The size assertion (π_K = one hash for this fixture) is the paper's own stated outcome |
| T-013 | R-IOMF-012 | Algorithm 1 update behaviour | Fig. 2 fixture IIMT; insert o9 with keywords {PUT, GET} | Bit 9 set in bm_PUT and bm_GET only; leaf hashes of PUT and GET recomputed; leaf hashes of Error and Warning **not** recomputed; all non-leaf hashes recomputed bottom-up; root changes | IV-B, Alg. 1 | BLOCKED(GAP-3): interaction of unchanged non-matching leaf hashes with a length-N bitmap is unresolved; the "which leaves are recomputed" assertion is the paper's literal text |
| T-014 | R-ALIS-005 | Absent entry keyword keeps an all-zero leaf | Entry-level IIMT for a file whose entries use only a subset of the generator's keyword set | Every generator keyword has exactly one leaf; unused keywords carry all-zero bitmaps; no leaf is removed | V-C | DETERMINISTIC for the structural assertion |
| T-015 | R-ALIS-006, R-FUNC-005 | New entry keyword appears | Generator emits file F1, then a new keyword w_new appears, then file F2 | w_new is appended to the generator's keyword set and has a leaf in F2's IIMT; F1 is not rebuilt and its committed roots are unchanged; for F1 the new keyword is treated as an all-zero bitmap | V-C, II-B | BLOCKED(GAP-13, GAP-14): how the verifier authenticates the all-zero treatment, and whether the currently open file counts as "subsequent", are unspecified |
| T-016 | R-IOMF-017, R-CRYPTO-005, R-IOMF-016 | RIMBT cumulative bitmap semantics | Fig. 2 fixture RIMBT | For every leaf v and object i: bm(v)[i] == 1 iff attr(i) ≤ v; leaves ascend 12, 15, 97, 99 with the fixture bitmaps | IV-C, Fig. 2 | DETERMINISTIC |
| T-017 | R-IOMF-019 | RIMBT exact-value insertion (leaf exists) | Fixture RIMBT; insert o9 with attr = 15 | No new leaf; bit 9 set in the bitmaps of leaves 15, 97, 99 (from position x to the last leaf) and **not** in leaf 12; affected leaf hashes and all affected internal hashes recomputed | IV-C prose | DETERMINISTIC for the bit pattern; hash values BLOCKED(GAP-2) |
| T-018 | R-IOMF-019 | RIMBT new-value insertion (value between existing values) | Fixture RIMBT; insert o9 with attr = 50 | New leaf v = 50 placed between 15 and 97; its bitmap initialized as a copy of leaf 15's bitmap; then bit 9 set in leaves 50, 97, 99; V sets updated; hashes recomputed bottom-up | IV-C prose | BLOCKED(GAP-6): node split/occupancy on insertion unspecified; the bitmap-copy and suffix-update assertions are deterministic |
| T-019 | R-IOMF-019 | RIMBT insertion below the current minimum | Fixture RIMBT; insert o9 with attr = 5 | Undefined: the paper's rule copies "the bitmap of its predecessor leaf_nodes[x−1]", which does not exist | IV-C prose | BLOCKED(GAP-8) — no expected result may be asserted until GAP-8 is decided |
| T-020 | R-IOMF-018, R-CRYPTO-006 | RIMBT internal-node value set V | Fixture RIMBT (F = 2) | Parent of {12, 15} has V = {15}; parent of {97, 99} has V = {99}; root has V = {97}; node hash is H(V \|\| h_c1 \|\| ... \|\| h_cF) | IV-C, Fig. 2 | BLOCKED(GAP-7): the definition of V_ci at leaf level is unstated, so V for F > 2 and for deeper trees cannot be derived; the Fig. 2 values are assertable |
| T-021 | R-IOMF-020 | Range boundary query (Alg. 5, worked example) | Fixture RIMBT; SR = [13, 98] | BM = {bm(12), bm(97)}; QR = ¬bm(12) ∧ bm(97) = 10001100 → {1, 5, 6}; π_SR = {\<15\>, \<99\>, \<15, h2\>, \<99, h4\>} | IV-D worked example, Alg. 5 | DETERMINISTIC for BM/QR; π_SR shape assertable from the example, general content BLOCKED(GAP-9) |
| T-022 | R-IOMF-020 | Range query where β equals an existing leaf value | Fixture RIMBT; SR = [13, 99] | Alg. 5 line "if ln_{y+1}.v = β" branch taken: BM = {bm(12), bm(99)}; QR = ¬bm(12) ∧ bm(99) | Alg. 5 | DETERMINISTIC |
| T-023 | R-IOMF-020, R-VER-006, R-VER-002 | Range proof verification and tampered-boundary rejection | Fixture; (a) genuine (BM, π_SR); (b) bm(97) with one bit flipped | (a) reconstructed RIMBT root == committed root, accept; (b) reject | IV-D, IV-E | BLOCKED(GAP-2) for digests; accept/reject assertable |
| T-024 | R-VER-006 | Boundary leaves must straddle α and β | Fixture; SP returns bm(12) and bm(99) for SR = [13, 98] (a non-adjacent, wrong-boundary pair that still verifies against the root) | Reject: the verifier must check that ln_x.v < α ≤ ln_{x+1}.v and the corresponding β condition, i.e. that the shipped boundary leaves are the correct adjacent pair | IV-D, Alg. 5, VII Theorem 2 case 3 | BLOCKED(GAP-9, GAP-11, GAP-12): the check is necessary for completeness but its exact form depends on whether leaf values are shipped and on adjacency evidence |
| T-025 | R-IOMF-020 | Range query with out-of-domain boundaries | Fixture; (a) SR = [5, 98] (α below min v, so ln_x does not exist); (b) SR = [13, 200] (β above max v, so ln_{y+1} does not exist); (c) α > β | Undefined by Alg. 5 | Alg. 5 | BLOCKED(GAP-11) — no expected result may be asserted |
| T-026 | R-IOMF-023, R-IOMF-022, R-IOMF-021, R-QUERY-006 | End-to-end IOMF query (Alg. 3) | Fixture IOMF; Q = \<(¬Error ∧ PUT ∨ GET), ([13, 98])\> | DivideQuery yields BSQ and RSQ = {[13,98]}; QR_K = 01010001 → {2, 4, 8}; QR_R = {1, 5, 6}; QR_O = QR_K ∩ QR_R = ∅ for this fixture (assert the computed intersection, not a guessed value); VO contains (BM_K, π_K), (BM_SR, π_SR), QR_O, π_O | IV-D, Alg. 3 | DETERMINISTIC for the set algebra; π components BLOCKED(GAP-9, GAP-10); empty-result VO handling BLOCKED(GAP-49) |
| T-027 | R-VER-001, R-VER-002, R-VER-004, R-IOMF-025 | IOMF-level verification, honest server | Honest (QR, VO) from T-026 with the committed RH | Integrity: every reconstructed root equals its RH entry. Correctness: QR'_O recomputed from BM_K/BM_R equals QR_O, and oids derived from QR equal QR_O. Accept | IV-E | BLOCKED(GAP-2) for digests; the two-part check and accept outcome are assertable |
| T-028 | R-VER-005, R-VER-003 | Verifier does not trust server-supplied result sets | VO whose QR_O has been replaced by a set that does **not** follow from BM_K/BM_R, all Merkle proofs valid | Reject: the verifier recomputes QR'_O from the authenticated bitmaps and finds QR'_O ≠ QR_O | IV-E ("independently computed set") | DETERMINISTIC |
| T-029 | R-SEC-012, R-SEC-009(c), R-SEC-007 | **Omission detection** | Honest run, then (a) an oid is removed from QR but left in QR_O; (b) an oid is removed from both QR and QR_O with bitmaps untouched | (a) reject on mismatch between oids derived from QR and QR_O; (b) reject because QR'_O recomputed from authenticated bitmaps contains the oid | VII Theorem 2 case 3 | DETERMINISTIC |
| T-030 | R-SEC-011, R-SEC-009(b), R-SEC-006 | Non-satisfying entry injected | Honest run, then an object not satisfying Q is added to QR (a) without and (b) with a matching addition to QR_O | (a) reject on QR/QR_O oid mismatch; (b) reject on QR'_O ≠ QR_O or on bitmap/proof integrity failure | VII Theorem 2 case 2 | DETERMINISTIC |
| T-031 | R-SEC-010, R-SEC-009(a) | Forged entry not in the dataset | Result contains an entry never committed, with a fabricated OMT proof | Reject: the entry-level OMT proof does not reconstruct the OMT root stored in the transaction | VII Theorem 2 case 1 | BLOCKED(GAP-2) for digests; reject outcome assertable |
| T-032 | R-SEC-008 | Maintenance security (Definition 1) smoke test | Two datasets LD_real ≠ LD_forged differing in exactly one entry field, one bit of one bitmap, and one oid respectively | RH_real ≠ RH_forged in all three cases | VII Definition 1, Theorem 1 | BLOCKED(GAP-4): if the object serialization omits a field (e.g. esn per GAP-37), differences in that field will not change RH — this test is also the detector for that failure mode |
| T-033 | R-FUNC-018, R-QUERY-001, R-QUERY-002, R-QUERY-018, R-QUERY-020, R-VER-007, R-ALIS-001 | **Three-layer ALIS query, end to end** | Small chain fixture (e.g. 4 blocks × 4 transactions × 8 entries); Q = \<lg, [ts, te], FK, EK\> | Q splits into Q_on = \<lg, [ts, te], FK\> and Q_off = \<lg, [ts, te], EK\>; stage 1 → candidate BlockHeights; stage 2 → transactions + VO; auditor filters by bm_EW and sends fids + Q_off; stage 3 → entries + entry-level VO; all three verification stages pass; the returned entry set equals the ground-truth set computed directly from the fixture | V-A, V-D, V-E, Fig. 4 | BLOCKED(GAP-33): whether stage-1/2 verification precedes the bm_EW filter, and whether fids or lids are sent, is unresolved. The end-to-end result-set equality is assertable |
| T-034 | R-QUERY-009, R-QUERY-010, R-QUERY-011, R-QUERY-013, R-ALIS-018..022, R-VER-008 | **Block candidate filtering** | Chain fixture; Q_on with lg and FK and [ts, te] | Candidate set = (LG bitmap) ∧ (FK expression result) ∧ (time-range result), all over BlockHeights; every block containing a matching transaction is in the candidate set (no false negatives); block-level VO integrates the three sub-query VOs and verifies against block-header roots | V-C, V-D, V-E | BLOCKED(GAP-24): which header holds the block-level roots is unresolved; BLOCKED(GAP-15) if FK contains ¬ |
| T-035 | R-QUERY-012, R-ALIS-021, R-QUERY-005, R-FUNC-017 | Block-level time-range sub-query via the two RIMBTs | Chain fixture with known FileTsMin/FileTsMax per block; range [ts, te] | Blocks with FileTsMax ≥ ts computed as ¬bm_ln1 ∧ bm_lnr (rightmost leaf lnr); blocks with FileTsMin ≤ te computed as bm_ln3; result is the intersection; result equals the ground-truth set of blocks whose [FileTsMin, FileTsMax] intersects [ts, te] | V-D | BLOCKED(GAP-12): proof content for lnr and missing-neighbour cases unspecified; the set-level expectation is assertable |
| T-036 | R-QUERY-014, R-QUERY-015, R-QUERY-016, R-ALIS-010, R-ALIS-013..015, R-VER-009 | **Transaction filtering** | One candidate block; Q_on | Per block: lg's transactions retrieved from the transaction-level OMT with proofs and with left/right neighbour leaves; FK evaluated on the transaction-level FK IIMT; time range via ts_min/ts_max RIMBTs using ts ≤ ts_max and te ≥ ts_min; the three results intersected; transaction-level VO verifies against that block's header roots | V-C, V-D | BLOCKED(GAP-19) for neighbour content; set-level expectation assertable |
| T-037 | R-QUERY-015, R-VER-009, R-ALIS-012, R-IOMF-005 | Transaction-level completeness via neighbours | Candidate block where lg owns transactions tsn 3..6; server returns only tsn 3, 4, 5 and claims tsn 6 does not exist, offering tsn 6's own leaf as the "right neighbour" | Reject: the offered right neighbour belongs to lg, so the run is not maximal | V-D ("to guarantee the completeness"), V-E | BLOCKED(GAP-19): the exact neighbour check and the behaviour when lg's run touches tsn = 0 or the last tsn are unspecified |
| T-038 | R-FUNC-008, R-ALIS-011, R-GO-007, R-GO-017 | Transaction ordering and tsn assignment | Block with transactions supplied in arbitrary order, with several lids and several fids per lid | Transactions sorted by lid then fid; tsn assigned 0, 1, 2, ... in that order; each lg's transactions are contiguous in tsn; re-running with a shuffled input produces an identical transaction-level OMT root | V-C | BLOCKED(GAP-20): sort direction, comparison semantics (numeric vs. lexicographic) and (lid, fid) uniqueness unspecified. The contiguity and shuffle-invariance assertions hold under any consistent total order |
| T-039 | R-QUERY-021, R-QUERY-022, R-ALIS-002, R-ALIS-007, R-FUNC-013 | **Entry filtering** | One log file; Q_off = \<lg, [ts, te], EK\> | EK evaluated on the entry-keyword IIMT over esn bitmaps; entries with t ∈ [ts, te] retrieved via the OMT + B+ Tree temporal information with proofs; the two results intersected; entry-level VO assembled; returned entry set equals ground truth | V-D | BLOCKED(GAP-17, GAP-18): temporal node contents and exact VO contents unspecified |
| T-040 | R-VER-010, R-SEC-007, R-ALIS-007 | Entry-level time-range completeness via boundary leaves | File where entries 10..20 fall in [ts, te]; SP returns 10..19 and boundary leaves chosen to hide entry 20 | Reject: the boundary OMT leaves for ts and te must prove that no in-range entry lies outside the returned run | V-D, V-E | BLOCKED(GAP-17): boundary-evidence structure unspecified |
| T-041 | R-SEC-001, R-SEC-006, R-VER-011 | **Final soundness verification** | Full three-stage run where the SP adds (a) an entry from a different log generator, (b) an entry outside [ts, te], (c) an entry not satisfying EK | Reject in all three cases | II-C, II-D, V-E | BLOCKED(GAP-2) for digests; reject outcomes assertable |
| T-042 | R-SEC-007, R-VER-011 | **Final completeness verification** | Full three-stage run: (a) honest; (b) SP omits one matching entry; (c) full node omits one matching transaction; (d) full node omits one candidate block | (a) accept and the entry set equals ground truth; (b), (c), (d) reject | II-D, V-E, VII Theorem 2 case 3 | BLOCKED(GAP-19, GAP-24, GAP-29) for (c)/(d): the completeness evidence at block and transaction level depends on neighbour semantics, header anchoring and the Opt. 1 segment-coverage check |
| T-043 | R-QUERY-019, R-QUERY-008, R-FUNC-011, R-VER-014 | Auditor's bm_EW file filter | Stage-2-verified metadata set; Q_off with EK containing (a) only ∧/∨, (b) a ¬ term | Auditor selects exactly the files whose bm_EW satisfies EK | V-D | BLOCKED(GAP-15, GAP-16): ¬ semantics over a coarse per-file keyword record, and whether bm_EW is a bitmap or a Bloom filter (which admits false positives and thus changes what "satisfies" means), are unspecified |
| T-044 | R-VER-010, R-VER-013, R-ALIS-008, R-FUNC-003 | Entry-level roots anchored in a verified transaction | Three-stage run; entry-level OMT root in the returned file's RH altered by the SP | Reject: the entry-level root used by the verifier comes from the stage-2-verified transaction, not from the SP's response | V-C, VII Theorem 2 case 1 | DETERMINISTIC for the anchoring behaviour |
| T-045 | R-VER-011, R-VER-012, R-VER-004, R-FUNC-015 | Accept iff all verification outputs are true | Three-stage run with, in turn, stage 1 failing, stage 2 failing, stage 3 failing, and none failing | Reject in the first three; accept in the fourth; the output is a single accept/reject with no other outcome kind | V-E, Fig. 4 | DETERMINISTIC |
| T-046 | R-OPT-001, R-OPT-003, R-ALIS-023, R-ALIS-024, R-FUNC-009 | **Optimization 1: block-level index partition** | Chain of B blocks with segment size S | One independent block-level index per segment; each header carries PastTsMax = max FileTsMax over all blocks up to this one, BlockTsMax and BlockTsMin over the current segment; FileTsMin/FileTsMax/TxNum computed from that block's own transactions | VI, V-B | BLOCKED(GAP-27, GAP-28): whether the whole block-level index or only the RIMBT is partitioned, segment boundaries, the values in non-final blocks of an open segment, and the BlockHeight→bit mapping inside a segment are unspecified |
| T-047 | R-OPT-004, R-OPT-005, R-OPT-006, R-OPT-007 | Opt. 1 backward header scan by the auditor | Headers of several segments; range [ts, te] chosen so one segment intersects, one does not, and an earlier segment has PastTsMax < ts | Scan proceeds newest-segment-first over segment-final headers; a segment with BlockTsMin < te and BlockTsMax > ts is selected; a segment with BlockTsMax < ts or BlockTsMin > te is skipped; the scan terminates when PastTsMax < ts | VI | BLOCKED(GAP-29): equality cases (BlockTsMin = te, BlockTsMax = ts) and the meaning of "continue forward" inside a back-to-front scan are unspecified |
| T-048 | R-OPT-008 | Opt. 1 segment-coverage consistency check | Full node answers Q_on but silently skips one intersecting segment | Reject: the auditor's backward scan identifies a segment the block-level VO does not cover | VI | BLOCKED(GAP-29): the concrete comparison is not written out in the paper |
| T-049 | R-OPT-002, R-OPT-009 | Opt. 1 restricts the search to intersecting segments | Chain with many segments; a narrow time range inside one segment | Only the intersecting segment's block-level index is accessed; blocks of other segments are not read; results equal those of the unpartitioned run | VI | BLOCKED(GAP-28, GAP-34): segment size is not a protocol value, and behaviour for a query with no time range under partitioning is unspecified |
| T-050 | R-OPT-011, R-ALIS-020 | **Optimization 2: on-chain index aggregation, file keywords** | One segment; the file-keyword bitmaps of all its blocks | Per keyword, one MHT over that keyword's bitmaps across the segment's blocks; its root deposited in the corresponding block-level FK IIMT leaf (Fig. 5 leaf: W_i \| h_i / bm_i \| RH_i); a bitmap in any block verifies through the aggregated root | VI, Fig. 5 | BLOCKED(GAP-30, GAP-31): the VI introduction says the MHT is over "the roots of these transaction-level indices" while the detailed text says over same-keyword bitmaps; arity, leaf order, leaf hash, included blocks and how RH_i enters the IIMT leaf hash are unspecified |
| T-051 | R-OPT-012, R-CRYPTO-008 | Optimization 2, log-generator aggregation | One segment; all transactions grouped by lid | Per lid, one MHT whose leaves are BlockHeight \|\| tsn \|\| H(tx); its root deposited in the corresponding block-level LG IIMT leaf; one of lg's transactions in any block of the segment verifies through the aggregated root | VI, Fig. 5 | BLOCKED(GAP-31): leaf ordering, arity, and whether the leaf is H(BlockHeight\|\|tsn\|\|H(tx)) or the raw concatenation are unspecified |
| T-052 | R-OPT-010, R-OPT-013 | Aggregation reduces VO size without changing results | Same query run with aggregation on and off, over a multi-block hit set | Result set and accept/reject identical in both runs; VO contains strictly fewer hash values with aggregation on; the reduction grows with the number of hit blocks | VI, VIII-C | BLOCKED(GAP-32): which stage-2 proofs are replaced by aggregated proofs, and how the verifier checks them, is unspecified |
| T-053 | R-OPT-014, R-OPT-015 | Independent switching of both optimizations | Same query and fixture under All, No-aggre, No-multi-win | Identical result sets and identical accept/reject in all three variants; only query time and VO size differ | VIII-C, Fig. 7 | BLOCKED(GAP-32, GAP-48): aggregation is defined "within a segment", so its behaviour with partitioning disabled is undefined, and "multi-window processing" is never defined in Sec. VI |
| T-054 | R-CRYPTO-001, R-CRYPTO-002, R-CRYPTO-011, R-GO-002, R-EVAL-005 | Hash-algorithm audit | Whole source tree | Every H(·) call site resolves to BLAKE2b with 256-bit output from `golang.org/x/crypto/blake2b`; no `crypto/sha256`, `crypto/sha512`, `crypto/md5`, `hash/fnv` or BLAKE2s appears in protocol packages; no domain-separation tag, key or salt is present unless GAP-1/GAP-2 adopted one | VIII; IV-B, IV-C | DETERMINISTIC as a static audit |
| T-055 | R-CRYPTO-014 | Digest width | Any node hash | Every stored digest is exactly 32 bytes; no truncation below 256 bits anywhere | VII | DETERMINISTIC |
| T-056 | R-GO-006, R-GO-008..R-GO-011, R-CRYPTO-009, R-CRYPTO-010, R-SEC-013, R-QUERY-025 | Build determinism | Same object set built (a) twice in one process, (b) in two processes, (c) with insertion order shuffled, (d) on two GOARCH targets | Identical root hashes and identical serialized proofs in all cases; set-valued results compare equal regardless of emission order | IV-A..IV-C, VII | DETERMINISTIC as a regression test; absolute digest values BLOCKED(GAP-1, GAP-2) |
| T-057 | R-GO-006 | Map-iteration independence | Build IIMT/RIMBT/OMT from inputs held in Go maps, repeated 100 times | Identical roots in every run (proves no hashed structure is built from map iteration order) | IV-B, IV-C | DETERMINISTIC |
| T-058 | R-IOMF-011, R-GO-018, R-IOMF-006, R-EVAL-008 | Fanout parameterization | Same objects with F = 2 and F = 4 | F = 2 reproduces the Fig. 2 fixture structure; F = 4 builds without code change and yields a different, self-consistent root; F is never hard-coded | IV-B, IV-D, VIII-B | BLOCKED(GAP-5, GAP-6): whether the OMT takes F at all, and the shape of incomplete nodes, are unspecified |
| T-059 | R-GO-003, R-GO-004, R-EVAL-006 | Dependency audit | `go.mod` | No module outside the approved list; `golang.org/x/crypto` present only if GAP-52 approved it; no blockchain, database, serialization or Merkle-tree framework | STRICT NO-EXTRA-FEATURE RULE | DETERMINISTIC as a static audit |
| T-060 | R-GO-027 | No experiment constants in protocol packages | Protocol packages | None of 4096, 2048, 256, 1024, 4, {128, 512, 1024, 4096, 8192}, 10%–50%, 2–10 appears as a protocol constant; all arrive as configuration | §12 | DETERMINISTIC as a static audit |
| T-061 | R-VER-002, R-VER-008, R-CRYPTO-013, R-ALIS-016, R-ALIS-022, R-FUNC-009 | Root anchoring in block headers | Stage-1/2 verification where a committed root in the auditor's header copy is altered | Reject whenever a reconstructed root differs from the header's RH entry; the auditor uses only header state it holds locally | V-B, V-C, V-E | BLOCKED(GAP-24, GAP-28): which roots are in RH, in what order, and which header carries the block-level roots |
| T-062 | R-CRYPTO-012, R-FUNC-007, R-FUNC-010 | Transaction signature presence and no extra signatures | Block body | Each transaction carries exactly the Fig. 3 fields including one Sig; no additional signature exists anywhere in the system | V-B, Fig. 3 | BLOCKED(GAP-25): algorithm, signer, keys, signed bytes and verifier are unspecified, so signature *validity* cannot be tested |
| T-063 | R-FUNC-020, R-CRYPTO-015, R-SEC-014 | No confidentiality machinery | Whole source tree | No encryption of log payloads or indexes, no TLS requirement, no access control, no key management beyond whatever GAP-25 introduces | X | DETERMINISTIC as a static audit |
| T-064 | R-FUNC-001 | Exactly four party roles | System composition | Log generator, full node, SP, auditor only; no fifth actor; "block generator" is realised as a full-node role | II-A, Fig. 1 | BLOCKED(GAP-23) for the block-generator identification |
| T-065 | R-FUNC-015, R-VER-013 | Auditor stores only headers | Auditor state after a full run | Auditor persists block headers only; it holds no block bodies, no index structures and no raw logs | II-A | DETERMINISTIC |
| T-066 | R-FUNC-006, R-FUNC-004, R-FUNC-014, R-ALIS-009 | Storage placement audit | Deployed fixture | Off-chain: raw log data, entry keyword sets, entry-level index, per-file metadata. On-chain: log metadata, keyword set file, entry keyword sets, block-level and transaction-level indexes | V-A, V-B, V-C | BLOCKED(GAP-26, GAP-36, GAP-39): the content of the on-chain "entry keyword sets", what the generator sends to the SP, and the storage backend are unspecified |
| T-067 | R-EVAL-001..004, R-EVAL-009..012, R-FUNC-016, R-QUERY-004, R-QUERY-007 | Evaluation harness: query-time and VO-size sweep | Android and Spark datasets; 4096 generators; 2048 entries/file; 256 files/block; block counts 128, 512, 1024, 4096, 8192; the seven query types; 10 random queries per point, averaged | Tables II and III reproduced in layout with the reproduction's own numbers next to the paper's | VIII, VIII-A, Tables II, III | BLOCKED(GAP-34, GAP-37, GAP-41, GAP-42, GAP-46, GAP-47): query types 1–3 have no time range and the query form of V-D carries one, the dataset/metadata representation conflicts with II-B, and dataset preparation, keyword extraction, timestamp parsing, RNG and seeds are unspecified |
| T-068 | R-EVAL-013, R-EVAL-007, R-EVAL-017 | ADS construction measurement (Table IV) | Block-level index size 1024, fanout 4 | Full-node construction time (s/block) and ADS size (KB/block); SP construction time (s/file) and ADS size (KB/file), reported next to 0.79 s / 124 KB and 0.05 s / 420 KB | VIII-B, Table IV | BLOCKED(GAP-36, GAP-43): what is counted in each figure, who builds the off-chain ADS, CPU vs. wall clock, and the KB unit are unspecified |
| T-069 | R-EVAL-014 | Range-width scalability (Tables V, VI) | Range widths 10%, 20%, 30%, 40%, 50%; both datasets | Query time and VO size per width, reported next to the paper's values | VIII-B, Tables V, VI | BLOCKED(GAP-42, GAP-45): query type, block count and window placement are unstated, and the published Spark VO row duplicates the Spark time row |
| T-070 | R-EVAL-015 | Keyword-count scalability (Fig. 6) | 2 to 10 query keywords, block count 1024, AND and OR variants | VO size (KB) and CPU time (ms) curves next to the paper's endpoints (AND 3.45→15.00 ms, 23.05→111.92 KB; OR 57.58→321.25 ms, 32.85→173.42 KB) | VIII-B, Fig. 6 | BLOCKED(GAP-42): dataset, keyword kind (file/entry/both) and the presence of lid or a range are unstated |
| T-071 | R-EVAL-016, R-OPT-015 | Ablation experiment (Fig. 7) | Two-keyword AND Boolean range queries, selectivity 0.2; All / No-aggre / No-multi-win; block counts 128–8192 | Query time and VO size per variant, reported next to the paper's stated points | VIII-C, Fig. 7 | BLOCKED(GAP-42, GAP-45, GAP-48): dataset unstated, the Fig. 7 legend names differ from the text, and "multi-window processing" is undefined |
| T-072 | R-EVAL-020 | Qualitative claim check | Results of T-067, T-069, T-070, T-071 | Query time and VO size grow with block count; aggregation dominates query-time reduction; partitioning dominates VO compactness at scale; OR costs substantially more than AND as keyword count grows; query time scales roughly linearly with range width | VIII | BLOCKED(GAP-43, GAP-44): "roughly linearly" has no stated tolerance and baseline comparisons depend on scope |
| T-073 | R-EVAL-021, R-GO-028 | Seed and configuration recording | Any experiment run | Every random choice is seeded from a recorded value; re-running with the same seed reproduces identical datasets, queries and measurements (within timing noise) | VIII | DETERMINISTIC as a harness property |
| T-074 | R-EVAL-019, R-EVAL-018 | Report layout | Final report | Results presented in the layout of Tables II–VI and Figs. 6–7 with the paper's values alongside; baseline columns present only if GAP-44 brings vChain+/MSTDB/MART into scope | VIII | BLOCKED(GAP-43, GAP-44) |
| T-075 | R-IOMF-015, R-FUNC-019, R-SEC-014, R-GO-001 | No-extra-feature audit | Whole source tree | No Ethereum/Solidity/Hardhat/Fabric, no PostgreSQL/Cassandra/Redis, no mandatory IPFS, no REST/gRPC, no Docker/Kubernetes, no custom consensus, no Bloom filter except where GAP-16 selects one for bm_EW, no bitmap compression unless GAP-3 selects it, no Sparse Merkle or Merkle Patricia tree, no B+ tree beyond the entry-level temporal structure of R-ALIS-007, no extra query operators, no extra authenticated structures, no invented caches or indexes | STRICT NO-EXTRA-FEATURE RULE | DETERMINISTIC as a static audit |
| T-076 | R-FUNC-021 | Deployment example not implemented | Whole source tree | No logger plugin, ELK-style stack or campus-network deployment is present as a requirement-bearing component | V-F | DETERMINISTIC as a static audit |
| T-077 | R-SEC-002..R-SEC-005, R-SEC-015, R-SEC-014 | Threat-model audit | Specification and code | SP untrusted and every SP response verified; generators and auditors trusted; blockchain assumption relied on but not implemented as a stronger claim; no claim of semantic truthfulness of source logs; no security property beyond II-D and VII | II-C, II-D, VII, X | BLOCKED(GAP-35): whether a full node answering Q_on is modelled as an adversary is unresolved, and that determines whether T-042(c)/(d) are security tests or robustness tests |
| T-078 | R-QUERY-017, R-QUERY-023, R-ALIS-017, R-GO-024 | Parallel execution is result-invariant | Same query executed with parallelism disabled and enabled | Identical result sets, identical VO contents and identical accept/reject; only timing differs | V-D | BLOCKED(GAP-40): whether the reproduction parallelises, and with what concurrency, is unspecified |
| T-079 | R-IOMF-023, R-VER-004, R-QUERY-013 | Empty result at each stage | Queries that match no block, no transaction, and no entry respectively | An authenticated empty answer is produced and the verifier accepts it as complete | — (the paper describes no empty-result case) | BLOCKED(GAP-49) — no expected result may be asserted |
| T-080 | R-IOMF-009, R-IOMF-024, R-GO-015, R-GO-016 | Bitmap length is authenticated before ¬ is applied | Server supplies a bitmap that is shorter or longer than N with a valid leaf proof | The verifier must determine N from committed state before computing ¬; otherwise ¬ silently yields a different set | IV-B, IV-D | BLOCKED(GAP-50): no committed source of N exists at entry level |

## 15. REQUIREMENT TRACEABILITY MATRIX

Every requirement ID defined in §4–§13 appears exactly once, in ID order grouped by prefix: 21 R-FUNC, 15 R-CRYPTO, 25 R-IOMF, 24 R-ALIS, 25 R-QUERY, 14 R-VER, 15 R-OPT, 15 R-SEC, 21 R-EVAL, 28 R-GO = **203 rows**.

"Paper Section" gives the roman-numeral section of the PDF as cited by the requirement itself. Classification repeats the tag carried by the requirement. "Open GAPs" lists every gap the requirement depends on, including gaps cited indirectly through the structure it uses; "—" means the requirement is fully determined. Requirements whose only check is a static audit point at the corresponding audit test.

| Requirement ID | Paper Section | Algorithm/Figure/Table | Classification | Open GAPs | Acceptance test(s) |
|---|---|---|---|---|---|
| R-FUNC-001 | II-A | Fig. 1 | `[MANDATED]` | GAP-23 | T-064 |
| R-FUNC-002 | II-B, V-C | — | `[MANDATED]` | GAP-21, GAP-22 | T-001, T-039 |
| R-FUNC-003 | II-B, V-C | — | `[MANDATED]` | GAP-4, GAP-37 | T-033, T-044 |
| R-FUNC-004 | II-A, II-C | — | `[MANDATED]` | GAP-36 | T-066 |
| R-FUNC-005 | II-B, V-C | — | `[MANDATED]` | GAP-14 | T-015 |
| R-FUNC-006 | V-A, V-B, V-C | Fig. 3 | `[MANDATED]` | GAP-26 | T-066 |
| R-FUNC-007 | II-A | — | `[MANDATED]` | GAP-25 | T-062 |
| R-FUNC-008 | V-C | — | `[MANDATED]` | GAP-20, GAP-23 | T-038 |
| R-FUNC-009 | V-B, VI | Fig. 3 | `[MANDATED]` | GAP-24, GAP-28 | T-046, T-061 |
| R-FUNC-010 | V-B | Fig. 3 | `[MANDATED]` | GAP-25, GAP-37 | T-062 |
| R-FUNC-011 | II-B, VIII | — | `[PERMITS]` | GAP-16 | T-043 |
| R-FUNC-012 | II-A, V-C, V-D | — | `[MANDATED]` | GAP-24, GAP-28 | T-034 |
| R-FUNC-013 | II-A, V-D | — | `[MANDATED]` | GAP-18, GAP-39 | T-039 |
| R-FUNC-014 | II-A | — | `[PERMITS]` | GAP-39 | T-066 |
| R-FUNC-015 | II-A, V-E | — | `[MANDATED]` | — | T-065, T-045 |
| R-FUNC-016 | II-B, II-D | — | `[MANDATED]` | GAP-34 | T-010, T-067 |
| R-FUNC-017 | II-B | — | `[MANDATED]` | GAP-22 | T-035, T-039 |
| R-FUNC-018 | V-A, V-D, V-E | Fig. 4 | `[MANDATED]` | GAP-33 | T-033 |
| R-FUNC-019 | II-A, V-F | — | `[MANDATED]` | GAP-23 | T-075 |
| R-FUNC-020 | X | — | `[MANDATED]` | — | T-063 |
| R-FUNC-021 | V-F | — | `[MANDATED]` | — | T-076 |
| R-CRYPTO-001 | VIII | — | `[MANDATED]` | GAP-1, GAP-52 | T-054, T-055 |
| R-CRYPTO-002 | IV-B, IV-C, VI, VIII | — | `[MANDATED]` | GAP-1 | T-054 |
| R-CRYPTO-003 | IV-B | Fig. 2 | `[MANDATED]` | GAP-2, GAP-3 | T-006 |
| R-CRYPTO-004 | IV-B | Fig. 2 | `[MANDATED]` | GAP-2, GAP-6 | T-007 |
| R-CRYPTO-005 | IV-C | Fig. 2 | `[MANDATED]` | GAP-2, GAP-3, GAP-22 | T-016 |
| R-CRYPTO-006 | IV-C | Fig. 2 | `[MANDATED]` | GAP-2, GAP-7 | T-020 |
| R-CRYPTO-007 | IV-A | Fig. 2 | `[MANDATED]` | GAP-4, GAP-5 | T-001, T-002 |
| R-CRYPTO-008 | VI | Fig. 5 | `[MANDATED]` | GAP-31 | T-051 |
| R-CRYPTO-009 | IV-A, IV-B, IV-C | Alg. 4 | `[MANDATED]` | — | T-056, T-057 |
| R-CRYPTO-010 | IV-A, IV-B, IV-C | — | `[UNSPECIFIED]` | GAP-2, GAP-3 | T-056 |
| R-CRYPTO-011 | IV-B, IV-C | — | `[MANDATED]` | GAP-1, GAP-2 | T-054 |
| R-CRYPTO-012 | V-B | Fig. 3 | `[MANDATED]` + `[UNSPECIFIED]` | GAP-25 | T-062 |
| R-CRYPTO-013 | V-B | Fig. 3 | `[MANDATED]` | GAP-24 | T-061 |
| R-CRYPTO-014 | VII | — | `[MANDATED]` | GAP-1 | T-055 |
| R-CRYPTO-015 | X | — | `[MANDATED]` | — | T-063 |
| R-IOMF-001 | IV-A | Fig. 2 | `[MANDATED]` | GAP-5 | T-001 |
| R-IOMF-002 | IV-A | — | `[MANDATED]` | GAP-4, GAP-5 | T-003, T-004 |
| R-IOMF-003 | IV-A | Fig. 2 | `[MANDATED]` | GAP-5, GAP-10 | T-002 |
| R-IOMF-004 | IV-A | Fig. 2 | `[MANDATED]` | GAP-5 | T-002, T-003 |
| R-IOMF-005 | III, V-D | — | `[MANDATED]` | GAP-17, GAP-19 | T-005, T-037, T-040 |
| R-IOMF-006 | IV-A | Fig. 2 | `[UNSPECIFIED]` | GAP-5, GAP-10 | T-002, T-058 |
| R-IOMF-007 | IV-B | — | `[MANDATED]` | GAP-6 | T-007 |
| R-IOMF-008 | IV-B, IV-D | Alg. 4 | `[MANDATED]` | GAP-13 | T-009 |
| R-IOMF-009 | III, IV-B, IV-D | — | `[MANDATED]` | GAP-3, GAP-50 | T-008, T-080 |
| R-IOMF-010 | IV-B | Fig. 2 | `[MANDATED]` | GAP-2, GAP-3 | T-006, T-007 |
| R-IOMF-011 | IV-B, IV-D, VIII-B | Fig. 2 | `[MANDATED]` (+`[EXPERIMENT]` for F = 4) | GAP-5, GAP-6 | T-058 |
| R-IOMF-012 | IV-B | Alg. 1 | `[MANDATED]` | GAP-3, GAP-14 | T-013 |
| R-IOMF-013 | IV-D | Alg. 4 | `[MANDATED]` | GAP-9, GAP-13 | T-008, T-011 |
| R-IOMF-014 | IV-D | Alg. 3, Fig. 2 | `[MANDATED]` | GAP-10 | T-012 |
| R-IOMF-015 | IV-D, X | — | `[PERMITS]` | GAP-3 | T-075 |
| R-IOMF-016 | IV-C, IV-D | — | `[MANDATED]` | GAP-6 | T-016, T-021 |
| R-IOMF-017 | IV-C | Fig. 2, Alg. 5 | `[MANDATED]` | GAP-22 | T-016 |
| R-IOMF-018 | IV-C | Fig. 2 | `[MANDATED]` | GAP-2, GAP-7 | T-020 |
| R-IOMF-019 | IV-C | (Alg. 2 defective) | `[MANDATED]` | GAP-6, GAP-8 | T-017, T-018, T-019 |
| R-IOMF-020 | IV-D | Alg. 5, Fig. 2 | `[MANDATED]` | GAP-9, GAP-10, GAP-11 | T-021, T-022, T-023, T-025 |
| R-IOMF-021 | IV-D | Fig. 2 | `[MANDATED]` | — | T-026 |
| R-IOMF-022 | IV-D | — | `[MANDATED]` + `[UNSPECIFIED]` | GAP-34 | T-026 |
| R-IOMF-023 | IV-D | Alg. 3 | `[MANDATED]` | GAP-10, GAP-49 | T-026, T-079 |
| R-IOMF-024 | I, III, IV-D | Fig. 2 | `[MANDATED]` | GAP-15, GAP-50 | T-010, T-021, T-080 |
| R-IOMF-025 | IV-D, IV-E | Alg. 3 | `[MANDATED]` | GAP-38 | T-026, T-027 |
| R-ALIS-001 | V-A, V-C | Fig. 3 | `[MANDATED]` | GAP-51 | T-033 |
| R-ALIS-002 | V-C | — | `[MANDATED]` | GAP-17, GAP-51 | T-039 |
| R-ALIS-003 | V-C | — | `[MANDATED]` | GAP-21 | T-001 |
| R-ALIS-004 | V-C | — | `[MANDATED]` | GAP-3 | T-008 |
| R-ALIS-005 | V-C | — | `[MANDATED]` | GAP-13 | T-014 |
| R-ALIS-006 | V-C | — | `[MANDATED]` | GAP-13, GAP-14 | T-015 |
| R-ALIS-007 | V-C | — | `[MANDATED]` | GAP-17 | T-039, T-040 |
| R-ALIS-008 | V-C, VIII | — | `[MANDATED]` (+`[EXPERIMENT]` for RH contents) | GAP-24, GAP-37 | T-044 |
| R-ALIS-009 | V-A, V-C | Table IV | `[MANDATED]` | GAP-36 | T-066 |
| R-ALIS-010 | V-C | — | `[MANDATED]` | — | T-036 |
| R-ALIS-011 | V-C | — | `[MANDATED]` | GAP-20 | T-038 |
| R-ALIS-012 | V-C, V-D | — | `[MANDATED]` | GAP-19 | T-036, T-037 |
| R-ALIS-013 | V-C | — | `[MANDATED]` | GAP-13 | T-036 |
| R-ALIS-014 | V-C | — | `[MANDATED]` | — | T-036 |
| R-ALIS-015 | V-C | — | `[MANDATED]` | GAP-12 | T-036 |
| R-ALIS-016 | V-B, V-C | Fig. 3 | `[MANDATED]` | GAP-24 | T-061 |
| R-ALIS-017 | V-C, V-D | — | `[MANDATED]` | GAP-40 | T-078 |
| R-ALIS-018 | V-C, VI | — | `[MANDATED]` | GAP-50 | T-034 |
| R-ALIS-019 | V-C | — | `[MANDATED]` | GAP-13 | T-034 |
| R-ALIS-020 | V-C | — | `[MANDATED]` | GAP-13 | T-034, T-050 |
| R-ALIS-021 | V-C | — | `[MANDATED]` | GAP-12 | T-035 |
| R-ALIS-022 | V-C | — | `[MANDATED]` | GAP-24, GAP-28 | T-034, T-061 |
| R-ALIS-023 | V-B | Fig. 3 | `[MANDATED]` | — | T-046 |
| R-ALIS-024 | V-C, VI | — | `[UNSPECIFIED]` | GAP-21, GAP-28 | T-046 |
| R-QUERY-001 | V-D | Fig. 4 | `[MANDATED]` | GAP-34 | T-033 |
| R-QUERY-002 | V-A, V-D | Fig. 4 | `[MANDATED]` | GAP-33 | T-033 |
| R-QUERY-003 | II-B, II-D, IV-D | — | `[MANDATED]` | GAP-15 | T-010 |
| R-QUERY-004 | IV-D, VIII-A | Fig. 2 | `[MANDATED]` | GAP-34 | T-010, T-067 |
| R-QUERY-005 | V-C, V-D | — | `[MANDATED]` | GAP-12, GAP-22 | T-035, T-039 |
| R-QUERY-006 | IV-D, V-D | Alg. 3 | `[MANDATED]` | — | T-026, T-034 |
| R-QUERY-007 | V-D, VIII-A | — | `[UNSPECIFIED]` | GAP-34 | T-067 |
| R-QUERY-008 | V-D | — | `[UNSPECIFIED]` | GAP-15 | T-043 |
| R-QUERY-009 | V-D | — | `[MANDATED]` | — | T-034 |
| R-QUERY-010 | V-D | Alg. 4 | `[MANDATED]` | GAP-13 | T-034 |
| R-QUERY-011 | V-D | — | `[MANDATED]` | GAP-15 | T-034 |
| R-QUERY-012 | V-D | — | `[MANDATED]` | GAP-12 | T-035 |
| R-QUERY-013 | V-D | — | `[MANDATED]` | GAP-24 | T-034 |
| R-QUERY-014 | V-D | — | `[MANDATED]` | — | T-036 |
| R-QUERY-015 | V-D | — | `[MANDATED]` | GAP-19 | T-036, T-037 |
| R-QUERY-016 | V-D | — | `[MANDATED]` | — | T-036 |
| R-QUERY-017 | V-D | — | `[PERMITS]` | GAP-40 | T-078 |
| R-QUERY-018 | V-D | Fig. 4 | `[MANDATED]` | GAP-38 | T-033 |
| R-QUERY-019 | V-D | — | `[MANDATED]` | GAP-15, GAP-16, GAP-33 | T-043 |
| R-QUERY-020 | V-D | Fig. 4 | `[MANDATED]` | GAP-33 | T-033 |
| R-QUERY-021 | V-D | — | `[MANDATED]` | GAP-17 | T-039 |
| R-QUERY-022 | V-D | — | `[MANDATED]` | GAP-18 | T-039 |
| R-QUERY-023 | V-D | — | `[PERMITS]` | GAP-40 | T-078 |
| R-QUERY-024 | V-D | Fig. 4 | `[MANDATED]` | GAP-38 | T-039 |
| R-QUERY-025 | IV-D, V-D | — | `[UNSPECIFIED]` | GAP-38 | T-056 |
| R-VER-001 | IV-E | — | `[MANDATED]` | GAP-38 | T-027 |
| R-VER-002 | IV-E | — | `[MANDATED]` | GAP-5, GAP-9, GAP-10 | T-027, T-061 |
| R-VER-003 | IV-E, VII | — | `[MANDATED]` | GAP-15 | T-028 |
| R-VER-004 | IV-E | — | `[MANDATED]` | — | T-027, T-045 |
| R-VER-005 | IV-E | — | `[MANDATED]` | — | T-028 |
| R-VER-006 | IV-D | Alg. 5 | `[MANDATED]` | GAP-9, GAP-11, GAP-12 | T-024 |
| R-VER-007 | V-E | Fig. 4 | `[MANDATED]` | — | T-033 |
| R-VER-008 | V-E | — | `[MANDATED]` | GAP-24, GAP-28 | T-034, T-061 |
| R-VER-009 | V-D, V-E | — | `[MANDATED]` | GAP-19 | T-037 |
| R-VER-010 | V-D, V-E, VII | — | `[MANDATED]` | GAP-17, GAP-18 | T-040, T-044 |
| R-VER-011 | V-E | Fig. 4 | `[MANDATED]` | — | T-045, T-041, T-042 |
| R-VER-012 | V-E | Fig. 4 | `[MANDATED]` | — | T-045 |
| R-VER-013 | II-A, V-C, VII | — | `[MANDATED]` | — | T-065, T-044 |
| R-VER-014 | V-D | — | `[UNSPECIFIED]` | GAP-33 | T-043 |
| R-OPT-001 | VI | — | `[MANDATED]` | GAP-27 | T-046 |
| R-OPT-002 | VI | — | `[MANDATED]` | GAP-28 | T-049 |
| R-OPT-003 | VI | Fig. 3 | `[MANDATED]` | GAP-28 | T-046 |
| R-OPT-004 | VI | — | `[MANDATED]` | — | T-047 |
| R-OPT-005 | VI | — | `[MANDATED]` | GAP-29 | T-047 |
| R-OPT-006 | VI | — | `[MANDATED]` | GAP-29 | T-047 |
| R-OPT-007 | VI | — | `[MANDATED]` | — | T-047 |
| R-OPT-008 | VI | — | `[MANDATED]` | GAP-29 | T-048 |
| R-OPT-009 | VI, VIII-B | Table IV | `[UNSPECIFIED]` (+`[EXPERIMENT]` for 1024) | GAP-28, GAP-34 | T-049 |
| R-OPT-010 | VI | — | `[MANDATED]` | GAP-32 | T-052 |
| R-OPT-011 | VI | Fig. 5 | `[MANDATED]` | GAP-30, GAP-31 | T-050 |
| R-OPT-012 | VI | Fig. 5 | `[MANDATED]` | GAP-31 | T-051 |
| R-OPT-013 | VI, VIII-C | — | `[MANDATED]` + `[UNSPECIFIED]` | GAP-32 | T-052 |
| R-OPT-014 | VI | — | `[MANDATED]` | GAP-32, GAP-48 | T-053 |
| R-OPT-015 | VIII-C | Fig. 7 | `[EXPERIMENT]` | GAP-48 | T-053, T-071 |
| R-SEC-001 | II-C | — | `[MANDATED]` | — | T-041 |
| R-SEC-002 | II-C, VII | — | `[MANDATED]` | GAP-23 | T-077 |
| R-SEC-003 | II-C | — | `[MANDATED]` | — | T-077 |
| R-SEC-004 | II-C | — | `[MANDATED]` | — | T-077 |
| R-SEC-005 | II-C | — | `[MANDATED]` | — | T-077 |
| R-SEC-006 | II-D | — | `[MANDATED]` | — | T-041 |
| R-SEC-007 | II-D | — | `[MANDATED]` | — | T-042 |
| R-SEC-008 | VII | Definition 1, Theorem 1 | `[MANDATED]` | GAP-4, GAP-37 | T-032 |
| R-SEC-009 | VII | Definition 2 | `[MANDATED]` | — | T-029, T-030, T-031 |
| R-SEC-010 | VII | Theorem 2 case 1 | `[MANDATED]` | — | T-031, T-044 |
| R-SEC-011 | VII | Theorem 2 case 2 | `[MANDATED]` | — | T-030 |
| R-SEC-012 | VII | Theorem 2 case 3 | `[MANDATED]` | — | T-029, T-042 |
| R-SEC-013 | VII | Theorem 1 proof | `[MANDATED]` + `[UNSPECIFIED]` | GAP-2, GAP-3 | T-056, T-032 |
| R-SEC-014 | II-C, II-D, VII, X | — | `[MANDATED]` | — | T-075, T-063 |
| R-SEC-015 | II-C, V-E, VII | Definition 2 | `[UNSPECIFIED]` | GAP-35 | T-077 |
| R-EVAL-001 | VIII | "Dataset" | `[EXPERIMENT]` | GAP-41 | T-067 |
| R-EVAL-002 | VIII | "Dataset" | `[EXPERIMENT]` | GAP-41 | T-067 |
| R-EVAL-003 | VIII | "Dataset" | `[EXPERIMENT]` | GAP-37, GAP-41, GAP-46, GAP-47 | T-067 |
| R-EVAL-004 | VIII | "Dataset" | `[EXPERIMENT]` | GAP-37, GAP-41, GAP-46 | T-067 |
| R-EVAL-005 | VIII | — | `[EXPERIMENT]` | GAP-1 | T-054 |
| R-EVAL-006 | VIII | "Implementation" | `[EXPERIMENT]` | GAP-52 | T-059 |
| R-EVAL-007 | VIII | — | `[EXPERIMENT]` | — | T-068 |
| R-EVAL-008 | VIII-B | Table IV | `[EXPERIMENT]` | GAP-5, GAP-42 | T-058, T-068 |
| R-EVAL-009 | VIII | "Experimental roadmap" | `[EXPERIMENT]` | — | T-067 |
| R-EVAL-010 | VIII-A | — | `[EXPERIMENT]` | GAP-42 | T-067, T-073 |
| R-EVAL-011 | VIII-A | Tables II, III | `[EXPERIMENT]` | GAP-34 | T-067 |
| R-EVAL-012 | VIII-B, VIII-C | Tables II, III, Fig. 7 | `[EXPERIMENT]` | — | T-067, T-071 |
| R-EVAL-013 | VIII-B | Table IV | `[EXPERIMENT]` | GAP-36, GAP-43 | T-068 |
| R-EVAL-014 | VIII-B | Tables V, VI | `[EXPERIMENT]` | GAP-42 | T-069 |
| R-EVAL-015 | VIII-B | Fig. 6 | `[EXPERIMENT]` | GAP-42 | T-070 |
| R-EVAL-016 | VIII-C | Fig. 7 | `[EXPERIMENT]` | GAP-42, GAP-48 | T-071 |
| R-EVAL-017 | VIII-A, VIII-B | Table IV | `[EXPERIMENT]` | GAP-43 | T-068 |
| R-EVAL-018 | VIII | — | `[EXPERIMENT]` | GAP-44 | T-074 |
| R-EVAL-019 | VIII | Tables II–VI, Figs. 6, 7 | `[EXPERIMENT]` | GAP-43, GAP-45 | T-074 |
| R-EVAL-020 | VIII | — | `[EXPERIMENT]` | GAP-43, GAP-44 | T-072 |
| R-EVAL-021 | VIII | — | `[EXPERIMENT]` | GAP-41, GAP-42 | T-073 |
| R-GO-001 | VIII | "Implementation" | `[MANDATED]` | — | T-075 |
| R-GO-002 | VIII | — | `[MANDATED]` | GAP-1, GAP-52 | T-054, T-055 |
| R-GO-003 | — (project rule over VIII) | — | `[MANDATED]` | — | T-059 |
| R-GO-004 | — (project rule over VIII) | — | `[UNSPECIFIED]` | GAP-52, GAP-3, GAP-23, GAP-25, GAP-39 | T-059 |
| R-GO-005 | VIII | — | `[EXPERIMENT]` | — | T-073 |
| R-GO-006 | IV-A, IV-B, IV-C, V-C | Alg. 4 | `[MANDATED]` | — | T-057, T-056 |
| R-GO-007 | V-C | — | `[MANDATED]` | GAP-20 | T-038 |
| R-GO-008 | IV-A, IV-B, IV-C | — | `[UNSPECIFIED]` | GAP-2 | T-056 |
| R-GO-009 | IV-A, IV-B, IV-C | — | `[UNSPECIFIED]` | GAP-2 | T-056 |
| R-GO-010 | IV-B, IV-C, VI, VII | Fig. 5 | `[UNSPECIFIED]` | GAP-2 | T-006, T-056 |
| R-GO-011 | IV-B, IV-C | — | `[UNSPECIFIED]` | GAP-2 | T-006, T-056 |
| R-GO-012 | IV-B, VIII | — | `[MANDATED]` | GAP-46 | T-008 |
| R-GO-013 | II-B, IV-C, V-B | — | `[MANDATED]` | GAP-2, GAP-22 | T-016, T-035 |
| R-GO-014 | IV-A, V-B, IV-E | Fig. 3 | `[MANDATED]` | GAP-2, GAP-4, GAP-24, GAP-38 | T-056 |
| R-GO-015 | III, IV-B, IV-C | Fig. 2 | `[MANDATED]` | GAP-3 | T-008, T-016 |
| R-GO-016 | IV-B, IV-D | — | `[MANDATED]` | GAP-3, GAP-50 | T-080 |
| R-GO-017 | V-C, IV-B | Alg. 4 | `[MANDATED]` | GAP-13, GAP-21 | T-038, T-009 |
| R-GO-018 | IV-B, IV-C, IV-D | — | `[MANDATED]` | GAP-5, GAP-6 | T-058, T-007 |
| R-GO-019 | IV-D | Alg. 4, Alg. 5 | `[MANDATED]` | GAP-9, GAP-10 | T-012, T-021 |
| R-GO-020 | IV-B | Alg. 1 | `[MANDATED]` | GAP-3 | T-013 |
| R-GO-021 | IV-C, IV-D | — | `[MANDATED]` | GAP-6, GAP-8 | T-017, T-018, T-019, T-020 |
| R-GO-022 | V-C | — | `[MANDATED]` | GAP-17 | T-039, T-040 |
| R-GO-023 | IV-D, IV-E, V-D | — | `[MANDATED]` | GAP-38 | T-056, T-027 |
| R-GO-024 | V-D | — | `[MANDATED]` | GAP-40 | T-078 |
| R-GO-025 | V-E | Fig. 4 | `[MANDATED]` | — | T-045 |
| R-GO-026 | II-C, V-E | — | `[MANDATED]` | — | T-041, T-045 |
| R-GO-027 | VIII | Tables II–VI | `[EXPERIMENT]` | — | T-060 |
| R-GO-028 | VIII | — | `[EXPERIMENT]` | GAP-41, GAP-42 | T-073 |

## 16. OPEN QUESTIONS / GAPS

**Verification note for this round.** GAP-1 … GAP-46 were already cited inline throughout §1–§12 but never defined. Their subjects below were derived from every citation site of each GAP ID in this document. In this session the PDF could not be re-opened: the environment's PDF renderer is unavailable and the file's text layer is compressed, so neither page rendering nor text search over the PDF succeeded. The definitions therefore rest on the verbatim transcriptions, quoted formulas, algorithm boxes and figure contents recorded in §1–§12 by the round that did read all 18 pages. Where a gap below asserts that the paper is silent, that assertion is inherited from §1–§12's own statement of silence, not from an independent re-reading. **Before any gap is closed, the cited section of the PDF should be re-read to confirm the silence.** GAP-47 … GAP-52 are new in this round and are flagged as such.

**Blocking labels.** **IMPLEMENTATION-BLOCKING** means the implementation cannot proceed without the decision, because a root hash, a proof, an accept/reject outcome, or a required experiment is undetermined. **NON-BLOCKING** means implementation can proceed and the decision can be recorded later without invalidating committed state.

**All gaps below were resolved by the project owner on 2026-09-12.** The rulings are in **`DECISIONS.md`** and summarised in §16.0. The analysis under each gap is retained unchanged, because it records *why* the decision was needed and *what* it affects — but the "choice is left to the user" framing in each entry is now historical. Where an entry lists candidate interpretations, consult §16.0 for which one was taken.

### 16.0 Resolution register

Every gap's ruling in one line. Full text, rationale, and consequences: `DECISIONS.md`.

| Gap | Ruling | Source |
|---|---|---|
| GAP-1 | Unkeyed BLAKE2b-256 (`blake2b.New256`), 32-byte output | A2 |
| GAP-2 | Fixed-width big-endian ints; raw 32-byte digests; length-prefixed variable-length fields | A3 |
| GAP-3 | 0-based, LSB-first, uncompressed, exactly ⌈N/8⌉ bytes, unused high bits zeroed; N from authenticated population. **Deviation D-1:** rehash a leaf whenever its serialized bytes change | A4 |
| GAP-4 | Hash all object fields in declared order | A5 |
| GAP-5 | OMT leaf = `H(oid‖H(o))`; arity a parameter F, F = 4 in experiments | A6 |
| GAP-6 | One incomplete rightmost node, hash only present children, no padding; RIMBT splits at the midpoint, root exempt from minimum occupancy, no delete/merge | A7 |
| GAP-7 | `V_ci` = values in child `c_i`'s subtree | B1 |
| GAP-8 | **Alg. 2 box is a typesetting error; IV-C prose governs.** Minimum-value insert uses a virtual all-zero predecessor | B2 |
| GAP-9 | All F−1 siblings + explicit child index per level; RIMBT proofs carry the internal V material | B3 |
| GAP-10 | Union of necessary siblings minus anything recomputable from included nodes | B4 |
| GAP-11 | Omit the non-existent neighbour, authenticate the real leftmost/rightmost leaf; reject α > β; boundary `v` values are shipped | B5 |
| GAP-12 | One-sided procedure implemented literally per V-D; rightmost leaf is the authenticated upper-universe bitmap | B6 |
| GAP-13 | kid from 0, monotonic, append-only universe, per generator at entry level; every known keyword gets a leaf even if all-zero; history from the on-chain keyword set, no new ADS | C1 |
| GAP-14 | Keyword set frozen at file **close**; the introducing entry is indexed | C2 |
| GAP-15 | Conservative Boolean expansion at coarse levels; exact NOT only where the predicate is defined. **Deviation D-2** | C3 |
| GAP-16 | `bm_EW` is an ordinary bitmap over the generator's keyword universe; no Bloom filter | C4 |
| GAP-17 | Entry-level B+ tree is a local accelerator only; no additional root | D1 |
| GAP-18 | Per file: matching entries, keyword bitmaps + merged IIMT proof, entry-ID set, one merged OMT proof incl. temporal boundary leaves | D2 |
| GAP-19 | Neighbour evidence carries full authenticated leaf material (enough to inspect lid); at edges prove first/last; empty run proved by bracketing leaves | D3 |
| GAP-20 | Unsigned integers, ascending numeric by (lid, fid); pair unique, fid unique within lid | D4 |
| GAP-21 | esn from 0, BlockHeight from 0; bit = height, or height − segmentBase under partitioning | D6 |
| GAP-22 | Signed 64-bit Unix milliseconds; equal timestamps allowed, ordered by esn | D7 |
| GAP-23 | Deterministic single-process emulator; no Ethereum/Fabric/BFT; chain integrity assumed | E1 |
| GAP-24 | RH = 4 transaction-level + 4 block-level roots, state **after** inclusion; final block carries completed segment roots | E2 |
| GAP-25 | Signature is an opaque byte field; never generated or verified; no algorithm selected | E3 |
| GAP-26 | Keyword-set information is on-chain metadata/registry; no new ADS | E4 |
| GAP-27 | **The complete block-level index is partitioned per segment**; the introduction is shorthand | F1 |
| GAP-28 | Segment size S a parameter, S = 1024 for evaluation, segment = height / S; non-final headers carry running Ts values | F2 |
| GAP-29 | Closed-interval `≤`/`≥` (**deviation D-3**); scan segment-final headers newest→oldest; coverage check is set equality | F3 |
| GAP-30 | **Detailed text governs:** per-keyword MHT over block bitmaps, per-lid MHT over transactions | F4 |
| GAP-31 | Fanout F; keyword-MHT by BlockHeight, generator-MHT by (BlockHeight, tsn), leaf `H(BlockHeight‖tsn‖H(tx))`; leaf becomes `H(w‖H(bm)‖RH_i)` (**deviation D-4**) | F5 |
| GAP-32 | Aggregation **replaces** the per-block proofs; with Opt. 1 off the chain is one segment | F6 |
| GAP-33 | Auditor sends fids + known lg (text over figure); verify stages 1–2 before the bm_EW filter; missing SP response ⇒ reject | G1 |
| GAP-34 | Missing range ⇒ skip the time predicate, keep generator/keyword sub-queries; covers all relevant segments | G2 |
| GAP-35 | **Yes** — full-node responses are untrusted; trust only committed state and roots | G5 |
| GAP-36 | Log generator builds the entry-level ADS; SP stores and serves it; Table IV "SP" = off-chain ADS cost | G6 |
| GAP-37 | **Section II-B is the normative data model**; Sec. VIII is an abbreviation | D5 |
| GAP-38 | Canonical binary per A3, raw bitmaps, sorted identifiers, component order per Algs. 3–5; VO size counts semantic payload bytes only | G4 |
| GAP-39 | In-memory + optional flat-file persistence; no database | G7 |
| GAP-40 | Sequential baseline; no unspecified worker pools | G8 |
| GAP-41 | No record reuse to reach 8192 blocks; deterministic assignment to 4096 generators, seed 1, stable-sort by timestamp, short final file/block allowed | H1 |
| GAP-42 | F = 4 everywhere, segment/index size 1024, query seed 1, Android for Fig. 6/7-style runs | H2 |
| GAP-43 | CPU/algorithmic time excluding network and storage I/O; verification measured separately; KB = 1024; success = functional correctness + trend agreement | H3 |
| GAP-44 | **BLAS only** — no vChain+, MSTDB, or MART | H6 |
| GAP-45 | Record discrepancies, never silently fix; exclude inconsistent cells from strict validation | H7 |
| GAP-46 | LogHub structured template/literal tokens as entry keywords; FW = union of the file's entry keywords | H5 |
| GAP-47 | Parse to Unix ms, deterministic fill for an absent year, stable-sort each generator's slice | H4 |
| GAP-48 | Multi-window = Sec. VI block-level segmentation; No-multi-win disables it | F7 |
| GAP-49 | Empty results still ship full authenticated evidence; the auditor derives the empty result itself | G3 |
| GAP-50 | N derived from the authenticated object population (TxNum / OMT extent); no unauthenticated N field | C5 |
| GAP-51 | Entry level is a **reduced IOMF**: OMT + IIMT + B+ temporal routing | D8 |
| GAP-52 | Approved: `golang.org/x/crypto/blake2b`; do not hand-implement | A1 |

### 16.1 Hashing and encoding

**GAP-1 — BLAKE2b parameterisation. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* which concrete BLAKE2b instantiation produces the 256-bit digest.
- *Why needed:* every leaf hash, node hash, root hash, H(tx) and object hash depends on it. Two instantiations that both "produce 256-bit hashes" produce completely different values.
- *Where the paper is silent:* Sec. VIII states only "Blake2b is used for 256-bit hash computations" (§1.4 D-4). No digest-length parameter, key, salt or personalization is given.
- *Affects:* all root hashes, all proofs, T-002, T-011, T-023, T-031, T-054, T-055, T-056; nothing in the reported timings is sensitive to this beyond a constant factor.
- *Candidate interpretations:* (a) `blake2b.New256` / BLAKE2b with digest-length parameter 32, unkeyed (the standard BLAKE2b-256); (b) BLAKE2b-512 truncated to the first 32 bytes; (c) keyed or personalized BLAKE2b. These give different digests for the same input. (a) is the conventional reading of "BLAKE2b with 256-bit output" but the paper does not say it.

**GAP-2 — Byte encoding and field framing of hash inputs. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* the exact byte representation of every term that enters a hash, and the meaning of `||`: integers (oid, esn, tsn, kid, BlockHeight, lid, fid), timestamps, keyword strings, numeric values v, the value set V, and nested digests; plus whether variable-length fields are length-prefixed, delimited, or simply concatenated.
- *Why needed:* R-CRYPTO-003..008 give formulas, not encodings. Without a fixed encoding no two implementations agree on any root, and R-SEC-013 (the Theorem 1 collision argument requires that node contents be uniquely recoverable from the hash input) cannot be honoured: unframed concatenation of variable-length keywords admits `H("ab"||x) = H("a"||"bx")`.
- *Where the paper is silent:* IV-B and IV-C give `h = H(w||H(bm))`, `h = H(h_c1||...||h_cF)`, `h = H(v||H(bm))`, `h = H(V||h_c1||...||h_cF)` with no encoding; §3 of this document records that the paper defines no field widths or serialization formats.
- *Affects:* every hash and proof; ADS size in Table IV (KB/block, KB/file) and VO size in Tables III, V, VI depend directly on the encoding chosen.
- *Candidate interpretations:* (a) fixed-width big-endian integers, raw 32-byte nested digests, length-prefixed variable-length fields; (b) the same without length prefixes (smaller, but breaks unique recoverability); (c) a textual/canonical encoding. Any choice must satisfy R-SEC-013.

**GAP-3 — Bitmap serialization, bit numbering, and compression. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* (i) whether bit index i is 0-based or 1-based and which serialized bit position it occupies; (ii) the byte/word layout and whether the serialization is padded to a boundary; (iii) how a length-N bitmap is serialized when N grows (block-level and transaction-level bitmaps grow as objects are added); (iv) whether compressed bitmaps are used.
- *Why needed:* `H(bm)` is inside every IIMT and RIMBT leaf hash. Bit order and padding change every leaf hash. Additionally, R-IOMF-012's note records a live problem: Alg. 1 updates the hashes only of *matching* leaves, yet a non-matching leaf's length-N bitmap has just gained a 0 bit at position oid — under a length-dependent serialization its hash would change too, so either the serialization is length-independent for trailing zeros, or Alg. 1 as printed is incomplete.
- *Where the paper is silent:* IV-B ("the i-th bit indicates whether object i contains that keyword"), IV-D ("N ... the length of each bitmap"). Fig. 2 prints leftmost bit = object 1, which §3.8 records as a depiction, not a serialization. IV-D and Sec. X *permit* compression (R-IOMF-015) without requiring it; the complexity analysis assumes uncompressed.
- *Affects:* all IIMT/RIMBT leaf and root hashes; the meaning of ¬ (jointly with GAP-50); VO size measurements throughout; the O(N) terms in the complexity claims.
- *Candidate interpretations:* (a) 1-based, MSB-first within bytes, serialized over exactly ⌈N/8⌉ bytes with N committed separately; (b) 0-based LSB-first; (c) compressed (RLE/roaring) — permitted by IV-D/X but changing both hashes and the stated complexity model.

**GAP-4 — Object serialization before hashing. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* which fields, in which order and encoding, form the pre-image of an object hash: for a log entry e_i = \<esn, ts, EW, V\> at the entry level, and for a transaction at the transaction level (specifically whether TSN and Sig are inside H(tx)).
- *Why needed:* the OMT leaf binds oid to "the corresponding object hash value" (IV-A) and H(tx) is a leaf component of the Opt. 2 aggregation MHT (VI, Fig. 5). Omitting a field from the pre-image means tampering with that field is undetectable, which would break R-SEC-008/T-032.
- *Where the paper is silent:* IV-A says only "object hash values"; Fig. 3 lists the transaction fields but not the hash input; Sec. VIII's experimental entry representation \<timestamp, {keywords}, content\> omits esn entirely (see GAP-37).
- *Affects:* OMT roots at both levels, RH, H(tx), aggregation MHT leaves, and all of soundness testing (T-003, T-031, T-032).
- *Candidate interpretations:* (a) all fields of the object in declaration order; (b) all fields except those already bound elsewhere (esn is already in the leaf next to the hash; TSN/Sig outside H(tx)); (c) the experiment's reduced representation. These differ in what tampering is detected.

### 16.2 Tree construction

**GAP-5 — OMT hash formulas, arity, and odd-node behaviour. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* the OMT leaf-node hash formula (is it H(oid||h_obj), H(h_obj), or is the leaf the pair (oid, h_obj) with the parent hashing both?), the internal-node formula, the tree arity, and what happens when the number of leaves is not a power of the arity.
- *Why needed:* the OMT anchors entry integrity and transaction integrity; it is the structure behind Theorem 2 case 1. No OMT root can be computed without this.
- *Where the paper is ambiguous:* IV-A calls the OMT a "modified MHT" and Sec. III defines an MHT as a *binary* tree, while VIII-B sets "the fanout of all trees to 4". §3.7 records this conflict. IV-B gives explicit formulas for the IIMT and IV-C for the RIMBT, but IV-A gives none for the OMT; only Fig. 2's labels "1||h1 ... 8||h8", h9..h14, h15 are available. The Fig. 2 path for oid = 5 is {6||h6, h12, h13}, which shows the *sibling leaf is shipped as `oid||hash`*, hinting that oid is part of the hashed leaf, but does not state the formula.
- *Affects:* every OMT root, RH, the entry-level and transaction-level proofs, Table IV ADS sizes, Table III VO sizes.
- *Candidate interpretations:* (a) binary OMT per Sec. III, leaf = H(oid||H(o)); (b) fanout-F OMT consistent with VIII-B's "all trees", leaf = H(oid||H(o)); (c) leaf hash = H(o) with oid carried unhashed beside it (weakest, and would fail T-004). Conflicts between Sec. III and VIII-B must be resolved by the user, not reconciled here.

**GAP-6 — Fixed-fanout shape: padding, incomplete nodes, RIMBT split and occupancy. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* (i) how a "fully balanced MHT with fixed fanout F" (IV-B) is built when the leaf count is not a power of F — pad with dummy leaves, allow one short node, or promote the odd child; (ii) for the RIMBT, the node split rule, minimum and maximum occupancy, and rebalancing when a new value leaf is inserted.
- *Why needed:* R-CRYPTO-004 hashes exactly F children; a node with fewer children has no defined input. The RIMBT is "a balanced index based on a B-tree structure" (IV-D), and every B-tree needs occupancy bounds; without them, two implementations given the same insertion sequence produce different trees and different roots.
- *Where the paper is silent:* IV-B states fully balanced and fixed fanout without a padding rule; IV-C describes leaf insertion but nothing about splitting; no algorithm box covers it (the Alg. 2 box is defective, GAP-8).
- *Affects:* all IIMT/RIMBT roots, proof lengths (hence VO size), tree heights (hence the O(log_F M) claims), Table IV ADS sizes.
- *Candidate interpretations:* (a) pad to F^h leaves with a defined dummy (which dummy? its hash is itself a decision); (b) allow a single incomplete rightmost node and hash only the present children; (c) allow a single incomplete node and hash the missing slots as a fixed placeholder. For the RIMBT, standard B-tree split at ⌈F/2⌉ is *conventional*, not stated.

**GAP-7 — Meaning of V_ci in the RIMBT internal node. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* what V_ci denotes for a child c_i, and therefore what V is for a node whose children are leaves.
- *Why needed:* R-CRYPTO-006 hashes V: `h = H(V||h_c1||...||h_cF)` with `V = {min(V_c2), ..., min(V_cF)}`. If V_ci is undefined at the leaf level the formula is not evaluable for the lowest internal level, and Alg. 5 ships `\<n.V, n.sibling.hash\>` in proofs.
- *Where the paper is ambiguous:* IV-C gives the formula and Fig. 2 gives an instance (parent of leaves 12, 15 has V = {15}; parent of 97, 99 has V = {99}; root has V = {97}), which is consistent with "V_c for a leaf child is {c.v}" and with a B-tree separator-key reading, but the paper does not define V_ci itself.
- *Affects:* every RIMBT internal hash and root, every range proof, VO size for range queries (Tables III, V, VI).
- *Candidate interpretations:* (a) V_ci = the set of values in the subtree of c_i, so min(V_ci) is that subtree's smallest value (matches Fig. 2); (b) V_ci = c_i's own V for internal children and {c_i.v} for leaf children (also matches Fig. 2); (c) V_ci = the separator keys of c_i. These coincide on Fig. 2's F = 2 instance and may diverge for F > 2, which is exactly the experiment's setting (F = 4).

**GAP-8 — Algorithm 2 is defective; RIMBT insertion has no complete specification. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* whether the published Alg. 2 box is a printing error (a duplicate of Alg. 1) and the IV-C prose is normative, or whether the box is normative and RIMBT bitmaps are set like IIMT bitmaps; and, under the prose, what happens when the inserted value is below every existing leaf value so that `leaf_nodes[x−1]` does not exist.
- *Why needed:* RIMBT insertion determines every range bitmap. Under the box's rule (`if ln.w ∈ o.keywords then ln.bm[oid] ← 1`) bitmaps would be equality-indexed, not cumulative, which contradicts IV-C's "less than or equal to" semantics and Fig. 2's nested bitmaps.
- *Where the paper is inconsistent:* §1.4 D-2 records that the Alg. 2 box titled "RIMBT Construction" is a verbatim copy of the Alg. 1 box, input "IIMT T". The only insertion description is the IV-C prose, transcribed in R-IOMF-019.
- *Affects:* every RIMBT bitmap, leaf hash and root; all range query results; Tables V, VI.
- *Candidate interpretations:* (a) the box is a typesetting error; IV-C prose governs (consistent with Fig. 2 and Alg. 5); (b) the box governs (contradicts IV-C and Fig. 2). For the minimum-value case: (a1) initialise the new first leaf's bitmap to all zeros; (a2) treat the predecessor as a virtual all-zero bitmap; these agree, but the paper states neither.

### 16.3 Proofs and query evaluation

**GAP-9 — Proof content for fanout F > 2, and the root's V. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* what `n.sibling.hash` means when a node has F−1 siblings (which siblings are shipped, in what order, and how the verifier learns the node's position among them); and whether the root's V is part of a RIMBT proof.
- *Why needed:* with F = 4 (the experiment's setting) a verifier cannot recompute a parent hash without all F−1 sibling hashes *and* the node's index. Alg. 4 and Alg. 5 are written as if each node has exactly one sibling. In Alg. 5, the loop adds `\<n.V, n.sibling.hash\>` only while n is not the root, so the root's own V is never included, yet R-CRYPTO-006 makes the root hash depend on it.
- *Where the paper is ambiguous:* Alg. 4 line "π ← π ∪ {n.sibling.hash}"; Alg. 5's two sibling cases; Fig. 2's example π_SR = {\<15\>, \<99\>, \<15, h2\>, \<99, h4\>} is an F = 2 instance.
- *Affects:* all proof sizes (Tables III, V, VI, Fig. 6 VO curves), verification correctness, T-008, T-021, T-024.
- *Candidate interpretations:* (a) ship all F−1 siblings plus an explicit child index per level; (b) ship all F children's hashes per level (redundant but index-free); (c) ship siblings in fixed left-to-right order and derive the index from the leaf position. These differ in VO size, which is a headline metric.

**GAP-10 — Proof merging rule. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* how multiple leaf-to-root paths are merged into a single π_K (multi-keyword IIMT), π (multi-boundary RIMBT, "merge π_ln into π"), and π_O (multiple OMT leaves), and how a verifier reconstructs the root from a merged proof.
- *Why needed:* VO size — a headline metric — depends entirely on the merge rule, and verification must be able to rebuild every path from the merged form.
- *Where the paper is ambiguous:* Alg. 3 line 5 "merge π_w into π_K"; Alg. 5 "merge π_ln into π"; IV-D gives one worked outcome: for keywords {Error, PUT, GET} in the Fig. 2 IIMT, π_K = {h2}. That outcome shows deduplication and the exclusion of hashes recomputable from the queried leaves, but states no rule.
- *Affects:* Tables III, V, VI, Fig. 6 VO sizes; T-012, T-021, T-026.
- *Candidate interpretations:* (a) union of sibling hashes minus every hash that is recomputable from included nodes (matches the π_K = {h2} example); (b) plain set union with deduplication only (would include recomputable hashes and inflate VO); (c) a canonical multiproof with an explicit index list. The paper's example is consistent with (a) but does not name it.

**GAP-11 — RIMBT range-boundary edge cases and whether leaf values are shipped. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* what Alg. 5 does when ln_x does not exist (α ≤ the smallest leaf value), when ln_{y+1} does not exist (β greater than the largest leaf value), and when α > β; and whether the values v of the bitmap-carrying boundary leaves are transmitted in the VO.
- *Why needed:* those cases occur routinely at ALIS level (a range that starts before the first timestamp, or ends after the last). Without the values v, the verifier cannot check that the shipped boundary leaves actually straddle α and β (R-VER-006), and completeness (Theorem 2 case 3) fails.
- *Where the paper is silent:* Alg. 5 defines ln_x, ln_{x+1}, ln_y, ln_{y+1} by the straddling conditions and assumes they exist; it adds `\<n.sibling.v, n.sibling.hash\>` for the *sibling* at leaf level but says nothing about the node's own v.
- *Affects:* range query correctness and completeness; T-024, T-025; Tables V, VI.
- *Candidate interpretations:* (a) a virtual all-zero leaf at −∞ and a virtual all-ones leaf at +∞; (b) omit the missing boundary and have the verifier check the absence via the leftmost/rightmost leaf proof; (c) reject such queries. Each changes both results and VO contents.

**GAP-12 — One-sided range queries at transaction and block level. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* how the V-D one-sided formulations relate to Alg. 5: which leaves are located, what proof is shipped for the rightmost leaf lnr, and what happens when the required neighbour does not exist.
- *Why needed:* R-ALIS-015 and R-QUERY-012 evaluate `FileTsMax ≥ ts` as `¬bm_ln1 ∧ bm_lnr` and `FileTsMin ≤ te` as `bm_ln3`. `bm_lnr` (the rightmost leaf) is by construction the all-ones universe bitmap, so the AND is a no-op unless lnr's role is to authenticate the bitmap universe/length (see GAP-50). The paper does not say which it is.
- *Where the paper is ambiguous:* V-C's interval-intersection rule and V-D's block-level time-range sub-query description; Alg. 5 covers only the two-sided case.
- *Affects:* stage-1 and stage-2 candidate sets, hence soundness and completeness of every time-range query; VO size for range variants in Tables II, III, V, VI; T-035, T-036.
- *Candidate interpretations:* (a) one-sided queries are Alg. 5 with α = −∞ or β = +∞, reusing the GAP-11 virtual leaves; (b) a distinct two-leaf procedure as V-D literally describes, with lnr present purely to bind N; (c) lnr present because the bitmap of the largest leaf is the only authenticated all-ones reference.

**GAP-13 — IIMT leaf universe, kid origin, and absent keywords. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* (i) the start value of kid and the rule assigning kids as the keyword set grows; (ii) which keywords get leaves at each ALIS level — the block-local set, or the global keyword-set-file universe; (iii) what a query for a keyword with no leaf returns, and how the verifier authenticates "no such keyword"; (iv) the same questions for the block-level LG IIMT's lid leaves; (v) how a verifier authenticates the V-C rule that a keyword absent from an older file "can be naturally treated as an all-zero bitmap".
- *Why needed:* the leaf set fixes the tree shape and hence every root. Queries for keywords the index does not carry are normal (the experiment draws keywords randomly), and an unauthenticated "not present" answer is a completeness hole: a malicious responder could claim a keyword has no leaf.
- *Where the paper is silent:* IV-B ("the IOMF maintainer determines the keyword set and assigns each keyword a kid in a monotonically increasing order"); Alg. 4 assumes the kid lookup succeeds; V-C states the all-zero treatment without saying how it is proven; Fig. 2 uses kid 1..4.
- *Affects:* all IIMT roots and heights; query results for absent keywords; Fig. 6 keyword-count experiment; T-009, T-014, T-015.
- *Candidate interpretations:* (a) every index carries a leaf for every keyword in the global keyword set file, so absence never occurs and R-ALIS-005's all-zero rule generalises to all levels; (b) each index carries only locally occurring keywords and non-existence is proven by leaf ordering (which requires sorted leaves — but IIMT leaves are kid-ordered, not lexicographic, so non-existence proofs are not available); (c) the keyword set file is itself authenticated and used to bound the leaf set. (b) appears unworkable, which is itself an argument the user should adjudicate.

**GAP-14 — When a new keyword's leaf is created. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* whether leaf creation for a newly appearing keyword happens before the current file/block is indexed or only from the next one, and which component performs it (Alg. 1 does not).
- *Why needed:* it decides whether the current file's IIMT contains the new keyword's leaf, which changes the tree shape and the committed root for that file.
- *Where the paper is ambiguous:* II-B (keyword sets are "predefined ... at a given time" and extensible); V-C says a new keyword is "indexed in subsequently generated log files"; Alg. 1's preconditions assume the leaf already exists (R-IOMF-012).
- *Affects:* entry-level IIMT roots, RH, and the interpretation of "subsequently"; T-015.
- *Candidate interpretations:* (a) the keyword set is frozen when a file is opened, so the new keyword first appears in the next file — the entry that introduced it is then unindexed for that keyword, which is a silent completeness loss the user must accept or reject; (b) the file is indexed against the keyword set as of file close, so the introducing entry is indexed; (c) the leaf is added immediately and the file's IIMT is rebuilt.

**GAP-15 — Semantics of ¬ over coarse-grained bitmaps. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* what ¬w means when the bitmap is a *coarse* filter — at block level a bit means "this block contains at least one transaction with keyword w", at transaction level "this file has keyword w in FW", and for bm_EW "some entry in this file uses keyword w". ¬ over such a bitmap selects units that contain *no* occurrence, which is not the negation of the entry-level predicate.
- *Why needed:* query types 3 and 6 of the evaluation (NOT-AND) and the complex type 7 all use ¬. If ¬ is applied at the coarse level as a filter, matching entries are lost (completeness violation); if it is only applied at entry level, the coarse stages must not prune on ¬ at all, which changes candidate sets and therefore the reported query times and VO sizes.
- *Where the paper is silent:* IV-D defines ¬ on IOMF bitmaps where bit i = object i, an exact predicate. V-D reuses the same machinery at levels where the bitmap is an existential summary. The paper never discusses the difference.
- *Affects:* correctness of NOT queries end to end; Table II/III NOT-AND rows; T-010, T-043.
- *Candidate interpretations:* (a) ¬ is applied only at the level where the predicate is exact (entry level for EK, file metadata for FW), and coarse levels treat ¬-terms as unconstrained; (b) ¬ is applied at every level as printed, accepting the resulting incompleteness; (c) the coarse filter uses a conservative expansion of the Boolean expression. Only (a) and (c) preserve completeness, but the paper states none of the three.

**GAP-16 — bm_EW: representation and semantics. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* whether bm_EW is a bitmap or a Bloom filter; over which keyword universe it is indexed; its length; and its serialization.
- *Why needed:* bm_EW is a field of m_i, therefore inside the transaction and inside H(tx) and the transaction-level OMT leaf. Its encoding changes those hashes. Its semantics change the auditor's file filter (R-QUERY-019): a Bloom filter admits false positives, so a "matching" file may contain no matching entry — permissible for a filter, but it interacts with GAP-15's ¬ handling, where false positives become false *negatives* under negation.
- *Where the paper permits both:* II-B "keyword usage record (e.g., a bitmap or Bloom filter)"; Table I "Entry keywords in a log file"; VIII's experiment representation says "bitmap".
- *Affects:* H(tx), transaction-level OMT roots, the auditor's file selection, metadata size (Table IV KB/block), T-043.
- *Candidate interpretations:* (a) bitmap over the generator's keyword set, one bit per keyword (matches the experiment); (b) Bloom filter with unstated parameters (m, k, hash functions) — these parameters would themselves be a further gap; (c) bitmap over the global keyword set file.

### 16.4 ALIS levels

**GAP-17 — Entry-level B+ Tree temporal information. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* what the B+ Tree's nodes contain, how (and whether) they are hashed and committed, the fanout, how it is "combined with" the OMT, and what constitutes the "boundary values of ts and te" evidence.
- *Why needed:* the entry-level time-range query and its completeness proof (R-QUERY-021, R-VER-010) run through this structure. If its nodes are not hashed into a committed root, time-range completeness at entry level is unverifiable.
- *Where the paper is ambiguous:* V-C ("combined with a B+ Tree whose nodes store temporal retrieval information"; superimposing it does not change OMT leaf order because esn order equals timestamp order); V-D asks for "the OMT leaf nodes that contain the matching and boundary values of ts and te" with their Merkle proofs, which suggests the boundary evidence is OMT leaves, not B+ Tree nodes; then the B+ Tree's own role in the VO is unclear. R-EVAL-008's RH lists only "IIMT root hash, OMT root hash" — no B+ Tree root.
- *Affects:* entry-level roots and RH composition, entry-level VO size (SP side of Table IV and of Tables III/V/VI), T-039, T-040.
- *Candidate interpretations:* (a) the B+ Tree is an unauthenticated local accelerator; correctness rests entirely on OMT boundary leaves plus the esn↔timestamp monotonicity assumption; (b) the B+ Tree is authenticated and contributes a third root to RH (contradicting the experiment's two-root RH); (c) the B+ Tree nodes are embedded in OMT internal nodes.

**GAP-18 — Entry-level VO contents and returned entries. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* exactly which components the SP returns per file: which entries (full payloads or digests), which bitmaps, which proofs, and how the two sub-query VOs are "integrated".
- *Why needed:* the auditor's stage-3 verification and the reported VO size both depend on it; it also determines whether payload V is transmitted for non-matching entries (it must not be, for soundness-neutral reasons of size, but the paper does not say).
- *Where the paper is ambiguous:* V-D ("integrates the sub-query VOs into the entry-level VO"); IV-E's generic VO form is specified for IOMF, and the entry level lacks a RIMBT (GAP-51), so the mapping is not mechanical.
- *Affects:* Tables III, V, VI VO sizes and the SP KB/file figure in Table IV; T-039.

**GAP-19 — Transaction-level neighbour leaves. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* what the left/right neighbour leaf of lg's transaction run contains (oid only, oid plus hash, or the whole transaction), what the verifier checks with it, and what is provided when lg's run begins at tsn = 0 or ends at the last tsn, or when lg has no transaction in the block at all.
- *Why needed:* this is the entire completeness mechanism at transaction level (R-QUERY-015, R-VER-009). Without a defined edge-case rule a malicious full node can claim lg's run ends at a block boundary.
- *Where the paper is ambiguous:* V-D specifies the left and right neighbour leaves "to guarantee the completeness" but neither their content nor the boundary cases; the check itself ("the neighbours are adjacent to the run and do not belong to lg") is inferred in R-VER-009, not stated.
- *Affects:* completeness of every query; VO size per hit block (a major term in Table III); T-036, T-037, T-042.
- *Candidate interpretations:* (a) the neighbour leaf is the full `tsn||H(tx)` leaf plus its path, and the verifier checks its lid ≠ lg — which requires the transaction body, not just the hash; (b) only the leaf hash is shipped, in which case lid ≠ lg cannot be checked and the mechanism is weaker; (c) at block boundaries, a proof that the run touches tsn = 0 / TxNum−1 replaces the neighbour.

**GAP-20 — lid and fid types, uniqueness, and sort semantics. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* the types and uniqueness scope of lid and fid (is fid unique globally or only per lid?), and whether the "sorted by lid then fid" order is ascending or descending, numeric or lexicographic.
- *Why needed:* the sort fixes tsn, hence the transaction-level OMT leaf order, hence its root; and it underpins the contiguity of lg's transactions that GAP-19's completeness mechanism relies on. A tie in (lid, fid) would make tsn assignment non-deterministic.
- *Where the paper is silent:* V-C ("the block generator sorts the transactions in the block by lid and then fid, and assigns tsn starting from 0"); Table I and II-B define lid and fid only as identifiers. The experiment's metadata representation has no fid at all (GAP-37).
- *Affects:* tsn assignment, transaction-level OMT roots, block-level LG IIMT correctness, T-038.
- *Candidate interpretations:* (a) both are unsigned integers, ascending numeric, (lid, fid) unique; (b) strings, ascending byte-lexicographic; (c) fid unique per lid only, requiring the pair as the sort key — under (c) a global-fid assumption would break.

**GAP-21 — Origin values and BlockHeight-to-bit mapping. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* the start value of esn within a file, the start value of BlockHeight, and how a BlockHeight maps to a bit position in block-level bitmaps (absolute height, or offset within the segment under Opt. 1).
- *Why needed:* bit i must mean the same object to prover and verifier. Under Opt. 1, block-level bitmaps are per segment, so an absolute-height mapping wastes a prefix of every bitmap and changes N, while a segment-relative mapping requires the segment's base height to be committed.
- *Where the paper is silent:* V-C says tsn starts from 0 but says nothing for esn; V-B defines BlockHeight as "the block sequence number" without an origin; VI introduces segments without a bit-mapping rule. Fig. 2 uses oid 1..8, hinting at 1-based objects while tsn is explicitly 0-based — an inconsistency the user should note.
- *Affects:* every bitmap's interpretation, bitmap length N (hence GAP-3 and GAP-50), block-level roots, T-001, T-046.
- *Candidate interpretations:* (a) esn from 0, heights absolute, bit = height; (b) esn from 0, bit = height − segment_base with the base committed in the header; (c) esn from 1 to match Fig. 2's oid numbering.

**GAP-22 — Timestamp representation, resolution, and ties. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* the timestamp type and resolution, its encoding as a RIMBT leaf value v, and the handling of entries with equal timestamps within a file.
- *Why needed:* timestamps are RIMBT leaf values, so their encoding is inside every RIMBT leaf hash; the resolution determines the number of distinct values, hence the RIMBT leaf count, hence tree height, proof size and construction cost. Equal timestamps make the esn↔timestamp monotonicity non-strict, which the entry-level boundary evidence (GAP-17) depends on.
- *Where the paper is silent:* II-B (no representation; "strict global clock synchronization is not required"); V-C ("locally consistent with the entry generation order"); Fig. 2's RIMBT uses bare integers 12, 15, 97, 99.
- *Affects:* all RIMBT hashes and heights, range query boundary behaviour, Tables V/VI range widths, Table IV construction cost, T-016, T-035.
- *Candidate interpretations:* (a) integer Unix milliseconds; (b) integer seconds (fewer distinct values, much smaller RIMBTs, materially different Table IV/V/VI numbers); (c) the dataset's native resolution (see GAP-47).

**GAP-23 — Blockchain layer: platform, consensus, block generation, block generator identity. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* how the reproduction represents the chain at all — whether blocks are produced by a simulated local producer, what triggers block formation (the experiment packs exactly 256 files per block), whether any consensus is executed, and whether "block generator" is a full node.
- *Why needed:* something must produce blocks, assign BlockHeight, fill headers and build transaction-level indexes for any experiment to run. The strict no-extra-feature rule forbids adopting Ethereum, Fabric, or a custom consensus.
- *Where the paper is silent:* II-A defines exactly four parties and names no platform; II-C gives BFT with ≤ ⅓ malicious nodes only as an example; V-C introduces "block generator" without defining it as a party (§1.3 records this); V-F's deployment sketch is explicitly illustrative.
- *Affects:* the whole harness; block-generation timing affects nothing in the hashes but everything in whether an experiment can be run; T-064.
- *Candidate interpretations:* (a) a single-process deterministic block producer with no consensus, treating the blockchain assumption as an axiom (smallest, matches the paper's silence and the security proofs' use of the assumption as a premise); (b) a multi-node simulated BFT — extra machinery the paper does not require; (c) an external chain — barred by the no-extra-feature rule.

**GAP-24 — Block-header serialization, block hash, and RH composition. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* the header field order and encoding; the exact input to the block hash referenced by PrevBlockHash; which root hashes are in RH, in what order; and which header carries the block-level index roots, given that the block-level index spans many blocks.
- *Why needed:* the auditor verifies everything against header roots (R-VER-008, R-VER-013). If RH's composition is undefined, the verifier cannot know which entry to compare a reconstructed root against. The "which header" question is sharper without Opt. 1: a global block-level index changes with every block, so the root in header h commits only to the state at height h — the paper does not say which height's root is authoritative for a query, nor how a full node proves it used that state.
- *Where the paper is ambiguous:* V-B lists header fields and says PrevBlockHash "records the hash value of the previous block" without defining the block hash; V-C says the transaction-level "root hash set is also stored in the block header" and the block-level index's "root hash set is stored in the block header" — the same phrase for a per-block structure and a global one.
- *Affects:* every verification anchor; the chain hash; Table IV KB/block; T-034, T-046, T-061.
- *Candidate interpretations:* (a) RH is an ordered tuple (tx-OMT, tx-FK-IIMT, tx-ts_min-RIMBT, tx-ts_max-RIMBT, blk-LG-IIMT, blk-FK-IIMT, blk-FileTsMin-RIMBT, blk-FileTsMax-RIMBT) and the block-level roots reflect the index state *after* including this block; (b) block-level roots appear only in segment-final headers (natural under Opt. 1, undefined without it); (c) block-level roots reflect the state *before* this block.

**GAP-25 — Transaction signatures. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* the signature algorithm, who signs (log generator or full node), key generation and distribution, which bytes are signed, who verifies and when, and what a full node's "validate" (II-A) actually checks.
- *Why needed:* Sig is a transaction field (Fig. 3) and may or may not be inside H(tx) (GAP-4). Without a decision the transaction cannot be constructed, and R-FUNC-007's validation step has no content.
- *Where the paper is silent:* V-B ("transactions recording the log file metadata and corresponding signatures"); II-A ("validate and store all transactions"); no scheme, key management or PKI is described anywhere; the security proofs (VII) do not use signatures.
- *Affects:* transaction bytes and size (Table IV KB/block), H(tx) if included, T-062. Note that the security theorems do not depend on the signature, so a decision here does not weaken the proven properties.
- *Candidate interpretations:* (a) Ed25519 by the log generator over the metadata fields, verified by full nodes on receipt; (b) ECDSA; (c) the signature is out of scope for the reproduction and the field is carried as an opaque fixed-size blob so that sizes match — this keeps the no-extra-feature rule but means R-FUNC-007 is untestable.

**GAP-26 — On-chain "entry keyword sets" and the keyword set file. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* what the on-chain entry keyword sets contain (per generator? per file? the keyword strings themselves or their kids?), how they are committed and updated, and how they relate to the "keyword set file".
- *Why needed:* if the keyword universe and kid assignment are on-chain and authenticated, GAP-13's leaf-universe question has an answer and absent-keyword queries become verifiable; if not, the auditor has no authenticated view of the keyword space.
- *Where the paper is ambiguous:* V-A states the on-chain storage "contains log metadata, the keyword set file, and the entry keyword sets", while the same paragraph puts entry keyword sets off-chain at the SP as well (§1.4 D-1 records the duplication). No structure, size or commitment mechanism is given for either.
- *Affects:* on-chain storage size, the leaf universe of every IIMT, and whether "keyword not indexed" is provable; T-066.
- *Candidate interpretations:* (a) a duplicated copy for query convenience with no authentication role; (b) an authenticated keyword→kid registry committed in block headers — this would add a structure the paper does not define; (c) a transcription artefact in V-A.

### 16.5 Optimizations

**GAP-27 — What exactly is partitioned under Optimization 1. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* whether Opt. 1 creates an independent *complete* block-level index (LG IIMT, FK IIMT, both RIMBTs) per segment, or only a separate RIMBT per partition with the IIMTs remaining global.
- *Why needed:* it determines the number of roots per segment, what goes in headers (GAP-24), the bitmap length N of block-level bitmaps (GAP-21, GAP-50), and the VO composition for stage 1.
- *Where the paper is internally inconsistent:* §1.4 D-9 records that the Sec. VI introduction says "building a separate RIMBT for each partition" while the detailed Sec. VI text says "we group a certain number of blocks into a segment and set an independent block-level index for each segment". These are different designs and the paper does not reconcile them.
- *Affects:* stage-1 query and VO for every range query; the No-multi-win ablation; Table IV KB/block; T-046.
- *Candidate interpretations:* (a) the full block-level index is per segment (detailed text); (b) only the RIMBTs are per segment, IIMTs global (introduction); (c) the introduction is loose phrasing for (a). **Do not reconcile — the user must choose.**

**GAP-28 — Segment definition: size, boundaries, field values, root storage. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* the segment size as a protocol parameter; whether segment boundaries are fixed by height (height div S) or by some other rule; the values of BlockTsMax and BlockTsMin in a block that is not the last of its segment (are they "so far in the segment" or "over the whole segment", which is not computable at the time that header is written?); and in which header(s) the per-segment index roots are stored.
- *Why needed:* R-OPT-004 has the auditor read *the header of the last block of each segment*, which implies segment-final headers carry whole-segment values; but non-final headers must carry something, and the verifier may encounter them. A "whole segment" value in a non-final header is unwritable at block-production time.
- *Where the paper is ambiguous:* VI defines the three fields ("of the blocks in the current segment") without a temporal qualifier; §3.6 records the ambiguity. "A certain number of blocks" is the only statement of segment size; VIII-B's 1024 is an experiment value (R-OPT-009).
- *Affects:* header contents and hence block hashes; the auditor's backward scan; stage-1 correctness; T-046, T-047, T-049.
- *Candidate interpretations:* (a) non-final headers carry running values ("so far"), final headers carry the complete segment values; (b) all headers in a segment carry the complete values, requiring deferred header writing — incompatible with a committed chain; (c) only segment-final blocks carry the fields at all.

**GAP-29 — Opt. 1 scan predicates, direction, and the coverage check. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* the equality cases of the intersection test (the paper writes strict `BlockTsMin < te` and `BlockTsMax > ts`, whereas R-QUERY-005's closed-interval semantics imply `≤`/`≥`); what "the lookup should continue forward" means inside a back-to-front scan; and the concrete form of the auditor's segment-coverage consistency check.
- *Why needed:* a strict inequality at the boundary drops a segment whose only matching block touches the range endpoint — a completeness violation. "Forward" is directionally ambiguous. The coverage check is the only thing preventing a full node from silently skipping a segment.
- *Where the paper is ambiguous:* VI gives the three conditions and "the log auditor checks the consistency of the block segments actually queried by the full node with the block-level VO", without saying what is compared.
- *Affects:* completeness of range queries under Opt. 1; T-047, T-048, T-042.
- *Candidate interpretations:* for equality, (a) treat as closed (`≤`/`≥`) to match R-QUERY-005, (b) follow the printed strict form. For the coverage check, (a) set equality between the segments the scan identifies and the segment identifiers present in the VO, (b) a count comparison, (c) a per-segment root presence check.

**GAP-30 — What the aggregated MHT is built over. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* whether the per-keyword aggregation MHT's leaves are the *file-keyword bitmaps* of each block in the segment, or the *roots of the transaction-level indices* of those blocks.
- *Why needed:* the two produce different trees, different roots, different proofs, and different verification procedures; the whole Opt. 2 verification path depends on it.
- *Where the paper is internally inconsistent:* R-OPT-011 records that the Sec. VI introduction speaks of "hashing the roots of these transaction-level indices" while the detailed text says to categorize "the file keyword bitmaps of all blocks within a segment" by keyword and build one MHT per keyword; Fig. 5 shows leaves as "bm_k | h_k", supporting the bitmap reading.
- *Affects:* Opt. 2 roots, stage-1/stage-2 VO composition, the No-aggre ablation; T-050, T-052.
- *Candidate interpretations:* (a) per-keyword MHT over bitmaps (detailed text + Fig. 5); (b) per-segment MHT over transaction-level index roots (introduction); (c) both structures exist. **Do not reconcile — the user must choose.**

**GAP-31 — Aggregated MHT details and the modified IIMT leaf. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* the MHT's arity; the leaf ordering (by BlockHeight? by tsn within a block?); the leaf hash (is the LG-aggregation leaf `H(BlockHeight||tsn||H(tx))` or the raw concatenation fed to the parent?); exactly which blocks/transactions of the segment are included (all, or only those where the keyword/lid occurs); and how the deposited root RH_i enters the block-level IIMT leaf hash, given that R-CRYPTO-003 defines that hash as `H(w||H(bm))` with no room for RH_i.
- *Why needed:* the last point is decisive: if RH_i is not in the leaf hash, the aggregated root is not committed and Opt. 2 provides no security; if it is, R-CRYPTO-003's formula is different at the block level than elsewhere, which the paper never states.
- *Where the paper is silent:* VI and Fig. 5 (leaves "W_i | h_i / bm_i | RH_i" and "LG_i | h_i / bm_i | RH_i") show the modified leaf but give no hash formula; §3.12 records all of this as unspecified.
- *Affects:* block-level IIMT roots under Opt. 2, stage-1 VO, the All vs. No-aggre comparison in Fig. 7; T-050, T-051.
- *Candidate interpretations:* (a) leaf hash becomes `H(w||H(bm)||RH_i)`; (b) `H(w||H(bm)||H(RH_i))`; (c) RH_i is stored beside the leaf but committed via a separate root added to the header's RH.

**GAP-32 — Which proofs aggregation replaces, and behaviour without partitioning. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* which stage-1/stage-2 proofs an aggregated proof replaces, what the verifier checks in their place, and what aggregation means when Opt. 1 is disabled (aggregation is defined "within a segment").
- *Why needed:* R-OPT-013 claims VO reduction "without changing the asymptotic structure of the VO", but the verifier's procedure under aggregation is never written. The No-multi-win ablation runs aggregation with partitioning disabled, which the definition does not cover.
- *Where the paper is silent:* VI states the motivation (per-block transaction-level paths cannot be aggregated) and the construction, but not the query-time or verification-time procedure; VIII-C reports the ablation numbers.
- *Affects:* Fig. 7 both panels; VO sizes in Tables III, V, VI when Opt. 2 is on; T-052, T-053.
- *Candidate interpretations:* (a) the per-block transaction-level FK-IIMT bitmap proofs are replaced by one aggregated-MHT proof per keyword per segment, and the per-block OMT proofs for lg's transactions by one aggregated-MHT proof per lid per segment; (b) aggregation supplements rather than replaces, and the reduction comes from shared internal hashes; (c) with Opt. 1 off, the whole chain is one segment. See also GAP-48.

### 16.6 Query protocol and scope

**GAP-33 — Stage-3 message contents and verification ordering. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* whether the auditor sends fids or lids (or both) with Q_off; whether stage-1 and stage-2 verification happen before the auditor applies the bm_EW filter and contacts the SP, or after everything returns; and what happens when the SP fails to answer for a selected file.
- *Why needed:* if the auditor filters on unverified metadata it may query files that stage-2 verification later rejects; and an unanswered file is either a rejection condition or a silently tolerated omission — the difference is a completeness hole.
- *Where the paper conflicts with itself:* §1.4 D-6 records that V-D's text says "log auditor sends SP fids of those log files with Q_off. SP locates log files based on lg and fids", while Fig. 4 labels the message "(4) lids, Q_off". V-E orders the three verification stages but does not interleave them with the query steps.
- *Affects:* the protocol message set, the SP's lookup path, and the accept/reject outcome when a file is missing; T-033, T-043.
- *Candidate interpretations:* (a) text governs: fids plus the already-known lg; (b) figure governs: lids; (c) both are sent. For ordering: (a) verify stages 1–2 before filtering (safe); (b) verify everything at the end (matches V-E's presentation).

**GAP-34 — Query forms the ALIS query does not cover. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* how to process a query with no time range (evaluation query types 1, 2, 3), a query with no lg, and the Boolean predicates over identifiers that II-B advertises but the V-D query form Q = \<lg, [ts, te], FK, EK\> does not provide; plus the general operator precedence and parenthesisation rule for expressions other than the paper's worked shape; plus what a range-free query means under Opt. 1's segment selection, which is driven entirely by the time range.
- *Why needed:* three of the seven evaluated query types have no range. Under Opt. 1 with no range, either all segments are searched (defeating the optimization and changing the reported timings) or a different rule applies.
- *Where the paper is ambiguous:* II-B/II-D promise Boolean queries "on keywords and identifiers"; V-D fixes a single query shape; IV-D's only worked precedence example is `¬a ∧ b ∨ c` = `((¬a) ∧ b) ∨ c`; VIII-A lists the seven types without saying how the range-free ones are routed.
- *Affects:* Tables II and III rows 1–3 (Boolean AND / OR / NOT-AND) entirely; Fig. 6; T-067.
- *Candidate interpretations:* (a) a missing range is treated as [−∞, +∞] and the range sub-query is skipped (Opt. 1 then scans all segments); (b) range-free queries bypass the block-level time-range sub-query but still use the LG and FK sub-queries; (c) the evaluation's range-free types implicitly use the whole dataset range.

**GAP-35 — Is the full node an adversary? NON-BLOCKING.**
- *Missing decision:* whether a full node answering Q_on is modelled as potentially malicious.
- *Why needed:* it does not change the implementation — verification of full-node responses is mandated regardless (V-E, VI) — but it determines how the test suite classifies T-042(c)/(d) and what the reproduction may claim about security.
- *Where the paper is ambiguous:* II-C names only the SP as untrusted and calls the blockchain's assumption the protection for on-chain data; V-E and VI nonetheless verify the full node's answers; Definition 2 speaks of "a malicious log audit system" without naming a party.
- *Affects:* documentation and test classification only; T-077.

**GAP-36 — Who builds and stores the entry-level ADS. NON-BLOCKING.**
- *Missing decision:* whether the log generator sends the SP raw entries only (SP rebuilds the index) or entries plus the built index; and which party's cost Table IV's "SP" column measures.
- *Why needed:* it does not change any hash — the generator computes RH either way (R-FUNC-003) — but it determines process placement and the meaning of the Table IV SP row (0.05 s/file, 420 KB/file).
- *Where the paper is ambiguous:* V-C attributes construction to the log generator; V-A says the SP "maintains the entry-level index"; Table IV labels the off-chain construction cost "SP".
- *Affects:* Table IV attribution; T-066, T-068.

**GAP-37 — Data-model mismatch between Sec. II-B and the Sec. VIII experiment. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* which data model is implemented: II-B's `m_i = \<lid, fid, [ts_min, ts_max], FW, bm_EW, RH\>` and `e_i = \<esn, ts, EW, V\>`, or the experiment's `\<lid, [timestamp_min, timestamp_max], {file keywords}, bitmap, IIMT root, OMT root\>` (no fid) and `\<timestamp, {entry keywords}, content\>` (no esn).
- *Why needed:* fid is the sort key for tsn assignment (R-ALIS-011) and the identifier the auditor sends to the SP (R-QUERY-020) — the experiment's model has neither. esn is the OMT oid at entry level — the experiment's entry has no esn. If the experiment's reduced model is authoritative, the protocol as specified in II-B/V-C cannot run; if II-B is authoritative, the reported numbers were produced from a different data layout than the one implemented, and metadata sizes (Table IV KB/block) will differ.
- *Where the paper conflicts with itself:* §1.4 D-7 records both statements verbatim.
- *Affects:* H(tx), transaction size and Table IV KB/block, OMT leaves, the stage-3 message (GAP-33); T-032, T-067.
- *Candidate interpretations:* (a) II-B governs and the VIII description is an abbreviation; (b) VIII governs for the evaluation only, with fid/esn implicit (positional); (c) both, with fid/esn derived from position — which makes them unauthenticated. **Do not reconcile — the user must choose.**

**GAP-38 — VO wire encoding and result ordering. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* the wire format of the VO (component order, framing, whether bitmaps are sent raw or compressed, whether oids are sent as a list or a bitmap) and the emission order of result sets.
- *Why needed:* VO size in KB is a primary reported metric across Tables III, V, VI and Figs. 6, 7. Any comparison with the paper's numbers is meaningless without a defined encoding. The paper's own numbers were produced with an encoding it does not describe, so exact agreement is not achievable — only a documented encoding plus trend comparison.
- *Where the paper is silent:* IV-E names VO's components; V-D says VOs are "integrated"; no serialization appears anywhere.
- *Affects:* every VO-size figure; T-056, T-069, T-070, T-071.

**GAP-39 — Storage backend and persistence format. NON-BLOCKING.**
- *Missing decision:* how the SP stores log files and how full nodes persist blocks and indexes (in-memory for the experiment, or on disk, and in what format).
- *Why needed:* it affects measured times (Table IV construction, query CPU time) but no hash. The paper permits a centralized or decentralized SP (II-A) and names no database; the no-extra-feature rule bars adopting one.
- *Where the paper is silent:* II-A ("e.g., cloud storage ... e.g., IPFS"); VIII gives no storage detail.
- *Affects:* Table IV timings and R-EVAL-017's CPU-time metric; T-066.
- *Candidate interpretations:* (a) in-memory with flat-file persistence, which matches a CPU-time metric; (b) an embedded KV store — an extra dependency requiring approval under R-GO-003.

**GAP-40 — Concurrency. NON-BLOCKING.**
- *Missing decision:* whether the reproduction parallelises transaction-level and entry-level query processing, and with what degree of parallelism.
- *Why needed:* V-D *permits* parallelism; it changes wall-clock numbers but must not change results (R-GO-024). Note that the baseline vChain+ is configured with "4 verification threads" (R-EVAL-018) while no thread count is given for BLAS, so a like-for-like comparison is undefined.
- *Where the paper is silent:* V-D states the independence that enables parallelism; VIII reports "CPU time" without stating single- or multi-threaded execution.
- *Affects:* Tables II, V and Fig. 6/7 timings, and their comparability with the paper; T-078.

### 16.7 Evaluation

**GAP-41 — Dataset preparation. IMPLEMENTATION-BLOCKING (evaluation scope).**
- *Missing decision:* the exact LogHub file versions and sizes; how much raw data is needed to reach 8192 blocks (8192 × 256 × 2048 ≈ 4.29 × 10⁹ entries, which very likely exceeds both datasets — so entries must be reused, or the block counts mean something else); the unit of "randomly and uniformly distributing the raw log data" among 4096 generators (per entry, per line, per contiguous chunk?); whether per-generator entry order is preserved (required for the esn/timestamp monotonicity assumption); the file-to-block packing order; the treatment of remainders (a final short file or block); and the RNG and seed.
- *Why needed:* every reported number depends on the data. The entry-count arithmetic above should be checked against the PDF before the reproduction commits to an interpretation.
- *Where the paper is silent:* VIII "Dataset" gives the counts (4096 generators, 2048 entries/file, 256 files/block) and "randomly and uniformly distributing" and nothing else.
- *Affects:* every table and figure; T-067.

**GAP-42 — Unstated experiment settings. IMPLEMENTATION-BLOCKING (evaluation scope).**
- *Missing decision:* whether the Table IV configuration (block-level index size 1024, fanout 4) also applies to Tables II, III, V, VI and Figs. 6, 7, and whether "all trees" includes the OMT and the aggregated MHTs; the query-generation procedure, RNG and seed for the "10 random queries"; the query type, block count and window placement for the range-width experiment (Tables V, VI); the dataset, keyword kind (file, entry or both) and presence of lid/range for Fig. 6; and the dataset for Fig. 7.
- *Why needed:* fanout and index size change tree heights, proof sizes and timings by large factors; window placement changes range selectivity; the Fig. 7 dataset is needed to compare against Table III (the All VO values 10.52 / 56.29 / 128.85 coincide exactly with the Table III Android AND+RANGE-20% row, which suggests but does not state Android).
- *Where the paper is silent:* VIII-B states the configuration only in the Table IV paragraph (§1.4 D-3); VIII-A states "10 random queries" with no generator.
- *Affects:* Tables II–VI, Figs. 6, 7; T-067, T-069, T-070, T-071.

**GAP-43 — Measurement and accounting rules, and the reproduction criterion. IMPLEMENTATION-BLOCKING (evaluation scope).**
- *Missing decision:* what "query time" includes (index traversal only? VO assembly? serialization? SP round trip? verification?); whether it is CPU time or wall clock and whether it aggregates across parties; whether KB means 1000 or 1024 bytes; what Table IV's construction time and ADS size include (all four block-level trees plus the transaction-level trees? amortized block-level cost per block?); and what counts as a successful reproduction (a numeric tolerance, or trend agreement only).
- *Why needed:* without the accounting rule, reproduced numbers cannot be compared to the paper's at all, and the acceptance tests T-068/T-072/T-074 have no pass condition.
- *Where the paper is ambiguous:* VIII-A lists the three metrics; VIII-B reports values; nothing defines composition.
- *Affects:* all reported numbers and the report's conclusions; T-068, T-072, T-074.

**GAP-44 — Are the baselines in scope? IMPLEMENTATION-BLOCKING (scope decision).**
- *Missing decision:* whether the reproduction implements vChain+, MSTDB and MART (each "adapted with an extra off-chain ADS layer") or reports BLAS numbers only.
- *Why needed:* it roughly triples or quadruples the project, and the comparative claims of R-EVAL-020 are unverifiable without them. The adaptation itself ("adapted with an extra off-chain ADS layer") is described in one phrase and would need its own specification.
- *Where the paper is silent on reproduction scope:* VIII gives the baselines and their parameters but the brief does not settle whether they are part of this reproduction.
- *Affects:* Tables II, III, IV, V, VI comparative columns; R-EVAL-018, R-EVAL-020; T-072, T-074.

**GAP-45 — Inconsistencies in the paper's reported values and labels. NON-BLOCKING.**
- *Missing decision:* how to treat apparent errors in the published results when comparing.
- *Items:* (i) the Fig. 7 legend reads "all", "no part", "no aggr" while the text uses All / No-aggre / No-multi-win (§1.4 D-5) — "no part" and "No-multi-win" may or may not be the same variant, which interacts with GAP-48; (ii) in Tables V/VI the Spark VO-size row is numerically identical to the Spark query-time row (63.936 / 106.94 / 152.98 / 197.079 / 242.051), which cannot be right for two different units; (iii) §12.4 records the Table II Android "Complex + RANGE 20%" row and the Table III Android "Boolean NOT-AND" row as the identical sequence 5.90 / 25.31 / 47.68 / 185.51 / 374.96 — one is probably a transcription or typesetting duplication (see ADDENDUM A-1); (iv) the Table III Android "NOT-AND + RANGE 20%" row begins 0.46 KB at 128 blocks, an order of magnitude below every neighbouring value (see ADDENDUM A-2).
- *Affects:* only the comparison targets in R-EVAL-019; T-069, T-071, T-074.

**GAP-46 — Keyword extraction and file-keyword determination. IMPLEMENTATION-BLOCKING (evaluation scope).**
- *Missing decision:* how entry keywords are "extracted from each entry" of the raw Android and Spark logs (tokenization, stop-words, log-template/parameter separation, case handling, vocabulary size) and how file keywords FW are "determined by the log generators".
- *Why needed:* the keyword extraction determines every IIMT bitmap, every IIMT root, the keyword universe size (hence tree heights and VO sizes), and query selectivity — and therefore every timing and VO number. It is the single largest uncontrolled variable in the evaluation.
- *Where the paper is silent:* II-B defines FW and EW abstractly; VIII says entry keywords are "extracted from each entry" and file keywords are "determined by the log generators", with no procedure.
- *Affects:* all of Tables II–VI and Figs. 6, 7; every root hash in the evaluation; T-067.
- *Candidate interpretations:* (a) whitespace tokenization of the message field with a frequency cutoff; (b) LogHub's own log templates as keywords; (c) a fixed small vocabulary (e.g. severity levels and component names), with FW = the union of its file's entry keywords or a generator-chosen tag set.

**GAP-47 — Timestamp parsing and normalization of the raw datasets. NEW IN THIS ROUND. IMPLEMENTATION-BLOCKING (evaluation scope).**
- *Missing decision:* how raw log timestamps are parsed into the protocol timestamp type when the raw records are incomplete or heterogeneous (e.g. records carrying no year, differing resolutions between the Android and Spark datasets, entries with no parseable timestamp, and records whose timestamps are non-monotonic within a generator's assigned slice).
- *Why needed:* R-EVAL-003 represents each entry as `\<timestamp, {keywords}, content\>`, and V-C requires that recorded timestamps within a file be "locally consistent with the entry generation order" — random distribution across 4096 generators (R-EVAL-002) does not guarantee that, so some normalization or sorting step must exist. The chosen resolution also sets the number of distinct RIMBT leaf values (GAP-22), which drives tree height, VO size and construction time.
- *Where the paper is silent:* VIII describes the datasets and the entry representation but no parsing, normalization or sorting step.
- *Affects:* all RIMBT structures and every range experiment; Tables V, VI; T-067.
- *Candidate interpretations:* (a) parse to milliseconds, supply a fixed year where absent, sort each generator's slice by timestamp before packing; (b) preserve raw order and use a synthetic monotonically increasing timestamp; (c) drop unparseable records.

**GAP-48 — "Multi-window processing" is never defined. NEW IN THIS ROUND. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* what "multi-window processing" is, and whether the No-multi-win ablation disables (i) block-level index partitioning entirely, (ii) a separate multi-window query-execution mechanism layered on partitioning, or (iii) something the Fig. 7 legend calls "no part".
- *Why needed:* R-OPT-015 requires each optimization to be independently switchable to reproduce the ablation. If "multi-window" is merely Sec. VI's segmentation, the variant is clear; if it is a distinct mechanism (e.g. processing several segment windows in one pass), Sec. VI never describes it and it cannot be implemented.
- *Where the paper is ambiguous:* Sec. VI's optimization is titled block-level index partition and speaks of segments, never of windows; VIII-C names the variant "No-multi-win"; the Fig. 7 legend reads "no part" (GAP-45(i)).
- *Affects:* Fig. 7 both panels; R-OPT-014, R-OPT-015; T-053, T-071.
- *Candidate interpretations:* (a) "multi-window" = the segment partitioning of Opt. 1, and "no part"/"No-multi-win" are the same variant under two names; (b) a distinct query-side mechanism spanning multiple segment windows, undescribed; (c) the partition exists but the query touches only one window.

**GAP-49 — Empty results. NEW IN THIS ROUND. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* what a prover returns and what a verifier accepts when a query matches no block, no transaction, or no entry at some stage.
- *Why needed:* an empty answer must still be *proven* complete, otherwise a malicious SP or full node can answer "nothing matched" to every query and never be caught — which is exactly the Definition 2 case (c) attack. The paper's algorithms all assume a non-empty path to a result: Alg. 3 returns QR and VO with no empty case; V-D's stage-2 neighbour mechanism presumes lg has transactions in the block; V-E presumes results to verify.
- *Where the paper is silent:* Sec. IV-D, IV-E, V-D, V-E and VII contain no empty-result case.
- *Affects:* soundness/completeness of every negative answer; T-079; potentially the behaviour of all three stages under Opt. 1 segment pruning.
- *Candidate interpretations:* (a) the VO still carries the full bitmap/proof set so the verifier recomputes an empty QR'_O itself and accepts — this follows the spirit of R-VER-005 but is not stated; (b) an explicit empty-result proof form; (c) empty results are outside the model.

**GAP-50 — Authenticated bitmap length N. NEW IN THIS ROUND. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* how the verifier learns, from committed state, the bitmap length N at each ALIS level, so that ¬ is well defined.
- *Why needed:* `¬bm` depends entirely on N: a prover that ships a bitmap truncated at the last set bit, or one padded with extra zeros, changes the result of every ¬ and of the Alg. 5 `¬bm_left ∧ bm_right` rule. At transaction level, TxNum in the header supplies N. At block level, N is the number of blocks in the (segment's) index — derivable from headers if the segment base is fixed (GAP-21, GAP-28). **At entry level there is no committed entry count**: m_i carries no entry count, and the experiment's 2048 entries/file is an experiment value, not a protocol field. Note also that V-D's use of the RIMBT's rightmost leaf bitmap `bm_lnr` (GAP-12) may be exactly the mechanism that supplies an authenticated all-ones reference of length N — the paper does not say so.
- *Where the paper is silent:* IV-B and IV-D define N as "the number of data objects" without a commitment mechanism; V-B's header fields include TxNum but II-B's m_i has no entry count.
- *Affects:* every ¬ evaluation and every range evaluation; the NOT-AND rows of Tables II and III; soundness and completeness of negated queries; T-010, T-080.
- *Candidate interpretations:* (a) N is implied by the structure being proven (leaf count of the OMT), requiring an authenticated leaf count; (b) N is carried in committed metadata (an added field — an extension the paper does not authorise); (c) N is the all-ones bitmap of the RIMBT's largest leaf, authenticated by its own proof.

**GAP-51 — The entry level has no RIMBT, yet ALIS is described as IOMF at three levels. NEW IN THIS ROUND. NON-BLOCKING.**
- *Missing decision:* whether the entry level is a full IOMF instance (which per §3.10 requires an OMT, an IIMT and one RIMBT per numerical attribute) or a reduced instance consisting of an OMT plus B+ Tree temporal information plus an IIMT.
- *Why needed:* it decides whether IV-E's IOMF verification procedure (which parses `VO = \<BM_K, π_K, BM_R, π_R, QR_O, π_O\>`, with a range component) applies verbatim at the entry level, or whether a level-specific verification procedure is needed (interacts with GAP-17 and GAP-18).
- *Where the paper is in tension:* V-C describes the entry-level index as an OMT combined with B+ Tree temporal information plus an entry-keyword IIMT — no RIMBT; §3.11 and R-ALIS-001 nonetheless present all three levels as built from IOMF components.
- *Affects:* documentation and the shape of entry-level verification; T-039, T-033.
- *Candidate interpretations:* (a) the entry level is deliberately a reduced IOMF, with the B+ Tree replacing the RIMBT for the single temporal attribute (V-C's literal reading); (b) the B+ Tree *is* the RIMBT under another name — contradicted by R-ALIS-007, which forbids that substitution.

**GAP-52 — Approval of the BLAKE2b dependency. NEW IN THIS ROUND. IMPLEMENTATION-BLOCKING.**
- *Missing decision:* whether `golang.org/x/crypto/blake2b` is approved as an external module, or whether BLAKE2b must be implemented in-repo from RFC 7693.
- *Why needed:* R-CRYPTO-001 mandates BLAKE2b and the Go standard library does not provide it, so R-GO-003's conditions (1) and (2) are both met; but condition (3) — user approval — has not been given, and this document must not grant it. Nothing can be hashed until this is settled.
- *Affects:* the entire implementation; `go.mod`; T-054, T-059.
- *Candidate interpretations:* (a) approve `golang.org/x/crypto/blake2b` (an official Go-team module, the conventional choice); (b) vendor a minimal RFC 7693 implementation in-repo, with correctness tested against RFC test vectors; (c) approve the module for now and revisit.

### 16.8 Gap summary

| Category | GAP IDs | Count |
|---|---|---|
| Hashing and encoding | 1, 2, 3, 4 | 4 |
| Tree construction | 5, 6, 7, 8 | 4 |
| Proofs and query evaluation | 9, 10, 11, 12, 13, 14, 15, 16 | 8 |
| ALIS levels | 17, 18, 19, 20, 21, 22, 23, 24, 25, 26 | 10 |
| Optimizations | 27, 28, 29, 30, 31, 32 | 6 |
| Query protocol and scope | 33, 34, 35, 36, 37, 38, 39, 40 | 8 |
| Evaluation | 41, 42, 43, 44, 45, 46, 47 (47 new this round) | 7 |
| Cross-cutting, all new this round | 48, 49, 50, 51, 52 | 5 |
| **Total** | **GAP-1 … GAP-52** | **52** |

**IMPLEMENTATION-BLOCKING (46):** GAP-1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 37, 38, 41, 42, 43, 44, 46, 47, 48, 49, 50, 52.

**NON-BLOCKING (6):** GAP-35, 36, 39, 40, 45, 51.

**Gaps arising from the paper contradicting itself** (the user must choose a reading; this document must not reconcile them): GAP-8 (Alg. 2 box vs. IV-C prose), GAP-27 (VI introduction vs. VI detail), GAP-30 (VI introduction vs. VI detail), GAP-33 (V-D text vs. Fig. 4), GAP-37 (II-B vs. VIII), GAP-45 (published values and labels), GAP-48 (VIII-C text vs. Fig. 7 legend), GAP-51 (V-C vs. the IOMF definition). GAP-5 additionally rests on a tension between Sec. III (MHT is binary) and VIII-B ("fanout of all trees to 4").

### 16.9 ADDENDA TO §1–§12

These were candidate corrections to §1–§12 raised in round 2. **All five were ruled on 2026-09-12; the outcomes are below and the approved edits have been applied.**

| Item | Outcome | Effect on §1–§12 |
|---|---|---|
| A-1 | **Confirmed against the published table** — the duplicated values really are printed. | No edit. Recorded under GAP-45. |
| A-2 | **Confirmed** — 0.46 KB is printed in Table III. | No edit. Marked suspicious; excluded from strict numeric validation. |
| A-3 | **Approved.** | Applied: R-CRYPTO-012, R-FUNC-019, R-FUNC-020 re-tagged `[PROJECT-RULE]` on the prohibitive clauses. Substance unchanged. |
| A-4 | **Approved.** | Applied: R-ALIS-001 qualified — block and transaction levels are full IOMF instances; the entry level is the reduced form. |
| A-5 | **Weak evidence only** — not sufficient to establish the dataset/configuration of Figs. 6–7. | No edit. Recorded under GAP-42. |

A new classification tag is introduced by the A-3 ruling: `[PROJECT-RULE]` = a constraint this reproduction adopts from the project's no-extra-feature rule, where the paper is silent rather than prohibitive. It is **not** a paper mandate and must not be cited as one.

The original analysis of each item follows, unchanged.

**A-1 — Possible duplicated row in §12.4 (Tables II and III).**
- *Existing text (§12.4, Table II, Android column):* "Complex + RANGE 20% | 5.90 / 25.31 / 47.68 / 185.51 / 374.96".
- *Existing text (§12.4, Table III, Android column):* "Boolean NOT-AND | 5.90 / 25.31 / 47.68 / 185.51 / 374.96".
- *Observation:* the identical five-value sequence appears as a query-time row (ms) in Table II and as a VO-size row (KB) in Table III. This matches the duplication pattern already flagged for Tables V and VI in §12.4 and GAP-45(ii), which §12.4 flags there but not here.
- *Proposed correction:* re-read Tables II and III in the PDF. If the duplication is in the paper, add a note to the Table II Complex row pointing at GAP-45. If it is a transcription slip, correct the affected row. **Decision left to the user.**

**A-2 — Implausible value in §12.4 (Table III, Android, NOT-AND + RANGE 20%).**
- *Existing text:* "NOT-AND + RANGE 20% | 0.46 / 21.80 / 36.61 / 121.53 / 224.17".
- *Observation:* 0.46 KB at 128 blocks is roughly an order of magnitude below every other 128-block VO value in the same table (4.17–17.48 KB) and breaks the otherwise monotone, roughly linear growth of its own row. It is likely either a typo in the published table (e.g. 20.46) or a transcription slip.
- *Proposed correction:* confirm the printed value. If the paper prints 0.46, record it as a further item under GAP-45 rather than treating it as a reproduction target. **Decision left to the user.**

**A-3 — Three `[MANDATED]` tags that rest on the paper's silence plus the project's no-extra-feature rule, not on a paper statement.**
- *Existing text (R-CRYPTO-012):* "`[MANDATED]` Transactions carry signatures ... **No other signatures shall be added.**"
- *Existing text (R-FUNC-019):* "`[MANDATED]` The implementation shall not require a specific blockchain platform, and none is selected. — II-A, V-F (**the paper names none**)."
- *Existing text (R-FUNC-020):* "`[MANDATED]` No confidentiality mechanism shall be part of the reproduction. Sec. X states confidentiality is **not a primary design goal and can be layered independently**."
- *Observation:* in each case the paper's actual statement is weaker than the requirement. Sec. X says confidentiality is not a design goal — it does not forbid it; II-A/V-F name no platform — they do not forbid selecting one; V-B mentions signatures — it does not forbid others. The prohibitions are correct for this reproduction, but they derive from the STRICT NO-EXTRA-FEATURE RULE of the brief, not from a paper mandate. Under the document's own tag definition, `[MANDATED]` means "explicitly stated by the paper".
- *Proposed correction:* keep the requirements unchanged in substance, but re-tag the prohibitive clauses (or annotate them) as project-rule constraints rather than paper mandates, so that the classification convention stays sound. **Decision left to the user.**

**A-4 — R-ALIS-001's claim that all three levels are "built from IOMF components".**
- *Existing text (R-ALIS-001):* "`[MANDATED]` ALIS shall have exactly three levels: block level, transaction level, entry level. **All are built from IOMF components.**"
- *Existing text (§3.10, IOMF):* the IOMF over one object set comprises "one OMT; one IIMT for keywords; one RIMBT per numerical attribute".
- *Existing text (§3.11 table, entry level):* "OMT (esn, entry) combined with B+ Tree temporal information; entry-keyword IIMT" — no RIMBT.
- *Observation:* the entry level as described contains no RIMBT, so it is not an IOMF instance under §3.10's own definition. Whether V-C actually claims the entry level is an IOMF instance could not be checked this session.
- *Proposed correction:* either qualify R-ALIS-001 ("the block and transaction levels are full IOMF instances; the entry level is a reduced instance in which B+ Tree temporal information replaces the RIMBT") or leave it and rely on GAP-51. **Decision left to the user.**

**A-5 — §12.4 Fig. 6 cross-reference worth recording.**
- *Existing text (§12.4, Fig. 6):* "OR: 57.58 ms → 321.25 ms and **32.85 KB** → 173.42 KB."
- *Existing text (§12.4, Table III, Android, Boolean AND at 1024 blocks):* "**32.85**".
- *Observation:* Fig. 6's two-keyword OR VO size equals the Table III Android two-keyword AND VO size at 1024 blocks, which is plausible (with two keywords, AND and OR ship the same bitmaps and proofs) and, together with the Fig. 7 coincidence already noted in §12.4, is weak evidence that Figs. 6 and 7 use the Android dataset. This is evidence for resolving GAP-42, not a correction.
- *Proposed action:* record the observation under GAP-42 when it is decided. No change to §12.4 required. **Decision left to the user.**
