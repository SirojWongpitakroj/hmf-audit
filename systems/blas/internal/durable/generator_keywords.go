package durable

import (
	"context"
	"database/sql"
)

type generatorKeyword struct {
	lid  uint64
	word string
}

// generatorKeywordPrefetch answers, for one Apply batch, whether a global
// keyword or a (LID, keyword) pair already exists, using two queries instead
// of one query per first occurrence. A present key holds its current existence
// and is set true once this batch assigns its KID. A key left out by the
// budget is absent and takes the original flush+query path. Records are still
// visited in canonical order, so KIDs are assigned exactly as before.
type generatorKeywordPrefetch struct {
	global map[string]bool
	local  map[generatorKeyword]bool
}

func (a Adapter) prefetchGeneratorKeywords(ctx context.Context, tx *sql.Tx, words [][]string, lids []uint64) (*generatorKeywordPrefetch, error) {
	limit, budget := 4096, 8<<20
	if a.Execution != nil {
		limit, budget = a.Execution.CacheMaxEntries, a.Execution.CacheMaxBytes
	}
	if limit <= 0 || budget <= 0 {
		return nil, nil
	}
	p := &generatorKeywordPrefetch{global: map[string]bool{}, local: map[generatorKeyword]bool{}}
	var globals, pairWords []string
	var pairLIDs []int64
	used := 0
	for i, record := range words {
		for _, word := range record {
			if _, ok := p.global[word]; !ok && len(p.global)+len(p.local) < limit && used+len(word)+64 <= budget {
				p.global[word] = false
				globals = append(globals, word)
				used += len(word) + 64
			}
			key := generatorKeyword{lids[i], word}
			if _, ok := p.local[key]; !ok && len(p.global)+len(p.local) < limit && used+len(word)+72 <= budget {
				p.local[key] = false
				pairLIDs, pairWords = append(pairLIDs, int64(key.lid)), append(pairWords, word)
				used += len(word) + 72
			}
		}
	}
	if len(globals) > 0 {
		rows, err := tx.QueryContext(ctx, `SELECT keyword FROM blas.durable_keywords WHERE run_id=$1 AND keyword=ANY($2::text[])`, a.Options.RunID, globals)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var word string
			if err = rows.Scan(&word); err != nil {
				rows.Close()
				return nil, err
			}
			p.global[word] = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	if len(pairLIDs) > 0 {
		rows, err := tx.QueryContext(ctx, `SELECT lid,keyword FROM blas.durable_generator_keywords WHERE run_id=$1 AND (lid,keyword) IN (SELECT * FROM unnest($2::bigint[],$3::text[]))`, a.Options.RunID, pairLIDs, pairWords)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var lid int64
			var word string
			if err = rows.Scan(&lid, &word); err != nil {
				rows.Close()
				return nil, err
			}
			p.local[generatorKeyword{uint64(lid), word}] = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return p, nil
}
