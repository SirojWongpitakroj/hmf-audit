package durable

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"blas/alis"
	"blas/bhash"
	"blas/bitmap"
	"blas/internal/diskindex"
)

type objectSummary struct {
	oid            uint64
	keywords, lids []string
	min, max       int64
	cost           int
}
type fileBounds struct {
	fid, count uint64
	min, max   int64
}

var errSummaryBudget = errors.New("BLAS metadata scratch budget reached; use original streamed query path")

type metadataSummary struct {
	kind  string
	owner uint64
	rows  []objectSummary
	bytes int
}

func (w *writer) accessOptimized() bool {
	return w.a.Execution != nil && w.a.Execution.StorageAccessOptimizations
}
func (w *writer) summaryBytes() int {
	n := w.dictionaryBytes
	for _, s := range w.summaries {
		n += s.bytes
	}
	return n
}
func (w *writer) summaryRows() int {
	n := len(w.dictionaryWords)
	for _, s := range w.summaries {
		n += len(s.rows)
	}
	return n
}
func (w *writer) observeSummaries() {
	if w.a.Metrics != nil {
		w.a.Metrics.Access.SummaryBytes(int64(w.summaryBytes()))
	}
}
func summaryCost(words, lids []string) int {
	n := 256 + 32*(len(words)+len(lids))
	for _, s := range words {
		n += len(s)
	}
	for _, s := range lids {
		n += len(s)
	}
	return n
}

// One transaction owns this cache. There is at most one current tx group and
// one current segment group; no state survives rollback, commit or restart.
func (w *writer) metadata(kind string, owner, n uint64) (*metadataSummary, error) {
	if !w.accessOptimized() || kind == "entry" {
		return nil, nil
	}
	if w.summaries == nil {
		w.summaries = make(map[string]*metadataSummary, 2)
	}
	if s := w.summaries[kind]; s != nil && s.owner == owner && uint64(len(s.rows)) == n {
		if w.a.Metrics != nil {
			w.a.Metrics.Access.SummaryCacheHits.Add(1)
		}
		return s, nil
	}
	delete(w.summaries, kind)
	if n > uint64(w.a.Execution.CacheMaxEntries-w.summaryRows()) {
		return nil, nil
	}
	s := &metadataSummary{kind: kind, owner: owner}
	for next := uint64(0); next < n; {
		rows, err := w.tx.QueryContext(w.ctx, `SELECT oid,octet_length(keywords::text)+octet_length(lids::text),CASE WHEN octet_length(keywords::text)+octet_length(lids::text)<=$5 THEN keywords::text ELSE NULL END,CASE WHEN octet_length(keywords::text)+octet_length(lids::text)<=$5 THEN lids::text ELSE NULL END,ts_min,ts_max FROM blas.durable_objects WHERE run_id=$1 AND kind=$2 AND owner=$3 AND oid >= $4 ORDER BY oid LIMIT $6`, w.a.Options.RunID, kind, int64(owner), int64(next), w.a.Options.ObjectBytes, w.a.Execution.BatchMaxRecords)
		if err != nil {
			return nil, err
		}
		read := 0
		over := false
		for rows.Next() {
			var row objectSummary
			var size int
			var words, lids sql.NullString
			if err = rows.Scan(&row.oid, &size, &words, &lids, &row.min, &row.max); err != nil {
				break
			}
			if row.oid != next || row.oid >= n {
				err = errors.New("BLAS metadata prefix gap")
				break
			}
			// Reject/stream before decoding beyond the explicit working budget.
			if !words.Valid || !lids.Valid || size > w.a.Options.ObjectBytes || size*16+256 > w.a.Execution.CacheMaxBytes-w.summaryBytes()-s.bytes {
				over = true
				break
			}
			if err = json.Unmarshal([]byte(words.String), &row.keywords); err != nil {
				break
			}
			if err = json.Unmarshal([]byte(lids.String), &row.lids); err != nil {
				break
			}
			row.cost = summaryCost(row.keywords, row.lids)
			if s.bytes+row.cost > w.a.Options.ObjectBytes || w.summaryBytes()+s.bytes+row.cost > w.a.Execution.CacheMaxBytes {
				over = true
				break
			}
			s.rows = append(s.rows, row)
			s.bytes += row.cost
			next++
			read++
		}
		e := rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		if e != nil {
			return nil, e
		}
		if over {
			return nil, nil
		}
		if read == 0 {
			return nil, errors.New("missing BLAS metadata")
		}
	}
	w.summaries[kind] = s
	w.observeSummaries()
	return s, nil
}

// Extend only after the writer's SQL flush succeeded; these are still private
// transaction-local rows, never shared with another transaction or an auditor.
func (w *writer) extendMetadata(kind string, owner, oid uint64, words, lids []string, minTS, maxTS int64) {
	if !w.accessOptimized() {
		return
	}
	s := w.summaries[kind]
	if s == nil {
		return
	}
	if s.owner != owner || uint64(len(s.rows)) != oid {
		delete(w.summaries, kind)
		return
	}
	cost := summaryCost(words, lids)
	if w.summaryRows() >= w.a.Execution.CacheMaxEntries || w.summaryBytes()+cost > w.a.Execution.CacheMaxBytes || s.bytes+cost > w.a.Options.ObjectBytes {
		delete(w.summaries, kind)
		return
	}
	s.rows = append(s.rows, objectSummary{oid, append([]string(nil), words...), append([]string(nil), lids...), minTS, maxTS, cost})
	s.bytes += cost
	w.observeSummaries()
}

