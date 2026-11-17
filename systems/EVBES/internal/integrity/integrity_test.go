package integrity

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"evseb/internal/blockchain"
	"evseb/internal/crypto"
	"evseb/internal/logstore"
)

func sha(parts ...[]byte) []byte { h := sha256.Sum256(bytes.Join(parts, nil)); return h[:] }

func leaves(n int) ([]string, [][]byte) {
	lids := make([]string, n)
	ls := make([][]byte, n)
	for i := range n {
		lids[i] = fmt.Sprintf("%012d", i)
		ls[i] = sha([]byte(lids[i]))
	}
	return lids, ls
}

func TestDHTEq28(t *testing.T) {
	d := NewDHT()
	d.Put("000000000007", []byte{1, 2})
	h := sha256.Sum256([]byte("000000000007"))
	if v, ok := d.Entries[hex.EncodeToString(h[:])]; !ok || !bytes.Equal(v, []byte{1, 2}) {
		t.Fatal("DHT not keyed by hex(H(LID))")
	}
	if v, ok := d.Get("000000000007"); !ok || !bytes.Equal(v, []byte{1, 2}) {
		t.Fatal("Get failed")
	}
}

func TestQuaternaryShape(t *testing.T) {
	// Fig. 3: 8 leaves -> 2 internal nodes -> root.
	lids, l := leaves(8)
	m := Build(lids, l)
	if len(m.Levels) != 3 || len(m.Levels[1]) != 2 {
		t.Fatalf("8 leaves: levels %d, level1 %d", len(m.Levels), len(m.Levels[1]))
	}
	p0 := sha(l[0], l[1], l[2], l[3])
	p1 := sha(l[4], l[5], l[6], l[7])
	if !bytes.Equal(m.Root, sha(p0, p1, p1, p1)) {
		t.Fatal("root is not the quaternary hash with B14 padding")
	}
	if bytes.Equal(m.Root, sha(sha(sha(l[0], l[1]), sha(l[2], l[3])), sha(sha(l[4], l[5]), sha(l[6], l[7])))) {
		t.Fatal("tree is binary")
	}
	lids, l = leaves(5)
	m = Build(lids, l)
	q0, q1 := sha(l[0], l[1], l[2], l[3]), sha(l[4], l[4], l[4], l[4])
	if !bytes.Equal(m.Root, sha(q0, q1, q1, q1)) {
		t.Fatal("B14 padding for 5 leaves wrong")
	}
	lids, l = leaves(1)
	if m = Build(lids, l); !bytes.Equal(m.Root, l[0]) {
		t.Fatal("single-leaf root")
	}
	if Build(nil, nil).Root != nil {
		t.Fatal("empty root not nil")
	}
	lids, l = leaves(16)
	if m = Build(lids, l); len(m.Levels) != 3 || len(m.Levels[1]) != 4 {
		t.Fatal("16 leaves should give 16/4/1")
	}
}

// Theorem 2 / Q9: every leaf's proof path folds to R; any other leaf value
// does not.
func TestVerifyProof(t *testing.T) {
	for _, n := range []int{1, 2, 3, 4, 5, 8, 16, 17, 21, 64, 65} {
		lids, l := leaves(n)
		m := Build(lids, l)
		for i := range n {
			if !VerifyProof(m, i, l[i], m.Root) {
				t.Fatalf("n=%d: proof of leaf %d fails", n, i)
			}
			if VerifyProof(m, i, sha([]byte("x")), m.Root) {
				t.Fatalf("n=%d: forged leaf %d verifies", n, i)
			}
		}
		if VerifyProof(m, n, l[0], m.Root) || VerifyProof(m, -1, l[0], m.Root) {
			t.Fatalf("n=%d: out-of-range index verifies", n)
		}
	}
}

type fixture struct {
	storage *logstore.Storage
	dht     *DHT
	chain   *blockchain.Memory
	stored  *MerkleTree
	lids    []string
}

