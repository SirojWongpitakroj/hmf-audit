package integrity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFairEVBESNativeCases(t *testing.T) {
	dir := t.TempDir()
	file := `{"queries":[
	 {"scheme":"DHT-backed","shape":"clustered","q":1,"set":1,"record_ordinals":[3]},
	 {"scheme":"EVSEB","shape":"scattered","q":2,"set":2,"record_ordinals":[5,9]},
	 {"scheme":"EVSEB","shape":"scattered","q":2,"set":1,"record_ordinals":[4,8]},
	 {"scheme":"EVSEB","shape":"clustered","q":1,"set":1,"record_ordinals":[3]}
	]}`
	if err := os.WriteFile(filepath.Join(dir, "dht_evseb_queries.json"), []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	cases, err := fairEVBESNativeCases(dir, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 2 || cases[0].Shape != "clustered" || cases[1].Shape != "scattered" || cases[1].Q != 2 {
		t.Fatalf("cases = %+v", cases)
	}
	if sets := cases[1].Sets; len(sets) != 2 || sets[0].Set != 1 || sets[0].LIDs[0] != "000000000004" {
		t.Fatalf("sets = %+v", sets)
	}
	// An ordinal beyond the seeded leaves means the query file is for another dataset.
	if _, err := fairEVBESNativeCases(dir, 8); err == nil {
		t.Fatal("ordinal 9 accepted with only 8 leaves")
	}
}
