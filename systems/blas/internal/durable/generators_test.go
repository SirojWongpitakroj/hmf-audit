package durable

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"blas/alis"
	"blas/chain"
	"blas/iomf"
	"blas/live"
	"blas/query"
	"blas/verify"
	contract "canonicalingestion"
	"canonicalingestion/testfixture"
)

func generatorEntries(n int) []contract.Entry {
	es := testfixture.Entries(1, n)
	for i := range es {
		var o map[string]any
		json.Unmarshal(es[i].Record, &o)
		o["device"] = fmt.Sprintf("device-%d", i%4)
		if i%17 == 0 {
			o["label"] = "attack"
		}
		// A real within-generator reversal; unrelated generator years stay mixed.
		if i == 112 {
			stamp, _ := time.Parse(time.RFC3339, o["timestamp"].(string))
			o["timestamp"] = stamp.Add(-48 * time.Hour).Format(time.RFC3339)
		}
		es[i].Record, _ = json.Marshal(o)
	}
	return es
}

func generatorAdapter(db *sql.DB) Adapter {
	a := lowRiskAdapter(db)
	a.Options.GeneratorMode = "device-v1"
	a.Options.MaxGenerators = 32
	a.Options.EntriesPerFile = 4
	a.Options.FilesPerBlock = 3
	a.Options.SegmentSize = 4
	return a
}

type generatorOracle struct {
	chain *chain.Chain
	files map[uint64]*alis.EntryFile
	metas map[uint64]alis.FileMetadata
	ids   []string
}

func buildGeneratorOracle(t *testing.T, es []contract.Entry, o Options) generatorOracle {
	t.Helper()
	c, _ := chain.NewChainWithOptions(o.Fanout, o.SegmentSize, o.Aggregation)
	result := generatorOracle{chain: c, files: map[uint64]*alis.EntryFile{}, metas: map[uint64]alis.FileMetadata{}}
	type active struct {
		gen        *alis.LogGenerator
		b          *alis.EntryFileBuilder
		fid, count uint64
		last       int64
	}
	gens := map[uint64]*active{}
	var pending []alis.FileMetadata
	block := func() {
		if len(pending) > 0 {
			if _, err := c.AddBlock(pending); err != nil {
				t.Fatal(err)
			}
			pending = nil
		}
	}
	closeFile := func(g *active) {
		if g.count == 0 {
			return
		}
		f, m, err := g.b.Close()
		if err != nil {
			t.Fatal(err)
		}
		result.files[g.fid] = f
		result.metas[g.fid] = m
		c.RegisterKeywordUniverse(g.gen.LID, g.gen.KeywordUniverse())
		pending = append(pending, m)
		g.count = 0
		g.b = nil
		if len(pending) == o.FilesPerBlock {
			block()
		}
	}
	for _, e := range es {
		var ob live.Observation
		json.Unmarshal(e.Record, &ob)
		_, lid := generatorIdentity(ob)
		g := gens[lid]
		if g == nil {
			g = &active{gen: alis.NewLogGenerator(lid)}
			gens[lid] = g
		}
		ts := ob.Timestamp.UnixMilli()
		if g.count > 0 && ts < g.last {
			closeFile(g)
		}
		if g.count == 0 {
			g.fid = e.Ordinal - 1
			g.b, _ = alis.NewEntryFileBuilder(g.gen, g.fid, o.Fanout)
		}
		result.ids = append(result.ids, fmt.Sprintf("%d/%d/%d", lid, g.fid, g.count))
		if _, err := g.b.AddEntry(ts, ob.Keywords(), e.Record); err != nil {
			t.Fatal(err)
		}
		g.count++
		g.last = ts
		if g.count == uint64(o.EntriesPerFile) {
			closeFile(g)
		}
	}
	var lids []uint64
	for lid := range gens {
		lids = append(lids, lid)
	}
	sort.Slice(lids, func(i, j int) bool { return lids[i] < lids[j] })
	for _, lid := range lids {
		closeFile(gens[lid])
	}
	block()
	return result
}

