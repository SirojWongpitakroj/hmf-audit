// Package durable is BLAS's PostgreSQL-owned native storage path. The in-memory
// chain remains a small-fixture correctness oracle, not a production cache.
package durable

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"blas/alis"
	"blas/bhash"
	"blas/bitmap"
	"blas/chain"
	"blas/internal/diskindex"
	"blas/live"
	contract "canonicalingestion"
)

//go:embed schema.sql
var schema string

type Options struct {
	GeneratorMode                         string // empty preserves the original single-generator format
	MaxGenerators                         int    // execution bound; no eviction-induced file closures
	RunID                                 string
	EntriesPerFile, FilesPerBlock, Fanout int
	SegmentSize                           uint64
	Aggregation                           bool
	Index                                 diskindex.Limits
	ObjectBytes, QueryBytes               int
}

func (o Options) Validate() error {
	if o.GeneratorMode != "" && o.GeneratorMode != "device-v1" {
		return errors.New("blas durable: unknown generator mode")
	}
	if o.MaxGenerators < 0 || o.MaxGenerators > 4096 {
		return errors.New("blas: generator bound must be between 1 and 4096, or zero for default")
	}
	if o.RunID == "" || o.EntriesPerFile < 1 || o.FilesPerBlock < 1 || o.Fanout < 2 || o.SegmentSize < 1 || o.ObjectBytes < 1 || o.QueryBytes < 1 {
		return errors.New("blas durable: explicit geometry and working-set budgets required")
	}
	return o.Index.Validate()
}
func (o Options) ConfigHash() string {
	b, _ := json.Marshal(struct {
		Version                int
		RunID                  string
		Entries, Files, Fanout int
		Segment                uint64
		Aggregation            bool
	}{1, o.RunID, o.EntriesPerFile, o.FilesPerBlock, o.Fanout, o.SegmentSize, o.Aggregation})
	h := bhash.H(b)
	if o.GeneratorMode != "" {
		h = bhash.HashConcat(h[:], []byte("blas-generator-format-v2/"+o.GeneratorMode))
	}
	return hex.EncodeToString(h[:])
}

type Adapter struct {
	DB        *sql.DB
	Options   Options
	Execution *contract.Execution
	Metrics   *contract.IngestMetrics
	// Execution-only rollback switch; deliberately excluded from Options.ConfigHash.
	DisableLowRiskOptimizations    bool
	DisableCompatibleIndexBatching bool                        // execution-only reference/rollback switch
	DisableKeywordPrefetch         bool                        // execution-only: per-keyword generator lookups as before
	SegmentComputations            *diskindex.ComputationCache // optional, bounded pure-input memo
	CompactZeroBitmaps             bool                        // physical representation only; logical bitmap/hash unchanged
}
type state struct {
	Generators                      []generatorState `json:",omitempty"`
	PendingFiles                    []uint64         `json:",omitempty"`
	Records, Files, Blocks, NextKID uint64
	InFile, InBlock                 uint64
	LastTS                          int64
	PastMax                         int64
	HasPast                         bool
}

func (a Adapter) Migrate(ctx context.Context) error {
	if err := a.Options.Validate(); err != nil {
		return err
	}
	if _, err := a.DB.ExecContext(ctx, schema); err != nil {
		return err
	}
	if a.Options.GeneratorMode != "" {
		if _, err := a.DB.ExecContext(ctx, generatorSchema); err != nil {
			return err
		}
	}
	return (diskindex.Store{Q: a.DB, Scope: a.Options.RunID, Limits: a.Options.Index}).Migrate(ctx)
}
func (a Adapter) lid() uint64 {
	h := bhash.H([]byte(a.Options.RunID))
	var n uint64
	for _, x := range h[:8] {
		n = n<<8 | uint64(x)
	}
	return n & math.MaxInt64
}
func encode(v any) ([]byte, error) {
	var b bytes.Buffer
	err := gob.NewEncoder(&b).Encode(v)
	return b.Bytes(), err
}
func decode(b []byte, v any) error { return gob.NewDecoder(bytes.NewReader(b)).Decode(v) }

