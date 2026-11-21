package integrity

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// Experiment 1 on the native query set (systems/experiments/native-queries):
// EVSEB audits the record batches of dht_evseb_queries.json, five per
// (shape, q). Clustered batches are q consecutive leaf positions aligned to q
// (one perfect subtree of the quaternary tree); scattered batches put every
// leaf under a different branch. FAIR_NATIVE_QUERIES_DIR selects the set; when
// it is unset the original clustered-2/scattered selection is used.

const fairNativeWorkloadVersion = 9

var fairNativeShapes = []string{"clustered", "scattered"}

const fairNativeSets = 5

type fairNativeBatch struct {
	Shape    string
	Q        int
	Set      int
	Ordinals []int
}

func fairNativeQueriesDir() string { return os.Getenv("FAIR_NATIVE_QUERIES_DIR") }

// fairBatchesFromHMF (FAIR_BATCHES_FROM_HMF=1) makes EVSEB audit the records of the HMF-Audit
// queries of R0_queries.json instead of its own dht_evseb_queries.json batches (see
// loadFairHMFBatches), so that HMF-Audit, DHT-backed and EVSEB verify the same logs.
func fairBatchesFromHMF() bool { return os.Getenv("FAIR_BATCHES_FROM_HMF") == "1" }

// loadFairNativeBatches reads one scheme's batches for the requested q values
// and checks that every (shape, q) has exactly sets 1..5 with q distinct
// ordinals inside [0, records).
func loadFairNativeBatches(dir, scheme string, counts []int, records int) ([]fairNativeBatch, error) {
	data, err := os.ReadFile(filepath.Join(dir, "dht_evseb_queries.json"))
	if err != nil {
		return nil, err
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
		return nil, fmt.Errorf("decode native batches: %w", err)
	}
	if file.Records != records {
		return nil, fmt.Errorf("native batches were found on %d records, the fixture has %d", file.Records, records)
	}
	wanted := make(map[int]bool, len(counts))
	for _, q := range counts {
		wanted[q] = true
	}
	shapeOrder := map[string]int{}
	for index, shape := range fairNativeShapes {
		shapeOrder[shape] = index
	}
	var batches []fairNativeBatch
	seen := map[[3]int]bool{}
	for _, entry := range file.Queries {
		if entry.Scheme != scheme || !wanted[entry.Q] {
			continue
		}
		order, ok := shapeOrder[entry.Shape]
		if !ok {
			return nil, fmt.Errorf("native batch has unknown shape %q", entry.Shape)
		}
		if entry.Set < 1 || entry.Set > fairNativeSets || len(entry.Ordinals) != entry.Q {
			return nil, fmt.Errorf("native batch %s/q=%d/set=%d is malformed", entry.Shape, entry.Q, entry.Set)
		}
		distinct := make(map[int]bool, entry.Q)
		for _, ordinal := range entry.Ordinals {
			if ordinal < 0 || ordinal >= records || distinct[ordinal] {
				return nil, fmt.Errorf("native batch %s/q=%d/set=%d has a bad ordinal %d", entry.Shape, entry.Q, entry.Set, ordinal)
			}
			distinct[ordinal] = true
		}
		key := [3]int{order, entry.Q, entry.Set}
		if seen[key] {
			return nil, fmt.Errorf("duplicate native batch %s/q=%d/set=%d", entry.Shape, entry.Q, entry.Set)
		}
		seen[key] = true
		batches = append(batches, fairNativeBatch{Shape: entry.Shape, Q: entry.Q, Set: entry.Set,
			Ordinals: append([]int(nil), entry.Ordinals...)})
	}
	if want := len(fairNativeShapes) * len(counts) * fairNativeSets; len(batches) != want {
		return nil, fmt.Errorf("found %d native %s batches, want %d", len(batches), scheme, want)
	}
	sort.Slice(batches, func(i, j int) bool {
		a, b := batches[i], batches[j]
		if a.Shape != b.Shape {
			return shapeOrder[a.Shape] < shapeOrder[b.Shape]
		}
		if a.Q != b.Q {
			return a.Q < b.Q
		}
		return a.Set < b.Set
	})
	return batches, nil
}

