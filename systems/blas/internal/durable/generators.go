package durable

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"

	"blas/alis"
	"blas/bhash"
	"blas/internal/diskindex"
	"blas/live"
	"blas/optimize"
	contract "canonicalingestion"
)

const generatorSchema = `CREATE TABLE IF NOT EXISTS blas.durable_generator_keywords(
 run_id text NOT NULL,lid bigint NOT NULL,kid bigint NOT NULL,keyword text COLLATE "C" NOT NULL,
 PRIMARY KEY(run_id,lid,kid),UNIQUE(run_id,lid,keyword));`

type generatorState struct {
	Key                                    string
	LID, FileID, InFile, Sequence, NextKID uint64
	LastTS                                 int64
}

func generatorIdentity(o live.Observation) (string, uint64) {
	b, _ := json.Marshal([3]string{o.Dataset, o.Site, o.Device})
	h := bhash.H(b)
	var lid uint64
	for _, v := range h[:8] {
		lid = lid<<8 | uint64(v)
	}
	return string(b), lid & math.MaxInt64
}

func (a Adapter) generatorLimit() int {
	if a.Options.MaxGenerators > 0 {
		return a.Options.MaxGenerators
	}
	return 512
}

func (w *writer) currentFileID() uint64 {
	if w.activeGenerator != nil {
		return w.activeGenerator.FileID
	}
	return w.state.Files
}
func (w *writer) currentLID() uint64 {
	if w.activeGenerator != nil {
		return w.activeGenerator.LID
	}
	return w.a.lid()
}
func (w *writer) dictionaryPopulation() uint64 {
	if w.activeGenerator != nil {
		return w.activeGenerator.NextKID
	}
	return w.state.NextKID
}

