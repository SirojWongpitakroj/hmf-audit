package benchmark

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFairDHTMaxQ(t *testing.T) {
	if got, err := fairDHTMaxQ(""); err != nil || got != 2048 {
		t.Fatalf("default = %d, %v; want 2048", got, err)
	}
	if got, err := fairDHTMaxQ("4096"); err != nil || got != 4096 {
		t.Fatalf("override = %d, %v", got, err)
	}
	for _, bad := range []string{"0", "-1", "x", "1.5"} {
		if _, err := fairDHTMaxQ(bad); err == nil {
			t.Fatalf("FAIR_DHT_MAX_Q=%q accepted", bad)
		}
	}
}

func TestFairDHTNativeCasesCapAndOrder(t *testing.T) {
	dir := t.TempDir()
	file := `{"queries":[
	 {"scheme":"EVSEB","shape":"clustered","q":1,"set":1,"record_ordinals":[7]},
	 {"scheme":"DHT-backed","shape":"scattered","q":2,"set":2,"record_ordinals":[5,9]},
	 {"scheme":"DHT-backed","shape":"scattered","q":2,"set":1,"record_ordinals":[4,8]},
	 {"scheme":"DHT-backed","shape":"clustered","q":1,"set":1,"record_ordinals":[3]},
	 {"scheme":"DHT-backed","shape":"clustered","q":4096,"set":1,"record_ordinals":[]}
	]}`
	if err := os.WriteFile(filepath.Join(dir, "dht_evseb_queries.json"), []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	// q = 4096 is above the cap, so its (inconsistent) entry is skipped, not an error.
	cases, err := fairDHTNativeCases(dir, 2048)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 2 || cases[0].Shape != "clustered" || cases[0].Q != 1 || cases[1].Shape != "scattered" || cases[1].Q != 2 {
		t.Fatalf("cases = %+v", cases)
	}
	if got := cases[1].Sets; len(got) != 2 || got[0].Set != 1 || got[1].Set != 2 || got[0].IDs[0] != "000000000004" {
		t.Fatalf("sets = %+v", got)
	}
	// Above the cap it is checked: a q = 4096 entry with no records is rejected.
	if _, err := fairDHTNativeCases(dir, 4096); err == nil {
		t.Fatal("a q=4096 entry listing 0 records was accepted")
	}
}
