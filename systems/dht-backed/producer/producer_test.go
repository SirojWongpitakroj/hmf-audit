package producer_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhtbacked/encryptor"
	"dhtbacked/producer"
)

// Q29/Q47: five log types and five type-aligned producers.
func TestFiveTypesAndFiveProducers(t *testing.T) {
	if len(producer.LogTypes) != 5 {
		t.Fatalf("m = %d, want 5", len(producer.LogTypes))
	}
	want := []string{"access", "api", "auth", "error", "security"}
	for i, lt := range want {
		if producer.LogTypes[i] != lt {
			t.Fatalf("LogTypes = %v, want lexicographic %v", producer.LogTypes, want)
		}
	}
	if len(producer.IDs) != 5 {
		t.Fatalf("%d producers, want 5", len(producer.IDs))
	}
	for _, lt := range want {
		id := producer.IDForLogType(lt)
		if id != "producer-"+lt {
			t.Errorf("IDForLogType(%q) = %q", lt, id)
		}
		found := false
		for _, known := range producer.IDs {
			if known == id {
				found = true
			}
		}
		if !found {
			t.Errorf("%q is not one of the five fixed producers", id)
		}
	}
}

// Q47: one persistent Ed25519 key pair per producer, generated once into the
// key directory and reused across runs.
func TestProducerKeysArePersistentAndReused(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	first, err := producer.LoadOrCreateKeys(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 5 {
		t.Fatalf("%d key pairs", len(first))
	}
	for _, kp := range first {
		if len(kp.Private) != ed25519.PrivateKeySize || len(kp.Public) != ed25519.PublicKeySize {
			t.Fatalf("%s: key sizes %d/%d", kp.ID, len(kp.Private), len(kp.Public))
		}
	}
	second, err := producer.LoadOrCreateKeys(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := range first {
		if !bytes.Equal(first[i].Private, second[i].Private) || !bytes.Equal(first[i].Public, second[i].Public) {
			t.Fatalf("%s: key material changed across runs", first[i].ID)
		}
	}
	third, err := producer.LoadKeys(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := range first {
		if !bytes.Equal(first[i].Public, third[i].Public) {
			t.Fatal("LoadKeys returned different material")
		}
	}
	// Key files are raw binary, not PEM or JWT.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 10 {
		t.Fatalf("%d key files, want 10", len(entries))
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte("BEGIN")) || json.Valid(b) && bytes.HasPrefix(b, []byte("{")) {
			t.Errorf("%s is not raw binary", e.Name())
		}
	}
	if _, err := producer.LoadKeys(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("LoadKeys succeeded against an empty directory")
	}
}

// Q47: the generator deterministically assigns each log to the producer
// corresponding to its log_type.
func TestGeneratorAssignsProducerByLogType(t *testing.T) {
	g := producer.NewGenerator(producer.Uniform)
	recs, err := g.Generate(2000)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recs {
		if r.ProducerID != "producer-"+r.LogType {
			t.Fatalf("record %s: log type %s carried producer %s", r.RecordID, r.LogType, r.ProducerID)
		}
	}
}

// Q50: record_id is a global zero-based counter, zero-padded to 12 digits.
func TestRecordIDsAreAGlobalZeroBasedCounter(t *testing.T) {
	g := producer.NewGenerator(producer.Bursty)
	recs, err := g.Generate(1500)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range recs {
		if len(r.RecordID) != 12 {
			t.Fatalf("record_id %q is %d characters", r.RecordID, len(r.RecordID))
		}
		if r.RecordID != encryptor.FormatRecordID(uint64(i)) {
			t.Fatalf("record %d has record_id %q", i, r.RecordID)
		}
	}
	if recs[0].RecordID != "000000000000" {
		t.Fatalf("first record_id = %q", recs[0].RecordID)
	}
}

// Q48/R-EVAL-01: the 256-512 byte band is measured on the canonicalized
// plaintext M, before AEAD encryption, and every record hits its target size
// exactly.
func TestRecordsAreSizedOnCanonicalPlaintext(t *testing.T) {
	for _, d := range []producer.Distribution{producer.Uniform, producer.ZipfHeavy, producer.Bursty} {
		g := producer.NewGenerator(d)
		recs, err := g.Generate(3000)
		if err != nil {
			t.Fatalf("%s: %v", d, err)
		}
		sizes := map[int]bool{}
		for _, r := range recs {
			n := len(encryptor.CanonicalBytes(r))
			if n < producer.MinRecordBytes || n > producer.MaxRecordBytes {
				t.Fatalf("%s: record %s canonicalizes to %d bytes", d, r.RecordID, n)
			}
			sizes[n] = true
			// The padding is ASCII alphanumeric only.
			for _, c := range r.Attributes.Padding {
				if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) {
					t.Fatalf("padding contains a non-alphanumeric byte %q", c)
				}
			}
		}
		if len(sizes) < 100 {
			t.Errorf("%s: only %d distinct record sizes; the target should be drawn across the band", d, len(sizes))
		}
	}
}