// Only small generator cursors live in the checkpoint. Entries and keyword
// history remain SQL-backed; no cache eviction introduces a native boundary.
func (a Adapter) applyGenerators(ctx context.Context, tx *sql.Tx, p contract.Progress, entries []contract.Entry) ([]string, []byte, error) {
	var st state
	if len(p.Checkpoint) > 0 {
		if err := json.Unmarshal(p.Checkpoint, &st); err != nil {
			return nil, nil, err
		}
	}
	if st.Records != p.Committed || len(st.Generators) > a.generatorLimit() {
		return nil, nil, errors.New("blas: invalid generator checkpoint")
	}
	if p.Committed > 0 && len(st.Generators) == 0 {
		return nil, nil, errors.New("blas: single-generator state requires BLAS-only rebuild")
	}
	// Bounded backing capacity keeps generator pointers stable during this Apply.
	gens := make([]generatorState, len(st.Generators), a.generatorLimit())
	copy(gens, st.Generators)
	st.Generators = gens
	byLID := make(map[uint64]int, len(gens))
	checkpointBytes := len(st.PendingFiles)*24 + 256
	for i, g := range gens {
		if _, exists := byLID[g.LID]; exists || g.InFile > uint64(a.Options.EntriesPerFile) {
			return nil, nil, errors.New("blas: invalid generator identity/state")
		}
		var tuple [3]string
		if err := json.Unmarshal([]byte(g.Key), &tuple); err != nil {
			return nil, nil, errors.New("blas: invalid generator tuple")
		}
		key, lid := generatorIdentity(live.Observation{Dataset: tuple[0], Site: tuple[1], Device: tuple[2]})
		if g.Key != key || g.LID != lid || tuple[0] == "" || tuple[1] == "" || tuple[2] == "" || (g.InFile > 0 && g.FileID >= st.Records) {
			return nil, nil, errors.New("blas: generator checkpoint identity mismatch")
		}
		checkpointBytes += len(g.Key) + 256
		byLID[g.LID] = i
	}
	w := writer{a: a, tx: tx, ctx: ctx, state: st, pages: diskindex.Store{Q: tx, Scope: a.Options.RunID, Limits: a.Options.Index}}
	w.beginPositionSample()
	if a.Execution != nil {
		batch, err := diskindex.NewWriteBatch(tx, a.Execution.BatchMaxRecords, a.Execution.BatchMaxBytes, a.Metrics)
		if err != nil {
			return nil, nil, err
		}
		w.pages.Batch = batch
		w.groupIndexWrites = a.Execution.StorageAccessOptimizations && !a.DisableCompatibleIndexBatching
		batch.HoldFinish = w.groupIndexWrites
	}
	type prepared struct {
		O   live.Observation
		Key string
		LID uint64
	}
	preparedRecords, err := contract.PrepareSlice(ctx, a.Execution, a.Metrics, entries, func(e contract.Entry) int { return len(e.Record)*4 + 2048 }, func(_ context.Context, e contract.Entry) (prepared, int, error) {
		if len(e.Record) > a.Options.ObjectBytes {
			return prepared{}, 0, errors.New("blas: object budget exceeded")
		}
		var o live.Observation
		if err := json.Unmarshal(e.Record, &o); err != nil {
			return prepared{}, 0, err
		}
		if err := o.Validate(); err != nil {
			return prepared{}, 0, err
		}
		key, lid := generatorIdentity(o)
		return prepared{o, key, lid}, len(e.Record) + len(key) + 512, nil
	})
	if err != nil {
		return nil, nil, err
	}
	known := map[string]bool{}
	knownBytes := 0
	if a.Metrics != nil {
		defer a.Metrics.TrackCache(0, 0)
	}
	remember := func(key string) {
		limit, bytes := 4096, 8<<20
		if a.Execution != nil {
			limit, bytes = a.Execution.CacheMaxEntries, a.Execution.CacheMaxBytes
		}
		if !known[key] && len(known) < limit && len(key)+64 <= bytes-knownBytes {
			known[key] = true
			knownBytes += len(key) + 64
			if a.Metrics != nil {
				a.Metrics.TrackCache(int64(len(known)), int64(knownBytes))
			}
		}
	}
	var prefetch *generatorKeywordPrefetch
	if !a.DisableKeywordPrefetch {
		words, lids := make([][]string, len(preparedRecords)), make([]uint64, len(preparedRecords))
		for i, r := range preparedRecords {
			words[i], lids[i] = r.O.Keywords(), r.LID
		}
		if prefetch, err = a.prefetchGeneratorKeywords(ctx, tx, words, lids); err != nil {
			return nil, nil, err
		}
	}
	ids := make([]string, len(entries))
	for i, e := range entries {
		if e.Ordinal != p.Committed+uint64(i)+1 {
			return nil, nil, errors.New("blas: noncontiguous canonical ordinal")
		}
		r := preparedRecords[i]
		idx, exists := byLID[r.LID]
		if !exists {
			if len(w.state.Generators) >= a.generatorLimit() {
				return nil, nil, errors.New("blas: generator limit reached; no files were artificially closed")
			}
			checkpointBytes += len(r.Key) + 256
			if checkpointBytes > a.Options.ObjectBytes {
				return nil, nil, errors.New("blas: generator metadata byte bound reached")
			}
			idx = len(w.state.Generators)
			byLID[r.LID] = idx
			w.state.Generators = append(w.state.Generators, generatorState{Key: r.Key, LID: r.LID})
		}
		g := &w.state.Generators[idx]
		if g.Key != r.Key {
			return nil, nil, errors.New("blas: generator LID collision")
		}
		w.activeGenerator = g
		w.state.InFile = g.InFile
		ts := r.O.Timestamp.UnixMilli()
		if g.InFile > 0 && ts < g.LastTS {
			if a.Metrics != nil {
				a.Metrics.Access.TimestampReversalClosures.Add(1)
			}
			if err := w.closeFile(); err != nil {
				return nil, nil, err
			}
		}
		if g.InFile == 0 {
			g.FileID = e.Ordinal - 1
		} // stable logical opening ordinal, not scheduling
		for _, word := range r.O.Keywords() {
			global := "global/" + word
			if !known[global] {
				exists, prefetched := false, false
				if prefetch != nil {
					exists, prefetched = prefetch.global[word]
				}
				if !prefetched {
					// The query must see rows this batch buffered, so flush first.
					if err := w.flushObjects(); err != nil {
						return nil, nil, err
					}
					if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM blas.durable_keywords WHERE run_id=$1 AND keyword=$2)`, a.Options.RunID, word).Scan(&exists); err != nil {
						return nil, nil, err
					}
				}
				if !exists {
					w.keywordRows = append(w.keywordRows, []any{a.Options.RunID, int64(w.state.NextKID), word})
					w.state.NextKID++
				}
				if prefetched {
					prefetch.global[word] = true
				}
				remember(global)
			}
			key := fmt.Sprint(g.LID) + "/" + word
			if !known[key] {
				local := generatorKeyword{g.LID, word}
				exists, prefetched := false, false
				if prefetch != nil {
					exists, prefetched = prefetch.local[local]
				}
				if !prefetched {
					if err := w.flushObjects(); err != nil {
						return nil, nil, err
					}
					if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM blas.durable_generator_keywords WHERE run_id=$1 AND lid=$2 AND keyword=$3)`, a.Options.RunID, int64(g.LID), word).Scan(&exists); err != nil {
						return nil, nil, err
					}
				}
				if !exists {
					w.generatorKeywordRows = append(w.generatorKeywordRows, []any{a.Options.RunID, int64(g.LID), int64(g.NextKID), word})
					g.NextKID++
				}
				if prefetched {
					prefetch.local[local] = true
				}
				remember(key)
			}
		}
		words := r.O.Keywords()
		object := alis.EncodeLogEntry(alis.LogEntry{ESN: g.InFile, TS: ts, EW: words, V: e.Record})
		if len(object) > a.Options.ObjectBytes {
			return nil, nil, errors.New("blas: encoded entry exceeds budget")
		}
		if err := w.object("entry", g.FileID, g.InFile, object, words, []string{fmt.Sprint(g.LID)}, ts, ts); err != nil {
			return nil, nil, err
		}
		ids[i] = fmt.Sprintf("%d/%d/%d", g.LID, g.FileID, g.InFile)
		g.InFile++
		g.LastTS = ts
		w.state.InFile = g.InFile
		w.state.Records++
		if g.InFile == uint64(a.Options.EntriesPerFile) {
			if err := w.closeFile(); err != nil {
				return nil, nil, err
			}
		}
	}
	if err := w.flushObjects(); err != nil {
		return nil, nil, err
	}
	if w.state.Records == p.Target {
		order := make([]int, len(w.state.Generators))
		for i := range order {
			order[i] = i
		}
		sort.Slice(order, func(i, j int) bool { return w.state.Generators[order[i]].LID < w.state.Generators[order[j]].LID })
		for _, i := range order {
			w.activeGenerator = &w.state.Generators[i]
			w.state.InFile = w.activeGenerator.InFile
			if err := w.closeFile(); err != nil {
				return nil, nil, err
			}
		}
		w.activeGenerator = nil
		if err := w.closeBlock(); err != nil {
			return nil, nil, err
		}
	}
	w.activeGenerator = nil
	w.state.InFile = 0
	if err := w.flushObjects(); err != nil {
		return nil, nil, err
	}
	if w.pages.Batch != nil {
		if err := w.pages.Batch.Flush(ctx); err != nil {
			return nil, nil, err
		}
	}
	// Sorting checkpoint metadata is independent of file membership/closure order.
	sort.Slice(w.state.Generators, func(i, j int) bool { return w.state.Generators[i].LID < w.state.Generators[j].LID })
	cp, err := json.Marshal(w.state)
	if len(cp) > a.Options.ObjectBytes {
		return nil, nil, errors.New("blas: generator checkpoint budget exceeded")
	}
	w.samplePosition(w.state.Blocks, 0, false)
	return ids, cp, err
}

