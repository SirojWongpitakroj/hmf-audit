package integrity

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// fairEVBESQuerySet is one audit batch: the LIDs of one query-file entry.
type fairEVBESQuerySet struct {
	Set  int
	LIDs []string
}

// fairEVBESNativeCase is every batch for one shape (clustered or scattered) and q.
type fairEVBESNativeCase struct {
	Shape string
	Q     int
	Sets  []fairEVBESQuerySet
}

// fairEVBESNativeCases reads dht_evseb_queries.json from dir and returns the EVSEB
// batches: shapes in the order clustered, scattered; q ascending; the sets of a cell
// in file order. A record's LID is its global ordinal; every ordinal must be a leaf.
func fairEVBESNativeCases(dir string, leafCount int) ([]fairEVBESNativeCase, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "dht_evseb_queries.json"))
	if err != nil {
		return nil, fmt.Errorf("read EVSEB query file: %w", err)
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
		return nil, fmt.Errorf("parse EVSEB query file: %w", err)
	}
	type cell struct {
		shape string
		q     int
	}
	cells := map[cell][]fairEVBESQuerySet{}
	for _, query := range file.Queries {
		if query.Scheme != "EVSEB" || query.Note != "" { // a note marks a cell with fewer sets
			continue
		}
		if len(query.RecordOrdinals) != query.Q {
			return nil, fmt.Errorf("EVSEB %s q=%d set %d lists %d records", query.Shape, query.Q, query.Set, len(query.RecordOrdinals))
		}
		lids := make([]string, len(query.RecordOrdinals))
		for index, ordinal := range query.RecordOrdinals {
			if ordinal < 0 || ordinal >= leafCount {
				return nil, fmt.Errorf("EVSEB %s q=%d set %d: ordinal %d is not one of the %d leaves (wrong query directory for this dataset?)",
					query.Shape, query.Q, query.Set, ordinal, leafCount)
			}
			lids[index] = fairEVBESLID(ordinal)
		}
		key := cell{query.Shape, query.Q}
		cells[key] = append(cells[key], fairEVBESQuerySet{Set: query.Set, LIDs: lids})
	}
	var result []fairEVBESNativeCase
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
			result = append(result, fairEVBESNativeCase{Shape: shape, Q: q, Sets: sets})
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no EVSEB queries in %s", dir)
	}
	return result, nil
}
