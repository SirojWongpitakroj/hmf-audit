package benchmark

import (
	contract "canonicalingestion"
	"context"
	"errors"
	"fmt"

	"dhtbacked/dht"
	"dhtbacked/internal/hashing"
	"dhtbacked/mktree"
	"dhtbacked/producer"
)

// Bound the entire epoch's retained leaf digests to 128 KiB, independently of
// dataset size. Larger epochs keep the existing disk-backed implementation.
const bufferedEpochRecords uint64 = 4096

// Read only durable encrypted digests. Keeping this read preserves transaction
// provenance on rollback/restart; the optimization removes the temporary spool
// and its repeated reads, not the authoritative SQL recovery source.
func (a *IncrementalDHT) bufferedDigests(ctx context.Context, epoch, count uint64, counts [5]uint64) ([5][]hashing.Digest, error) {
	var leaves [5][]hashing.Digest
	if count == 0 || count > bufferedEpochRecords {
		return leaves, errors.New("dht publication: buffered epoch bound exceeded")
	}
	var total uint64
	for i, n := range counts {
		if n > count-total {
			return leaves, errors.New("dht publication: per-type counts differ")
		}
		total += n
		leaves[i] = make([]hashing.Digest, 0, n)
	}
	if total != count {
		return leaves, errors.New("dht publication: per-type counts differ")
	}
	batch := a.Options.proofLimits().RecordFetchBatch
	if batch < 1 {
		return leaves, errors.New("dht publication: positive digest fetch bound required")
	}
	total = 0
	var lastType string
	var lastIndex uint64
	for {
		limit := min(uint64(batch), count-total+1)
		query := `SELECT log_type,leaf_index,d FROM dhtbacked.records WHERE epoch=$1`
		args := []any{int64(epoch), int64(limit)}
		if total > 0 {
			query += ` AND (log_type,leaf_index)>($3,$4)`
			args = append(args, lastType, int64(lastIndex))
		}
		query += ` ORDER BY log_type,leaf_index LIMIT $2`
		rows, err := a.DB.QueryContext(ctx, query, args...)
		if err != nil {
			return leaves, err
		}
		var read uint64
		for rows.Next() {
			var typ string
			var index uint64
			var raw []byte
			if err = rows.Scan(&typ, &index, &raw); err != nil {
				break
			}
			i := -1
			for j, name := range producer.LogTypes {
				if name == typ {
					i = j
					break
				}
			}
			if i < 0 || len(raw) != 32 || index != uint64(len(leaves[i])) || index >= counts[i] || total >= count {
				err = errors.New("dht publication: persisted digest prefix differs")
				break
			}
			var digest hashing.Digest
			copy(digest[:], raw)
			leaves[i] = append(leaves[i], digest)
			total++
			read++
			lastType, lastIndex = typ, index
		}
		readErr := rows.Err()
		rows.Close()
		if err != nil {
			return leaves, err
		}
		if readErr != nil {
			return leaves, readErr
		}
		if read < limit {
			break
		}
	}
	for i, n := range counts {
		if uint64(len(leaves[i])) != n {
			return leaves, errors.New("dht publication: missing persisted digest")
		}
	}
	return leaves, nil
}