func (w *writer) generatorDictionary(visit func(uint64, string) error) error {
	g := w.activeGenerator
	limit := 1000
	if w.a.Execution != nil {
		limit = w.a.Execution.BatchMaxRecords
	}
	for next := uint64(0); next < g.NextKID; {
		rows, err := w.tx.QueryContext(w.ctx, `SELECT kid,keyword FROM blas.durable_generator_keywords WHERE run_id=$1 AND lid=$2 AND kid >= $3 ORDER BY kid LIMIT $4`, w.a.Options.RunID, int64(g.LID), int64(next), limit)
		if err != nil {
			return err
		}
		type word struct {
			kid   uint64
			value string
		}
		var words []word
		used := 0
		for rows.Next() {
			var v word
			if err = rows.Scan(&v.kid, &v.value); err != nil {
				break
			}
			used += len(v.value) + 64
			if used > w.a.Options.ObjectBytes {
				err = errors.New("blas: generator dictionary batch budget exceeded")
				break
			}
			words = append(words, v)
		}
		readErr := rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if readErr != nil {
			return readErr
		}
		if len(words) == 0 {
			return errors.New("blas: missing generator keyword prefix")
		}
		for _, v := range words {
			if v.kid != next || v.kid >= g.NextKID {
				return errors.New("blas: generator keyword gap")
			}
			if err := visit(v.kid, v.value); err != nil {
				return err
			}
			next++
		}
	}
	return nil
}