func (a Adapter) Apply(ctx context.Context, tx *sql.Tx, p contract.Progress, entries []contract.Entry) ([]string, []byte, error) {
	if a.Options.GeneratorMode != "" {
		return a.applyGenerators(ctx, tx, p, entries)
	}
	var st state
	if len(p.Checkpoint) > 0 {
		if err := json.Unmarshal(p.Checkpoint, &st); err != nil {
			return nil, nil, err
		}
	}
	if st.Records != p.Committed {
		return nil, nil, errors.New("blas durable: native count differs from canonical cursor")
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
		if w.groupIndexWrites {
			w.pages.Batch.HoldFinish = true
		}
	}
	type preparedRecord struct {
		Timestamp int64
		Keywords  []string
	}
	prepared, err := contract.PrepareSlice(ctx, a.Execution, a.Metrics, entries, func(e contract.Entry) int { return len(e.Record)*4 + (2 << 10) }, func(ctx context.Context, e contract.Entry) (preparedRecord, int, error) {
		if len(e.Record) > a.Options.ObjectBytes {
			return preparedRecord{}, 0, errors.New("blas durable: canonical object byte limit exceeded")
		}
		var o live.Observation
		if err := json.Unmarshal(e.Record, &o); err != nil {
			return preparedRecord{}, 0, err
		}
		if err := o.Validate(); err != nil {
			return preparedRecord{}, 0, err
		}
		return preparedRecord{o.Timestamp.UnixMilli(), o.Keywords()}, len(e.Record) + 512, nil
	})
	if err != nil {
		return nil, nil, err
	}
	// The memo covers only this bounded transaction, never historical vocabulary.
	known := map[string]bool{}
	if a.Metrics != nil {
		defer a.Metrics.TrackCache(0, 0)
	}
	if a.Execution != nil && a.Execution.CacheMaxEntries > 0 && a.Execution.CacheMaxBytes > 0 {
		var words []string
		used := 0
		for _, r := range prepared {
			for _, word := range r.Keywords {
				if _, ok := known[word]; !ok && len(known) < a.Execution.CacheMaxEntries && used+len(word)+64 <= a.Execution.CacheMaxBytes {
					known[word] = false
					words = append(words, word)
					used += len(word) + 64
				}
			}
		}
		if len(words) > 0 {
			a.Metrics.TrackCache(int64(len(known)), int64(used))
			rows, err := tx.QueryContext(ctx, `SELECT keyword FROM blas.durable_keywords WHERE run_id=$1 AND keyword=ANY($2::text[])`, a.Options.RunID, words)
			if err != nil {
				return nil, nil, err
			}
			for rows.Next() {
				var word string
				if err := rows.Scan(&word); err != nil {
					rows.Close()
					return nil, nil, err
				}
				known[word] = true
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, nil, err
			}
		}
	}
	ids := make([]string, len(entries))
	for i, e := range entries {
		if len(e.Record) > a.Options.ObjectBytes {
			return nil, nil, errors.New("blas durable: canonical object byte limit exceeded")
		}
		ts := prepared[i].Timestamp
		if w.state.InFile > 0 && (w.state.InFile >= uint64(a.Options.EntriesPerFile) || ts < w.state.LastTS) {
			if ts < w.state.LastTS && a.Metrics != nil {
				a.Metrics.Access.TimestampReversalClosures.Add(1)
			}
			if err := w.closeFile(); err != nil {
				return nil, nil, err
			}
		}
		keywords := prepared[i].Keywords
		for _, word := range keywords {
			if exists, cached := known[word]; cached {
				if !exists {
					w.keywordRows = append(w.keywordRows, []any{a.Options.RunID, int64(w.state.NextKID), word})
					w.state.NextKID++
					known[word] = true
				}
				continue
			}
			var kid int64
			err := tx.QueryRowContext(ctx, `INSERT INTO blas.durable_keywords(run_id,kid,keyword) VALUES($1,$2,$3) ON CONFLICT(run_id,keyword) DO NOTHING RETURNING kid`, a.Options.RunID, int64(w.state.NextKID), word).Scan(&kid)
			if err == nil {
				w.state.NextKID++
			} else if !errors.Is(err, sql.ErrNoRows) {
				return nil, nil, err
			}
		}
		entry := alis.LogEntry{ESN: w.state.InFile, TS: ts, EW: keywords, V: e.Record}
		object := alis.EncodeLogEntry(entry)
		if len(object) > a.Options.ObjectBytes {
			return nil, nil, errors.New("blas durable: encoded object byte limit exceeded")
		}
		if err := w.object("entry", w.state.Files, w.state.InFile, object, keywords, []string{fmt.Sprint(a.lid())}, ts, ts); err != nil {
			return nil, nil, err
		}
		ids[i] = fmt.Sprintf("%d/%d/%d", a.lid(), w.state.Files, w.state.InFile)
		w.state.InFile++
		w.state.Records++
		w.state.LastTS = ts
		if w.state.InFile == uint64(a.Options.EntriesPerFile) {
			if err := w.closeFile(); err != nil {
				return nil, nil, err
			}
		}
	}
	if err := w.flushObjects(); err != nil {
		return nil, nil, err
	}
	// Close native immutable units only at natural limits, timestamp reversal, or
	// an explicit dataset milestone. Transaction batches/restarts add no boundary.
	if w.state.Records == p.Target {
		if err := w.closeFile(); err != nil {
			return nil, nil, err
		}
		if err := w.closeBlock(); err != nil {
			return nil, nil, err
		}
	}
	if err := w.flushObjects(); err != nil {
		return nil, nil, err
	}
	// Flush before Consumer.Step can persist mappings and advance its cursor in
	// this same transaction. No required index row may remain only in RAM.
	if w.groupIndexWrites {
		if err := w.pages.Batch.Flush(ctx); err != nil {
			return nil, nil, err
		}
	}
	checkpoint, err := json.Marshal(w.state)
	if err == nil {
		w.samplePosition(w.state.Blocks, 0, false)
	}
	return ids, checkpoint, err
}