// loadFairHMFBatches reads the HMF-Audit batches of R0_queries.json for Experiment 2: EVSEB
// localizes the same records HMF-Audit audits. A cell with fewer sets than five (or none) is
// simply smaller, as it is for HMF-Audit; entries carrying a note are skipped. Every batch has
// q distinct ordinals inside [0, records).
func loadFairHMFBatches(dir string, counts []int, records int) ([]fairNativeBatch, error) {
	data, err := os.ReadFile(filepath.Join(dir, "R0_queries.json"))
	if err != nil {
		return nil, err
	}
	var file struct {
		Queries []struct {
			Scheme   string `json:"scheme"`
			Shape    string `json:"shape"`
			Q        int    `json:"q"`
			Set      int    `json:"set"`
			Note     string `json:"note"`
			Ordinals []int  `json:"record_ordinals"`
		} `json:"queries"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("decode HMF-Audit batches: %w", err)
	}
	wanted := make(map[int]bool, len(counts))
	for _, q := range counts {
		wanted[q] = true
	}
	shapeOrder := map[string]int{}
	for index, shape := range fairNativeShapes {
		shapeOrder[shape] = index
	}
	var batches []fairNativeBatch
	seen := map[[3]int]bool{}
	for _, entry := range file.Queries {
		if entry.Scheme != "HMF-Audit" || entry.Note != "" || !wanted[entry.Q] {
			continue
		}
		order, ok := shapeOrder[entry.Shape]
		if !ok {
			return nil, fmt.Errorf("HMF-Audit batch has unknown shape %q", entry.Shape)
		}
		if entry.Set < 1 || len(entry.Ordinals) != entry.Q {
			return nil, fmt.Errorf("HMF-Audit batch %s/q=%d/set=%d is malformed", entry.Shape, entry.Q, entry.Set)
		}
		distinct := make(map[int]bool, entry.Q)
		for _, ordinal := range entry.Ordinals {
			if ordinal < 0 || ordinal >= records || distinct[ordinal] {
				return nil, fmt.Errorf("HMF-Audit batch %s/q=%d/set=%d has a bad ordinal %d", entry.Shape, entry.Q, entry.Set, ordinal)
			}
			distinct[ordinal] = true
		}
		key := [3]int{order, entry.Q, entry.Set}
		if seen[key] {
			return nil, fmt.Errorf("duplicate HMF-Audit batch %s/q=%d/set=%d", entry.Shape, entry.Q, entry.Set)
		}
		seen[key] = true
		ordinals := append([]int(nil), entry.Ordinals...)
		sort.Ints(ordinals) // the same ascending order HMF-Audit's localization batch uses
		batches = append(batches, fairNativeBatch{Shape: entry.Shape, Q: entry.Q, Set: entry.Set, Ordinals: ordinals})
	}
	if len(batches) == 0 {
		return nil, fmt.Errorf("no HMF-Audit batches in %s", dir)
	}
	sort.Slice(batches, func(i, j int) bool {
		a, b := batches[i], batches[j]
		if a.Shape != b.Shape {
			return shapeOrder[a.Shape] < shapeOrder[b.Shape]
		}
		if a.Q != b.Q {
			return a.Q < b.Q
		}
		return a.Set < b.Set
	})
	return batches, nil
}

var fairEVBESNativeRequestCounts = []int{1, 16, 64, 256, 1024, 4096}

func TestFairNativeBatchesMatchTheirShapes(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "experiments", "native-queries", "10000000")
	batches, err := loadFairNativeBatches(dir, "EVSEB", fairEVBESNativeRequestCounts, 10_000_000)
	if err != nil {
		t.Fatal(err)
	}
	for _, batch := range batches {
		ordinals := append([]int(nil), batch.Ordinals...)
		sort.Ints(ordinals)
		parents := map[int]bool{}
		for _, ordinal := range ordinals {
			parents[ordinal/Arity] = true
		}
		switch batch.Shape {
		case "clustered": // q consecutive leaves aligned to q: one perfect subtree
			if ordinals[len(ordinals)-1]-ordinals[0] != batch.Q-1 || ordinals[0]%batch.Q != 0 {
				t.Fatalf("clustered q=%d set=%d is not one aligned run", batch.Q, batch.Set)
			}
		case "scattered": // every leaf under a different parent
			if len(parents) != batch.Q {
				t.Fatalf("scattered q=%d set=%d shares a parent", batch.Q, batch.Set)
			}
		}
	}
}

func TestFairHMFBatchesFeedExperiment2(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "experiments", "native-queries", "10000000-skewed")
	batches, err := loadFairHMFBatches(dir, fairEVBESNativeRequestCounts, 10_000_000)
	if err != nil {
		t.Fatal(err)
	}
	// The skewed set has five HMF-Audit batches for every (shape, q): 2 x 6 x 5.
	if want := len(fairNativeShapes) * len(fairEVBESNativeRequestCounts) * fairNativeSets; len(batches) != want {
		t.Fatalf("got %d HMF-Audit batches, want %d", len(batches), want)
	}
	for _, batch := range batches {
		if len(batch.Ordinals) != batch.Q || !sort.IntsAreSorted(batch.Ordinals) {
			t.Fatalf("%s/q=%d/set=%d is not %d ascending ordinals", batch.Shape, batch.Q, batch.Set, batch.Q)
		}
		// Every batch lies in R0 (2,500,000 records): ALL answers one exact region.
		if last := batch.Ordinals[len(batch.Ordinals)-1]; last >= 2_500_000 {
			t.Fatalf("%s/q=%d/set=%d reaches ordinal %d, outside R0", batch.Shape, batch.Q, batch.Set, last)
		}
	}
	// A dataset with fewer records than the ordinals is refused.
	if _, err := loadFairHMFBatches(dir, fairEVBESNativeRequestCounts, 100_000); err == nil {
		t.Fatal("ordinals beyond the dataset were accepted")
	}
}
