package producer_test

// Independent verification of the synthetic dataset (M10).
//
// Expected probabilities are derived here from Q29's own numbers, not read out
// of the generator: the Zipf weights 1/r^1.2 are recomputed with math.Pow, and
// the bursty 80/20 split is checked against the shares it implies. Record sizes
// are measured by re-canonicalizing each record through encoding/json-free
// byte counting of encryptor.CanonicalBytes.
//
// The generator is seeded at 42, so every run draws the same sequence. These
// are exact regression checks on a fixed dataset, not statistical tests that
// could flake; the tolerances below are there only so the assertions read as
// "matches the distribution Q29 names", and each sits several standard errors
// wide at n = 100,000.

import (
	"math"
	"testing"

	"dhtbacked/encryptor"
	"dhtbacked/producer"
)

var fiveTypes = []string{"access", "api", "auth", "error", "security"}

func typeCounts(recs []encryptor.Record) map[string]int {
	c := map[string]int{}
	for _, r := range recs {
		c[r.LogType]++
	}
	return c
}

// Q29 Uniform: each of the five types with probability 1/5.
func TestIndependentUniformDistribution(t *testing.T) {
	const n = 100000
	recs, err := producer.NewGenerator(producer.Uniform).Generate(n)
	if err != nil {
		t.Fatal(err)
	}
	c := typeCounts(recs)
	if len(c) != 5 {
		t.Fatalf("uniform dataset has %d types, want 5", len(c))
	}
	for _, lt := range fiveTypes {
		if p := float64(c[lt]) / n; math.Abs(p-0.2) > 0.01 {
			t.Errorf("uniform: %s share %.4f, want 0.2000 +/- 0.01", lt, p)
		}
	}
}

// Q29 Zipf-heavy: exponent 1.2 over the five ranked types. The expected share
// of rank r is (1/r^1.2) / sum_{i=1..5} 1/i^1.2, recomputed here.
func TestIndependentZipfExponent(t *testing.T) {
	const n = 100000
	recs, err := producer.NewGenerator(producer.ZipfHeavy).Generate(n)
	if err != nil {
		t.Fatal(err)
	}
	c := typeCounts(recs)
	var z float64
	for r := 1; r <= 5; r++ {
		z += 1 / math.Pow(float64(r), 1.2)
	}
	for r, lt := range fiveTypes { // rank order recorded as GAP-6 in TRACKER.md
		want := (1 / math.Pow(float64(r+1), 1.2)) / z
		if p := float64(c[lt]) / n; math.Abs(p-want) > 0.01 {
			t.Errorf("zipf: rank %d (%s) share %.4f, want %.4f +/- 0.01", r+1, lt, p, want)
		}
	}
	// A distribution with the wrong exponent would shift rank 1 visibly:
	// exponent 1.0 gives rank 1 about 0.438, exponent 1.5 about 0.585.
	if p := float64(c[fiveTypes[0]]) / n; p < 0.46 || p > 0.52 {
		t.Errorf("zipf: rank-1 share %.4f is not the ~0.491 that exponent 1.2 implies", p)
	}
}

// Q29 Bursty: 1,024-record windows, one hot type per window supplying 80%,
// the other four sharing the remaining 20% uniformly (5% each).
//
// An exact 80% split of 1,024 records would be 819.2 records, which is not an
// integer, so 80% is necessarily the hot type's per-record probability; that is
// what is checked, aggregated over every full window.
func TestIndependentBurstyWindows(t *testing.T) {
	const n = 100000
	recs, err := producer.NewGenerator(producer.Bursty).Generate(n)
	if err != nil {
		t.Fatal(err)
	}
	var hot, cold, coldTypes, windows int
	for start := 0; start+producer.BurstWindow <= n; start += producer.BurstWindow {
		c := typeCounts(recs[start : start+producer.BurstWindow])
		hotType, hotN := "", -1
		for lt, k := range c {
			if k > hotN {
				hotType, hotN = lt, k
			}
		}
		if float64(hotN)/producer.BurstWindow < 0.7 {
			t.Fatalf("window at %d: no single hot type (top share %.3f)", start, float64(hotN)/producer.BurstWindow)
		}
		for lt, k := range c {
			if lt == hotType {
				continue
			}
			cold += k
			coldTypes++
		}
		hot += hotN
		windows++
	}
	total := windows * producer.BurstWindow
	if p := float64(hot) / float64(total); math.Abs(p-0.8) > 0.01 {
		t.Errorf("bursty: hot-type share %.4f over %d windows, want 0.8000 +/- 0.01", p, windows)
	}
	if p := float64(cold) / float64(total) / 4; math.Abs(p-0.05) > 0.005 {
		t.Errorf("bursty: each cold type's share %.4f, want 0.0500 +/- 0.005", p)
	}
}

// Q48 / R-EVAL-01: every canonical plaintext M is between 256 and 512 bytes
// inclusive, and hits its target exactly. Checked on all three dataset sizes.
func TestIndependentRecordSizesOnCanonicalPlaintext(t *testing.T) {
	for _, n := range []int{1000, 10000, 100000} {
		recs, err := producer.NewGenerator(producer.Uniform).Generate(n)
		if err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}
		if len(recs) != n {
			t.Fatalf("asked for %d records, got %d", n, len(recs))
		}
		minB, maxB := 1<<30, 0
		for _, r := range recs {
			size := len(encryptor.CanonicalBytes(r))
			if size < producer.MinRecordBytes || size > producer.MaxRecordBytes {
				t.Fatalf("n=%d: record %s is %d bytes, outside [256, 512]", n, r.RecordID, size)
			}
			if size < minB {
				minB = size
			}
			if size > maxB {
				maxB = size
			}
		}
		// Targets are drawn uniformly over the whole band, so a large dataset
		// must reach both ends of it.
		if n == 100000 && (minB != 256 || maxB != 512) {
			t.Errorf("n=%d: sizes span [%d, %d]; a uniform target over [256, 512] should hit both ends", n, minB, maxB)
		}
	}
}

// Q47: each record goes to the producer for its log type, and Q50: record_id
// is a global zero-based 12-digit counter across the whole run.
func TestIndependentProducerAssignmentAndRecordIDs(t *testing.T) {
	recs, err := producer.NewGenerator(producer.ZipfHeavy).Generate(5000)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range recs {
		if r.ProducerID != "producer-"+r.LogType {
			t.Fatalf("record %d: type %s assigned to %s", i, r.LogType, r.ProducerID)
		}
		if want := encryptor.FormatRecordID(uint64(i)); r.RecordID != want || len(r.RecordID) != 12 {
			t.Fatalf("record %d: record_id %q, want %q", i, r.RecordID, want)
		}
	}
}

// R-EVAL-08: two generators at the same seed produce byte-identical datasets,
// for every distribution.
func TestIndependentByteForByteReproducibility(t *testing.T) {
	for _, d := range []producer.Distribution{producer.Uniform, producer.ZipfHeavy, producer.Bursty} {
		a, err := producer.NewGenerator(d).Generate(3000)
		if err != nil {
			t.Fatal(err)
		}
		b, err := producer.NewGenerator(d).Generate(3000)
		if err != nil {
			t.Fatal(err)
		}
		for i := range a {
			if string(encryptor.CanonicalBytes(a[i])) != string(encryptor.CanonicalBytes(b[i])) {
				t.Fatalf("%s: record %d differs between two seed-42 runs", d, i)
			}
		}
	}
}