func newFixture(t testing.TB, n int) *fixture {
	t.Helper()
	keys, _ := crypto.GenerateKeys()
	f := &fixture{storage: logstore.NewStorage(), dht: NewDHT(), chain: blockchain.NewMemory(keys.Kauth, keys.Kindex, time.Now)}
	var a logstore.LIDAssigner
	for i := range n {
		lg := logstore.Log{LID: a.Next(), Content: fmt.Sprintf("log %d deny tcp", i), Si: "fw01", LType: "Firewall Logs"}
		e, err := logstore.EncryptLog(lg, keys.Kenc)
		if err != nil {
			t.Fatal(err)
		}
		f.storage.Put(e)
		f.dht.Put(lg.LID, HashLog(e.CTLog))
		f.lids = append(f.lids, lg.LID)
	}
	var err error
	if f.stored, err = AnchorRoot(f.lids, f.dht, f.chain); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) tamper(lid string) {
	e, _ := f.storage.Get(lid)
	e.CTLog[len(e.CTLog)-1] ^= 0xff
	f.storage.Put(e)
}

func (f *fixture) verify(t *testing.T, cands []string) ([]string, bool) {
	t.Helper()
	v, ok, err := IdentifyTamperedFiles(cands, f.stored, f.storage, f.chain)
	if err != nil {
		t.Fatal(err)
	}
	return v, ok
}

func TestAnchoredTreeIsBuiltFromDHT(t *testing.T) {
	f := newFixture(t, 9)
	r, _ := f.chain.VerifyIntegrity()
	if !bytes.Equal(r, f.stored.Root) {
		t.Fatal("on-chain R differs from the stored tree root")
	}
	for i, lid := range f.lids {
		if v, _ := f.dht.Get(lid); !bytes.Equal(v, f.stored.Leaves[i]) {
			t.Fatalf("leaf %d is not the DHT hLID", i)
		}
	}
}

func TestNoTamperGlobalAndSubset(t *testing.T) {
	f := newFixture(t, 37)
	if v, ok := f.verify(t, f.lids); !ok || !slices.Equal(v, f.lids) {
		t.Fatalf("untampered global: ok=%v", ok)
	}
	// Q9: a subset's proof paths recompute exactly R, so an untampered subset
	// verifies with Integrity=TRUE and no trace.
	cands := []string{f.lids[3], f.lids[10], f.lids[11], f.lids[36]}
	root, _, unknown := RecomputeMerkleRoot(cands, f.stored, f.storage)
	if !bytes.Equal(root, f.stored.Root) || unknown != nil {
		t.Fatal("subset proof paths do not recompute R")
	}
	if v, ok := f.verify(t, cands); !ok || !slices.Equal(v, cands) {
		t.Fatalf("untampered subset: %v %v", v, ok)
	}
}

func TestTamperLocalization(t *testing.T) {
	for _, n := range []int{1, 2, 4, 5, 16, 17, 64, 100} {
		f := newFixture(t, n)
		var want []string
		for i := 0; i < n; i += 3 {
			f.tamper(f.lids[i])
			want = append(want, f.lids[i])
		}
		v, ok := f.verify(t, f.lids)
		if ok {
			t.Fatalf("n=%d: tamper not detected", n)
		}
		for _, lid := range v {
			if slices.Contains(want, lid) {
				t.Fatalf("n=%d: tampered %s kept", n, lid)
			}
		}
		if len(v)+len(want) != n {
			t.Fatalf("n=%d: %d verified + %d tampered != n", n, len(v), len(want))
		}
		_, computed, _ := RecomputeMerkleRoot(f.lids, f.stored, f.storage)
		if got := TraceTamperedNode(computed, f.stored.Root); !slices.Equal(got, want) {
			t.Fatalf("n=%d: TraceTamperedNode = %v want %v", n, got, want)
		}
	}
}

