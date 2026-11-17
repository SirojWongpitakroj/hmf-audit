package dht_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"testing"
	"time"

	"dhtbacked/auth"
	"dhtbacked/dht"
	"dhtbacked/internal/clock"
	"dhtbacked/internal/hashing"
)

func fixedClock() *clock.Manual {
	return &clock.Manual{T: time.Date(2026, 9, 11, 4, 30, 0, 0, time.UTC)}
}

func newCA(t *testing.T, c *clock.Manual) *auth.CA {
	t.Helper()
	dir := t.TempDir()
	priv, _, err := auth.LoadOrCreateCAKeys(dir+"/ca.sk", dir+"/ca.pk")
	if err != nil {
		t.Fatal(err)
	}
	ca, err := auth.NewCA(priv, c)
	if err != nil {
		t.Fatal(err)
	}
	return ca
}

// Q10: 16 logical nodes, replication factor 1, key -> node by the first eight
// bytes of SHA256(key) as an unsigned integer modulo 16.
func TestNodeAssignment(t *testing.T) {
	if dht.NodeCount != 16 || dht.ReplicationFactor != 1 {
		t.Fatalf("node count = %d, replication = %d", dht.NodeCount, dht.ReplicationFactor)
	}
	for i := 0; i < 500; i++ {
		var key hashing.Digest
		binary.BigEndian.PutUint64(key[:8], uint64(i))
		h := sha256.Sum256(key[:])
		want := int(binary.BigEndian.Uint64(h[:8]) % 16)
		if got := dht.NodeIndex(key); got != want {
			t.Fatalf("key %d assigned to node %d, want %d", i, got, want)
		}
		if got := dht.NodeIndex(key); got < 0 || got >= dht.NodeCount {
			t.Fatalf("node index %d out of range", got)
		}
	}
	// A stored record lives on exactly one node: replication factor 1.
	d := dht.New(nil, fixedClock())
	tid := dht.TreeID(1, "access")
	d.PutRoot(dht.RootRecord{TreeID: tid, Seq: 1, LeafCnt: 0, LogType: "access"})
	if d.Count() != 1 {
		t.Fatalf("one write produced %d stored records", d.Count())
	}
	held := 0
	for n := 0; n < dht.NodeCount; n++ {
		if d.CountAt(n) > 0 {
			held++
		}
	}
	if held != 1 {
		t.Fatalf("record is held by %d nodes, want 1", held)
	}
}

// Q45 / §0.9: keys are derived without seq.
func TestKeyDerivationOmitsSeq(t *testing.T) {
	tid := dht.TreeID(7, "auth")
	u := func(v uint64) []byte {
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], v)
		return b[:]
	}
	if tid != hashing.H([]byte("TREE"), u(7), []byte("auth")) {
		t.Fatal("treeID is not H(\"TREE\" || epoch || logType)")
	}
	if dht.AncestorID(tid, 8, 3) != hashing.H([]byte("ANC"), tid[:], u(8), u(3)) {
		t.Fatal("ancestorID is wrong")
	}
	if dht.KeyP(tid, 9, 5) != hashing.H([]byte("P"), tid[:], u(9), u(5)) {
		t.Fatal("Key_P is wrong")
	}
	if dht.KeyC(tid, 8, 3) != hashing.H([]byte("C"), tid[:], u(8), u(3)) {
		t.Fatal("Key_C is wrong")
	}
	if dht.KeyR(tid) != hashing.H([]byte("R"), tid[:]) {
		t.Fatal("Key_R is wrong")
	}
	if dht.KeyM(tid, 2) != hashing.H([]byte("M"), tid[:], u(2)) {
		t.Fatal("Key_M is wrong")
	}
	// Keys are stable: the same logical object always has the same key,
	// whatever seq the record inside carries.
	if dht.KeyP(tid, 9, 5) != dht.KeyP(tid, 9, 5) {
		t.Fatal("Key_P is not stable")
	}
	// Distinct epochs and types occupy distinct keys.
	if dht.TreeID(7, "auth") == dht.TreeID(8, "auth") || dht.TreeID(7, "auth") == dht.TreeID(7, "api") {
		t.Fatal("treeID collides across epoch or log type")
	}
}