func (w *writer) pendingFileMetadata() ([]alis.FileMetadata, error) {
	if len(w.state.PendingFiles) != int(w.state.InBlock) || len(w.state.PendingFiles) > w.a.Options.FilesPerBlock {
		return nil, errors.New("blas: invalid pending file set")
	}
	var metas []alis.FileMetadata
	used := 0
	for _, fid := range w.state.PendingFiles {
		var raw []byte
		var size int
		if err := w.tx.QueryRowContext(w.ctx, `SELECT octet_length(metadata),CASE WHEN octet_length(metadata)<=$3 THEN metadata ELSE NULL END FROM blas.durable_files WHERE run_id=$1 AND fid=$2`, w.a.Options.RunID, int64(fid), w.a.Options.ObjectBytes).Scan(&size, &raw); err != nil {
			return nil, err
		}
		used += size*16 + 256
		if size > w.a.Options.ObjectBytes || used > w.a.Options.QueryBytes {
			return nil, errors.New("blas: block metadata budget exceeded")
		}
		var m alis.FileMetadata
		if err := decode(raw, &m); err != nil {
			return nil, err
		}
		metas = append(metas, m)
	}
	sort.Slice(metas, func(i, j int) bool {
		if metas[i].LID != metas[j].LID {
			return metas[i].LID < metas[j].LID
		}
		return metas[i].FID < metas[j].FID
	})
	return metas, nil
}

func (w *writer) objectLIDs(kind string, owner uint64) ([]string, error) {
	return w.objectLIDsThrough(kind, owner, math.MaxInt64)
}
func (w *writer) objectLIDsThrough(kind string, owner, through uint64) ([]string, error) {
	rows, err := w.tx.QueryContext(w.ctx, `SELECT DISTINCT v.lid::bigint AS lid FROM blas.durable_objects o CROSS JOIN LATERAL jsonb_array_elements_text(o.lids) AS v(lid) WHERE run_id=$1 AND kind=$2 AND owner=$3 AND oid<=$4 ORDER BY lid`, w.a.Options.RunID, kind, int64(owner), int64(through))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var lids []string
	for rows.Next() {
		var lid uint64
		if err := rows.Scan(&lid); err != nil {
			return nil, err
		}
		if len(lids) >= w.a.generatorLimit() {
			return nil, errors.New("blas: generator universe exceeds bound")
		}
		lids = append(lids, fmt.Sprint(lid))
	}
	return lids, rows.Err()
}

