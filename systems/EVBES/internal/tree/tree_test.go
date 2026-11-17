package tree

import (
	"bytes"
	"reflect"
	"slices"
	"testing"

	"evseb/internal/crypto"
	"evseb/internal/index"
	"evseb/internal/logstore"
	"evseb/internal/trapdoor"
)

func TestTreeSetupAlg1(t *testing.T) {
	tr := TreeSetup(512, 4)
	if len(tr.PrimaryNodes) != 3 {
		t.Fatalf("want 3 primary nodes, got %d", len(tr.PrimaryNodes))
	}
	for _, lt := range SeedLTypes {
		n := tr.PrimaryNodes[lt]
		if n == nil || n.LType != lt || n.InvertedIndex == nil || n.SubNodes == nil {
			t.Fatalf("primary node %q malformed", lt)
		}
	}
}

func build(t *testing.T) (*Tree, [32]byte) {
	t.Helper()
	k, _ := crypto.Rand256()
	tr := TreeSetup(1024, 5)
	logs := []struct {
		lg logstore.Log
		kw []string
	}{
		{logstore.Log{LID: "000000000000", Si: "fw01", LType: "Firewall Logs"}, []string{"deny", "tcp"}},
		{logstore.Log{LID: "000000000001", Si: "fw01", LType: "Firewall Logs"}, []string{"allow", "tcp"}},
		{logstore.Log{LID: "000000000002", Si: "fw02", LType: "Firewall Logs"}, []string{"deny", "udp"}},
		{logstore.Log{LID: "000000000003", Si: "apache", LType: "Web Server Logs"}, []string{"get"}},
	}
	for _, x := range logs {
		InsertIndices(tr, x.lg.LType, x.lg, x.kw, k)
	}
	return tr, k
}

func TestInsertIndicesAlg2(t *testing.T) {
	tr, k := build(t)
	// Line 1-5: unknown LType creates a primary node.
	if tr.PrimaryNodes["Web Server Logs"] == nil {
		t.Fatal("primary node not created for new LType")
	}
	fw := tr.PrimaryNodes["Firewall Logs"]
	if len(fw.SubNodes) != 2 {
		t.Fatalf("want sub-nodes fw01, fw02; got %d", len(fw.SubNodes))
	}
	// B2: primary-level INL stays empty.
	if len(fw.InvertedIndex.Entries) != 0 {
		t.Fatal("Alg. 2 must not populate the primary-node index")
	}
	s := fw.SubNodes["fw01"]
	tcp := trapdoor.Generate("tcp", k).Tw
	if got := s.InvertedIndex.Get(tcp); !slices.Equal(got, []string{"000000000000", "000000000001"}) {
		t.Fatalf("In(fw01)[tcp] = %v", got)
	}
	if !s.BloomFilter.Check(tcp) {
		t.Fatal("Bloom filter missing PRF(h(tcp))")
	}
	// B3a / Theorem 5: only PRF outputs are keys; plaintext and h(w) are not.
	h := crypto.H([]byte("tcp"))
	for key := range s.InvertedIndex.Entries {
		if key == "tcp" || key == index.Key(h[:]) {
			t.Fatal("index keyed by plaintext or unkeyed hash")
		}
	}
}

// Q8 ruling: LocateTreeNode resolves the LType's primary node; the Bloom
// test runs on every sub-node, and positives are retrieved.
func TestLocateCheckRetrieveAcrossSubNodes(t *testing.T) {
	tr, k := build(t)
	n := LocateTreeNode(tr, "Firewall Logs")
	if n == nil || n.LType != "Firewall Logs" {
		t.Fatalf("LocateTreeNode = %+v", n)
	}
	subs := n.SubNodesInOrder()
	if len(subs) != 2 || subs[0].Key != "fw01" || subs[1].Key != "fw02" {
		t.Fatalf("sub-nodes %v", subs)
	}
	// "deny" is under both fw01 and fw02: both Bloom tests are positive and
	// both retrieve.
	deny := trapdoor.Generate("deny", k).Tw
	var got []string
	for _, s := range subs {
		if BloomFilterCheck(s, deny) {
			got = append(got, RetrieveFromIndex(s, deny)...)
		}
	}
	if !slices.Equal(got, []string{"000000000000", "000000000002"}) {
		t.Fatalf("deny across sub-nodes = %v", got)
	}
	// "udp" only under fw02: fw01's filter is negative and is skipped.
	udp := trapdoor.Generate("udp", k).Tw
	if BloomFilterCheck(subs[0], udp) || !BloomFilterCheck(subs[1], udp) {
		t.Fatal("udp Bloom tests wrong")
	}
	absent := trapdoor.Generate("nothere", k).Tw
	for _, s := range subs {
		if BloomFilterCheck(s, absent) {
			t.Fatal("absent keyword passed a Bloom check")
		}
	}
	if LocateTreeNode(tr, "No Such Logs") != nil || BloomFilterCheck(nil, udp) || RetrieveFromIndex(nil, udp) != nil {
		t.Fatal("unknown LType / nil node handling")
	}
	// Lall under the primary node spans both sub-nodes.
	if !slices.Equal(n.AllLIDs(), []string{"000000000000", "000000000001", "000000000002"}) {
		t.Fatalf("primary AllLIDs = %v", n.AllLIDs())
	}
}

func TestEncryptEq17RoundTrip(t *testing.T) {
	tr, _ := build(t)
	kindex, _ := crypto.Rand256()
	e, err := Encrypt(tr, kindex)
	if err != nil {
		t.Fatal(err)
	}
	for _, ct := range [][]byte{e.CTT, e.CTBF, e.CTInv} {
		if bytes.Contains(ct, []byte("Firewall")) || bytes.Contains(ct, []byte("000000000001")) {
			t.Fatal("plaintext visible in encrypted index")
		}
	}
	got, err := Decrypt(e, kindex)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, tr) {
		t.Fatal("decrypted tree differs from original")
	}
	other, _ := crypto.Rand256()
	if g, err := Decrypt(e, other); err == nil && reflect.DeepEqual(g, tr) {
		t.Fatal("index decrypts under wrong key")
	}
	// Determinism of the plaintext encoding: two encryptions of the same tree
	// decrypt to identical trees.
	e2, _ := Encrypt(tr, kindex)
	g2, _ := Decrypt(e2, kindex)
	if !reflect.DeepEqual(g2, got) {
		t.Fatal("encoding not deterministic")
	}
}
