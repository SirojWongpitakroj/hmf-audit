package serial_test

import (
	"bytes"
	"testing"

	"dhtbacked/internal/hashing"
	"dhtbacked/internal/serial"
)

func TestFieldEncodings(t *testing.T) {
	w := serial.NewWriter(serial.TypeParentNode)
	w.Uint64(1)
	w.String("ab")
	var d hashing.Digest
	d[0] = 0xFF
	w.Digest(d)
	got := w.Result()

	want := []byte{serial.TypeParentNode,
		0, 0, 0, 0, 0, 0, 0, 1,
		0, 0, 0, 2, 'a', 'b'}
	want = append(want, d[:]...)
	if !bytes.Equal(got, want) {
		t.Fatalf("encoding = % x, want % x", got, want)
	}
}

func TestRoundTripDeterminism(t *testing.T) {
	build := func() []byte {
		w := serial.NewWriter(serial.TypeRootRecord)
		w.Uint64(7)
		w.String("access")
		w.Bytes([]byte{1, 2, 3})
		return w.Result()
	}
	first := build()
	for i := 0; i < 100; i++ {
		if !bytes.Equal(first, build()) {
			t.Fatal("serialization is not deterministic across repetitions")
		}
	}
	r, err := serial.NewReader(first, serial.TypeRootRecord)
	if err != nil {
		t.Fatal(err)
	}
	if v, err := r.Uint64(); err != nil || v != 7 {
		t.Fatalf("uint64 round trip: %v %v", v, err)
	}
	if s, err := r.String(); err != nil || s != "access" {
		t.Fatalf("string round trip: %q %v", s, err)
	}
	if b, err := r.Bytes(); err != nil || !bytes.Equal(b, []byte{1, 2, 3}) {
		t.Fatalf("bytes round trip: %v %v", b, err)
	}
	if err := r.End(); err != nil {
		t.Fatalf("unconsumed bytes remain: %v", err)
	}
}

func TestWrongRecordTypePrefixRejected(t *testing.T) {
	w := serial.NewWriter(serial.TypeParentNode)
	w.Uint64(1)
	if _, err := serial.NewReader(w.Result(), serial.TypeRootRecord); err == nil {
		t.Fatal("reader accepted the wrong record-type prefix")
	}
}
