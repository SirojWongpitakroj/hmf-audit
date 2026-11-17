package bitmap

import (
	"bytes"
	"encoding/gob"
	"reflect"
	"testing"
)

func TestSetGetRoundTrip(t *testing.T) {
	b := New(10)
	b.Set(0)
	b.Set(9)
	for i := uint64(0); i < 10; i++ {
		want := i == 0 || i == 9
		if got := b.Get(i); got != want {
			t.Fatalf("Get(%d) = %v, want %v", i, got, want)
		}
	}
}

func TestGobRoundTrip(t *testing.T) {
	original := New(10)
	original.Set(0)
	original.Set(9)

	var encoded bytes.Buffer
	if err := gob.NewEncoder(&encoded).Encode(original); err != nil {
		t.Fatal(err)
	}
	var decoded Bitmap
	if err := gob.NewDecoder(&encoded).Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	if !original.Equal(decoded) {
		t.Fatalf("decoded bitmap differs: got N=%d bytes=%v", decoded.Len(), decoded.Bytes())
	}
}

func TestByteLengthCeil(t *testing.T) {
	cases := map[uint64]int{0: 0, 1: 1, 7: 1, 8: 1, 9: 2, 16: 2, 17: 3}
	for n, want := range cases {
		b := New(n)
		if len(b.Bytes()) != want {
			t.Fatalf("N=%d: byte length = %d, want %d", n, len(b.Bytes()), want)
		}
	}
}

// TestLSBFirst pins down the A4 ruling precisely: bit 0 is the LSB of byte 0.
func TestLSBFirst(t *testing.T) {
	b := New(8)
	b.Set(0)
	raw := b.Bytes()
	if raw[0] != 0x01 {
		t.Fatalf("bit 0 set should produce byte 0x01 (LSB-first), got 0x%02x", raw[0])
	}
	b2 := New(8)
	b2.Set(7)
	raw2 := b2.Bytes()
	if raw2[0] != 0x80 {
		t.Fatalf("bit 7 set should produce byte 0x80, got 0x%02x", raw2[0])
	}
}

func TestUnusedTailBitsZero(t *testing.T) {
	b := New(3)
	b.Set(0)
	b.Set(1)
	b.Set(2)
	raw := b.Bytes()
	if raw[0] != 0x07 {
		t.Fatalf("expected 0x07 (bits 0-2 set, bits 3-7 zero), got 0x%02x", raw[0])
	}
}

func TestFromBytesRejectsWrongLength(t *testing.T) {
	if _, err := FromBytes(9, []byte{0x00}); err == nil {
		t.Fatalf("expected error for wrong-length input")
	}
}

func TestFromBytesRejectsStrayHighBits(t *testing.T) {
	// N=3 means only bits 0-2 may be set; bit 3 (0x08) is a stray high bit.
	if _, err := FromBytes(3, []byte{0x08}); err == nil {
		t.Fatalf("expected error for stray high bits")
	}
}

func TestAndOrNot(t *testing.T) {
	a := New(8)
	a.Set(0)
	a.Set(1)
	b := New(8)
	b.Set(1)
	b.Set(2)

	and := And(a, b)
	if !reflect.DeepEqual(and.Bytes(), []byte{0x02}) {
		t.Fatalf("AND = %x, want 02", and.Bytes())
	}
	or := Or(a, b)
	if !reflect.DeepEqual(or.Bytes(), []byte{0x07}) {
		t.Fatalf("OR = %x, want 07", or.Bytes())
	}
	not := Not(a)
	if !reflect.DeepEqual(not.Bytes(), []byte{0xFC}) {
		t.Fatalf("NOT = %x, want fc", not.Bytes())
	}
}

// TestFig2WorkedExample reproduces the IV-D worked example: ¬Error ∧ PUT ∨
// GET = 01010001 over Error=11001001, PUT=10011000, GET=01000001 (T-010).
func TestFig2WorkedExample(t *testing.T) {
	mk := func(bits string) Bitmap {
		b := New(uint64(len(bits)))
		for i, c := range bits {
			if c == '1' {
				b.Set(uint64(i))
			}
		}
		return b
	}
	toStr := func(b Bitmap) string {
		s := make([]byte, b.Len())
		for i := uint64(0); i < b.Len(); i++ {
			if b.Get(i) {
				s[i] = '1'
			} else {
				s[i] = '0'
			}
		}
		return string(s)
	}

	errBM := mk("11001001")
	putBM := mk("10011000")
	getBM := mk("01000001")

	result := Or(And(Not(errBM), putBM), getBM)
	if got := toStr(result); got != "01010001" {
		t.Fatalf("¬Error ∧ PUT ∨ GET = %s, want 01010001", got)
	}
}

func TestSetIndicesAscending(t *testing.T) {
	b := New(10)
	b.Set(5)
	b.Set(1)
	b.Set(8)
	got := b.SetIndices()
	want := []uint64{1, 5, 8}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SetIndices = %v, want %v", got, want)
	}
}

func TestEqualAndClone(t *testing.T) {
	a := New(8)
	a.Set(3)
	c := a.Clone()
	if !a.Equal(c) {
		t.Fatalf("clone should be equal")
	}
	c.Set(4)
	if a.Equal(c) {
		t.Fatalf("mutating clone should not affect original / equality")
	}
}
