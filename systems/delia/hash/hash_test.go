package hash

import (
	"bytes"
	"testing"
)

func TestHDeterministic(t *testing.T) {
	a := H([]byte("hello"))
	b := H([]byte("hello"))
	if a != b {
		t.Fatalf("H is not deterministic: %x != %x", a, b)
	}
}

func TestHChangesWithInput(t *testing.T) {
	a := H([]byte("hello"))
	b := H([]byte("hellO"))
	if a == b {
		t.Fatalf("H did not change for different input")
	}
}

func TestHKnownVector(t *testing.T) {
	// SHA-256("") = e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b85
	got := H([]byte(""))
	want := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	gotHex := bytesToHex(got[:])
	if gotHex != want {
		t.Fatalf("H(\"\") = %s, want %s", gotHex, want)
	}
}

func bytesToHex(b []byte) string {
	const hexDigits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, c := range b {
		out[i*2] = hexDigits[c>>4]
		out[i*2+1] = hexDigits[c&0x0f]
	}
	return string(out)
}

func TestConcatRawBytes(t *testing.T) {
	got := Concat([]byte{0x01, 0x02}, []byte{0x03})
	want := []byte{0x01, 0x02, 0x03}
	if !bytes.Equal(got, want) {
		t.Fatalf("Concat = %v, want %v", got, want)
	}
}

func TestZeroSentinel(t *testing.T) {
	var z Digest
	if z != Zero {
		t.Fatalf("Zero is not the all-zero digest")
	}
}