func (a *IncrementalDHT) publishBufferedEpoch(ctx context.Context, sink publicationSink, epoch, count, capacity uint64, counts [5]uint64) error {
	if capacity < 2 || count > capacity {
		return errors.New("dht publication: invalid epoch capacity")
	}
	leaves, err := a.bufferedDigests(ctx, epoch, count, counts)
	if err != nil {
		return err
	}
	cfg := epochConfig(capacity)
	roots := make([]hashing.Digest, len(cfg.LogTypes))
	guard := &guardedPublicationSink{base: sink}
	if a.Execution == nil {
		for i, typ := range cfg.LogTypes {
			roots[i], err = buildBufferedSparseTree(leaves[i], epoch, typ, cfg, count, contextProofSink{ctx, guard})
			if err != nil {
				return err
			}
		}
	} else {
		indices := make([]int, len(cfg.LogTypes))
		for i := range indices {
			indices[i] = i
		}
		// Account for owned reduction scratch, upper levels, micro-root input
		// and encoding overhead. The retained leaf buffer is fixed above.
		scratch := func(i int) int { return len(leaves[i])*64 + int((uint64(1)<<(cfg.Stride+1))*32) + 64<<10 }
		err = contract.Ordered(ctx, *a.Execution, a.Metrics, indices, scratch, func(ctx context.Context, i int) (hashing.Digest, int, error) {
			root, err := buildBufferedSparseTree(leaves[i], epoch, cfg.LogTypes[i], cfg, count, contextProofSink{ctx, guard})
			return root, 32, err
		}, func(i int, root hashing.Digest) error { roots[i] = root; return nil })
		if err != nil {
			return err
		}
	}
	if err := guard.Flush(); err != nil {
		return err
	}
	return a.commitPublishedEpoch(ctx, epoch, count, capacity, mktree.RootOfRoots(roots))
}

// Match the file-backed reduction exactly, including sparse dummy siblings,
// parent indices, certificate order, micro-root ranges and commit sequence.
func buildBufferedSparseTree(leaves []hashing.Digest, epoch uint64, typ string, cfg mktree.Config, seq uint64, sink fairProofSink) (hashing.Digest, error) {
	levels := fairTreeLevels(cfg.EpochCapacity)
	if uint64(len(leaves)) > bufferedEpochRecords || uint64(len(leaves)) > cfg.EpochCapacity || cfg.Stride > levels || cfg.Window == 0 {
		return hashing.Digest{}, fmt.Errorf("dht publication: invalid buffered tree geometry")
	}
	treeID := dht.TreeID(epoch, typ)
	dummy := fairDummyNodes(levels)
	for first, window := uint64(0), uint64(0); first < uint64(len(leaves)); first, window = first+cfg.Window, window+1 {
		last := min(first+cfg.Window, uint64(len(leaves)))
		record := dht.MicroRootRecord{TreeID: treeID, WindowID: window, IMin: first, IMax: last - 1, MicroRoot: mktree.MicroRoot(leaves[first:last]), Seq: seq}
		if err := sink.Put(dht.KeyM(treeID, window), record.Serialize()); err != nil {
			return hashing.Digest{}, err
		}
	}
	current := append([]hashing.Digest(nil), leaves...)
	for level := levels; level > cfg.Stride; level-- {
		active := uint64(len(current))
		for i := uint64(0); i < active; i += 2 {
			left, right := current[i], dummy[level]
			if err := sink.Put(dht.KeyP(treeID, level, i), (dht.ParentNode{TreeID: treeID, J: level, K: i, Node: left, CommitSeq: seq}).Serialize()); err != nil {
				return hashing.Digest{}, err
			}
			if i+1 < active {
				right = current[i+1]
			}
			if i+1 < uint64(1)<<level {
				if err := sink.Put(dht.KeyP(treeID, level, i+1), (dht.ParentNode{TreeID: treeID, J: level, K: i + 1, Node: right, CommitSeq: seq}).Serialize()); err != nil {
					return hashing.Digest{}, err
				}
			}
			current[i/2] = mktree.Internal(left, right)
		}
		current = current[:(active+1)/2]
	}
	upper := make([][]hashing.Digest, cfg.Stride+1)
	upper[cfg.Stride] = make([]hashing.Digest, uint64(1)<<cfg.Stride)
	for i := range upper[cfg.Stride] {
		upper[cfg.Stride][i] = dummy[cfg.Stride]
	}
	copy(upper[cfg.Stride], current)
	for level := cfg.Stride; level > 0; level-- {
		upper[level-1] = make([]hashing.Digest, len(upper[level])/2)
		for i := range upper[level-1] {
			upper[level-1][i] = mktree.Internal(upper[level][i*2], upper[level][i*2+1])
		}
	}
	return publishFairUpperTree(upper, uint64(len(current)), uint64(len(leaves)), epoch, typ, cfg, seq, sink)
}
