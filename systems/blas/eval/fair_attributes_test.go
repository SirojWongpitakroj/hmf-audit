package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestAttributeKeywordsFollowTheDatasetFields(t *testing.T) {
	record := fairBLASDatasetRecord{TenantID: "T3", ServiceID: "spark-executor", LogType: "INFO", RegionID: "R0"}
	got := fairBLASAttributeKeywords(record)
	want := []string{"tenant=T3", "service=spark-executor", "type=INFO", "region=R0"}
	if len(got) != len(want) {
		t.Fatalf("keywords %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] || !fairBLASIsAttributeKeyword(got[index]) {
			t.Fatalf("keyword %d is %q, want %q", index, got[index], want[index])
		}
	}
	for _, term := range []string{"info", "other", "tenant=", "tenant", "svc=x"} {
		if fairBLASIsAttributeKeyword(term) {
			t.Fatalf("%q was taken for an attribute keyword", term)
		}
	}
	t.Setenv("FAIR_BLAS_ATTRIBUTES", "0")
	if keywords := fairBLASAttributeKeywords(record); keywords != nil {
		t.Fatalf("FAIR_BLAS_ATTRIBUTES=0 still added %v", keywords)
	}
}

// TestNativeScenariosAcceptAnAttributeQuery: a query file whose BLAS entry is the AND of the four
// attribute keywords and a time range loads as a scenario that expects the listed records.
func TestNativeScenariosAcceptAnAttributeQuery(t *testing.T) {
	dir := t.TempDir()
	file := map[string]any{"queries": []map[string]any{{
		"scheme": "BLAS", "shape": "clustered", "q": 3, "set": 1, "keyword_operator": "AND",
		"keyword_terms":   []string{"tenant=T0", "service=spark-executor", "type=INFO", "region=R0"},
		"time_range_ms":   []int64{1496918265000, 1496918269999},
		"record_ordinals": []int{10, 25, 40},
		"layout":          "event_time",
		"record_times_ms": []int64{1496918265000, 1496918265000, 1496918269000},
	}}}
	raw, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "R0_queries.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	scenarios, err := fairBLASNativeScenarios(dir, []string{"info", "task"})
	if err != nil {
		t.Fatal(err)
	}
	if len(scenarios) != 1 || scenarios[0].batchSize != 3 || len(scenarios[0].expectedTS) != 3 {
		t.Fatalf("unexpected scenarios: %+v", scenarios)
	}
	if !scenarios[0].request.HasRange || scenarios[0].request.TS != 1496918265000 || scenarios[0].request.TE != 1496918269999 ||
		scenarios[0].expectedTS[2] != 1496918269000 {
		t.Fatalf("time range not carried into the request: %+v", scenarios[0].request)
	}
	// An ordinal-timestamp query set is refused by the event-time fixture.
	file["queries"].([]map[string]any)[0]["layout"] = ""
	raw, _ = json.Marshal(file)
	if err := os.WriteFile(filepath.Join(dir, "R0_queries.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fairBLASNativeScenarios(dir, []string{"info", "task"}); err == nil {
		t.Fatal("an ordinal-timestamp query set was accepted by the event-time fixture")
	}
	file["queries"].([]map[string]any)[0]["layout"] = "event_time"
	// An unknown, non-attribute term is still refused.
	file["queries"].([]map[string]any)[0]["keyword_terms"] = []string{"tenant=T0", "nonsense"}
	raw, _ = json.Marshal(file)
	if err := os.WriteFile(filepath.Join(dir, "R0_queries.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fairBLASNativeScenarios(dir, []string{"info", "task"}); err == nil {
		t.Fatal("a term outside the vocabulary and the attributes was accepted")
	}
}

// TestTimeOrderedRecordsComeBackInRegionThenEventTimeOrder: lines written with event times out of
// order across two regions are served sorted by (region, event time, file position), unchanged.
func TestTimeOrderedRecordsComeBackInRegionThenEventTimeOrder(t *testing.T) {
	type line struct {
		region, when string
		serial       int
	}
	input := []line{
		{"R0", "2017-06-08T10:00:05Z", 0}, {"R0", "2017-06-08T10:00:01Z", 1}, {"R0", "2017-06-08T10:00:05Z", 2},
		{"R0", "2017-06-08T10:00:03Z", 3}, {"R1", "2017-06-08T09:00:00Z", 4}, {"R1", "2017-06-08T08:00:00Z", 5},
	}
	path := filepath.Join(t.TempDir(), "records.jsonl")
	var content []byte
	for _, item := range input {
		content = append(content, []byte(fmt.Sprintf(
			`{"source_id":"s%d","raw_log":"log %d","region_id":"%s","tenant_id":"T0","service_id":"svc","log_type":"INFO","event_time":"%s"}`+"\n",
			item.serial, item.serial, item.region, item.when))...)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	records, err := newFairBLASTimeOrderedRecords(file, len(input))
	if err != nil {
		t.Fatal(err)
	}
	var got []int
	for records.Scan() {
		var record fairBLASDatasetRecord
		if err := json.Unmarshal(records.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		var serial int
		fmt.Sscanf(record.SourceID, "s%d", &serial)
		got = append(got, serial)
	}
	if records.Err() != nil {
		t.Fatal(records.Err())
	}
	want := []int{1, 3, 0, 2, 5, 4}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("order %v, want %v", got, want)
	}
	if _, err := newFairBLASTimeOrderedRecords(file, len(input)+1); err == nil {
		t.Fatal("a record-count mismatch was accepted")
	}
	stamp, err := fairBLASRecordTime(fairBLASDatasetRecord{EventTime: "2017-06-08T10:00:01Z"}, 0)
	if err != nil || stamp != 1496916001000 {
		t.Fatalf("event time %d, %v", stamp, err)
	}
	if _, err := fairBLASRecordTime(fairBLASDatasetRecord{EventTime: "yesterday"}, 0); err == nil {
		t.Fatal("an unparsable event time was accepted")
	}
}
