package encryptor_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"dhtbacked/encryptor"
)

func sampleRecord() encryptor.Record {
	return encryptor.Record{
		RecordID:   "000000000000",
		ProducerID: "producer-access",
		LogType:    "access",
		// 2026-09-11T04:30:00.500000000Z
		TimestampNanos: 1789101000500000000,
		Message:        "access event 17",
		Attributes: encryptor.Attributes{
			Host:      "host-3",
			Padding:   "aB9",
			RequestID: "req-0042",
			SourceIP:  "10.0.0.7",
			Status:    "200",
		},
	}
}

func TestCanonicalKeyOrderingAndFraming(t *testing.T) {
	got := string(encryptor.CanonicalBytes(sampleRecord()))
	want := `{"attributes":{"host":"host-3","padding":"aB9","request_id":"req-0042",` +
		`"source_ip":"10.0.0.7","status":"200"},"log_type":"access",` +
		`"message":"access event 17","producer_id":"producer-access",` +
		`"record_id":"000000000000","timestamp":"2026-09-11T04:30:00.500000000Z"}`
	if got != want {
		t.Fatalf("canonical form:\n got %s\nwant %s", got, want)
	}
	if strings.ContainsAny(got, "\n\t") || strings.Contains(got, ": ") {
		t.Fatal("canonical form contains insignificant whitespace")
	}
	var probe map[string]any
	if err := json.Unmarshal([]byte(got), &probe); err != nil {
		t.Fatalf("canonical form is not valid JSON: %v", err)
	}
	if len(probe) != 6 {
		t.Fatalf("record has %d top-level fields, want exactly 6", len(probe))
	}
	attrs := probe["attributes"].(map[string]any)
	if len(attrs) != 5 {
		t.Fatalf("attributes has %d keys, want exactly 5", len(attrs))
	}
	for _, k := range []string{"host", "source_ip", "status", "request_id", "padding"} {
		if _, ok := attrs[k]; !ok {
			t.Errorf("attributes is missing key %q", k)
		}
	}
}

// Q51: fixed 9-digit fraction, trailing zeros preserved, exactly 30 characters.
func TestTimestampIsFixedWidthWithTrailingZeros(t *testing.T) {
	cases := []int64{
		1789101000500000000, // .500000000
		1789101000000000000, // .000000000
		1789101000123456789, // .123456789
		0,                   // epoch
	}
	for _, ns := range cases {
		r := sampleRecord()
		r.TimestampNanos = ns
		s := r.TimestampString()
		if len(s) != encryptor.TimestampWidth {
			t.Errorf("timestamp %q has %d characters, want %d", s, len(s), encryptor.TimestampWidth)
		}
		if !strings.HasSuffix(s, "Z") {
			t.Errorf("timestamp %q does not end in Z", s)
		}
		frac := s[20:29]
		if len(frac) != 9 {
			t.Errorf("timestamp %q fractional part is %q", s, frac)
		}
	}
	r := sampleRecord()
	r.TimestampNanos = 1789101000000000000
	if !strings.Contains(r.TimestampString(), ".000000000Z") {
		t.Errorf("trailing zeros were suppressed: %q", r.TimestampString())
	}
}

// Q50: zero-padded 12-digit decimal, zero-based, first record "000000000000".
func TestRecordIDFormat(t *testing.T) {
	if got := encryptor.FormatRecordID(0); got != "000000000000" {
		t.Errorf("first record_id = %q, want 000000000000", got)
	}
	if got := encryptor.FormatRecordID(1); got != "000000000001" {
		t.Errorf("second record_id = %q", got)
	}
	for _, n := range []uint64{0, 1, 42, 999, 100000, 999999999999} {
		if got := encryptor.FormatRecordID(n); len(got) != encryptor.RecordIDWidth {
			t.Errorf("record_id %q for %d is %d characters", got, n, len(got))
		}
	}
}

func TestCanonRejectsUnknownAndMissingFields(t *testing.T) {
	bad := []string{
		`{"record_id":"000000000000","producer_id":"p","log_type":"access","timestamp":"2026-09-11T04:30:00.500000000Z","message":"m","attributes":{"host":"h","padding":"","request_id":"r","source_ip":"i","status":"200"},"extra":1}`,
		`{"producer_id":"p","log_type":"access","timestamp":"2026-09-11T04:30:00.500000000Z","message":"m","attributes":{"host":"h","padding":"","request_id":"r","source_ip":"i","status":"200"}}`,
		`{"record_id":"1","producer_id":"p","log_type":"access","timestamp":"2026-09-11T04:30:00.500000000Z","message":"m","attributes":{"host":"h","padding":"","request_id":"r","source_ip":"i","status":"200"}}`,
	}
	for i, s := range bad {
		if _, err := encryptor.Canon([]byte(s)); err == nil {
			t.Errorf("case %d: Canon accepted an invalid raw record", i)
		}
	}
}

// Canon normalizes field order, whitespace and time zone.
func TestCanonNormalizesInput(t *testing.T) {
	a := `{
	  "timestamp": "2026-09-11T11:30:00.500000000+07:00",
	  "message": "access event 17",
	  "record_id": "000000000000",
	  "log_type": "access",
	  "producer_id": "producer-access",
	  "attributes": {"status":"200","source_ip":"10.0.0.7","request_id":"req-0042","padding":"aB9","host":"host-3"}
	}`
	got, err := encryptor.Canon([]byte(a))
	if err != nil {
		t.Fatal(err)
	}
	want := encryptor.CanonicalBytes(sampleRecord())
	if !bytes.Equal(got, want) {
		t.Fatalf("Canon did not normalize:\n got %s\nwant %s", got, want)
	}
}

// R-EVAL-08: the same logical record canonicalizes to the same bytes across
// repeated runs and across process restarts.
func TestCanonicalizationIsDeterministicAcrossRepetitions(t *testing.T) {
	first := encryptor.CanonicalBytes(sampleRecord())
	for i := 0; i < 1000; i++ {
		if !bytes.Equal(first, encryptor.CanonicalBytes(sampleRecord())) {
			t.Fatal("canonicalization is not repeatable within a process")
		}
	}
}

const restartEnv = "DHTBACKED_CANON_CHILD"

func TestCanonicalizationIsDeterministicAcrossProcessRestarts(t *testing.T) {
	if os.Getenv(restartEnv) == "1" {
		fmt.Printf("CANON:%s\n", hex.EncodeToString(encryptor.CanonicalBytes(sampleRecord())))
		return
	}
	want := hex.EncodeToString(encryptor.CanonicalBytes(sampleRecord()))
	for i := 0; i < 3; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=TestCanonicalizationIsDeterministicAcrossProcessRestarts", "-test.v")
		cmd.Env = append(os.Environ(), restartEnv+"=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("child process: %v\n%s", err, out)
		}
		var got string
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "CANON:") {
				got = strings.TrimPrefix(line, "CANON:")
			}
		}
		if got != want {
			t.Fatalf("restart %d produced %s, want %s", i, got, want)
		}
	}
}
