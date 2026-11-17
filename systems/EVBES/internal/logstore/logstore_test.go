package logstore

import (
	"bytes"
	"testing"

	"evseb/internal/crypto"
)

func TestLIDsUniqueAndOrdered(t *testing.T) {
	var a LIDAssigner
	seen := map[string]bool{}
	prev := ""
	for range 2000 {
		lid := a.Next()
		if seen[lid] || len(lid) != 12 || lid <= prev {
			t.Fatalf("bad LID %q after %q", lid, prev)
		}
		seen[lid] = true
		prev = lid
	}
}

func TestEncryptLogEq14Eq15(t *testing.T) {
	keys, _ := crypto.GenerateKeys()
	var a LIDAssigner
	lg := Log{LID: a.Next(), Content: "deny tcp 10.0.0.1", Ti: 1758240000, Si: "fw01", LType: "Firewall Logs"}
	e, err := EncryptLog(lg, keys.Kenc)
	if err != nil {
		t.Fatal(err)
	}
	if e.LID != lg.LID {
		t.Fatal("LID not carried")
	}
	for _, ct := range [][]byte{e.CTLog, e.CTMetadata} {
		for _, s := range []string{"deny", "fw01", "Firewall"} {
			if bytes.Contains(ct, []byte(s)) {
				t.Fatalf("plaintext %q visible in ciphertext", s)
			}
		}
	}
	pt, _ := crypto.DecAES(e.CTLog, keys.Kenc)
	if string(pt) != lg.Content {
		t.Fatal("CTLog is not EncAES(Ln, Kenc)")
	}
	got, err := DecryptLog(e, keys.Kenc)
	if err != nil || got != lg {
		t.Fatalf("DecryptLog = %+v, %v", got, err)
	}
	if g, err := DecryptLog(e, keys.Kindex); err == nil && g == lg {
		t.Fatal("log decrypts under Kindex")
	}
	if _, err := EncryptLog(Log{Content: "x"}, keys.Kenc); err == nil {
		t.Fatal("log without LID accepted")
	}
}

func TestStorage(t *testing.T) {
	s := NewStorage()
	s.Put(EncryptedLog{LID: "000000000001", CTLog: []byte{1}})
	s.Put(EncryptedLog{LID: "000000000000", CTLog: []byte{0}})
	e, ok := s.Get("000000000001")
	if !ok || e.CTLog[0] != 1 {
		t.Fatal("Get failed")
	}
	e.CTLog[0] = 7
	if e2, _ := s.Get("000000000001"); e2.CTLog[0] != 1 {
		t.Fatal("Get returned aliased bytes")
	}
	if l := s.LIDs(); len(l) != 2 || l[0] != "000000000000" {
		t.Fatalf("LIDs = %v", l)
	}
	s.Delete("000000000000")
	if _, ok := s.Get("000000000000"); ok || s.Len() != 1 {
		t.Fatal("Delete failed")
	}
}
