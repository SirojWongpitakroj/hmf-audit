package durable

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"blas/alis"
	"blas/bhash"
	"blas/bitmap"
	"blas/internal/diskindex"
	"blas/optimize"
)

func (w *writer) words(kind string, owner uint64) ([]string, error) {
	if w.accessOptimized() {
		n := w.state.InBlock
		if kind == "block" {
			n = w.state.Blocks%w.a.Options.SegmentSize + 1
		}
		s, err := w.metadata(kind, owner, n)
		if err != nil {
			return nil, err
		}
		if s != nil {
			set := map[string]bool{}
			used := 0
			for _, r := range s.rows {
				for _, word := range r.keywords {
					if !set[word] {
						used += len(word) + 64
						if used > w.a.Options.ObjectBytes {
							return w.wordsFromSQL(kind, owner)
						}
						set[word] = true
					}
				}
			}
			var result []string
			err = w.dictionary(func(_ uint64, word string) error {
				if set[word] {
					result = append(result, word)
				}
				return nil
			})
			sort.Strings(result)
			return result, err
		}
	}
	return w.wordsFromSQL(kind, owner)
}
func (w *writer) wordsFromSQL(kind string, owner uint64) ([]string, error) {
	var out []string
	last := ""
	bytes := 0
	for {
		word, err := w.nextWord(kind, owner, last, false)
		if errors.Is(err, sql.ErrNoRows) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		bytes += len(word) + 32
		if bytes > w.a.Options.ObjectBytes {
			return nil, errors.New("blas durable: active block vocabulary exceeds working-set limit")
		}
		out = append(out, word)
		last = word
	}
}
func (w *writer) nextWord(kind string, owner uint64, last string, dictionary bool) (string, error) {
	var word string
	if dictionary {
		return "", errors.New("dictionary iteration uses kid ordering")
	}
	err := w.tx.QueryRowContext(w.ctx, `SELECT k.keyword FROM blas.durable_keywords k WHERE k.run_id=$1 AND k.keyword > $4 COLLATE "C" AND EXISTS(SELECT 1 FROM blas.durable_objects o WHERE o.run_id=k.run_id AND o.kind=$2 AND o.owner=$3 AND o.keywords ? k.keyword) ORDER BY k.keyword COLLATE "C" LIMIT 1`, w.a.Options.RunID, kind, int64(owner), last).Scan(&word)
	return word, err
}
func (w *writer) buildBitmap(tree, kind string, owner, n uint64, attribute string) (bhash.Digest, error) {
	if w.a.Options.GeneratorMode != "" && attribute == "lids" {
		return w.generatorBitmap(tree, kind, owner, n)
	}
	started := time.Now()
	if w.a.Metrics != nil {
		defer func() { w.a.Metrics.Access.BitmapBuildNanos.Add(uint64(time.Since(started))) }()
	}
	if (w.a.Execution != nil || w.activeGenerator != nil) && attribute == "dictionary" && kind == "entry" {
		return w.dictionaryBitmap(tree, owner, n)
	}
	if w.accessOptimized() && attribute != "dictionary" {
		s, err := w.metadata(kind, owner, n)
		if err != nil {
			return bhash.Digest{}, err
		}
		if s != nil {
			root, e := w.metadataBitmap(tree, s, n, attribute)
			if !errors.Is(e, errSummaryBudget) {
				return root, e
			}
		}
	}
	treeKind := "iimt"
	if attribute == "ts_min" || attribute == "ts_max" {
		treeKind = "rimbt"
	}
	builder, err := diskindex.NewBuilder(w.pages, tree, treeKind, w.a.Options.Fanout, n)
	if err != nil {
		return bhash.Digest{}, err
	}
	if attribute == "lids" {
		bm, err := w.selectBitmap(kind, owner, n, "lids", fmt.Sprint(w.a.lid()), 0)
		if err != nil {
			return bhash.Digest{}, err
		}
		if err = builder.AddBitmap(w.ctx, alis.LIDKey(w.a.lid()), 0, bm); err != nil {
			return bhash.Digest{}, err
		}
	} else if attribute == "ts_min" || attribute == "ts_max" {
		var last int64
		first := true
		for {
			var value sql.NullInt64
			query := `SELECT min(` + attribute + `) FROM blas.durable_objects WHERE run_id=$1 AND kind=$2 AND owner=$3`
			args := []any{w.a.Options.RunID, kind, int64(owner)}
			if !first {
				query += ` AND ` + attribute + `>$4`
				args = append(args, last)
			}
			if err = w.tx.QueryRowContext(w.ctx, query, args...).Scan(&value); err != nil {
				return bhash.Digest{}, err
			}
			if !value.Valid {
				break
			}
			bm, e := w.selectBitmap(kind, owner, n, attribute, "", value.Int64)
			if e != nil {
				return bhash.Digest{}, e
			}
			if err = builder.AddBitmap(w.ctx, "", value.Int64, bm); err != nil {
				return bhash.Digest{}, err
			}
			last = value.Int64
			first = false
		}
	} else if attribute == "dictionary" {
		for kid := uint64(0); kid < w.state.NextKID; kid++ {
			var word string
			if err = w.tx.QueryRowContext(w.ctx, `SELECT keyword FROM blas.durable_keywords WHERE run_id=$1 AND kid=$2`, w.a.Options.RunID, int64(kid)).Scan(&word); err != nil {
				return bhash.Digest{}, err
			}
			bm, e := w.selectBitmap(kind, owner, n, "keywords", word, 0)
			if e != nil {
				return bhash.Digest{}, e
			}
			if err = builder.AddBitmap(w.ctx, word, 0, bm); err != nil {
				return bhash.Digest{}, err
			}
		}
	} else if attribute == "keywords" {
		last := ""
		for {
			word, e := w.nextWord(kind, owner, last, false)
			if errors.Is(e, sql.ErrNoRows) {
				break
			}
			if e != nil {
				return bhash.Digest{}, e
			}
			bm, e := w.selectBitmap(kind, owner, n, "keywords", word, 0)
			if e != nil {
				return bhash.Digest{}, e
			}
			if err = builder.AddBitmap(w.ctx, word, 0, bm); err != nil {
				return bhash.Digest{}, err
			}
			last = word
		}
	} else {
		return bhash.Digest{}, errors.New("blas durable: unknown bitmap attribute")
	}
	metadata, err := builder.Finish(w.ctx)
	return metadata.Root, err
}
func (w *writer) selectBitmap(kind string, owner, n uint64, attribute, word string, value int64) (bitmap.Bitmap, error) {
	if (n+7)/8 > uint64(w.a.Options.Index.BitmapBytes) {
		return bitmap.Bitmap{}, errors.New("blas durable: active bitmap byte limit exceeded")
	}
	bm := bitmap.New(n)
	query := `SELECT oid FROM blas.durable_objects WHERE run_id=$1 AND kind=$2 AND owner=$3 AND oid>$4 AND `
	var predicate any
	switch attribute {
	case "keywords", "lids":
		query += attribute + ` ? $5`
		predicate = word
	case "ts_min", "ts_max":
		query += attribute + ` <= $5`
		predicate = value
	default:
		return bitmap.Bitmap{}, errors.New("invalid bitmap source")
	}
	query += ` ORDER BY oid LIMIT 1024`
	last := int64(-1)
	for {
		rows, err := w.tx.QueryContext(w.ctx, query, w.a.Options.RunID, kind, int64(owner), last, predicate)
		if err != nil {
			return bitmap.Bitmap{}, err
		}
		count := 0
		for rows.Next() {
			var oid int64
			if err = rows.Scan(&oid); err != nil {
				break
			}
			if oid < 0 || uint64(oid) >= n {
				err = errors.New("blas durable: object population mismatch")
				break
			}
			bm.Set(uint64(oid))
			last = oid
			count++
		}
		readErr := rows.Err()
		rows.Close()
		if err != nil {
			return bitmap.Bitmap{}, err
		}
		if readErr != nil {
			return bitmap.Bitmap{}, readErr
		}
		if count < 1024 {
			break
		}
	}
	return bm, nil
}
func (w *writer) buildObjects(tree, kind string, owner, n uint64) (bhash.Digest, error) {
	started := time.Now()
	if w.a.Metrics != nil {
		defer func() { w.a.Metrics.Access.ObjectBuildNanos.Add(uint64(time.Since(started))) }()
	}
	if kind == "entry" {
		if f := w.completeFileInput(owner, n); f != nil {
			return w.cachedFileObjects(tree, f)
		}
	}
	if w.a.Execution != nil {
		return w.objectsBatched(tree, kind, owner, n)
	}
	builder, err := diskindex.NewBuilder(w.pages, tree, "omt", w.a.Options.Fanout, n)
	if err != nil {
		return bhash.Digest{}, err
	}
	for i := uint64(0); i < n; i++ {
		var object []byte
		var size int
		if err = w.tx.QueryRowContext(w.ctx, `SELECT octet_length(object),CASE WHEN octet_length(object)<=$5 THEN object ELSE NULL END FROM blas.durable_objects WHERE run_id=$1 AND kind=$2 AND owner=$3 AND oid=$4`, w.a.Options.RunID, kind, int64(owner), int64(i), w.a.Options.ObjectBytes).Scan(&size, &object); err != nil {
			return bhash.Digest{}, err
		}
		if size > w.a.Options.ObjectBytes {
			return bhash.Digest{}, errors.New("blas durable: object byte limit exceeded")
		}
		if err = builder.AddObject(w.ctx, i, object); err != nil {
			return bhash.Digest{}, err
		}
	}
	meta, err := builder.Finish(w.ctx)
	return meta.Root, err
}
func (w *writer) aggregation(base string, segment, height uint64, _ bhash.Digest) (bhash.Digest, error) {
	if w.a.Options.GeneratorMode != "" {
		return w.aggregateGenerators(base, segment, height)
	}
	// The live baseline has exactly one log generator (the run LID), as before.
	// Transactions are streamed in (height,tsn) order, with no complete index map.
	gen, err := diskindex.NewBuilder(w.pages, base+"/generator", "aggregate", w.a.Options.Fanout, 0)
	if err != nil {
		return bhash.Digest{}, err
	}
	for h := segment * w.a.Options.SegmentSize; h <= height; h++ {
		var count uint64
		if err = w.tx.QueryRowContext(w.ctx, `SELECT count(*) FROM blas.durable_objects WHERE run_id=$1 AND kind='tx' AND owner=$2`, w.a.Options.RunID, int64(h)).Scan(&count); err != nil {
			return bhash.Digest{}, err
		}
		for i := uint64(0); i < count; i++ {
			var object []byte
			if err = w.tx.QueryRowContext(w.ctx, `SELECT object FROM blas.durable_objects WHERE run_id=$1 AND kind='tx' AND owner=$2 AND oid=$3`, w.a.Options.RunID, int64(h), int64(i)).Scan(&object); err != nil {
				return bhash.Digest{}, err
			}
			if err = gen.AddHash(w.ctx, optimize.GeneratorTxLeafHash(h, i, bhash.H(object))); err != nil {
				return bhash.Digest{}, err
			}
		}
	}
	meta, err := gen.Finish(w.ctx)
	if err != nil {
		return bhash.Digest{}, err
	}
	n := height%w.a.Options.SegmentSize + 1
	bm := bitmap.Not(bitmap.New(n))
	extended, err := diskindex.NewBuilder(w.pages, base+"/extended-lg", "aggregate", w.a.Options.Fanout, 0)
	if err != nil {
		return bhash.Digest{}, err
	}
	if err = extended.AddHash(w.ctx, optimize.ExtendedIIMTLeafHash(alis.LIDKey(w.a.lid()), bm, meta.Root)); err != nil {
		return bhash.Digest{}, err
	}
	meta, err = extended.Finish(w.ctx)
	return meta.Root, err
}