func TestGeneratorFormationRootsProofsAndDeterministicRestart(t *testing.T) {
	es := generatorEntries(257)
	for _, agg := range []bool{false, true} {
		t.Run(fmt.Sprint(agg), func(t *testing.T) {
			var snapshots [2]string
			var checkpoints [2][]byte
			for mode := range 2 {
				db, dsn := testfixture.Database(t, "BLAS_TEST_POSTGRES_DSN")
				a := generatorAdapter(db)
				a.Options.Aggregation = agg
				a.CompactZeroBitmaps = mode == 1
				ctx := context.Background()
				if err := a.Migrate(ctx); err != nil {
					t.Fatal(err)
				}
				oracle := buildGeneratorOracle(t, es, a.Options)
				p := contract.Progress{Target: uint64(len(es))}
				var ids []string
				for p.Committed < uint64(len(es)) {
					if p.Committed == 85 && mode == 1 {
						db.Close()
						var err error
						db, err = contract.OpenPostgres(ctx, dsn, 6)
						if err != nil {
							t.Fatal(err)
						}
						a.DB = db
						defer db.Close()
					}
					n := min(17, len(es)-int(p.Committed))
					tx, err := db.Begin()
					if err != nil {
						t.Fatal(err)
					}
					got, cp, err := a.Apply(ctx, tx, p, es[p.Committed:p.Committed+uint64(n)])
					if err != nil {
						tx.Rollback()
						t.Fatal(err)
					}
					if err := tx.Commit(); err != nil {
						t.Fatal(err)
					}
					ids = append(ids, got...)
					p.Committed += uint64(n)
					p.Checkpoint = cp
				}
				if !reflect.DeepEqual(ids, oracle.ids) {
					t.Fatal("generator membership/ESN/identity drift")
				}
				if a.Metrics.Access.TimestampReversalClosures.Load() != 1 {
					t.Fatal("within-generator reversal was not preserved", a.Metrics.Access.TimestampReversalClosures.Load())
				}
				for _, b := range oracle.chain.Blocks() {
					h, err := a.NewReader(ctx, nil).Header(b.Header.BlockHeight)
					if err != nil || !reflect.DeepEqual(h, b.Header) {
						t.Fatalf("block root mismatch at %d: %v\ngot=%+v\nwant=%+v", b.Header.BlockHeight, err, h, b.Header)
					}
				}
				for fid, f := range oracle.files {
					var raw []byte
					if err := db.QueryRow(`SELECT metadata FROM blas.durable_files WHERE fid=$1`, int64(fid)).Scan(&raw); err != nil {
						t.Fatal(err)
					}
					var m alis.FileMetadata
					if err := decode(raw, &m); err != nil || !reflect.DeepEqual(m, oracle.metas[fid]) {
						t.Fatal("file roots/dictionary/metadata drift", fid, err)
					}
					for _, word := range []string{f.Entries[0].EW[0], "missing"} {
						for _, hasRange := range []bool{false, true} {
							q := alis.EntryQuery{EK: iomf.Keyword(word), HasRange: hasRange, TS: f.Entries[0].TS, TE: f.Entries[len(f.Entries)/2].TS}
							want, err := f.ProcessQuery(q)
							if err != nil {
								t.Fatal(err)
							}
							got, err := a.NewReader(ctx, nil).Entry(fid, q)
							if err != nil || !reflect.DeepEqual(want, got) || !alis.VerifyEntryQuery(m.RHIIMT, m.RHOMT, q, got) {
								t.Fatal("entry proof differs", fid, err)
							}
						}
					}
				}
				node := query.NewFullNode(oracle.chain)
				var st state
				json.Unmarshal(p.Checkpoint, &st)
				var generations uint64
				if err := db.QueryRow(`SELECT count(DISTINCT split_part(tree,'/',2)||'/'||split_part(tree,'/',3)) FROM blas.index_pages WHERE tree LIKE 'segment/%'`).Scan(&generations); err != nil || generations != (st.Blocks+a.Options.SegmentSize-1)/a.Options.SegmentSize {
					t.Fatal("superseded segment generations were retained", generations, err)
				}
				for _, g := range st.Generators {
					q := query.QOn{LG: g.LID}
					r := a.NewReader(ctx, nil)
					s1, err := r.Stage1(q, false)
					if err != nil {
						t.Fatal(err)
					}
					want, e := node.Stage1(q)
					if e != nil || !reflect.DeepEqual(s1, want) {
						t.Fatal("multi-generator block proof differs", e)
					}
					s2, err := r.Stage2(q, s1.CandidateHeights)
					if err != nil {
						t.Fatal(err)
					}
					want2, e := node.Stage2(q, want.CandidateHeights)
					if e != nil || !reflect.DeepEqual(s2, want2) {
						t.Fatal("multi-generator transaction proof differs", e)
					}
					trusted, err := verify.NewStoredAuditor(len(oracle.chain.Blocks()), a.Options.SegmentSize, a.NewReader(ctx, nil).Header)
					if err != nil {
						t.Fatal(err)
					}
					heights, ok := trusted.VerifyStage1(q, s1, false)
					if !ok {
						t.Fatal("stage 1 rejected")
					}
					if _, ok := trusted.VerifyStage2(q, heights, s2); !ok {
						t.Fatal("stage 2 rejected")
					}
					if agg {
						got, bindings, err := r.Stage2Aggregated(q, s1)
						if err != nil {
							t.Fatal(err)
						}
						want, wb, e := node.Stage2Aggregated(q, s1)
						if e != nil || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(bindings, wb) {
							t.Fatal("multi-generator aggregate proof differs", e)
						}
						if _, ok := trusted.VerifyStage2Aggregated(q, heights, got, bindings); !ok {
							t.Fatal("aggregate proof rejected")
						}
					}
				}
				// Compare logical SQL state; physical zero-marker pages intentionally differ.
				snapshots[mode] = testfixture.Snapshot(t, db, `SELECT * FROM blas.durable_objects ORDER BY kind,owner,oid`, `SELECT * FROM blas.durable_files ORDER BY fid`, `SELECT * FROM blas.durable_headers ORDER BY height`, `SELECT * FROM blas.durable_generator_keywords ORDER BY lid,kid`)
				checkpoints[mode] = p.Checkpoint
			}
			if snapshots[0] != snapshots[1] || !bytes.Equal(checkpoints[0], checkpoints[1]) {
				t.Fatal("clean rebuild/restart/physical compaction changed logical state")
			}
		})
	}
}
