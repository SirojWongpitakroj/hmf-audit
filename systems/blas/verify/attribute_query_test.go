package verify

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"

	"blas/alis"
	"blas/chain"
	"blas/iomf"
	"blas/query"
)

// TestAttributeKeywordQueriesWithTiedAndRegionResetTimestamps: logs carry tenant, service, type and region
// keywords and an event time in milliseconds (many logs share one time; a second region starts again at an
// earlier time, so block times are not monotone across the chain). A query that ANDs the four keywords over
// a time range returns exactly the logs a brute-force scan of the same attributes and times selects.
func TestAttributeKeywordQueriesWithTiedAndRegionResetTimestamps(t *testing.T) {
	random := rand.New(rand.NewSource(5))
	c, err := chain.NewChainWithOptions(2, 3, true)
	if err != nil {
		t.Fatal(err)
	}
	sp := query.NewSP()
	registry := map[uint64][]string{}
	gen := alis.NewLogGenerator(0)

	type entry struct {
		ts                          int64
		tenant, service, kind, area string
	}
	var all []entry
	tenants, services, kinds := []string{"T0", "T1", "T2"}, []string{"driver", "executor"}, []string{"INFO", "WARN"}
	fid := uint64(0)
	for _, region := range []struct {
		name  string
		start int64
	}{{"R0", 1_700_000_000_000}, {"R1", 1_600_000_000_000}} { // R1 restarts at an earlier time
		clock := region.start
		for file := 0; file < 14; file++ {
			builder, err := alis.NewEntryFileBuilder(gen, fid, 2)
			if err != nil {
				t.Fatal(err)
			}
			for index := 0; index < 12; index++ {
				if random.Intn(3) == 0 { // time advances in whole seconds; several logs share one
					clock += 1000
				}
				item := entry{clock, tenants[random.Intn(3)], services[random.Intn(2)], kinds[random.Intn(2)], region.name}
				words := []string{"tenant=" + item.tenant, "service=" + item.service, "type=" + item.kind, "region=" + item.area}
				if _, err := builder.AddEntry(item.ts, words, []byte(fmt.Sprint(item))); err != nil {
					t.Fatal(err)
				}
				all = append(all, item)
			}
			file, meta, err := builder.Close()
			if err != nil {
				t.Fatal(err)
			}
			sp.Store(query.FileRef{LID: 0, FID: fid}, file)
			registry[0] = gen.KeywordUniverse()
			if _, err := c.AddBlock([]alis.FileMetadata{meta}); err != nil {
				t.Fatal(err)
			}
			fid++
		}
	}
	headers := make([]chain.BlockHeader, len(c.Blocks()))
	for i, block := range c.Blocks() {
		headers[i] = block.Header
	}
	auditor := NewAuditor(headers, c.SegmentSize())
	node := query.NewFullNode(c)
	options := Options{UseSegmentScan: true, UseAggregation: true}

	checked := 0
	for trial := 0; trial < 40; trial++ {
		area := []string{"R0", "R1"}[random.Intn(2)]
		base := map[string]int64{"R0": 1_700_000_000_000, "R1": 1_600_000_000_000}[area]
		from := base + int64(random.Intn(40))*1000
		to := from + int64(random.Intn(25))*1000 + 999
		tenant, service, kind := tenants[random.Intn(3)], services[random.Intn(2)], kinds[random.Intn(2)]
		expression := iomf.And(iomf.And(iomf.Keyword("tenant="+tenant), iomf.Keyword("service="+service)),
			iomf.And(iomf.Keyword("type="+kind), iomf.Keyword("region="+area)))
		request := query.Q{LG: 0, HasRange: true, TS: from, TE: to, FK: expression, EK: expression}

		var want []int64
		for _, item := range all {
			if item.tenant == tenant && item.service == service && item.kind == kind && item.area == area &&
				item.ts >= from && item.ts <= to {
				want = append(want, item.ts)
			}
		}
		response, ok := auditor.PrepareResponse(node, sp, registry, request, options)
		if !ok {
			t.Fatalf("trial %d: no response", trial)
		}
		result := auditor.VerifyResponse(registry, request, response, options)
		if !result.Accept {
			t.Fatalf("trial %d: the auditor rejected a correct response", trial)
		}
		got := make([]int64, len(result.Entries))
		for index, found := range result.Entries {
			got[index] = found.TS
		}
		sort.Slice(got, func(a, b int) bool { return got[a] < got[b] })
		sort.Slice(want, func(a, b int) bool { return want[a] < want[b] })
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("trial %d (%s %s %s %s [%d,%d]): returned %d logs, brute force selects %d", trial, tenant, service, kind, area, from, to, len(got), len(want))
		}
		checked += len(want)
	}
	if checked == 0 {
		t.Fatal("no trial selected any log, so the test proved nothing")
	}
}
