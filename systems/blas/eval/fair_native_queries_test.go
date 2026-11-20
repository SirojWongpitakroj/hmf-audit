package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"blas/iomf"
	"blas/query"
)

// fairBLASBaseMillis is the fair fixture's timestamp origin: a log is stamped
// 2017-01-01T00:00:00Z plus its global ordinal in milliseconds, because no log text
// carries a parseable timestamp (fairBLASTimestamp).
const fairBLASBaseMillis = int64(1483228800000)

// fairBLASNativeScenarios reads R0_queries.json from dir and returns one scenario
// per BLAS query: shapes in the order clustered, scattered; q ascending; the sets of
// a cell in file order. Each is the time range plus keyword expression of the file,
// and carries the timestamps of the records it must return.
func fairBLASNativeScenarios(dir string, vocabulary []string) ([]fairBLASScenario, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "R0_queries.json"))
	if err != nil {
		return nil, fmt.Errorf("read BLAS query file: %w", err)
	}
	var file struct {
		Queries []struct {
			Scheme         string   `json:"scheme"`
			Shape          string   `json:"shape"`
			Q              int      `json:"q"`
			Set            int      `json:"set"`
			Operator       string   `json:"keyword_operator"`
			Terms          []string `json:"keyword_terms"`
			TimeRangeMS    []int64  `json:"time_range_ms"`
			RecordOrdinals []int    `json:"record_ordinals"`
			Layout         string   `json:"layout"`
			RecordTimesMS  []int64  `json:"record_times_ms"`
			Note           string   `json:"note"`
		} `json:"queries"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("parse BLAS query file: %w", err)
	}
	known := map[string]bool{"other": true}
	for _, word := range vocabulary {
		known[word] = true
	}
	type cell struct {
		shape string
		q     int
	}
	cells := map[cell][]fairBLASScenario{}
	for _, entry := range file.Queries {
		if entry.Scheme != "BLAS" || entry.Note != "" { // a note marks a cell with fewer sets
			continue
		}
		if len(entry.RecordOrdinals) != entry.Q || len(entry.TimeRangeMS) != 2 {
			return nil, fmt.Errorf("BLAS %s q=%d set %d: malformed query", entry.Shape, entry.Q, entry.Set)
		}
		for _, term := range entry.Terms {
			if !known[term] && !fairBLASIsAttributeKeyword(term) {
				return nil, fmt.Errorf("BLAS %s q=%d set %d: keyword %q is not in this dataset's vocabulary (wrong query directory?)",
					entry.Shape, entry.Q, entry.Set, term)
			}
		}
		expression, err := fairBLASExpression(entry.Operator, entry.Terms)
		if err != nil {
			return nil, err
		}
		// A query set for the attribute fixture lists the event time of every record it must return
		// ("layout": "event_time"); the original sets list ordinals, which are timestamps of the
		// original fixture. Mixing them would compare different clocks, so it is refused.
		eventTime := entry.Layout == "event_time"
		if eventTime != fairBLASAttributesEnabled() {
			return nil, fmt.Errorf("BLAS %s q=%d set %d: this query set is for the %s fixture, but the %s fixture is selected (FAIR_BLAS_ATTRIBUTES)",
				entry.Shape, entry.Q, entry.Set, map[bool]string{true: "event-time", false: "ordinal-timestamp"}[eventTime],
				map[bool]string{true: "event-time", false: "ordinal-timestamp"}[fairBLASAttributesEnabled()])
		}
		expected := make([]int64, len(entry.RecordOrdinals))
		if eventTime {
			if len(entry.RecordTimesMS) != entry.Q {
				return nil, fmt.Errorf("BLAS %s q=%d set %d: record_times_ms lists %d times", entry.Shape, entry.Q, entry.Set, len(entry.RecordTimesMS))
			}
			copy(expected, entry.RecordTimesMS)
		} else {
			for index, ordinal := range entry.RecordOrdinals {
				expected[index] = fairBLASBaseMillis + int64(ordinal)
			}
		}
		key := cell{entry.Shape, entry.Q}
		cells[key] = append(cells[key], fairBLASScenario{
			placement: entry.Shape, batchSize: entry.Q, set: entry.Set, expectedTS: expected,
			request: query.Q{
				LG: 0, HasRange: true, TS: entry.TimeRangeMS[0], TE: entry.TimeRangeMS[1],
				FK: expression, EK: expression,
			},
		})
	}
	var result []fairBLASScenario
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
			sort.Slice(sets, func(i, j int) bool { return sets[i].set < sets[j].set })
			result = append(result, sets...)
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no BLAS queries in %s", dir)
	}
	return result, nil
}

// fairBLASExpression builds a keyword expression: AND or OR of the terms, or, for
// "ALL" (a query that matches every log), the OR of every keyword.
func fairBLASExpression(operator string, terms []string) (*iomf.Expr, error) {
	if len(terms) == 0 {
		return nil, fmt.Errorf("keyword expression has no terms")
	}
	combine := iomf.And
	switch operator {
	case "AND":
	case "OR", "ALL":
		combine = iomf.Or
	default:
		return nil, fmt.Errorf("unknown keyword operator %q", operator)
	}
	expression := iomf.Keyword(terms[0])
	for _, term := range terms[1:] {
		expression = combine(expression, iomf.Keyword(term))
	}
	return expression, nil
}

// fairBLASSameTimestamps reports whether two timestamp lists hold the same values,
// ignoring order.
func fairBLASSameTimestamps(got, want []int64) error {
	if len(got) != len(want) {
		return fmt.Errorf("returned %d records, want %d", len(got), len(want))
	}
	left := append([]int64(nil), got...)
	right := append([]int64(nil), want...)
	sort.Slice(left, func(i, j int) bool { return left[i] < left[j] })
	sort.Slice(right, func(i, j int) bool { return right[i] < right[j] })
	for index := range left {
		if left[index] != right[index] {
			return fmt.Errorf("record %d has timestamp %d, want %d", index, left[index], right[index])
		}
	}
	return nil
}