type writer struct {
	activeGenerator              *generatorState
	generatorKeywordRows         [][]any
	fileBounds                   *fileBounds
	summaries                    map[string]*metadataSummary
	dictionaryWords              []string
	dictionaryBytes              int
	a                            Adapter
	tx                           *sql.Tx
	ctx                          context.Context
	state                        state
	pages                        diskindex.Store
	objectRows, keywordRows      [][]any
	fileKeywords                 *fileKeywords
	transactionMetadata          []objectSummary
	transactionBytes             int
	fileInput                    *currentFileInput
	keywordInputs                map[string]diskindex.PreparedKeyword
	keywordInputBytes            int
	zeroBitmap                   *diskindex.PreparedBitmap
	positionStarted              time.Time
	positionRecords, positionSQL uint64
	positionHashes               diskindex.ComputationStats
	groupIndexWrites             bool
}

func (w *writer) flushObjects() error {
	maxRows, maxBytes := 1000, 8<<20
	if w.a.Execution != nil {
		maxRows = w.a.Execution.BatchMaxRecords
		maxBytes = w.a.Execution.BatchMaxBytes
	}
	if err := contract.InsertRows(w.ctx, w.tx, `INSERT INTO blas.durable_keywords(run_id,kid,keyword) VALUES `, "", w.keywordRows, maxRows, maxBytes, w.a.Metrics); err != nil {
		return err
	}
	w.keywordRows = nil
	if err := contract.InsertRows(w.ctx, w.tx, `INSERT INTO blas.durable_generator_keywords(run_id,lid,kid,keyword) VALUES `, "", w.generatorKeywordRows, maxRows, maxBytes, w.a.Metrics); err != nil {
		return err
	}
	w.generatorKeywordRows = nil
	var txRows, before uint64
	if w.a.Metrics != nil {
		before = w.a.Metrics.SQLBatches.Load()
		for _, row := range w.objectRows {
			if row[1] == "tx" {
				txRows++
			}
		}
	}
	if err := contract.InsertRows(w.ctx, w.tx, `INSERT INTO blas.durable_objects(run_id,kind,owner,oid,object,keywords,lids,ts_min,ts_max) VALUES `, "", w.objectRows, maxRows, maxBytes, w.a.Metrics); err != nil {
		return err
	}
	if txRows > 0 {
		w.a.Metrics.Access.TransactionObjectRows.Add(txRows)
		w.a.Metrics.Access.TransactionObjectInsertCalls.Add(w.a.Metrics.SQLBatches.Load() - before)
	}
	// Acknowledged within this transaction, never shared across commit/rollback.
	for _, row := range w.transactionMetadata {
		w.extendMetadata("tx", rowOwner(w.objectRows), row.oid, row.keywords, row.lids, row.min, row.max)
	}
	w.transactionMetadata = nil
	w.transactionBytes = 0
	w.objectRows = nil
	return nil
}

