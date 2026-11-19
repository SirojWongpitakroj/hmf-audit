package benchmark

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"dhtbacked/encryptor"
)

// fairDHTDefaultMaxQ is the largest request size DHT-backed is run at when
// FAIR_DHT_MAX_Q is not set. Its working-memory bounds (cassandradht.DefaultLimits:
// 32,768 requests and 8 MiB of proof bytes per call) reject the scattered batches
// at q = 4096, so the series stops at 2^11. Of the protocol's request sizes that
// leaves 1 to 1024.
const fairDHTDefaultMaxQ = 2048

// fairDHTMaxQ parses FAIR_DHT_MAX_Q (empty means the default).
func fairDHTMaxQ(value string) (int, error) {
	if value == "" {
		return fairDHTDefaultMaxQ, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("FAIR_DHT_MAX_Q must be a positive integer, got %q", value)
	}
	return parsed, nil
}

// fairDHTQuerySet is one audit batch: the record IDs of one query-file entry.
type fairDHTQuerySet struct {
	Set int
	IDs []string
}

// fairDHTNativeCase is every batch for one shape (clustered or scattered) and q.
type fairDHTNativeCase struct {
	Shape string
	Q     int
	Sets  []fairDHTQuerySet
}

// fairDHTNativeCases reads dht_evseb_queries.json from dir and returns the
// DHT-backed batches: shapes in the order clustered, scattered; q ascending and no
// larger than maxQ; the sets of a cell in file order. Record ID = global ordinal.
func fairDHTNativeCases(dir string, maxQ int) ([]fairDHTNativeCase, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "dht_evseb_queries.json"))
	if err != nil {
		return nil, fmt.Errorf("read DHT-backed query file: %w", err)
	}
	var file struct {
		Queries []struct {
			Scheme         string `json:"scheme"`
			Shape          string `json:"shape"`
			Q              int    `json:"q"`
			Set            int    `json:"set"`
			RecordOrdinals []int  `json:"record_ordinals"`
			Note           string `json:"note"`
		} `json:"queries"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("parse DHT-backed query file: %w", err)
	}
	type cell struct {
		shape string
		q     int
	}
	cells := map[cell][]fairDHTQuerySet{}
	for _, query := range file.Queries {
		if query.Scheme != "DHT-backed" || query.Q > maxQ || query.Note != "" { // a note marks a cell with fewer sets
			continue
		}
		if len(query.RecordOrdinals) != query.Q {
			return nil, fmt.Errorf("DHT-backed %s q=%d set %d lists %d records", query.Shape, query.Q, query.Set, len(query.RecordOrdinals))
		}
		ids := make([]string, len(query.RecordOrdinals))
		for index, ordinal := range query.RecordOrdinals {
			ids[index] = encryptor.FormatRecordID(uint64(ordinal))
		}
		key := cell{query.Shape, query.Q}
		cells[key] = append(cells[key], fairDHTQuerySet{Set: query.Set, IDs: ids})
	}
	var result []fairDHTNativeCase
	for _, shape := range []string{"clustered", "scattered"} {
		var sizes []int
		for key := range cells {
			if key.shape == shape {
				sizes = append(sizes, key.q)
			}
		}
		sort.Ints(sizes)
		for _, q := range sizes {
			sets := cells[cell{shape, q}]
			sort.Slice(sets, func(i, j int) bool { return sets[i].Set < sets[j].Set })
			result = append(result, fairDHTNativeCase{Shape: shape, Q: q, Sets: sets})
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no DHT-backed queries up to q=%d in %s", maxQ, dir)
	}
	return result, nil
}
