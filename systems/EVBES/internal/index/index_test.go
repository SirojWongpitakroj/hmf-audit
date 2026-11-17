package index

import (
	"slices"
	"testing"
)

func TestAddGetAndInit(t *testing.T) {
	ix := New()
	tw1, tw2 := []byte{1}, []byte{2}
	if ix.Has(tw1) {
		t.Fatal("fresh index has entry")
	}
	ix.Init(tw1)
	if !ix.Has(tw1) || len(ix.Get(tw1)) != 0 {
		t.Fatal("Init did not create empty entry")
	}
	ix.Add(tw1, "000000000001")
	ix.Add(tw1, "000000000002")
	ix.Add(tw1, "000000000001")
	ix.Add(tw2, "000000000003")
	if got := ix.Get(tw1); !slices.Equal(got, []string{"000000000001", "000000000002"}) {
		t.Fatalf("Get = %v", got)
	}
	if ix.Get([]byte{9}) != nil {
		t.Fatal("absent trapdoor returned LIDs")
	}
	if got := ix.AllLIDs(); !slices.Equal(got, []string{"000000000001", "000000000002", "000000000003"}) {
		t.Fatalf("AllLIDs = %v", got)
	}
}