func (w *writer) generatorBitmap(tree, kind string, owner, n uint64) (bhash.Digest, error) {
	lids, err := w.objectLIDs(kind, owner)
	if err != nil {
		return bhash.Digest{}, err
	}
	b, err := diskindex.NewBuilder(w.pages, tree, "iimt", w.a.Options.Fanout, n)
	if err != nil {
		return bhash.Digest{}, err
	}
	for _, text := range lids {
		var lid uint64
		if _, err := fmt.Sscan(text, &lid); err != nil {
			return bhash.Digest{}, err
		}
		bm, err := w.selectBitmap(kind, owner, n, "lids", text, 0)
		if err != nil {
			return bhash.Digest{}, err
		}
		if err := b.AddBitmap(w.ctx, alis.LIDKey(lid), 0, bm); err != nil {
			return bhash.Digest{}, err
		}
	}
	m, err := b.Finish(w.ctx)
	return m.Root, err
}

func (w *writer) aggregateGenerators(base string, segment, height uint64) (bhash.Digest, error) {
	lids, err := w.objectLIDs("block", segment)
	if err != nil {
		return bhash.Digest{}, err
	}
	extended, err := diskindex.NewBuilder(w.pages, base+"/extended-lg", "aggregate", w.a.Options.Fanout, 0)
	if err != nil {
		return bhash.Digest{}, err
	}
	for _, text := range lids {
		var lid uint64
		fmt.Sscan(text, &lid)
		gen, err := diskindex.NewBuilder(w.pages, base+"/generator/"+text, "aggregate", w.a.Options.Fanout, 0)
		if err != nil {
			return bhash.Digest{}, err
		}
		for h := segment * w.a.Options.SegmentSize; h <= height; h++ {
			for next := uint64(0); ; {
				limit := 1000
				if w.a.Execution != nil {
					limit = w.a.Execution.BatchMaxRecords
				}
				rows, err := w.tx.QueryContext(w.ctx, `SELECT oid,octet_length(object),CASE WHEN octet_length(object)<=$5 THEN object ELSE NULL END FROM blas.durable_objects WHERE run_id=$1 AND kind='tx' AND owner=$2 AND oid >= $3 AND lids ? $4 ORDER BY oid LIMIT $6`, w.a.Options.RunID, int64(h), int64(next), text, w.a.Options.ObjectBytes, limit)
				if err != nil {
					return bhash.Digest{}, err
				}
				type item struct {
					id     uint64
					object []byte
				}
				var items []item
				used := 0
				for rows.Next() {
					var v item
					var size int
					if err = rows.Scan(&v.id, &size, &v.object); err != nil {
						break
					}
					used += size + 128
					if size > w.a.Options.ObjectBytes || used > w.a.Options.QueryBytes {
						err = errors.New("blas: aggregation batch exceeds budget")
						break
					}
					items = append(items, v)
				}
				readErr := rows.Err()
				rows.Close()
				if err != nil {
					return bhash.Digest{}, err
				}
				if readErr != nil {
					return bhash.Digest{}, readErr
				}
				if len(items) == 0 {
					break
				}
				for _, v := range items {
					if err := gen.AddHash(w.ctx, optimize.GeneratorTxLeafHash(h, v.id, bhash.H(v.object))); err != nil {
						return bhash.Digest{}, err
					}
					next = v.id + 1
				}
			}
		}
		meta, err := gen.Finish(w.ctx)
		if err != nil {
			return bhash.Digest{}, err
		}
		bm, err := w.selectBitmap("block", segment, height%w.a.Options.SegmentSize+1, "lids", text, 0)
		if err != nil {
			return bhash.Digest{}, err
		}
		if err := extended.AddHash(w.ctx, optimize.ExtendedIIMTLeafHash(alis.LIDKey(lid), bm, meta.Root)); err != nil {
			return bhash.Digest{}, err
		}
	}
	m, err := extended.Finish(w.ctx)
	return m.Root, err
}
