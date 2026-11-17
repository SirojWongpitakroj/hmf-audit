package eval

import (
	"math/rand"

	"blas/iomf"
	"blas/query"
)

// QueryType names the seven R-EVAL-011 workload shapes.
type QueryType int

const (
	QAnd QueryType = iota
	QOr
	QNotAnd
	QAndRange
	QOrRange
	QNotAndRange
	QComplexRange
)

var queryTypeNames = map[QueryType]string{
	QAnd: "AND", QOr: "OR", QNotAnd: "NOT-AND",
	QAndRange: "AND+RANGE", QOrRange: "OR+RANGE", QNotAndRange: "NOT-AND+RANGE",
	QComplexRange: "Complex+RANGE",
}

func (t QueryType) String() string { return queryTypeNames[t] }

// GenerateQueries implements R-EVAL-010/011: `count` random queries of
// `qt`'s shape, using a fixed-seed PRNG (R-GO-028), over the given lid
// universe, keyword vocabulary, and (for range variants) timeline
// [minTS,maxTS] with a range width of rangeWidthPct percent of that
// timeline.
func GenerateQueries(qt QueryType, count int, seed int64, lids []uint64, vocab []string, minTS, maxTS int64, rangeWidthPct int) []query.Q {
	rng := rand.New(rand.NewSource(seed))
	pick := func() string { return vocab[rng.Intn(len(vocab))] }
	pickLID := func() uint64 { return lids[rng.Intn(len(lids))] }

	span := maxTS - minTS
	if span < 0 {
		span = 0
	}
	width := span * int64(rangeWidthPct) / 100

	out := make([]query.Q, count)
	for i := 0; i < count; i++ {
		lid := pickLID()
		w1, w2, w3 := pick(), pick(), pick()

		var fk, ek *iomf.Expr
		hasRange := false
		var ts, te int64

		switch qt {
		case QAnd:
			fk = iomf.And(iomf.Keyword(w1), iomf.Keyword(w2))
			ek = iomf.And(iomf.Keyword(w1), iomf.Keyword(w2))
		case QOr:
			fk = iomf.Or(iomf.Keyword(w1), iomf.Keyword(w2))
			ek = iomf.Or(iomf.Keyword(w1), iomf.Keyword(w2))
		case QNotAnd:
			fk = iomf.And(iomf.Not(iomf.Keyword(w1)), iomf.Not(iomf.Keyword(w2)))
			ek = iomf.And(iomf.Not(iomf.Keyword(w1)), iomf.Not(iomf.Keyword(w2)))
		case QAndRange:
			fk = iomf.And(iomf.Keyword(w1), iomf.Keyword(w2))
			ek = iomf.And(iomf.Keyword(w1), iomf.Keyword(w2))
			hasRange = true
		case QOrRange:
			fk = iomf.Or(iomf.Keyword(w1), iomf.Keyword(w2))
			ek = iomf.Or(iomf.Keyword(w1), iomf.Keyword(w2))
			hasRange = true
		case QNotAndRange:
			fk = iomf.And(iomf.Not(iomf.Keyword(w1)), iomf.Not(iomf.Keyword(w2)))
			ek = iomf.And(iomf.Not(iomf.Keyword(w1)), iomf.Not(iomf.Keyword(w2)))
			hasRange = true
		case QComplexRange:
			fk = iomf.Or(iomf.And(iomf.Not(iomf.Keyword(w1)), iomf.Keyword(w2)), iomf.Keyword(w3))
			ek = iomf.Or(iomf.And(iomf.Not(iomf.Keyword(w1)), iomf.Keyword(w2)), iomf.Keyword(w3))
			hasRange = true
		}

		if hasRange && span > 0 {
			startMax := span - width
			if startMax < 0 {
				startMax = 0
			}
			start := minTS + rng.Int63n(startMax+1)
			ts, te = start, start+width
		}

		out[i] = query.Q{LG: lid, HasRange: hasRange, TS: ts, TE: te, FK: fk, EK: ek}
	}
	return out
}
