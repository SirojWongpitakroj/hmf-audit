package logcache

import (
	"testing"

	"delia/hash"
)

func TestNewRequiresTheta(t *testing.T) {
	if _, err := New(0); err == nil {
		t.Fatalf("expected error for theta = 0")
	}
	if _, err := New(-1); err == nil {
		t.Fatalf("expected error for negative theta")
	}
	if _, err := New(4); err != nil {
		t.Fatalf("unexpected error for theta = 4: %v", err)
	}
}

// TEST-01-style: n=3-analog, theta=4; p1 generates 8 records -> two batches
// of 4, each server's own store holds psi_{p1,0} = {H(d1)..H(d4)} then
// psi_{p1,1} = {H(d5)..H(d8)}.
func TestAppendBatchBoundaries(t *testing.T) {
	s, err := New(4)
	if err != nil {
		t.Fatal(err)
	}
	var lines [][]byte
	for i := byte(1); i <= 8; i++ {
		lines = append(lines, []byte{'d', i})
	}

	var batch0, batch1 []hash.Digest
	for i, line := range lines {
		full, batch := s.Append("p1", RecordHash(line))
		if i < 3 {
			if full {
				t.Fatalf("record %d: unexpected batch-full before theta reached", i+1)
			}
		}
		if i == 3 {
			if !full {
				t.Fatalf("record 4: expected batch-full")
			}
			batch0 = batch
		}
		if i == 7 {
			if !full {
				t.Fatalf("record 8: expected batch-full")
			}
			batch1 = batch
		}
	}

	if len(batch0) != 4 || len(batch1) != 4 {
		t.Fatalf("batches not exactly theta long: %d, %d", len(batch0), len(batch1))
	}
	for i := 0; i < 4; i++ {
		want := RecordHash(lines[i])
		if batch0[i] != want {
			t.Fatalf("batch0[%d] mismatch", i)
		}
	}
	for i := 0; i < 4; i++ {
		want := RecordHash(lines[4+i])
		if batch1[i] != want {
			t.Fatalf("batch1[%d] mismatch", i)
		}
	}

	// After the second batch completes, current resets to empty for batch 2.
	if cur := s.Current("p1"); len(cur) != 0 {
		t.Fatalf("expected empty current batch after completion, got %d entries", len(cur))
	}
}

func TestNoPartialBatchReported(t *testing.T) {
	s, _ := New(4)
	full, _ := s.Append("p1", RecordHash([]byte("a")))
	if full {
		t.Fatalf("batch reported full before theta records")
	}
	if got := len(s.Current("p1")); got != 1 {
		t.Fatalf("current batch length = %d, want 1", got)
	}
}

func TestDuplicatesKeptNoDeduplication(t *testing.T) {
	// GAP-04(a): a batch with two identical log lines has theta elements,
	// with the same digest at both positions.
	s, _ := New(2)
	line := []byte("same line")
	_, batch := twoAppends(s, "p1", line, line)
	if len(batch) != 2 {
		t.Fatalf("batch length = %d, want 2", len(batch))
	}
	if batch[0] != batch[1] {
		t.Fatalf("duplicate lines did not produce equal digests")
	}
	if batch[0] != RecordHash(line) {
		t.Fatalf("digest mismatch")
	}
}

func twoAppends(s *Store, id ServerID, a, b []byte) (bool, []hash.Digest) {
	s.Append(id, RecordHash(a))
	return s.Append(id, RecordHash(b))
}

func TestDomainWideSeparatePerServer(t *testing.T) {
	// DATA-006: every server maintains a psi per server, including its own.
	s, _ := New(2)
	s.Append("p1", RecordHash([]byte("a1")))
	s.Append("p2", RecordHash([]byte("b1")))
	if len(s.Current("p1")) != 1 || len(s.Current("p2")) != 1 {
		t.Fatalf("per-server batches are not independent")
	}
}

func TestSetCurrent(t *testing.T) {
	s, _ := New(3)
	want := []hash.Digest{RecordHash([]byte("x")), RecordHash([]byte("y"))}
	s.SetCurrent("p2", want)
	got := s.Current("p2")
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("SetCurrent/Current mismatch")
	}
}

func TestRecordHashExcludesLineEnding(t *testing.T) {
	// GAP-03(a): the record is the UTF-8 line bytes, excluding line-ending
	// bytes; "abc\n" and "abc" must hash identically once the line ending is
	// stripped by the caller (this package does not itself see raw file
	// bytes with terminators — it hashes exactly what it is given).
	a := RecordHash([]byte("abc"))
	b := RecordHash([]byte("abc"))
	if a != b {
		t.Fatalf("RecordHash not deterministic")
	}
	c := RecordHash([]byte("abc\n"))
	if a == c {
		t.Fatalf("RecordHash of line with vs without terminator must differ (terminator stripping is the caller's job)")
	}
}
