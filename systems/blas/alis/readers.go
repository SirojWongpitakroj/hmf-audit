package alis

import (
	"blas/iimt"
	"blas/iomf"
	"blas/omt"
	"blas/rimbt"
)

// Query readers let storage-backed nodes execute the original ALIS algorithms
// without loading a complete tree. Storage failures must be checked by the
// caller before returning evidence (legacy Query methods encode absence as bool).
type KeywordReader interface {
	Query(string) ([]uint64, iimt.Proof, bool)
	ProveAbsent(string) (iimt.AbsenceProof, bool)
}
type ObjectReader interface {
	Prove(uint64) (omt.Proof, bool)
}
type RangeReader interface {
	RangeQuery(int64, int64) (rimbt.RangeResult, error)
}
type BlockReaders struct {
	LG, FK               KeywordReader
	FileTsMin, FileTsMax RangeReader
}
type TxReaders struct {
	IIMT   KeywordReader
	OMT    ObjectReader
	RIMBTs map[string]RangeReader
}

func txReaders(s *iomf.Store) TxReaders {
	return TxReaders{IIMT: s.IIMT, OMT: s.OMT, RIMBTs: map[string]RangeReader{"ts_min": s.RIMBTs["ts_min"], "ts_max": s.RIMBTs["ts_max"]}}
}

type EntryReaders struct {
	Count           uint64
	IIMT            KeywordReader
	OMT             ObjectReader
	TimeRangeLookup func(int64, int64) ([]uint64, *omt.Proof, *omt.Proof, error)
}