func (w *writer) metadataBitmap(tree string, s *metadataSummary, n uint64, attribute string) (bhash.Digest, error) {
	if (n+7)/8 > uint64(w.a.Options.Index.BitmapBytes) {
		return bhash.Digest{}, errors.New("BLAS bitmap budget exceeded")
	}
	kind := "iimt"
	if attribute == "ts_min" || attribute == "ts_max" {
		kind = "rimbt"
	}
	b, err := diskindex.NewBuilder(w.pages, tree, kind, w.a.Options.Fanout, n)
	if err != nil {
		return bhash.Digest{}, err
	}
	if attribute == "lids" {
		bm := bitmap.New(n)
		lid := strconv.FormatUint(w.a.lid(), 10)
		for _, r := range s.rows {
			for _, v := range r.lids {
				if v == lid {
					bm.Set(r.oid)
					break
				}
			}
		}
		err = b.AddBitmap(w.ctx, alis.LIDKey(w.a.lid()), 0, bm)
	} else if attribute == "keywords" {
		positions := make(map[string][]uint64)
		used := 0
		for _, r := range s.rows {
			for _, word := range r.keywords {
				if _, ok := positions[word]; !ok {
					used += len(word) + 64
				}
				used += 8
				if used > w.a.Options.ObjectBytes {
					return bhash.Digest{}, errSummaryBudget
				}
				positions[word] = append(positions[word], r.oid)
			}
		}
		// Match the original join against the registered dictionary, followed by
		// its explicit PostgreSQL COLLATE C order (UTF-8 byte lexical order).
		var words []string
		err = w.dictionary(func(_ uint64, word string) error {
			if _, ok := positions[word]; ok {
				words = append(words, word)
			}
			return nil
		})
		if err == nil {
			sort.Strings(words)
			for _, word := range words {
				bm := bitmap.New(n)
				for _, oid := range positions[word] {
					bm.Set(oid)
				}
				if err = b.AddBitmap(w.ctx, word, 0, bm); err != nil {
					break
				}
			}
		}
	} else if attribute == "ts_min" || attribute == "ts_max" {
		rows := append([]objectSummary(nil), s.rows...)
		value := func(r objectSummary) int64 {
			if attribute == "ts_min" {
				return r.min
			}
			return r.max
		}
		sort.Slice(rows, func(i, j int) bool {
			if value(rows[i]) == value(rows[j]) {
				return rows[i].oid < rows[j].oid
			}
			return value(rows[i]) < value(rows[j])
		})
		bm := bitmap.New(n)
		for i := 0; i < len(rows); {
			v := value(rows[i])
			for i < len(rows) && value(rows[i]) == v {
				bm.Set(rows[i].oid)
				i++
			}
			if err = b.AddBitmap(w.ctx, "", v, bm); err != nil {
				break
			}
		}
	} else {
		return bhash.Digest{}, fmt.Errorf("unsupported metadata attribute %s", attribute)
	}
	if err != nil {
		return bhash.Digest{}, err
	}
	meta, err := b.Finish(w.ctx)
	return meta.Root, err
}

// Batch file reads in native FID order and close SQL cursors before writes.
func (w *writer) files(first, last uint64, visit func(uint64, alis.FileMetadata) error) error {
	for next := first; next < last; {
		rows, err := w.tx.QueryContext(w.ctx, `SELECT fid,octet_length(metadata),CASE WHEN octet_length(metadata)<=$4 THEN metadata ELSE NULL END FROM blas.durable_files WHERE run_id=$1 AND fid >= $2 AND fid<$3 ORDER BY fid LIMIT $5`, w.a.Options.RunID, int64(next), int64(last), w.a.Options.ObjectBytes, w.a.Execution.BatchMaxRecords)
		if err != nil {
			return err
		}
		var values []alis.FileMetadata
		used := 0
		for rows.Next() {
			var id uint64
			var size int
			var raw []byte
			if err = rows.Scan(&id, &size, &raw); err != nil {
				break
			}
			if id != next+uint64(len(values)) {
				err = errors.New("BLAS file metadata gap")
				break
			}
			cost := size*16 + 256
			if size > w.a.Options.ObjectBytes {
				err = errors.New("BLAS file read exceeds bounded batch")
				break
			}
			if len(values) > 0 && used+cost > w.a.Execution.BatchMaxBytes {
				break
			}
			var m alis.FileMetadata
			if err = decode(raw, &m); err != nil {
				break
			}
			values = append(values, m)
			used += cost
			// A single existing native object can exceed the conservative decoded
			// batch estimate. Preserve the original one-object acceptance limit.
			if cost > w.a.Execution.BatchMaxBytes {
				break
			}
		}
		e := rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if e != nil {
			return e
		}
		if len(values) == 0 {
			return errors.New("missing BLAS file metadata")
		}
		for _, m := range values {
			if err = visit(next, m); err != nil {
				return err
			}
			next++
		}
	}
	return nil
}