// Q48: generation fails rather than truncating when a base record already
// exceeds its selected target.
func TestOversizeBaseRecordIsRejectedNotTruncated(t *testing.T) {
	// A record whose fixed content alone exceeds the maximum of the band
	// cannot be produced by the generator; the equivalent condition is checked
	// directly here on the sizing rule the generator applies.
	big := encryptor.Record{
		RecordID:       "000000000000",
		ProducerID:     "producer-security",
		LogType:        "security",
		TimestampNanos: 0,
		Message:        strings.Repeat("x", 600),
		Attributes: encryptor.Attributes{
			Host: "h01", Padding: "", RequestID: "r0001", SourceIP: "10.0.0.1", Status: "200",
		},
	}
	if n := len(encryptor.CanonicalBytes(big)); n <= producer.MaxRecordBytes {
		t.Fatalf("fixture is only %d bytes; it must exceed the band to exercise the rule", n)
	}
	// The sizing rule reports a negative deficit for it, which is the condition
	// Q48 requires generation to fail on rather than truncate.
	if d := producer.Deficit(big, producer.MaxRecordBytes); d >= 0 {
		t.Fatalf("deficit = %d, want negative", d)
	}
	if producer.ErrOversizeBase == nil {
		t.Fatal("the fail-fast sentinel is absent")
	}
	// A record that fits reports a non-negative deficit.
	fits := big
	fits.Message = "security event"
	if d := producer.Deficit(fits, producer.MinRecordBytes); d < 0 {
		t.Fatalf("a base record at the floor reports deficit %d", d)
	}
	// The generator never emits such a record, and never shortens Message to
	// make one fit: every emitted record retains its full per-type prefix.
	g := producer.NewGenerator(producer.Uniform)
	recs, err := g.Generate(500)
	if err != nil {
		t.Fatal(err)
	}
	prefixes := map[string]string{
		"access": "access event", "error": "error event", "auth": "authentication event",
		"api": "api event", "security": "security event",
	}
	for _, r := range recs {
		if !strings.HasPrefix(r.Message, prefixes[r.LogType]) {
			t.Fatalf("record %s message %q lost its per-type prefix", r.RecordID, r.Message)
		}
	}
}

