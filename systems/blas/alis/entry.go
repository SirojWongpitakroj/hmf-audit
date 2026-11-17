// Package alis implements the Authenticated Layered Index Structure
// (Sec. V-C): entry level (off-chain, per log file), transaction level
// (on-chain, per block) and block level (on-chain, global — Opt. 1
// segmentation is layered on top in a later milestone).
//
// Entry level (R-ALIS-002..009, qualified by addendum A-4 / DECISIONS.md
// D8): a reduced IOMF — OMT + IIMT, with a local (non-authenticated) B+ Tree
// -style temporal accelerator replacing the RIMBT (D1: "the entry-level B+
// tree is a local temporal search accelerator only... contributes no
// additional root"). Because esn order equals timestamp order within a file
// (R-ALIS-007's stated assumption), the accelerator is simply a binary
// search over the already esn-ordered entries; authentication of any
// returned range comes entirely from OMT leaf/boundary proofs.
package alis

import (
	"fmt"
	"sort"

	"blas/bhash"
	"blas/bitmap"
	"blas/iimt"
	"blas/omt"
)

// LogEntry is e_i = <esn, ts, EW, V> (R-FUNC-002, II-B). ts is a signed
// Unix-millisecond timestamp (D7 in DECISIONS.md).
type LogEntry struct {
	ESN uint64
	TS  int64
	EW  []string
	V   []byte
}

// EncodeLogEntry serializes a LogEntry's declared fields, in declared
// order, for H(o) (A5 in DECISIONS.md): esn, ts, EW, V. EW is a set with no
// protocol-defined order; it is sorted ascending byte-wise here for a
// canonical, deterministic serialization (an explicit interpretation, since
// A5 fixes cross-field order but not within-set order — recorded as an
// assumption, not silently invented, per TRACKER.md's discipline).
func EncodeLogEntry(e LogEntry) []byte {
	ew := make([]string, len(e.EW))
	copy(ew, e.EW)
	sort.Strings(ew)

	out := bhash.Uint64BE(e.ESN)
	out = append(out, bhash.Int64BE(e.TS)...)
	out = append(out, bhash.Uint32BE(uint32(len(ew)))...)
	for _, w := range ew {
		out = append(out, bhash.FramedString(w)...)
	}
	out = append(out, bhash.FramedBytes(e.V)...)
	return out
}

// LogGenerator holds one log generator's persistent, append-only
// entry-keyword universe (C1 in DECISIONS.md: kid starts at 0, increases
// monotonically; entry-level universes are per log generator).
type LogGenerator struct {
	LID     uint64
	order   []string        // keywords in kid order (kid == index)
	present map[string]bool // lookup accelerator only (R-GO-006)
}

// NewLogGenerator returns a generator with an empty keyword universe.
func NewLogGenerator(lid uint64) *LogGenerator {
	return &LogGenerator{LID: lid, present: map[string]bool{}}
}

// ensureKeyword appends w to the persistent universe if new, returning its
// kid and whether it was newly created.
func (g *LogGenerator) ensureKeyword(w string) (kid uint64, isNew bool) {
	if g.present[w] {
		for i, k := range g.order {
			if k == w {
				return uint64(i), false
			}
		}
	}
	kid = uint64(len(g.order))
	g.order = append(g.order, w)
	g.present[w] = true
	return kid, true
}

// KeywordUniverse returns the generator's current keyword universe, in kid
// order. The returned slice is a copy.
func (g *LogGenerator) KeywordUniverse() []string {
	out := make([]string, len(g.order))
	copy(out, g.order)
	return out
}

// EntryFileBuilder accumulates entries for one immutable log file
// (R-FUNC-003). Its entry-level IIMT is frozen with exactly the generator's
// keyword universe as of Close (C2 in DECISIONS.md): every keyword known at
// that moment gets a leaf, including one first seen inside this same file.
type EntryFileBuilder struct {
	gen     *LogGenerator
	fid     uint64
	fanout  int
	iimt    *iimt.Tree
	entries []LogEntry
}

// NewEntryFileBuilder starts a new file for generator gen with the given
// fanout F for its IIMT and OMT.
func NewEntryFileBuilder(gen *LogGenerator, fid uint64, fanout int) (*EntryFileBuilder, error) {
	tree, err := iimt.NewEmpty(fanout)
	if err != nil {
		return nil, err
	}
	// Seed the file's IIMT with the generator's keyword universe as it
	// stands right now, in the same (kid) order, so kids stay aligned.
	for _, w := range gen.order {
		tree.AddKeyword(w)
	}
	return &EntryFileBuilder{gen: gen, fid: fid, fanout: fanout, iimt: tree}, nil
}

// AddEntry appends one entry in chronological order (esn assigned
// sequentially from 0, R-ALIS-003). Any keyword not yet in the generator's
// universe is appended to it (new kid) and also added as a leaf of this
// file's IIMT (C2: a keyword first occurring in this file is included in
// this file's IIMT).
func (b *EntryFileBuilder) AddEntry(ts int64, ew []string, v []byte) (uint64, error) {
	esn := uint64(len(b.entries))
	for _, w := range ew {
		if _, isNew := b.gen.ensureKeyword(w); isNew {
			b.iimt.AddKeyword(w)
		}
	}
	if err := b.iimt.Insert(esn, ew); err != nil {
		return 0, err
	}
	b.entries = append(b.entries, LogEntry{ESN: esn, TS: ts, EW: ew, V: v})
	return esn, nil
}

