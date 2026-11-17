package durable

import (
	contract "canonicalingestion"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"blas/alis"
	"blas/chain"
	"blas/internal/diskindex"
	"blas/omt"
	"blas/optimize"
	"blas/query"
)

// Reader is one query's workspace. Cache is reusable and explicitly bounded;
// Budget and all returned proof/bitmap state belong to this request only.
type Reader struct {
	BlockInputs                                                         *contract.AuditReadCache[uint64, [][]byte]
	TransactionQueries, TransactionRowsLoaded, TransactionLogicalVisits uint64
	Adapter                                                             Adapter
	Cache                                                               *diskindex.Cache
	Budget                                                              *diskindex.Budget
	Ctx                                                                 context.Context
	NativeReads                                                         uint64
	IO                                                                  diskindex.IOTiming
	FixedBlockCount                                                     *uint64
}

func (a Adapter) NewReader(ctx context.Context, cache *diskindex.Cache) *Reader {
	r := &Reader{Adapter: a, Cache: cache, Budget: &diskindex.Budget{MaxBytes: a.Options.QueryBytes}, Ctx: ctx}
	if c, ok := ctx.Value(blockInputsKey{}).(*contract.AuditReadCache[uint64, [][]byte]); ok {
		r.BlockInputs = c
	}
	return r
}
func (r *Reader) pageStore() diskindex.Store {
	return diskindex.Store{Q: r.Adapter.DB, Scope: r.Adapter.Options.RunID, Limits: r.Adapter.Options.Index, Cache: r.Cache, Timing: &r.IO}
}
func (r *Reader) open(tree string) (*diskindex.Reader, error) {
	return diskindex.Open(r.Ctx, r.pageStore(), tree, r.Budget)
}
func (r *Reader) charge(n int) error {
	if n < 0 || n > r.Budget.MaxBytes-r.Budget.ReadBytes {
		r.Budget.Err = errors.New("blas durable: query byte limit exceeded")
	}
	if r.Budget.Err != nil {
		return r.Budget.Err
	}
	r.Budget.ReadBytes += n
	return nil
}

