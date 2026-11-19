package eval

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFairBLASNativeScenarios(t *testing.T) {
	dir := t.TempDir()
	file := `{"queries":[
	 {"scheme":"HMF-Audit","shape":"clustered","q":1,"set":1,"record_ordinals":[1]},
	 {"scheme":"BLAS","shape":"scattered","q":4096,"note":"only 3 distinct queries exist"},
	 {"scheme":"BLAS","shape":"scattered","q":2,"set":2,"keyword_operator":"OR","keyword_terms":["info","task"],"time_range_ms":[1483228800010,1483228800090],"record_ordinals":[10,90]},
	 {"scheme":"BLAS","shape":"scattered","q":2,"set":1,"keyword_operator":"AND","keyword_terms":["info","other"],"time_range_ms":[1483228800001,1483228800003],"record_ordinals":[1,3]},
	 {"scheme":"BLAS","shape":"clustered","q":1,"set":1,"keyword_operator":"ALL","keyword_terms":["info","task","other"],"time_range_ms":[1483228800005,1483228800005],"record_ordinals":[5]}
	]}`
	if err := os.WriteFile(filepath.Join(dir, "R0_queries.json"), []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	scenarios, err := fairBLASNativeScenarios(dir, []string{"info", "task"})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, scenario := range scenarios {
		names = append(names, scenario.name())
	}
	want := []string{"clustered/q=1/set=1", "scattered/q=2/set=1", "scattered/q=2/set=2"}
	if len(names) != len(want) {
		t.Fatalf("scenarios = %v, want %v", names, want)
	}
	for index := range want {
		if names[index] != want[index] {
			t.Fatalf("scenarios = %v, want %v", names, want)
		}
	}
	first := scenarios[1]
	if !first.request.HasRange || first.request.TS != 1483228800001 || first.request.TE != 1483228800003 ||
		first.request.FK == nil || first.expectedTS[1] != 1483228800003 {
		t.Fatalf("request = %+v expected = %v", first.request, first.expectedTS)
	}
	// A keyword outside the vocabulary means the file is for another dataset.
	if _, err := fairBLASNativeScenarios(dir, []string{"info"}); err == nil {
		t.Fatal("keyword 'task' accepted although it is not in the vocabulary")
	}
}

func TestFairBLASSameTimestamps(t *testing.T) {
	if err := fairBLASSameTimestamps([]int64{3, 1, 2}, []int64{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if fairBLASSameTimestamps([]int64{1, 2}, []int64{1, 3}) == nil || fairBLASSameTimestamps([]int64{1}, []int64{1, 2}) == nil {
		t.Fatal("different timestamp sets accepted")
	}
}