func (w *writer) object(kind string, owner, oid uint64, object []byte, keywords, lids []string, minTS, maxTS int64) error {
	if kind == "entry" && w.lowRiskEnabled() {
		w.rememberFileInput(owner, oid, object, keywords)
	}
	if w.accessOptimized() && kind == "entry" {
		if oid == 0 {
			w.fileBounds = &fileBounds{fid: owner}
		}
		if f := w.fileBounds; f != nil && f.fid == owner && f.count == oid {
			if oid == 0 {
				f.min = minTS
			}
			f.max = maxTS
			f.count++
		}
	}
	kw, err := json.Marshal(keywords)
	if err != nil {
		return err
	}
	lg, err := json.Marshal(lids)
	if err != nil {
		return err
	}
	row := []any{w.a.Options.RunID, kind, int64(owner), int64(oid), object, string(kw), string(lg), minTS, maxTS}
	if kind == "tx" && w.lowRiskEnabled() {
		return w.bufferTransaction(row, owner, oid, keywords, lids, minTS, maxTS)
	}
	w.objectRows = append(w.objectRows, row)
	// The nil-execution path is retained as the pre-optimization oracle.
	maxRows := 1000
	if w.a.Execution != nil {
		maxRows = w.a.Execution.BatchMaxRecords
	}
	if kind != "entry" || w.a.Execution == nil || len(w.objectRows) >= maxRows {
		if err := w.flushObjects(); err != nil {
			return err
		}
		w.extendMetadata(kind, owner, oid, keywords, lids, minTS, maxTS)
		return nil
	}
	return nil
}
func (w *writer) closeFile() error {
	if err := w.flushObjects(); err != nil {
		return err
	}
	if w.state.InFile == 0 {
		return nil
	}
	if w.a.Metrics != nil {
		w.a.Metrics.Access.FileClosures.Add(1)
		w.a.Metrics.Access.ClosedFileEntries.Add(w.state.InFile)
	}
	fid := w.currentFileID()
	compact := w.pages.CompactZeroBitmaps
	w.pages.CompactZeroBitmaps = w.a.CompactZeroBitmaps && w.a.Options.GeneratorMode != ""
	defer func() { w.pages.CompactZeroBitmaps = compact }()
	base := fmt.Sprintf("file/%d", fid)
	if w.accessOptimized() {
		previousHold := w.pages.Batch.HoldFinish
		w.pages.Batch.HoldFinish = true
		defer func() { w.pages.Batch.HoldFinish = previousHold }()
	}
	ir, err := w.buildBitmap(base+"/iimt", "entry", fid, w.state.InFile, "dictionary")
	if err != nil {
		return err
	}
	or, err := w.buildObjects(base+"/omt", "entry", fid, w.state.InFile)
	if err != nil {
		return err
	}
	var minTS, maxTS int64
	if w.accessOptimized() && !w.groupIndexWrites {
		if err = w.pages.Batch.Flush(w.ctx); err != nil {
			return err
		}
		w.pages.Batch.HoldFinish = false
	}
	if f := w.fileBounds; w.accessOptimized() && f != nil && f.fid == fid && f.count == w.state.InFile {
		minTS, maxTS = f.min, f.max
	} else if err = w.tx.QueryRowContext(w.ctx, `SELECT min(ts_min),max(ts_max) FROM blas.durable_objects WHERE run_id=$1 AND kind='entry' AND owner=$2`, w.a.Options.RunID, int64(fid)).Scan(&minTS, &maxTS); err != nil {
		return err
	}
	if (w.dictionaryPopulation()+7)/8 > uint64(w.a.Options.Index.BitmapBytes) {
		return errors.New("blas durable: file keyword bitmap exceeds configured working-set limit")
	}
	bm := bitmap.New(w.dictionaryPopulation())
	var fw []string
	var last int64 = -1
	usedBytes := 0
	if w.fileKeywords != nil && w.fileKeywords.owner == fid {
		bm = w.fileKeywords.bitmap
		fw = w.fileKeywords.words
		w.fileKeywords = nil
	} else if w.activeGenerator != nil {
		err = w.generatorDictionary(func(kid uint64, word string) error {
			var present bool
			if e := w.tx.QueryRowContext(w.ctx, `SELECT EXISTS(SELECT 1 FROM blas.durable_objects WHERE run_id=$1 AND kind='entry' AND owner=$2 AND keywords ? $3)`, w.a.Options.RunID, int64(fid), word).Scan(&present); e != nil {
				return e
			}
			if present {
				usedBytes += len(word) + 32
				if usedBytes > w.a.Options.ObjectBytes {
					return errors.New("blas: file metadata budget exceeded")
				}
				bm.Set(kid)
				fw = append(fw, word)
			}
			return nil
		})
		if err != nil {
			return err
		}
	} else {
		for {
			var kid int64
			var word string
			err = w.tx.QueryRowContext(w.ctx, `SELECT k.kid,k.keyword FROM blas.durable_keywords k WHERE k.run_id=$1 AND k.kid>$2 AND EXISTS(SELECT 1 FROM blas.durable_objects o WHERE o.run_id=k.run_id AND o.kind='entry' AND o.owner=$3 AND o.keywords ? k.keyword) ORDER BY k.kid LIMIT 1`, w.a.Options.RunID, last, int64(w.state.Files)).Scan(&kid, &word)
			if errors.Is(err, sql.ErrNoRows) {
				break
			}
			if err != nil {
				return err
			}
			usedBytes += len(word) + 32
			if usedBytes > w.a.Options.ObjectBytes {
				return errors.New("blas durable: current file metadata budget exceeded")
			}
			bm.Set(uint64(kid))
			fw = append(fw, word)
			last = kid
		}
	}
	meta := alis.FileMetadata{LID: w.currentLID(), FID: fid, TsMin: minTS, TsMax: maxTS, FW: fw, BmEW: bm, RHIIMT: ir, RHOMT: or}
	payload, err := encode(meta)
	if err != nil {
		return err
	}
	if len(payload) > w.a.Options.ObjectBytes {
		return errors.New("blas durable: file metadata byte limit exceeded")
	}
	_, err = w.tx.ExecContext(w.ctx, `INSERT INTO blas.durable_files(run_id,fid,block_height,entries,metadata) VALUES($1,$2,$3,$4,$5)`, w.a.Options.RunID, int64(fid), int64(w.state.Blocks), int64(w.state.InFile), payload)
	if err != nil {
		return err
	}
	w.state.Files++
	if w.activeGenerator != nil {
		w.activeGenerator.InFile = 0
		w.activeGenerator.Sequence++
		w.state.PendingFiles = append(w.state.PendingFiles, fid)
	}
	w.state.InFile = 0
	w.fileBounds = nil
	w.fileInput = nil
	w.state.InBlock++
	if w.state.InBlock == uint64(w.a.Options.FilesPerBlock) {
		g := w.activeGenerator
		w.activeGenerator = nil
		defer func() { w.activeGenerator = g }()
		return w.closeBlock()
	}
	return nil
}
func (w *writer) closeBlock() error {
	compact := w.pages.CompactZeroBitmaps
	w.pages.CompactZeroBitmaps = false // zero compaction is confined to entry files
	defer func() { w.pages.CompactZeroBitmaps = compact }()
	started := time.Now()
	if w.a.Metrics != nil {
		defer func() { w.a.Metrics.Access.BlockBuildNanos.Add(uint64(time.Since(started))) }()
	}
	if w.state.InBlock == 0 {
		return nil
	}
	height := w.state.Blocks
	segment := height / w.a.Options.SegmentSize
	local := height % w.a.Options.SegmentSize
	base := fmt.Sprintf("block/%d", height)
	first := w.state.Files - w.state.InBlock
	minTS := int64(math.MaxInt64)
	maxTS := int64(math.MinInt64)
	addFile := func(fid uint64, meta alis.FileMetadata) error {
		i := fid - first
		native := alis.NewTransaction(i, meta)
		if err := w.object("tx", height, i, alis.EncodeTransaction(native), native.FW, []string{fmt.Sprint(native.LID)}, native.TsMin, native.TsMax); err != nil {
			return err
		}
		minTS = min(minTS, native.TsMin)
		maxTS = max(maxTS, native.TsMax)
		return nil
	}
	if w.a.Options.GeneratorMode != "" {
		metas, e := w.pendingFileMetadata()
		if e != nil {
			return e
		}
		for i, meta := range metas {
			native := alis.NewTransaction(uint64(i), meta)
			if err := w.object("tx", height, uint64(i), alis.EncodeTransaction(native), native.FW, []string{fmt.Sprint(native.LID)}, native.TsMin, native.TsMax); err != nil {
				return err
			}
			minTS = min(minTS, native.TsMin)
			maxTS = max(maxTS, native.TsMax)
		}
	} else if w.accessOptimized() {
		if err := w.files(first, w.state.Files, addFile); err != nil {
			return err
		}
	} else {
		for i := uint64(0); i < w.state.InBlock; i++ {
			var data []byte
			if err := w.tx.QueryRowContext(w.ctx, `SELECT metadata FROM blas.durable_files WHERE run_id=$1 AND fid=$2`, w.a.Options.RunID, int64(first+i)).Scan(&data); err != nil {
				return err
			}
			var meta alis.FileMetadata
			if err := decode(data, &meta); err != nil {
				return err
			}
			native := alis.NewTransaction(i, meta)
			if err := w.object("tx", height, i, alis.EncodeTransaction(native), native.FW, []string{fmt.Sprint(native.LID)}, native.TsMin, native.TsMax); err != nil {
				return err
			}
			minTS = min(minTS, native.TsMin)
			maxTS = max(maxTS, native.TsMax)
		}
	}
	// All tx objects must be SQL-visible before vocabulary, OMT, bitmap and
	// optional aggregation readers. This does not move the SQL commit boundary.
	if err := w.flushObjects(); err != nil {
		return err
	}
	// Collecting a block's vocabulary is bounded by ObjectBytes; the complete
	// historical registry and completed segments are never loaded.
	words, err := w.words("tx", height)
	if err != nil {
		return err
	}
	lids := []string{fmt.Sprint(w.a.lid())}
	if w.a.Options.GeneratorMode != "" {
		lids, err = w.objectLIDs("tx", height)
		if err != nil {
			return err
		}
	}
	if err = w.object("block", segment, local, []byte{}, words, lids, minTS, maxTS); err != nil {
		return err
	}
	txO, err := w.buildObjects(base+"/omt", "tx", height, w.state.InBlock)
	if err != nil {
		return err
	}
	txK, err := w.buildBitmap(base+"/fk", "tx", height, w.state.InBlock, "keywords")
	if err != nil {
		return err
	}
	txMin, err := w.buildBitmap(base+"/min", "tx", height, w.state.InBlock, "ts_min")
	if err != nil {
		return err
	}
	txMax, err := w.buildBitmap(base+"/max", "tx", height, w.state.InBlock, "ts_max")
	if err != nil {
		return err
	}
	sb := fmt.Sprintf("segment/%d/%d", segment, height)
	indexStarted := time.Now()
	if w.a.SegmentComputations != nil {
		w.a.SegmentComputations.BeginSegment(fmt.Sprintf("%s/%d", w.a.Options.RunID, segment))
		w.pages.Computations = w.a.SegmentComputations
		defer func() { w.pages.Computations = nil }()
	}
	lg, err := w.buildBitmap(sb+"/lg", "block", segment, local+1, "lids")
	if err != nil {
		return err
	}
	fk, err := w.buildBitmap(sb+"/fk", "block", segment, local+1, "keywords")
	if err != nil {
		return err
	}
	smin, err := w.buildBitmap(sb+"/min", "block", segment, local+1, "ts_min")
	if err != nil {
		return err
	}
	smax, err := w.buildBitmap(sb+"/max", "block", segment, local+1, "ts_max")
	if err != nil {
		return err
	}
	indexElapsed := time.Since(indexStarted)
	w.pages.Computations = nil // transaction/file/optional aggregate trees retain their path
	var prev bhash.Digest
	if height > 0 {
		var payload []byte
		if err = w.tx.QueryRowContext(w.ctx, `SELECT payload FROM blas.durable_headers WHERE run_id=$1 AND height=$2`, w.a.Options.RunID, int64(height-1)).Scan(&payload); err != nil {
			return err
		}
		var header chain.BlockHeader
		if err = decode(payload, &header); err != nil {
			return err
		}
		prev = chain.HashBlockHeader(header)
	}
	if !w.state.HasPast || maxTS > w.state.PastMax {
		w.state.PastMax = maxTS
		w.state.HasPast = true
	}
	var segMin, segMax int64
	if err = w.tx.QueryRowContext(w.ctx, `SELECT min(ts_min),max(ts_max) FROM blas.durable_objects WHERE run_id=$1 AND kind='block' AND owner=$2`, w.a.Options.RunID, int64(segment)).Scan(&segMin, &segMax); err != nil {
		return err
	}
	var agg bhash.Digest
	if w.a.Options.Aggregation {
		agg, err = w.aggregation(sb, segment, height, lg)
		if err != nil {
			return err
		}
	}
	header := chain.BlockHeader{BlockHeight: height, PrevBlockHash: prev, FileTsMin: minTS, FileTsMax: maxTS, TxNum: w.state.InBlock, RH: chain.RootHashSet{TxOMTRoot: txO, TxFKRoot: txK, TxTsMinRoot: txMin, TxTsMaxRoot: txMax, BlockLGRoot: lg, BlockFKRoot: fk, BlockFileTsMinRoot: smin, BlockFileTsMaxRoot: smax}, PastTsMax: w.state.PastMax, BlockTsMax: segMax, BlockTsMin: segMin, AggBlockLGRoot: agg}
	payload, err := encode(header)
	if err != nil {
		return err
	}
	// These builders read native objects, never pending index pages. Complete
	// all index writes before inserting the header and deleting its predecessor.
	if w.groupIndexWrites {
		if err := w.pages.Batch.Flush(w.ctx); err != nil {
			return err
		}
	}
	_, err = w.tx.ExecContext(w.ctx, `INSERT INTO blas.durable_headers(run_id,height,segment,payload) VALUES($1,$2,$3,$4)`, w.a.Options.RunID, int64(height), int64(segment), payload)
	if err != nil {
		return err
	}
	// As in Chain.current.Store, only the newest index generation per segment is
	// queryable. Immutable block headers retain every historical root commitment.
	if local > 0 {
		if err = w.cleanupGeneration(segment, height-1); err != nil {
			return err
		}
	}
	w.samplePosition(height, indexElapsed, true)
	w.state.Blocks++
	w.state.InBlock = 0
	w.state.PendingFiles = nil
	if w.accessOptimized() && w.state.Blocks%w.a.Options.SegmentSize == 0 {
		delete(w.summaries, "block")
	}
	return nil
}