func (r *Reader) workspace(n uint64) error {
	if n > uint64(r.Adapter.Options.QueryBytes)/128 {
		return errors.New("blas durable: native result workspace exceeds query budget")
	}
	return r.charge(int(n * 128))
}
func (r *Reader) Header(height uint64) (chain.BlockHeader, error) {
	key := fmt.Sprintf("header/%s/%d", r.Adapter.Options.RunID, height)
	if r.Cache != nil {
		if b, ok := r.Cache.GetValue(key); ok {
			var h chain.BlockHeader
			err := decode(b, &h)
			return h, err
		}
	}
	r.NativeReads++
	var b []byte
	err := r.pageStore().Row(r.Ctx, `SELECT payload FROM blas.durable_headers WHERE run_id=$1 AND height=$2`, r.Adapter.Options.RunID, int64(height)).Scan(&b)
	if err != nil {
		return chain.BlockHeader{}, err
	}
	if err = r.charge(len(b)); err != nil {
		return chain.BlockHeader{}, err
	}
	var h chain.BlockHeader
	err = decode(b, &h)
	if err == nil && r.Cache != nil {
		r.Cache.PutValue(key, b)
	}
	return h, err
}
func (r *Reader) BlockCount() (uint64, error) {
	if r.FixedBlockCount != nil {
		return *r.FixedBlockCount, nil
	}
	r.NativeReads++
	var n uint64
	err := r.pageStore().Row(r.Ctx, `SELECT COALESCE(max(height)+1,0) FROM blas.durable_headers WHERE run_id=$1`, r.Adapter.Options.RunID).Scan(&n)
	return n, err
}
func (r *Reader) Stage1(q query.QOn, scan bool) (query.Stage1Result, error) {
	count, err := r.BlockCount()
	if err != nil {
		return query.Stage1Result{}, err
	}
	size := r.Adapter.Options.SegmentSize
	segments := (count + size - 1) / size
	var indices []uint64
	for i := segments; i > 0; i-- {
		if uint64(len(indices)) >= uint64(r.Adapter.Options.QueryBytes)/128 {
			return query.Stage1Result{}, errors.New("BLAS segment selection exceeds configured query workspace")
		}
		idx := i - 1
		anchor := min((idx+1)*size, count) - 1
		if scan && q.HasRange {
			h, e := r.Header(anchor)
			if e != nil {
				return query.Stage1Result{}, e
			}
			if h.BlockTsMin <= q.TE && h.BlockTsMax >= q.TS {
				indices = append(indices, idx)
			}
			if h.PastTsMax < q.TS {
				break
			}
		} else {
			indices = append(indices, idx)
		}
	}
	var out query.Stage1Result
	for i := len(indices) - 1; i >= 0; i-- {
		idx := indices[i]
		anchor := min((idx+1)*size, count) - 1
		base := fmt.Sprintf("segment/%d/%d", idx, anchor)
		lg, e := r.open(base + "/lg")
		if e != nil {
			return out, e
		}
		fk, e := r.open(base + "/fk")
		if e != nil {
			return out, e
		}
		mn, e := r.open(base + "/min")
		if e != nil {
			return out, e
		}
		mx, e := r.open(base + "/max")
		if e != nil {
			return out, e
		}
		population := anchor - idx*size + 1
		if e = r.workspace(population); e != nil {
			return out, e
		}
		for _, tree := range []*diskindex.Reader{lg, fk, mn, mx} {
			if tree.Meta.N != population {
				return out, errors.New("BLAS segment metadata population differs")
			}
		}
		vo, e := alis.ProcessBlockLevelReaders(alis.BlockReaders{LG: lg, FK: fk, FileTsMin: mn, FileTsMax: mx}, population, alis.BlockLevelQuery{LG: q.LG, FK: q.FK, HasRange: q.HasRange, TS: q.TS, TE: q.TE})
		if e != nil {
			return out, e
		}
		for _, tree := range []*diskindex.Reader{lg, fk, mn, mx} {
			if tree.Err() != nil {
				return out, tree.Err()
			}
		}
		out.Segments = append(out.Segments, query.SegmentBlockResult{SegmentBase: idx * size, AnchorHeight: anchor, VO: vo})
		for _, h := range vo.CandidateHeights {
			out.CandidateHeights = append(out.CandidateHeights, idx*size+h)
		}
	}
	return out, r.Budget.Err
}
func (r *Reader) transactions(height uint64) ([]alis.Transaction, error) {
	if contract.AuditReadsFrom(r.Ctx).Legacy {
		return r.transactionsLegacy(height)
	}
	h, err := r.Header(height)
	if err != nil {
		return nil, err
	}
	if h.TxNum > uint64(r.Adapter.Options.QueryBytes)/128 {
		return nil, fmt.Errorf("BLAS transaction workspace exceeds configured bound")
	}
	load := func() ([][]byte, int, error) {
		var objects [][]byte
		used := 0
		policy := contract.AuditReadsFrom(r.Ctx)
		for uint64(len(objects)) < h.TxNum {
			start := time.Now()
			r.IO.Statements++
			r.NativeReads++
			r.TransactionQueries++
			rows, e := r.Adapter.DB.QueryContext(r.Ctx, `SELECT oid,octet_length(object),CASE WHEN octet_length(object)<=$5 THEN object ELSE NULL END FROM blas.durable_objects WHERE run_id=$1 AND kind='tx' AND owner=$2 AND oid>=$3 AND oid<$4 ORDER BY oid LIMIT $6`, r.Adapter.Options.RunID, int64(height), int64(len(objects)), int64(h.TxNum), r.Adapter.Options.ObjectBytes, policy.BatchRows)
			if e != nil {
				r.IO.Read += time.Since(start)
				return nil, 0, e
			}
			count := 0
			for rows.Next() {
				var oid uint64
				var size int
				var b []byte
				if e = rows.Scan(&oid, &size, &b); e != nil {
					break
				}
				r.IO.Rows++
				r.IO.Bytes += uint64(len(b))
				r.TransactionRowsLoaded++
				if oid != uint64(len(objects)) || size > r.Adapter.Options.ObjectBytes || size < 0 || len(b) != size || size > r.Adapter.Options.QueryBytes-used {
					e = fmt.Errorf("BLAS transaction membership/order/byte bound differs")
					break
				}
				used += size
				objects = append(objects, b)
				count++
			}
			rowErr := rows.Err()
			rows.Close()
			r.IO.Read += time.Since(start)
			if e != nil {
				return nil, 0, e
			}
			if rowErr != nil {
				return nil, 0, rowErr
			}
			if count == 0 {
				return nil, 0, fmt.Errorf("BLAS transaction missing from committed block")
			}
		}
		return objects, used + len(objects)*24 + 128, nil
	}
	var objects [][]byte
	if r.BlockInputs != nil {
		objects, _, err = r.BlockInputs.Get(r.Ctx, height, load)
	} else {
		objects, _, err = load()
	}
	if err != nil {
		return nil, err
	}
	if uint64(len(objects)) != h.TxNum {
		return nil, fmt.Errorf("BLAS cached transaction population differs")
	}
	txs := make([]alis.Transaction, 0, len(objects))
	for _, b := range objects {
		if err = r.charge(len(b)); err != nil {
			return nil, err
		}
		tx, e := alis.DecodeTransaction(b)
		if e != nil {
			return nil, e
		}
		txs = append(txs, tx)
	}
	r.TransactionLogicalVisits += uint64(len(txs))
	return txs, nil
}
func (r *Reader) transactionsLegacy(height uint64) ([]alis.Transaction, error) {
	h, err := r.Header(height)
	if err != nil {
		return nil, err
	}
	var txs []alis.Transaction
	for i := uint64(0); i < h.TxNum; i++ {
		r.TransactionQueries++
		r.NativeReads++
		var b []byte
		var size int
		err = r.pageStore().Row(r.Ctx, `SELECT octet_length(object),CASE WHEN octet_length(object)<=$4 THEN object ELSE NULL END FROM blas.durable_objects WHERE run_id=$1 AND kind='tx' AND owner=$2 AND oid=$3`, r.Adapter.Options.RunID, int64(height), int64(i), r.Adapter.Options.ObjectBytes).Scan(&size, &b)
		if err != nil {
			return nil, err
		}
		if size > r.Adapter.Options.ObjectBytes {
			return nil, errors.New("blas durable: stored transaction exceeds budget")
		}
		if err = r.charge(size); err != nil {
			return nil, err
		}
		native, e := alis.DecodeTransaction(b)
		if e != nil {
			return nil, e
		}
		txs = append(txs, native)
		r.TransactionRowsLoaded++
	}
	r.TransactionLogicalVisits += uint64(len(txs))
	return txs, nil
}
func (r *Reader) Stage2(q query.QOn, heights []uint64) (query.Stage2Result, error) {
	var out query.Stage2Result
	for _, height := range heights {
		base := fmt.Sprintf("block/%d", height)
		fk, err := r.open(base + "/fk")
		if err != nil {
			return out, err
		}
		om, err := r.open(base + "/omt")
		if err != nil {
			return out, err
		}
		mn, err := r.open(base + "/min")
		if err != nil {
			return out, err
		}
		mx, err := r.open(base + "/max")
		if err != nil {
			return out, err
		}
		txs, err := r.transactions(height)
		if err != nil {
			return out, err
		}
		if err = r.workspace(uint64(len(txs))); err != nil {
			return out, err
		}
		for _, tree := range []*diskindex.Reader{fk, mn, mx} {
			if tree.Meta.N != uint64(len(txs)) {
				return out, errors.New("BLAS transaction metadata population differs")
			}
		}
		vo, err := alis.ProcessTxLevelReaders(alis.TxReaders{IIMT: fk, OMT: om, RIMBTs: map[string]alis.RangeReader{"ts_min": mn, "ts_max": mx}}, txs, alis.TxQuery{LG: q.LG, FK: q.FK, HasRange: q.HasRange, TS: q.TS, TE: q.TE})
		if err != nil {
			return out, err
		}
		for _, tree := range []*diskindex.Reader{fk, om, mn, mx} {
			if tree.Err() != nil {
				return out, tree.Err()
			}
		}
		out.Blocks = append(out.Blocks, query.BlockTxResult{Height: height, VO: vo})
	}
	return out, r.Budget.Err
}
func (r *Reader) Entry(fid uint64, q alis.EntryQuery) (alis.EntryVO, error) {
	r.NativeReads++
	var n uint64
	err := r.pageStore().Row(r.Ctx, `SELECT entries FROM blas.durable_files WHERE run_id=$1 AND fid=$2`, r.Adapter.Options.RunID, int64(fid)).Scan(&n)
	if err != nil {
		return alis.EntryVO{}, err
	}
	base := fmt.Sprintf("file/%d", fid)
	ir, err := r.open(base + "/iimt")
	if err != nil {
		return alis.EntryVO{}, err
	}
	or, err := r.open(base + "/omt")
	if err != nil {
		return alis.EntryVO{}, err
	}
	timeRange := func(ts, te int64) ([]uint64, *omt.Proof, *omt.Proof, error) {
		bound := func(op string, value int64) (uint64, error) {
			r.NativeReads++
			var p sql.NullInt64
			err := r.pageStore().Row(r.Ctx, `SELECT min(oid) FROM blas.durable_objects WHERE run_id=$1 AND kind='entry' AND owner=$2 AND ts_min `+op+` $3`, r.Adapter.Options.RunID, int64(fid), value).Scan(&p)
			if err != nil {
				return 0, err
			}
			if !p.Valid {
				return n, nil
			}
			return uint64(p.Int64), nil
		}
		lo, e := bound(">=", ts)
		if e != nil {
			return nil, nil, nil, e
		}
		hi, e := bound(">", te)
		if e != nil {
			return nil, nil, nil, e
		}
		var ids []uint64
		if hi > lo {
			if e = r.charge(int(hi-lo) * 8); e != nil {
				return nil, nil, nil, e
			}
			for i := lo; i < hi; i++ {
				ids = append(ids, i)
			}
		}
		var left, right *omt.Proof
		if lo > 0 {
			p, ok := or.Prove(lo - 1)
			if !ok {
				return nil, nil, nil, errors.New("missing lower boundary")
			}
			left = &p
		}
		if hi < n {
			p, ok := or.Prove(hi)
			if !ok {
				return nil, nil, nil, errors.New("missing upper boundary")
			}
			right = &p
		}
		return ids, left, right, or.Err()
	}
	if err = r.workspace(n); err != nil {
		return alis.EntryVO{}, err
	}
	if ir.Meta.N != n || or.Meta.Leaves != n {
		return alis.EntryVO{}, errors.New("BLAS entry metadata population differs")
	}
	vo, err := alis.ProcessEntryReaders(alis.EntryReaders{Count: n, IIMT: ir, OMT: or, TimeRangeLookup: timeRange}, q)
	if err != nil {
		return alis.EntryVO{}, err
	}
	if ir.Err() != nil {
		return alis.EntryVO{}, ir.Err()
	}
	if or.Err() != nil {
		return alis.EntryVO{}, or.Err()
	}
	return vo, r.Budget.Err
}