// EntryFile is a closed, immutable entry-level index (R-ALIS-002).
type EntryFile struct {
	LID     uint64
	FID     uint64
	Entries []LogEntry // esn order == ts order (R-ALIS-007 assumption)
	IIMT    *iimt.Tree
	OMT     *omt.Tree
}

// FileMetadata is m_i = <lid, fid, [ts_min, ts_max], FW, bm_EW, RH>
// (R-FUNC-003, II-B).
type FileMetadata struct {
	LID    uint64
	FID    uint64
	TsMin  int64
	TsMax  int64
	FW     []string // union of this file's entry keywords (H5 in DECISIONS.md)
	BmEW   bitmap.Bitmap
	RHIIMT bhash.Digest // RH[0]: entry-level IIMT root
	RHOMT  bhash.Digest // RH[1]: entry-level OMT root ("IIMT root hash, OMT root hash" order, VIII)
}

// Close finalizes the file: builds its OMT, computes m_i. Requires at least
// one entry (an empty file has no ts_min/ts_max and is out of scope — the
// paper describes no empty-file case).
func (b *EntryFileBuilder) Close() (*EntryFile, FileMetadata, error) {
	if len(b.entries) == 0 {
		return nil, FileMetadata{}, fmt.Errorf("alis: cannot close an empty log file")
	}
	leaves := make([]omt.Leaf, len(b.entries))
	for i, e := range b.entries {
		leaves[i] = omt.Leaf{OID: e.ESN, Object: EncodeLogEntry(e)}
	}
	omtTree, err := omt.Build(b.fanout, leaves)
	if err != nil {
		return nil, FileMetadata{}, err
	}
	iimtRoot, err := b.iimt.Root()
	if err != nil {
		return nil, FileMetadata{}, err
	}
	omtRoot, err := omtTree.Root()
	if err != nil {
		return nil, FileMetadata{}, err
	}

	tsMin, tsMax := b.entries[0].TS, b.entries[0].TS
	fwSet := map[string]bool{}
	for _, e := range b.entries {
		if e.TS < tsMin {
			tsMin = e.TS
		}
		if e.TS > tsMax {
			tsMax = e.TS
		}
		for _, w := range e.EW {
			fwSet[w] = true
		}
	}
	// FW as a deterministic ascending list (never a map iteration order).
	var fw []string
	for _, w := range b.gen.order {
		if fwSet[w] {
			fw = append(fw, w)
		}
	}

	// bm_EW: an ordinary bitmap over the generator's maintained
	// entry-keyword universe as of file close (C4 ruling: no Bloom
	// filter), bit kid = 1 iff that keyword is in FW.
	bmEW := bitmap.New(uint64(len(b.gen.order)))
	for kid, w := range b.gen.order {
		if fwSet[w] {
			bmEW.Set(uint64(kid))
		}
	}

	file := &EntryFile{LID: b.gen.LID, FID: b.fid, Entries: append([]LogEntry{}, b.entries...), IIMT: b.iimt, OMT: omtTree}
	meta := FileMetadata{
		LID: b.gen.LID, FID: b.fid,
		TsMin: tsMin, TsMax: tsMax,
		FW: fw, BmEW: bmEW,
		RHIIMT: iimtRoot, RHOMT: omtRoot,
	}
	return file, meta, nil
}

// TimeRangeLookup implements the local, non-authenticated B+-tree-style
// temporal accelerator (D1): a binary search over the esn/ts-ordered
// entries for [ts, te], returning the matching esns plus, for completeness
// evidence, the OMT proof of the entry immediately before the range and the
// one immediately after (when they exist) — the "OMT leaf nodes that
// contain the matching and boundary values of ts and te" (R-QUERY-021).
func (f *EntryFile) TimeRangeLookup(ts, te int64) (matchingESNs []uint64, lowerBoundaryProof *omt.Proof, upperBoundaryProof *omt.Proof, err error) {
	n := len(f.Entries)
	lo := sort.Search(n, func(i int) bool { return f.Entries[i].TS >= ts })
	hi := sort.Search(n, func(i int) bool { return f.Entries[i].TS > te })
	for i := lo; i < hi; i++ {
		matchingESNs = append(matchingESNs, f.Entries[i].ESN)
	}
	if lo-1 >= 0 {
		p, ok := f.OMT.Prove(f.Entries[lo-1].ESN)
		if !ok {
			return nil, nil, nil, fmt.Errorf("alis: missing OMT proof for lower boundary")
		}
		lowerBoundaryProof = &p
	}
	if hi < n {
		p, ok := f.OMT.Prove(f.Entries[hi].ESN)
		if !ok {
			return nil, nil, nil, fmt.Errorf("alis: missing OMT proof for upper boundary")
		}
		upperBoundaryProof = &p
	}
	return matchingESNs, lowerBoundaryProof, upperBoundaryProof, nil
}
