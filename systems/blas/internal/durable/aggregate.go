package durable

import (
	"errors"
	"fmt"

	"blas/alis"
	"blas/query"
)

func (r *Reader) Stage2Aggregated(q query.QOn, s1 query.Stage1Result) (query.Stage2Result, map[uint64]query.AggregatedLGBinding, error) {
	if !r.Adapter.Options.Aggregation {
		return query.Stage2Result{}, nil, errors.New("blas durable: aggregation was not enabled during construction")
	}
	out, err := r.Stage2(q, s1.CandidateHeights)
	if err != nil {
		return out, nil, err
	}
	bindings := map[uint64]query.AggregatedLGBinding{}
	size := r.Adapter.Options.SegmentSize
	for _, segment := range s1.Segments {
		if !segment.VO.LGPresent {
			continue
		}
		idx := segment.SegmentBase / size
		base := fmt.Sprintf("segment/%d/%d", idx, segment.AnchorHeight)
		genName := base + "/generator"
		if r.Adapter.Options.GeneratorMode != "" {
			genName += fmt.Sprintf("/%d", q.LG)
		}
		gen, e := r.open(genName)
		if e != nil {
			return out, nil, e
		}
		ext, e := r.open(base + "/extended-lg")
		if e != nil {
			return out, nil, e
		}
		pos := uint64(0)
		if r.Adapter.Options.GeneratorMode != "" {
			lg, e := r.open(base + "/lg")
			if e != nil {
				return out, nil, e
			}
			var present bool
			pos, present = lg.KID(alis.LIDKey(q.LG))
			if !present {
				return out, nil, errors.New("missing generator in aggregation universe")
			}
		}
		proof, ok := ext.AggregateProof(pos)
		if !ok {
			return out, nil, ext.Err()
		}
		bindings[idx] = query.AggregatedLGBinding{SegmentIndex: idx, LID: q.LG, Bitmap: segment.VO.LGProof.Bitmap, AggRoot: gen.Meta.Root, LeafProof: proof}
	}
	for i := range out.Blocks {
		block := &out.Blocks[i]
		idx := block.Height / size
		var anchor uint64
		for _, s := range s1.Segments {
			if s.SegmentBase == idx*size {
				anchor = s.AnchorHeight
				break
			}
		}
		genName := fmt.Sprintf("segment/%d/%d/generator", idx, anchor)
		if r.Adapter.Options.GeneratorMode != "" {
			genName += fmt.Sprintf("/%d", q.LG)
		}
		gen, e := r.open(genName)
		if e != nil {
			return out, nil, e
		}
		r.NativeReads++
		var offset uint64
		if r.Adapter.Options.GeneratorMode == "" {
			e = r.pageStore().Row(r.Ctx, `SELECT count(*) FROM blas.durable_objects WHERE run_id=$1 AND kind='tx' AND owner >= $2 AND owner < $3`, r.Adapter.Options.RunID, int64(idx*size), int64(block.Height)).Scan(&offset)
		}
		if e != nil {
			return out, nil, e
		}
		for _, p := range block.VO.ResultProofs {
			native, e := alis.DecodeTransaction(p.Object)
			if e != nil {
				return out, nil, e
			}
			// EncodeTransaction sorts FW for hashing. Aggregated evidence carries
			// the original transaction struct, whose FW retains generator order.
			r.NativeReads++
			var metadata []byte
			if e = r.pageStore().Row(r.Ctx, `SELECT metadata FROM blas.durable_files WHERE run_id=$1 AND fid=$2`, r.Adapter.Options.RunID, int64(native.FID)).Scan(&metadata); e != nil {
				return out, nil, e
			}
			if e = r.charge(len(metadata)); e != nil {
				return out, nil, e
			}
			var original alis.FileMetadata
			if e = decode(metadata, &original); e != nil {
				return out, nil, e
			}
			native.FW = original.FW
			position := offset + p.OID
			if r.Adapter.Options.GeneratorMode != "" {
				r.NativeReads++
				if e = r.pageStore().Row(r.Ctx, `SELECT count(*) FROM blas.durable_objects WHERE run_id=$1 AND kind='tx' AND owner >= $2 AND (owner < $3 OR (owner=$3 AND oid<$4)) AND lids ? $5`, r.Adapter.Options.RunID, int64(idx*size), int64(block.Height), int64(p.OID), fmt.Sprint(q.LG)).Scan(&position); e != nil {
					return out, nil, e
				}
			}
			proof, ok := gen.AggregateProof(position)
			if !ok {
				if gen.Err() != nil {
					return out, nil, gen.Err()
				}
				return out, nil, errors.New("missing generator aggregation proof")
			}
			block.VO.AggregatedResultTxs = append(block.VO.AggregatedResultTxs, alis.AggregatedTxProof{BlockHeight: block.Height, Transaction: native, Proof: proof})
		}
		block.VO.ResultProofs = nil
	}
	return out, bindings, r.Budget.Err
}
