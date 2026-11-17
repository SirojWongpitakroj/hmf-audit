// Transaction-level ALIS (R-ALIS-010..017): one full IOMF instance per
// block — OMT over (tsn, transaction), a file-keyword IIMT over tsn
// bitmaps, and two RIMBTs (ts_min, ts_max) over tsn bitmaps. Because this
// level shares the same oid domain (tsn) across all three trees, it reuses
// the iomf.Store/ProcessQuery/Verify machinery from milestone M5 directly.
package alis

import (
	"fmt"
	"sort"

	"blas/bhash"
	"blas/bitmap"
	"blas/iimt"
	"blas/iomf"
	"blas/omt"
	"blas/rimbt"
)

// SigWidth is the fixed width of the opaque, unauthenticated-content
// signature placeholder (R-2 in DECISIONS.md: the reproduction neither
// generates nor verifies signatures, but the bytes must be fixed
// deterministically so H(tx) is reproducible). 64 bytes matches a typical
// signature size; the content is arbitrary (all-zero) and carries no
// meaning.
const SigWidth = 64

var zeroSig = make([]byte, SigWidth)

// Transaction carries one log file's metadata plus its (placeholder)
// signature, in the Fig. 3 field order: TSN, Sig, LID, FID, Ts_min, Ts_max,
// FW, BM_EW, RH (R-FUNC-010).
type Transaction struct {
	TSN    uint64
	Sig    []byte // always SigWidth bytes
	LID    uint64
	FID    uint64
	TsMin  int64
	TsMax  int64
	FW     []string
	BmEW   bitmap.Bitmap
	RHIIMT bhash.Digest
	RHOMT  bhash.Digest
}

// NewTransaction builds a Transaction from one file's metadata and its
// assigned tsn. Sig is the fixed zero placeholder (R-2).
func NewTransaction(tsn uint64, m FileMetadata) Transaction {
	return Transaction{
		TSN: tsn, Sig: zeroSig,
		LID: m.LID, FID: m.FID,
		TsMin: m.TsMin, TsMax: m.TsMax,
		FW: append([]string{}, m.FW...), BmEW: m.BmEW,
		RHIIMT: m.RHIIMT, RHOMT: m.RHOMT,
	}
}

// EncodeTransaction serializes every declared field, in Fig. 3 order, for
// H(tx) (A5 in DECISIONS.md — "hash all fields ... including TSN and Sig",
// directly resolving GAP-4 for transactions). FW is sorted ascending for a
// canonical set encoding (same rationale as EncodeLogEntry).
func EncodeTransaction(tx Transaction) []byte {
	fw := make([]string, len(tx.FW))
	copy(fw, tx.FW)
	sort.Strings(fw)

	out := bhash.Uint64BE(tx.TSN)
	sig := tx.Sig
	if len(sig) != SigWidth {
		sig = zeroSig
	}
	out = append(out, sig...)
	out = append(out, bhash.Uint64BE(tx.LID)...)
	out = append(out, bhash.Uint64BE(tx.FID)...)
	out = append(out, bhash.Int64BE(tx.TsMin)...)
	out = append(out, bhash.Int64BE(tx.TsMax)...)
	out = append(out, bhash.Uint32BE(uint32(len(fw)))...)
	for _, w := range fw {
		out = append(out, bhash.FramedString(w)...)
	}
	out = append(out, bhash.Uint64BE(tx.BmEW.Len())...)
	out = append(out, bhash.FramedBytes(tx.BmEW.Bytes())...)
	out = append(out, bhash.DigestBytes(tx.RHIIMT)...)
	out = append(out, bhash.DigestBytes(tx.RHOMT)...)
	return out
}

// HashTransaction returns H(tx).
func HashTransaction(tx Transaction) bhash.Digest {
	return bhash.H(EncodeTransaction(tx))
}

// TransactionLevelBuilder assembles one block's transaction-level index
// (R-ALIS-010..014): sorts by (lid, fid) ascending (D4 in DECISIONS.md:
// unsigned integers, pair must be unique, fid unique within its lid),
// assigns tsn from 0 (D6), and builds the OMT/FK-IIMT/ts_min-RIMBT/
// ts_max-RIMBT over that tsn domain.
type TransactionLevelBuilder struct {
	fanout       int
	transactions []Transaction // sorted by (lid, fid), tsn already assigned
}

// BuildTransactionLevel sorts the given file metadata by (lid, fid),
// assigns tsn from 0, and builds the transaction-level Store.
func BuildTransactionLevel(fanout int, metas []FileMetadata) (*TransactionLevelBuilder, *iomf.Store, error) {
	sorted := make([]FileMetadata, len(metas))
	copy(sorted, metas)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].LID != sorted[j].LID {
			return sorted[i].LID < sorted[j].LID
		}
		return sorted[i].FID < sorted[j].FID
	})
	for i := 1; i < len(sorted); i++ {
		if sorted[i].LID == sorted[i-1].LID && sorted[i].FID == sorted[i-1].FID {
			return nil, nil, fmt.Errorf("alis: duplicate (lid=%d, fid=%d)", sorted[i].LID, sorted[i].FID)
		}
	}

	txs := make([]Transaction, len(sorted))
	for i, m := range sorted {
		txs[i] = NewTransaction(uint64(i), m)
	}

	fkTree, err := iimt.NewEmpty(fanout)
	if err != nil {
		return nil, nil, err
	}
	tsMinTree, err := rimbt.NewEmpty(fanout)
	if err != nil {
		return nil, nil, err
	}
	tsMaxTree, err := rimbt.NewEmpty(fanout)
	if err != nil {
		return nil, nil, err
	}
	// Seed FK IIMT with the union of all file keywords across this block's
	// transactions (block-local keyword universe for this tree, C1: kid
	// from 0, append-only, within this tree's own scope).
	seen := map[string]bool{}
	var universe []string
	for _, tx := range txs {
		for _, w := range tx.FW {
			if !seen[w] {
				seen[w] = true
				universe = append(universe, w)
			}
		}
	}
	sort.Strings(universe) // canonical, deterministic kid assignment order
	for _, w := range universe {
		fkTree.AddKeyword(w)
	}

	var omtLeaves []omt.Leaf
	for i, tx := range txs {
		tsn := uint64(i)
		if err := fkTree.Insert(tsn, tx.FW); err != nil {
			return nil, nil, err
		}
		if err := tsMinTree.Insert(tsn, tx.TsMin); err != nil {
			return nil, nil, err
		}
		if err := tsMaxTree.Insert(tsn, tx.TsMax); err != nil {
			return nil, nil, err
		}
		omtLeaves = append(omtLeaves, omt.Leaf{OID: tsn, Object: EncodeTransaction(tx)})
	}
	omtTree, err := omt.Build(fanout, omtLeaves)
	if err != nil {
		return nil, nil, err
	}

	store := &iomf.Store{
		IIMT: fkTree,
		OMT:  omtTree,
		RIMBTs: map[string]*rimbt.Tree{
			"ts_min": tsMinTree,
			"ts_max": tsMaxTree,
		},
	}
	return &TransactionLevelBuilder{fanout: fanout, transactions: txs}, store, nil
}

// Transactions returns the sorted, tsn-assigned transaction list.
func (b *TransactionLevelBuilder) Transactions() []Transaction {
	return append([]Transaction{}, b.transactions...)
}