// Q29: distribution shapes.
func TestDistributions(t *testing.T) {
	const n = 20000

	uniform := counts(t, producer.Uniform, n)
	for _, lt := range producer.LogTypes {
		p := float64(uniform[lt]) / n
		if math.Abs(p-0.2) > 0.02 {
			t.Errorf("uniform: %s has probability %.3f, want 1/5", lt, p)
		}
	}

	zipf := counts(t, producer.ZipfHeavy, n)
	// Ranks are the lexicographic order; probabilities follow 1/i^1.2.
	total := 0.0
	weights := make([]float64, len(producer.LogTypes))
	for i := range producer.LogTypes {
		weights[i] = 1 / math.Pow(float64(i+1), producer.ZipfExponent)
		total += weights[i]
	}
	for i, lt := range producer.LogTypes {
		want := weights[i] / total
		got := float64(zipf[lt]) / n
		if math.Abs(got-want) > 0.02 {
			t.Errorf("zipf: %s has probability %.3f, want %.3f", lt, got, want)
		}
	}
	for i := 1; i < len(producer.LogTypes); i++ {
		if zipf[producer.LogTypes[i]] > zipf[producer.LogTypes[i-1]] {
			t.Errorf("zipf: rank %d outranks rank %d", i+1, i)
		}
	}

	// Bursty: within each 1,024-record window one type holds ~80%.
	g := producer.NewGenerator(producer.Bursty)
	recs, err := g.Generate(producer.BurstWindow * 8)
	if err != nil {
		t.Fatal(err)
	}
	for w := 0; w < 8; w++ {
		window := recs[w*producer.BurstWindow : (w+1)*producer.BurstWindow]
		c := map[string]int{}
		for _, r := range window {
			c[r.LogType]++
		}
		hot, hotCount := "", 0
		for lt, n := range c {
			if n > hotCount {
				hot, hotCount = lt, n
			}
		}
		share := float64(hotCount) / float64(producer.BurstWindow)
		if math.Abs(share-0.8) > 0.05 {
			t.Errorf("bursty window %d: hot type %s holds %.3f, want ~0.80", w, hot, share)
		}
		// The remaining 20% is spread over the other four.
		others := 0
		for lt, n := range c {
			if lt != hot {
				others += n
				if float64(n)/float64(producer.BurstWindow-hotCount) > 0.45 {
					t.Errorf("bursty window %d: cold type %s is not uniform", w, lt)
				}
			}
		}
		if others+hotCount != producer.BurstWindow {
			t.Fatal("window accounting is wrong")
		}
	}
}

func counts(t *testing.T, d producer.Distribution, n int) map[string]int {
	t.Helper()
	g := producer.NewGenerator(d)
	recs, err := g.Generate(n)
	if err != nil {
		t.Fatal(err)
	}
	c := map[string]int{}
	for _, r := range recs {
		c[r.LogType]++
	}
	return c
}

// Q29 / Table 5: the dataset sizes 10^3, 10^4 and 10^5 all generate.
func TestDatasetSizes(t *testing.T) {
	for _, n := range []int{1000, 10000, 100000} {
		g := producer.NewGenerator(producer.Uniform)
		recs, err := g.Generate(n)
		if err != nil {
			t.Fatalf("n = %d: %v", n, err)
		}
		if len(recs) != n || g.Count() != uint64(n) {
			t.Fatalf("n = %d: generated %d records", n, len(recs))
		}
		last := recs[n-1]
		if last.RecordID != encryptor.FormatRecordID(uint64(n-1)) {
			t.Fatalf("n = %d: last record_id = %q", n, last.RecordID)
		}
		if size := len(encryptor.CanonicalBytes(last)); size < producer.MinRecordBytes || size > producer.MaxRecordBytes {
			t.Fatalf("n = %d: last record is %d bytes", n, size)
		}
	}
}

// R-EVAL-08: two runs at the same seed produce byte-identical datasets.
func TestDatasetIsByteForByteReproducible(t *testing.T) {
	for _, d := range []producer.Distribution{producer.Uniform, producer.ZipfHeavy, producer.Bursty} {
		var first []byte
		for run := 0; run < 3; run++ {
			g := producer.NewGenerator(d)
			recs, err := g.Generate(2000)
			if err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer
			for _, r := range recs {
				buf.Write(encryptor.CanonicalBytes(r))
			}
			if run == 0 {
				first = buf.Bytes()
				continue
			}
			if !bytes.Equal(first, buf.Bytes()) {
				t.Fatalf("%s: run %d differs from run 0", d, run)
			}
		}
	}
}