// Q45: a newer write overwrites the previous value at the same key; seq is
// version metadata inside the record.
func TestNewerWriteOverwritesAtTheSameKey(t *testing.T) {
	d := dht.New(nil, fixedClock())
	tid := dht.TreeID(1, "access")
	var v1, v2 hashing.Digest
	v1[0], v2[0] = 1, 2
	d.PutParent(dht.ParentNode{TreeID: tid, J: 9, K: 3, Node: v1, CommitSeq: 1})
	d.PutParent(dht.ParentNode{TreeID: tid, J: 9, K: 3, Node: v2, CommitSeq: 2})
	if d.Count() != 1 {
		t.Fatalf("two writes to one logical object left %d records", d.Count())
	}
	raw, err := d.GetRaw(dht.KeyP(tid, 9, 3))
	if err != nil {
		t.Fatal(err)
	}
	p, err := dht.ParseParentNode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.Node != v2 || p.CommitSeq != 2 {
		t.Fatal("the newer write did not overwrite the previous value")
	}
}

// Q23: Fig. 2's Prev Hash field is not implemented. Every record's serialized
// length is fully accounted for by its specified fields.
func TestRecordSerializationHasNoExtraField(t *testing.T) {
	tid := dht.TreeID(1, "access")
	p := dht.ParentNode{TreeID: tid, J: 9, K: 3, CommitSeq: 7}
	if got, want := len(p.Serialize()), 1+32+8+8+32+8; got != want {
		t.Errorf("P_j,k is %d bytes, want %d", got, want)
	}
	rr := dht.RootRecord{TreeID: tid, Seq: 3, LeafCnt: 1000, LogType: "access"}
	if got, want := len(rr.Serialize()), 1+32+8+32+8+4+len("access"); got != want {
		t.Errorf("root record is %d bytes, want %d", got, want)
	}
	m := dht.MicroRootRecord{TreeID: tid, WindowID: 0, IMin: 0, IMax: 999, Seq: 5}
	if got, want := len(m.Serialize()), 1+32+8+8+8+32+8; got != want {
		t.Errorf("micro-root record is %d bytes, want %d", got, want)
	}
	c := dht.AncestorCert{TreeID: tid, Level: 8, Index: 1, UpProof: make([]hashing.Digest, 8)}
	if got, want := len(c.Serialize()), 1+32+32+8+8+32+8+8*32+8+8+8; got != want {
		t.Errorf("certificate is %d bytes, want %d", got, want)
	}
}

// R-DHT-06: every DHT item can be locally rehashed and round-tripped.
func TestRecordsRoundTripDeterministically(t *testing.T) {
	tid := dht.TreeID(4, "error")
	var node, root, micro, anc hashing.Digest
	node[0], root[0], micro[0], anc[0] = 1, 2, 3, 4

	p := dht.ParentNode{TreeID: tid, J: 10, K: 7, Node: node, CommitSeq: 11}
	gotP, err := dht.ParseParentNode(p.Serialize())
	if err != nil || gotP != p {
		t.Fatalf("P round trip: %v %+v", err, gotP)
	}
	rr := dht.RootRecord{TreeID: tid, Seq: 12, Root: root, LeafCnt: 900, LogType: "error"}
	gotR, err := dht.ParseRootRecord(rr.Serialize())
	if err != nil || gotR != rr {
		t.Fatalf("root record round trip: %v", err)
	}
	m := dht.MicroRootRecord{TreeID: tid, WindowID: 1, IMin: 1024, IMax: 1100, MicroRoot: micro, Seq: 13}
	gotM, err := dht.ParseMicroRootRecord(m.Serialize())
	if err != nil || gotM != m {
		t.Fatalf("micro-root round trip: %v", err)
	}
	c := dht.AncestorCert{
		AncestorID: anc, TreeID: tid, Level: 8, Index: 2, AncestorHash: node,
		UpProof: []hashing.Digest{node, root, micro, anc, node, root, micro, anc},
		IMin:    8, IMax: 11, CommitSeq: 14,
	}
	gotC, err := dht.ParseAncestorCert(c.Serialize())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotC.Serialize(), c.Serialize()) {
		t.Fatal("certificate round trip changed the bytes")
	}
	// Repeated serialization of the same logical record is byte-identical.
	first := c.Serialize()
	for i := 0; i < 50; i++ {
		if !bytes.Equal(first, c.Serialize()) {
			t.Fatal("certificate serialization is not deterministic")
		}
	}
	if _, err := dht.ParseParentNode(rr.Serialize()); err == nil {
		t.Fatal("a root record parsed as a parent node record")
	}
	if _, err := dht.ParseParentNode(append(p.Serialize(), 0x00)); err == nil {
		t.Fatal("trailing bytes were accepted")
	}
}

func TestMissingKeyIsNotFound(t *testing.T) {
	d := dht.New(nil, fixedClock())
	if _, err := d.GetRaw(dht.KeyR(dht.TreeID(1, "access"))); err != dht.ErrNotFound {
		t.Fatalf("absent key returned %v", err)
	}
}