// Q10: at Level 0 only leaves under the affected level-1 branches are
// inspected.
func TestTraceInspectsOnlyAffectedBranches(t *testing.T) {
	f := newFixture(t, 64)
	f.tamper(f.lids[41])
	_, computed, _ := RecomputeMerkleRoot(f.lids, f.stored, f.storage)
	tampered, inspected := trace(computed, f.stored.Root)
	if !slices.Equal(tampered, []string{f.lids[41]}) {
		t.Fatalf("tampered = %v", tampered)
	}
	// 64 leaves -> 16 -> 4 -> 1: one path of affected nodes; leaf 41 sits under
	// level-1 node 10, whose children are leaves 40..43.
	if !slices.Equal(inspected, []int{40, 41, 42, 43}) {
		t.Fatalf("inspected leaves %v, want [40 41 42 43]", inspected)
	}
}

func TestCandidateSubsetTamper(t *testing.T) {
	f := newFixture(t, 50)
	cands := []string{f.lids[3], f.lids[10], f.lids[11], f.lids[40]}
	f.tamper(f.lids[10])
	f.tamper(f.lids[20]) // not a candidate: must not affect this result
	v, ok := f.verify(t, cands)
	if ok || !slices.Equal(v, []string{f.lids[3], f.lids[11], f.lids[40]}) {
		t.Fatalf("subset tamper: %v %v", v, ok)
	}
}

// Q9: changing a file AND its DHT entry together is detected, because the
// candidate is checked against the on-chain root R, not against the DHT.
func TestCoordinatedFileAndDHTTamperDetected(t *testing.T) {
	f := newFixture(t, 30)
	lid := f.lids[13]
	f.tamper(lid)
	e, _ := f.storage.Get(lid)
	f.dht.Put(lid, HashLog(e.CTLog)) // DHT now agrees with the tampered file
	for _, cands := range [][]string{f.lids, {f.lids[12], lid, f.lids[14]}} {
		v, ok := f.verify(t, cands)
		if ok || slices.Contains(v, lid) || len(v) != len(cands)-1 {
			t.Fatalf("coordinated tamper not detected: %v %v", v, ok)
		}
	}
}

func TestDeletedAndUnanchoredCandidates(t *testing.T) {
	f := newFixture(t, 9)
	f.storage.Delete(f.lids[2])
	v, ok := f.verify(t, append(slices.Clone(f.lids), "999999999999"))
	if ok || slices.Contains(v, f.lids[2]) || slices.Contains(v, "999999999999") || len(v) != 8 {
		t.Fatalf("deleted/unanchored not excluded: %v %v", v, ok)
	}
}

// A DHT entry changed alone (file intact) does not exclude the file: the file
// still verifies against R.
func TestDHTOnlyChangeDoesNotExcludeIntactFile(t *testing.T) {
	f := newFixture(t, 9)
	f.dht.Put(f.lids[5], []byte("forged"))
	if v, ok := f.verify(t, f.lids); !ok || len(v) != 9 {
		t.Fatalf("intact file excluded: %v %v", v, ok)
	}
}

// A corrupted stored tree (off-chain) fails closed: candidates whose proof
// path runs through the corrupted node are excluded, and Integrity is FALSE.
func TestCorruptStoredSiblingFailsClosed(t *testing.T) {
	f := newFixture(t, 16)
	f.stored.Levels[0][1] = sha([]byte("corrupt")) // sibling of leaf 0; root left unchanged
	v, ok := f.verify(t, []string{f.lids[0], f.lids[8]})
	if ok || slices.Contains(v, f.lids[0]) || !slices.Contains(v, f.lids[8]) {
		t.Fatalf("corrupt sibling: %v %v", v, ok)
	}
}

func TestStoredTreeMustMatchOnChainRoot(t *testing.T) {
	f := newFixture(t, 5)
	other := newFixture(t, 5)
	if _, _, err := IdentifyTamperedFiles(f.lids, other.stored, f.storage, f.chain); !errors.Is(err, ErrStoredTreeMismatch) {
		t.Fatalf("want ErrStoredTreeMismatch, got %v", err)
	}
	keys, _ := crypto.GenerateKeys()
	if _, _, err := IdentifyTamperedFiles(f.lids, f.stored, f.storage, blockchain.NewMemory(keys.Kauth, keys.Kindex, time.Now)); !errors.Is(err, blockchain.ErrNoRoot) {
		t.Fatalf("want ErrNoRoot, got %v", err)
	}
}
