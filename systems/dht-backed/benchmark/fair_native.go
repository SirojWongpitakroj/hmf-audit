package benchmark

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"dhtbacked/encryptor"
	"dhtbacked/internal/cassandradht"
)

// Experiment 1 on the native query set (systems/experiments/native-queries):
// DHT-backed audits the record batches of dht_evseb_queries.json, five per
// (shape, q). Clustered batches are q consecutive leaves of one log type's
// MK-Tree inside one level-8 ancestor (shared ancestor certificates);
// scattered batches spread over as many (log type, ancestor) pairs as exist.
// FAIR_NATIVE_QUERIES_DIR selects the set; unset keeps clustered-2/scattered.

const fairNativeWorkloadVersion = 9

var fairNativeShapes = []string{"clustered", "scattered"}

const fairNativeSets = 5

func fairNativeQueriesDir() string { return os.Getenv("FAIR_NATIVE_QUERIES_DIR") }

// fairBatchesFromHMF (FAIR_BATCHES_FROM_HMF=1) makes DHT-backed audit the records of the
// HMF-Audit queries of R0_queries.json instead of its own dht_evseb_queries.json batches, so
// that HMF-Audit, DHT-backed and EVSEB verify the same logs. The records are taken in
// ascending ordinal order.
func fairBatchesFromHMF() bool { return os.Getenv("FAIR_BATCHES_FROM_HMF") == "1" }

// fairNativeDHTBatches returns the native batches as record IDs, keyed and
// ordered by (shape, q, set), after checking that every (shape, q) has exactly
// sets 1..5 with q distinct ordinals inside [0, total).
func fairNativeDHTBatches(dir string, total int) (map[fairDHTCase][]string, []fairDHTCase, error) {
	fileName, scheme := "dht_evseb_queries.json", "DHT-backed"
	if fairBatchesFromHMF() {
		fileName, scheme = "R0_queries.json", "HMF-Audit"
	}
	data, err := os.ReadFile(filepath.Join(dir, fileName))
	if err != nil {
		return nil, nil, err
	}
	var file struct {
		Records int `json:"records"`
		Queries []struct {
			Scheme   string `json:"scheme"`
			Shape    string `json:"shape"`
			Q        int    `json:"q"`
			Set      int    `json:"set"`
			Ordinals []int  `json:"record_ordinals"`
		} `json:"queries"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, nil, fmt.Errorf("decode native batches: %w", err)
	}
	// R0_queries.json records the size of region R0, not of the dataset; its ordinals are
	// range-checked against the fixture below.
	if !fairBatchesFromHMF() && file.Records != total {
		return nil, nil, fmt.Errorf("native batches were found on %d records, the fixture has %d", file.Records, total)
	}
	wanted := make(map[int]bool, len(fairDHTRequestCounts))
	for _, q := range fairDHTRequestCounts {
		wanted[q] = true
	}
	shapeOrder := map[string]int{}
	for index, shape := range fairNativeShapes {
		shapeOrder[shape] = index
	}
	batches := map[fairDHTCase][]string{}
	var cases []fairDHTCase
	for _, entry := range file.Queries {
		if entry.Scheme != scheme || !wanted[entry.Q] {
			continue
		}
		if _, ok := shapeOrder[entry.Shape]; !ok {
			return nil, nil, fmt.Errorf("native batch has unknown shape %q", entry.Shape)
		}
		if entry.Set < 1 || entry.Set > fairNativeSets || len(entry.Ordinals) != entry.Q {
			return nil, nil, fmt.Errorf("native batch %s/q=%d/set=%d is malformed", entry.Shape, entry.Q, entry.Set)
		}
		key := fairDHTCase{placement: entry.Shape, count: entry.Q, set: entry.Set}
		if _, duplicate := batches[key]; duplicate {
			return nil, nil, fmt.Errorf("duplicate native batch %s/q=%d/set=%d", entry.Shape, entry.Q, entry.Set)
		}
		distinct := make(map[int]bool, entry.Q)
		ids := make([]string, len(entry.Ordinals))
		ordinals := entry.Ordinals
		if fairBatchesFromHMF() {
			ordinals = append([]int(nil), ordinals...)
			sort.Ints(ordinals)
		}
		for index, ordinal := range ordinals {
			if ordinal < 0 || ordinal >= total || distinct[ordinal] {
				return nil, nil, fmt.Errorf("native batch %s/q=%d/set=%d has a bad ordinal %d", entry.Shape, entry.Q, entry.Set, ordinal)
			}
			distinct[ordinal] = true
			ids[index] = encryptor.FormatRecordID(uint64(ordinal))
		}
		batches[key] = ids
		cases = append(cases, key)
	}
	if want := len(fairNativeShapes) * len(fairDHTRequestCounts) * fairNativeSets; len(cases) != want {
		return nil, nil, fmt.Errorf("found %d native %s batches, want %d", len(cases), scheme, want)
	}
	sort.Slice(cases, func(i, j int) bool {
		a, b := cases[i], cases[j]
		if a.placement != b.placement {
			return shapeOrder[a.placement] < shapeOrder[b.placement]
		}
		if a.count != b.count {
			return a.count < b.count
		}
		return a.set < b.set
	})
	return batches, cases, nil
}

// fairDHTBenchmarkLimits raises the per-call working-memory bounds of the
// Cassandra DHT client for the benchmark so that the largest native batch
// (scattered q=4096, about 53,800 proof lookups) can run. The scheme's own
// defaults (cassandradht.DefaultLimits) are unchanged; every bound can be
// overridden with FAIR_DHT_MAX_REQUESTS, FAIR_DHT_PROOF_BYTES and
// FAIR_DHT_ANCESTOR_MAP_BYTES, and the values are reported per result line.
func fairDHTBenchmarkLimits(concurrency int) (cassandradht.Limits, error) {
	limits := cassandradht.DefaultLimits()
	limits.Workers = concurrency
	limits.MaxRequests = 1 << 16
	limits.ProofBytes = 64 << 20
	limits.AncestorMapBytes = 128 << 20
	for name, target := range map[string]*int{
		"FAIR_DHT_MAX_REQUESTS":       &limits.MaxRequests,
		"FAIR_DHT_PROOF_BYTES":        &limits.ProofBytes,
		"FAIR_DHT_ANCESTOR_MAP_BYTES": &limits.AncestorMapBytes,
	} {
		if value := os.Getenv(name); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				return limits, fmt.Errorf("invalid %s %q", name, value)
			}
			*target = parsed
		}
	}
	return limits, limits.Validate()
}
