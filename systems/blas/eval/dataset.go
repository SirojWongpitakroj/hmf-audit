package eval

import (
	"fmt"
	"math/rand"
	"sort"

	"blas/alis"
	"blas/chain"
	"blas/query"
)

// Record is the harness's logical log record shape: a timestamp (Unix
// milliseconds, D7) and a set of keywords extracted from it (H5: "use
// LogHub's structured template/literal tokens as entry keywords where
// available" — the synthetic generator below stands in for that
// extraction step, see config.go's scope note).
type Record struct {
	Timestamp int64
	Keywords  []string
	Data      []byte
}

// GenerateSyntheticRecords produces a deterministic (seeded) sequence of
// records standing in for a parsed LogHub dataset (see config.go's scope
// note). Timestamps increase monotonically (as a real log's would);
// keywords are drawn from a small fixed vocabulary so keyword-scalability
// (Fig. 6) and Boolean-query (Table II) workloads have realistic overlap.
func GenerateSyntheticRecords(count int, seed int64) []Record {
	rng := rand.New(rand.NewSource(seed))
	vocab := []string{"login", "logout", "error", "warning", "put", "get", "timeout", "retry", "connect", "disconnect"}
	records := make([]Record, count)
	ts := int64(1_600_000_000_000) // arbitrary fixed epoch start (2020-09-13T12:26:40Z)
	for i := 0; i < count; i++ {
		ts += int64(1 + rng.Intn(50)) // strictly increasing, small random gaps
		numKW := 1 + rng.Intn(3)
		seen := map[string]bool{}
		var kws []string
		for len(kws) < numKW {
			w := vocab[rng.Intn(len(vocab))]
			if !seen[w] {
				seen[w] = true
				kws = append(kws, w)
			}
		}
		records[i] = Record{Timestamp: ts, Keywords: kws}
	}
	return records
}

// AssignToGenerators implements R-EVAL-002/H1: "randomly and uniformly
// distributing the raw log data" among NumGenerators, via a fixed-seed PRNG
// (deterministic per-entry assignment), then stable-sorts each generator's
// assigned records by timestamp before file packing (H1/H4).
func AssignToGenerators(cfg Config, records []Record) [][]Record {
	rng := rand.New(rand.NewSource(cfg.Seed))
	perGen := make([][]Record, cfg.NumGenerators)
	for _, r := range records {
		g := rng.Intn(cfg.NumGenerators)
		perGen[g] = append(perGen[g], r)
	}
	for g := range perGen {
		sort.SliceStable(perGen[g], func(i, j int) bool { return perGen[g][i].Timestamp < perGen[g][j].Timestamp })
	}
	return perGen
}

// BuildChain packs each generator's assigned records into files of
// EntriesPerFile (R-EVAL-003, a short final file permitted per H1), packs
// every FilesPerBlock file metadatas into one block (R-EVAL-004, a short
// final block permitted), and commits them to a fresh chain, storing each
// file on an SP. FW is the union of a file's entry keywords (H5).
//
// This is sequential, single-goroutine construction throughout (G8): no
// worker pool, matching every other package in this codebase.
func BuildChain(cfg Config, perGenRecords [][]Record) (*chain.Chain, *query.SP, map[uint64][]string, error) {
	c, err := chain.NewChainWithOptions(cfg.Fanout, cfg.SegmentSize, cfg.UseAggregation)
	if err != nil {
		return nil, nil, nil, err
	}
	sp := query.NewSP()
	registry := map[uint64][]string{}

	var pendingMetas []alis.FileMetadata
	flushBlock := func() error {
		if len(pendingMetas) == 0 {
			return nil
		}
		if _, err := c.AddBlock(pendingMetas); err != nil {
			return err
		}
		pendingMetas = nil
		return nil
	}

	for lid := 0; lid < len(perGenRecords); lid++ {
		recs := perGenRecords[lid]
		if len(recs) == 0 {
			continue
		}
		gen := alis.NewLogGenerator(uint64(lid))
		var fid uint64
		for start := 0; start < len(recs); start += cfg.EntriesPerFile {
			end := start + cfg.EntriesPerFile
			if end > len(recs) {
				end = len(recs)
			}
			b, err := alis.NewEntryFileBuilder(gen, fid, cfg.Fanout)
			if err != nil {
				return nil, nil, nil, err
			}
			for _, r := range recs[start:end] {
				if _, err := b.AddEntry(r.Timestamp, r.Keywords, r.Data); err != nil {
					return nil, nil, nil, err
				}
			}
			file, meta, err := b.Close()
			if err != nil {
				return nil, nil, nil, err
			}
			ref := query.FileRef{LID: uint64(lid), FID: fid}
			sp.Store(ref, file)
			pendingMetas = append(pendingMetas, meta)
			fid++

			if len(pendingMetas) == cfg.FilesPerBlock {
				if err := flushBlock(); err != nil {
					return nil, nil, nil, err
				}
			}
		}
		registry[uint64(lid)] = gen.KeywordUniverse()
	}
	if err := flushBlock(); err != nil {
		return nil, nil, nil, err
	}
	if len(c.Blocks()) == 0 {
		return nil, nil, nil, fmt.Errorf("eval: no blocks were produced (dataset too small for the given packing sizes)")
	}
	return c, sp, registry, nil
}
