package verify

import (
	"bytes"
	"encoding/gob"
	"testing"

	"blas/alis"
	"blas/chain"
	"blas/iomf"
	"blas/optimize"
	"blas/query"
)

// gobView is a query.ChainView that answers from gob copies of a chain's segments and blocks, the
// encodings query.PostgresChain stores, so a full node on it reads only decoded rows and never the
// in-memory chain. It checks that the stored encodings carry everything Stage 1 and Stage 2 need.
type gobView struct {
	t        *testing.T
	segSize  uint64
	segments [][]byte
	blocks   [][]byte
	headers  []optimize.HeaderTimes // anchor header times per segment, as PostgresChain stores them
	registry map[uint64][]string
	decoded  int
}

func newGobView(t *testing.T, c *chain.Chain) *gobView {
	t.Helper()
	view := &gobView{t: t, segSize: c.SegmentSize(), registry: c.KeywordRegistry()}
	encode := func(value any) []byte {
		var out bytes.Buffer
		if err := gob.NewEncoder(&out).Encode(value); err != nil {
			t.Fatalf("gob encode: %v", err)
		}
		return out.Bytes()
	}
	for _, segment := range c.Segments() {
		view.segments = append(view.segments, encode(segment))
	}
	for _, block := range c.Blocks() {
		view.blocks = append(view.blocks, encode(block))
	}
	for i := range view.segments {
		header := c.Blocks()[optimize.AnchorHeight(uint64(i), c.SegmentSize(), len(c.Blocks()))].Header
		view.headers = append(view.headers, optimize.HeaderTimes{
			BlockTsMin: header.BlockTsMin, BlockTsMax: header.BlockTsMax, PastTsMax: header.PastTsMax})
	}
	return view
}

func (v *gobView) SegmentSize() uint64                  { return v.segSize }
func (v *gobView) KeywordRegistry() map[uint64][]string { return v.registry }
func (v *gobView) BeginRequest()                        {}

func (v *gobView) IntersectingSegments(hasRange bool, ts, te int64) ([]uint64, error) {
	return query.BackwardScan(uint64(len(v.headers)), hasRange, ts, te, func(upper int64) ([]query.AnchorRow, error) {
		var rows []query.AnchorRow
		for seg := upper; seg >= 0 && seg > upper-4; seg-- { // small pages exercise the paging
			rows = append(rows, query.AnchorRow{Seg: uint64(seg), Times: v.headers[seg]})
		}
		return rows, nil
	})
}

func (v *gobView) Segments(indexes []uint64) ([]*chain.Segment, error) {
	out := make([]*chain.Segment, len(indexes))
	for i, index := range indexes {
		var segment chain.Segment
		if err := gob.NewDecoder(bytes.NewReader(v.segments[index])).Decode(&segment); err != nil {
			return nil, err
		}
		v.decoded++
		out[i] = &segment
	}
	return out, nil
}

func (v *gobView) Blocks(heights []uint64) ([]*chain.Block, error) {
	out := make([]*chain.Block, len(heights))
	for i, height := range heights {
		var block chain.Block
		if err := gob.NewDecoder(bytes.NewReader(v.blocks[height])).Decode(&block); err != nil {
			return nil, err
		}
		v.decoded++
		out[i] = &block
	}
	return out, nil
}

// TestStoredChainViewMatchesInMemoryChain answers the same queries from the in-memory chain and
// from decoded stored rows, with both optimizations on, and requires the same accepted entries.
func TestStoredChainViewMatchesInMemoryChain(t *testing.T) {
	c, err := chain.NewChainWithOptions(2, 2, true)
	if err != nil {
		t.Fatal(err)
	}
	sp := query.NewSP()
	registry := map[uint64][]string{}
	gen := alis.NewLogGenerator(1)
	words := [][]string{{"login"}, {"error"}, {"login", "error"}}
	timestamps := []int64{100, 200, 2000, 2100, 5000, 5100, 5200, 9000, 9100, 9200, 9300}
	for i, ts := range timestamps {
		builder, err := alis.NewEntryFileBuilder(gen, uint64(i), 2)
		if err != nil {
			t.Fatal(err)
		}
		for entry := int64(0); entry < 3; entry++ {
			if _, err := builder.AddEntry(ts+entry, words[(i+int(entry))%len(words)], nil); err != nil {
				t.Fatal(err)
			}
		}
		file, meta, err := builder.Close()
		if err != nil {
			t.Fatal(err)
		}
		sp.Store(query.FileRef{LID: 1, FID: uint64(i)}, file)
		registry[1] = gen.KeywordUniverse()
		if _, err := c.AddBlock([]alis.FileMetadata{meta}); err != nil {
			t.Fatal(err)
		}
	}
	headers := make([]chain.BlockHeader, len(c.Blocks()))
	for i, block := range c.Blocks() {
		headers[i] = block.Header
	}
	auditor := NewAuditor(headers, c.SegmentSize())
	memory := query.NewFullNode(c)
	view := newGobView(t, c)
	stored := query.NewFullNodeFromView(view)
	options := Options{UseSegmentScan: true, UseAggregation: true}

	queries := []query.Q{
		{LG: 1, HasRange: true, TS: 1900, TE: 2200, EK: iomf.Keyword("login")},
		{LG: 1, HasRange: true, TS: 0, TE: 100000, EK: iomf.Keyword("error")},
		{LG: 1, HasRange: true, TS: 5000, TE: 5300, EK: iomf.Keyword("login")},
		{LG: 1, HasRange: true, TS: 9100, TE: 9400, EK: iomf.Keyword("error")},
		{LG: 1, HasRange: true, TS: 200000, TE: 300000, EK: iomf.Keyword("login")},
		{LG: 1, EK: iomf.Keyword("login")},
	}
	for index, q := range queries {
		want, wantOK := auditor.PrepareResponse(memory, sp, registry, q, options)
		got, gotOK := auditor.PrepareResponse(stored, sp, registry, q, options)
		if wantOK != gotOK {
			t.Fatalf("query %d: response ok in memory=%v, stored=%v", index, wantOK, gotOK)
		}
		if !wantOK {
			continue
		}
		wantResult := auditor.VerifyResponse(registry, q, want, options)
		gotResult := auditor.VerifyResponse(registry, q, got, options)
		if wantResult.Accept != gotResult.Accept || len(wantResult.Entries) != len(gotResult.Entries) {
			t.Fatalf("query %d: in memory accept=%v entries=%d, stored accept=%v entries=%d", index,
				wantResult.Accept, len(wantResult.Entries), gotResult.Accept, len(gotResult.Entries))
		}
		if len(want.Stage1.CandidateHeights) != len(got.Stage1.CandidateHeights) ||
			len(want.Stage2.Blocks) != len(got.Stage2.Blocks) {
			t.Fatalf("query %d: stage sizes differ between the in-memory and stored node", index)
		}
	}
	if view.decoded == 0 {
		t.Fatal("the stored node decoded no rows, so the test did not use it")
	}
}
