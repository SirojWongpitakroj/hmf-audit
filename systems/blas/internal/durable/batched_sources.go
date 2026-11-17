package durable

import (
	"blas/bhash"
	"blas/bitmap"
	"blas/internal/diskindex"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

type fileKeywords struct {
	owner  uint64
	words  []string
	bitmap bitmap.Bitmap
}

// dictionary walks bounded indexed chunks in the original KID order. It never
// loads the historical registry, and closes each SQL cursor before builder I/O.
func (w *writer) dictionary(visit func(uint64, string) error) error {
	if w.activeGenerator != nil {
		return w.generatorDictionary(visit)
	}
	if !w.accessOptimized() {
		return w.walkDictionary(0, visit)
	}
	if uint64(len(w.dictionaryWords)) > w.state.NextKID {
		w.dictionaryWords = nil
		w.dictionaryBytes = 0
	}
	start := uint64(len(w.dictionaryWords))
	for kid, word := range w.dictionaryWords {
		if err := visit(uint64(kid), word); err != nil {
			return err
		}
	}
	return w.walkDictionary(start, func(kid uint64, word string) error {
		cost := len(word) + 64
		if kid == uint64(len(w.dictionaryWords)) && w.summaryRows() < w.a.Execution.CacheMaxEntries && w.summaryBytes()+cost <= w.a.Execution.CacheMaxBytes && w.dictionaryBytes+cost <= w.a.Options.ObjectBytes {
			w.dictionaryWords = append(w.dictionaryWords, word)
			w.dictionaryBytes += cost
			w.observeSummaries()
		}
		return visit(kid, word)
	})
}
func (w *writer) walkDictionary(start uint64, visit func(uint64, string) error) error {
	limit, budget := 1000, 8<<20
	if w.a.Execution != nil {
		limit = w.a.Execution.BatchMaxRecords
		budget = w.a.Execution.BatchMaxBytes
	}
	for next := start; next < w.state.NextKID; {
		rows, err := w.tx.QueryContext(w.ctx, `SELECT kid,octet_length(keyword),CASE WHEN octet_length(keyword)<=$4 THEN keyword ELSE NULL END FROM blas.durable_keywords WHERE run_id=$1 AND kid >= $2 AND kid<$3 ORDER BY kid LIMIT $5`, w.a.Options.RunID, int64(next), int64(w.state.NextKID), w.a.Options.ObjectBytes, limit)
		if err != nil {
			return err
		}
		var words []string
		used := 0
		for rows.Next() {
			var kid uint64
			var size int
			var word sql.NullString
			if err = rows.Scan(&kid, &size, &word); err != nil {
				break
			}
			cost := size + 64
			if !word.Valid || cost > budget {
				err = errors.New("blas: dictionary word exceeds batch budget")
				break
			}
			if used > budget-cost {
				break
			}
			if kid != next+uint64(len(words)) {
				err = errors.New("blas: dictionary KID gap")
				break
			}
			words = append(words, word.String)
			used += cost
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
			return errors.New("blas: missing dictionary prefix")
		}
		for _, word := range words {
			if err := visit(next, word); err != nil {
				return err
			}
			next++
		}
	}
	return nil
}

// membership retains only the current file's keyword positions, with a hard
// working-set budget. Large vocabularies fall back to the original SQL bitmap
// iterator instead of changing native acceptance or allocating more memory.
func (w *writer) membership(owner, n uint64) (map[string][]uint64, error) {
	if f := w.completeFileInput(owner, n); f != nil {
		result := map[string][]uint64{}
		used := 0
		for oid, entry := range f.entries {
			for _, word := range entry.words {
				if _, ok := result[word]; !ok {
					used += len(word) + 64
				}
				used += 8
				if used > w.a.Options.ObjectBytes {
					return nil, nil
				}
				result[word] = append(result[word], uint64(oid))
			}
		}
		if w.a.Metrics != nil {
			w.a.Metrics.Access.EntryKeywordRereadsAvoided.Add(n)
		}
		return result, nil
	}
	result := map[string][]uint64{}
	used := 0
	next := uint64(0)
	limit := 1000
	if w.a.Execution != nil {
		limit = w.a.Execution.BatchMaxRecords
	}
	for next < n {
		rows, err := w.tx.QueryContext(w.ctx, `SELECT oid,octet_length(keywords::text),CASE WHEN octet_length(keywords::text)<=$4 THEN keywords::text ELSE NULL END FROM blas.durable_objects WHERE run_id=$1 AND kind='entry' AND owner=$2 AND oid >= $3 ORDER BY oid LIMIT $5`, w.a.Options.RunID, int64(owner), int64(next), w.a.Options.ObjectBytes, limit)
		if err != nil {
			return nil, err
		}
		read := 0
		overBudget := false
		for rows.Next() {
			var oid uint64
			var size int
			var raw sql.NullString
			if err = rows.Scan(&oid, &size, &raw); err != nil {
				break
			}
			if !raw.Valid || size > w.a.Options.ObjectBytes {
				overBudget = true
				break
			}
			if oid != next || oid >= n {
				err = errors.New("blas: file object gap")
				break
			}
			var words []string
			if err = json.Unmarshal([]byte(raw.String), &words); err != nil {
				break
			}
			for _, word := range words {
				if _, ok := result[word]; !ok {
					used += len(word) + 64
				}
				used += 8
				if used > w.a.Options.ObjectBytes {
					overBudget = true
					break
				}
				result[word] = append(result[word], oid)
			}
			if overBudget {
				break
			}
			next++
			read++
			if w.a.Metrics != nil {
				w.a.Metrics.Access.EntryKeywordReadRows.Add(1)
			}
		}
		readErr := rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		if readErr != nil {
			return nil, readErr
		}
		if overBudget {
			return nil, nil
		}
		if read == 0 {
			return nil, errors.New("blas: missing current file")
		}
	}
	return result, nil
}

func (w *writer) dictionaryBitmap(tree string, owner, n uint64) (bhash.Digest, error) {
	if (w.dictionaryPopulation()+7)/8 > uint64(w.a.Options.Index.BitmapBytes) {
		return bhash.Digest{}, errors.New("blas: keyword bitmap limit exceeded")
	}
	members, err := w.membership(owner, n)
	if err != nil {
		return bhash.Digest{}, err
	}
	builder, err := diskindex.NewBuilder(w.pages, tree, "iimt", w.a.Options.Fanout, n)
	if err != nil {
		return bhash.Digest{}, err
	}
	meta := &fileKeywords{owner: owner, bitmap: bitmap.New(w.dictionaryPopulation())}
	usedWords := 0
	err = w.dictionary(func(kid uint64, word string) error {
		var bm bitmap.Bitmap
		if members != nil && w.lowRiskEnabled() && len(members[word]) == 0 {
			return builder.AddPreparedKeywordBitmap(w.ctx, w.keywordInput(word), w.zeroInput(n))
		}
		if members == nil {
			var err error
			bm, err = w.selectBitmap("entry", owner, n, "keywords", word, 0)
			if err != nil {
				return err
			}
		} else {
			bm = bitmap.New(n)
			for _, oid := range members[word] {
				bm.Set(oid)
			}
			if len(members[word]) > 0 {
				usedWords += len(word) + 32
				if usedWords > w.a.Options.ObjectBytes {
					return errors.New("blas: file metadata budget exceeded")
				}
				meta.words = append(meta.words, word)
				meta.bitmap.Set(kid)
			}
		}
		if w.lowRiskEnabled() {
			return builder.AddPreparedKeywordBitmap(w.ctx, w.keywordInput(word), diskindex.PrepareBitmap(bm))
		}
		return builder.AddBitmap(w.ctx, word, 0, bm)
	})
	if err != nil {
		return bhash.Digest{}, err
	}
	root, err := builder.Finish(w.ctx)
	if err == nil && members != nil {
		w.fileKeywords = meta
	}
	return root.Root, err
}

func (w *writer) objectsBatched(tree, kind string, owner, n uint64) (bhash.Digest, error) {
	builder, err := diskindex.NewBuilder(w.pages, tree, "omt", w.a.Options.Fanout, n)
	if err != nil {
		return bhash.Digest{}, err
	}
	for next := uint64(0); next < n; {
		rows, err := w.tx.QueryContext(w.ctx, `SELECT oid,octet_length(object),CASE WHEN octet_length(object)<=$5 THEN object ELSE NULL END FROM blas.durable_objects WHERE run_id=$1 AND kind=$2 AND owner=$3 AND oid >= $4 ORDER BY oid LIMIT $6`, w.a.Options.RunID, kind, int64(owner), int64(next), w.a.Options.ObjectBytes, w.a.Execution.BatchMaxRecords)
		if err != nil {
			return bhash.Digest{}, err
		}
		var objects [][]byte
		used := 0
		for rows.Next() {
			var oid uint64
			var size int
			var object []byte
			if err = rows.Scan(&oid, &size, &object); err != nil {
				break
			}
			if size > w.a.Options.ObjectBytes || size+64 > w.a.Execution.BatchMaxBytes {
				err = errors.New("blas: object exceeds read batch budget")
				break
			}
			if used > w.a.Execution.BatchMaxBytes-size-64 {
				break
			}
			if oid != next+uint64(len(objects)) || oid >= n {
				err = fmt.Errorf("blas: object prefix gap")
				break
			}
			objects = append(objects, object)
			if kind == "entry" && w.a.Metrics != nil {
				w.a.Metrics.Access.EntryObjectReadRows.Add(1)
			}
			used += size + 64
		}
		readErr := rows.Err()
		rows.Close()
		if err != nil {
			return bhash.Digest{}, err
		}
		if readErr != nil {
			return bhash.Digest{}, readErr
		}
		if len(objects) == 0 {
			return bhash.Digest{}, errors.New("blas: missing object prefix")
		}
		for _, object := range objects {
			if err := builder.AddObject(w.ctx, next, object); err != nil {
				return bhash.Digest{}, err
			}
			next++
		}
	}
	meta, err := builder.Finish(w.ctx)
	return meta.Root, err
}