// KeywordID seeks the public, append-only generator registry. It does not load
// every historical keyword just to apply the auditor's bm_EW coarse filter.
func (r *Reader) KeywordID(word string) (uint64, bool, error) {
	r.NativeReads++
	var n uint64
	err := r.pageStore().Row(r.Ctx, `SELECT kid FROM blas.durable_keywords WHERE run_id=$1 AND keyword=$2`, r.Adapter.Options.RunID, word).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return n, err == nil, err
}

func (r *Reader) GeneratorKeywordID(lid uint64, word string) (uint64, bool, error) {
	r.NativeReads++
	var kid uint64
	err := r.pageStore().Row(r.Ctx, `SELECT kid FROM blas.durable_generator_keywords WHERE run_id=$1 AND lid=$2 AND keyword=$3`, r.Adapter.Options.RunID, int64(lid), word).Scan(&kid)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return kid, err == nil, err
}

// HeaderTimesForSegment reads a single trusted anchor for the original
// backward-scan predicate; it is also useful for independently checking coverage.
func (r *Reader) HeaderTimesForSegment(index, count uint64) (optimize.HeaderTimes, error) {
	h, err := r.Header(min((index+1)*r.Adapter.Options.SegmentSize, count) - 1)
	return optimize.HeaderTimes{BlockTsMin: h.BlockTsMin, BlockTsMax: h.BlockTsMax, PastTsMax: h.PastTsMax}, err
}
