package producer

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"time"

	"dhtbacked/encryptor"
)

// Seed is the PRNG seed for every synthetic dataset (Q29).
const Seed = int64(42)

// Distribution names the three input distributions of Table 5 (Q29).
type Distribution string

const (
	// Uniform gives each of the five log types probability 1/5.
	Uniform Distribution = "uniform"
	// ZipfHeavy uses exponent 1.2 over the five ranked types.
	ZipfHeavy Distribution = "zipf"
	// Bursty uses 1,024-record windows with one hot type per window: 80% of a
	// window comes from that type and the remaining 20% is uniform across the
	// other four.
	Bursty Distribution = "bursty"
)

// ZipfExponent is the Zipf-heavy exponent (Q29).
const ZipfExponent = 1.2

// BurstWindow is the bursty distribution's window length in records (Q29).
const BurstWindow = 1024

// BurstHotShare is the share of a bursty window drawn from its hot type (Q29).
const BurstHotShare = 0.8

// Record size band, measured on the canonicalized plaintext M before AEAD
// encryption (Q48, R-EVAL-01).
const (
	MinRecordBytes = 256
	MaxRecordBytes = 512
)

// messagePrefix returns the per-type message prefix (Q48).
func messagePrefix(logType string) string {
	switch logType {
	case "access":
		return "access event"
	case "error":
		return "error event"
	case "auth":
		return "authentication event"
	case "api":
		return "api event"
	case "security":
		return "security event"
	}
	return ""
}

// rankedTypes is the ranking used by the Zipf-heavy distribution.
//
// Q29 fixes the exponent but not which type holds which rank; the five types
// are ranked in the same lexicographic order used everywhere else in this
// system (Q26).
var rankedTypes = []string{"access", "api", "auth", "error", "security"}

// baseInstant is the first record's timestamp.
//
// Q29/Q48 fix the record schema and size band but not the timestamp values.
// They are generated deterministically: record i carries baseInstant plus i
// milliseconds, so ingestion order and timestamp order agree and two runs at
// the same seed produce identical values.
var baseInstant = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

const recordInterval = time.Millisecond

// WIDTH NOTE — why the generated values are narrow.
//
// The Q6 canonical envelope is 156 bytes of keys and punctuation; Q50's
// 12-character record_id and Q51's 30-character timestamp add a further 42, so
// an otherwise empty canonical record is already 198 bytes. Adding the widest
// SRS-mandated content (log_type "security", producer_id "producer-security",
// message prefix "security event") brings the floor to 237 bytes, leaving
// exactly 19 bytes for the four non-padding attribute values before the
// 256-byte lower bound of R-EVAL-01 is reached.
//
// Q48 fixes the attribute key set and the message prefixes but not the value
// formats, and it requires generation to FAIL rather than truncate when a base
// record already exceeds its selected target. The formats below are therefore
// chosen so that every base record is at most 256 bytes: host is 3 characters,
// source_ip 8, status 3, request_id 5, and message is exactly the per-type
// prefix. Value content still varies with the seed; only the widths are fixed,
// and attributes.padding remains the mechanism that reaches the target size.

// paddingAlphabet is the deterministic ASCII alphanumeric fill alphabet. Each
// appended character raises the canonical byte length by exactly one, since
// none of them requires JSON escaping (Q48).
const paddingAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// statusCodes are all three characters wide (see the width note above).
var statusCodes = []string{"200", "201", "204", "301", "400", "401", "403", "404", "500", "503"}

// ErrOversizeBase reports a base record that already exceeds its selected
// target size. Q48 requires generation to fail in this case; no field is ever
// truncated to make a record fit.
var ErrOversizeBase = errors.New("producer: base record exceeds its target size")

// Deficit returns target minus the canonical byte length of rec. A negative
// deficit is the condition Q48 requires generation to fail on.
func Deficit(rec encryptor.Record, target int) int {
	return target - len(encryptor.CanonicalBytes(rec))
}

// Generator produces the deterministic synthetic dataset.
//
// The generation order is fixed for a given seed, so record_id (a global
// zero-based counter, Q50) advances in a reproducible order.
type Generator struct {
	rng          *rand.Rand
	dist         Distribution
	counter      uint64
	zipfCDF      []float64
	burstHotType string
}

// NewGenerator starts a generator at PRNG seed 42 (Q29).
func NewGenerator(dist Distribution) *Generator {
	g := &Generator{rng: rand.New(rand.NewSource(Seed)), dist: dist}
	if dist == ZipfHeavy {
		g.zipfCDF = zipfCDF(len(rankedTypes), ZipfExponent)
	}
	return g
}

func zipfCDF(n int, exponent float64) []float64 {
	weights := make([]float64, n)
	total := 0.0
	for i := 0; i < n; i++ {
		weights[i] = 1.0 / math.Pow(float64(i+1), exponent)
		total += weights[i]
	}
	cdf := make([]float64, n)
	running := 0.0
	for i := 0; i < n; i++ {
		running += weights[i] / total
		cdf[i] = running
	}
	return cdf
}

// Count returns the number of records generated so far.
func (g *Generator) Count() uint64 { return g.counter }

// nextLogType draws the next record's log type from the configured
// distribution.
func (g *Generator) nextLogType() string {
	switch g.dist {
	case ZipfHeavy:
		u := g.rng.Float64()
		for i, c := range g.zipfCDF {
			if u <= c {
				return rankedTypes[i]
			}
		}
		return rankedTypes[len(rankedTypes)-1]
	case Bursty:
		if g.counter%BurstWindow == 0 {
			g.burstHotType = rankedTypes[g.rng.Intn(len(rankedTypes))]
		}
		if g.rng.Float64() < BurstHotShare {
			return g.burstHotType
		}
		others := make([]string, 0, len(rankedTypes)-1)
		for _, t := range rankedTypes {
			if t != g.burstHotType {
				others = append(others, t)
			}
		}
		return others[g.rng.Intn(len(others))]
	default:
		return rankedTypes[g.rng.Intn(len(rankedTypes))]
	}
}

// Next produces the next synthetic record.
//
// The record is sized exactly as Q48 states: a deterministic target is chosen
// uniformly in [256, 512] inclusive; the record is canonicalized; the byte
// deficit is computed; attributes.padding is filled with deterministic ASCII
// alphanumeric bytes; and the final canonical length is verified before the
// record is returned for encryption. Generation fails if a base record already
// exceeds its selected target — no field is ever truncated.
func (g *Generator) Next() (encryptor.Record, error) {
	logType := g.nextLogType()
	target := MinRecordBytes + g.rng.Intn(MaxRecordBytes-MinRecordBytes+1)

	host := fmt.Sprintf("h%02d", g.rng.Intn(100))
	sourceIP := fmt.Sprintf("10.%d.%d.%d", g.rng.Intn(10), g.rng.Intn(10), g.rng.Intn(10))
	status := statusCodes[g.rng.Intn(len(statusCodes))]
	requestID := fmt.Sprintf("r%04x", g.rng.Intn(0x10000))
	message := messagePrefix(logType)

	rec := encryptor.Record{
		RecordID:       encryptor.FormatRecordID(g.counter),
		ProducerID:     IDForLogType(logType),
		LogType:        logType,
		TimestampNanos: baseInstant.Add(time.Duration(g.counter) * recordInterval).UnixNano(),
		Message:        message,
		Attributes: encryptor.Attributes{
			Host:      host,
			Padding:   "",
			RequestID: requestID,
			SourceIP:  sourceIP,
			Status:    status,
		},
	}

	deficit := Deficit(rec, target)
	if deficit < 0 {
		return encryptor.Record{}, fmt.Errorf("%w: record %s canonicalizes to %d bytes against a %d-byte target",
			ErrOversizeBase, rec.RecordID, target-deficit, target)
	}
	pad := make([]byte, deficit)
	for i := range pad {
		pad[i] = paddingAlphabet[g.rng.Intn(len(paddingAlphabet))]
	}
	rec.Attributes.Padding = string(pad)

	if final := len(encryptor.CanonicalBytes(rec)); final != target {
		return encryptor.Record{}, fmt.Errorf(
			"producer: record %s canonicalizes to %d bytes, want exactly %d",
			rec.RecordID, final, target)
	}
	g.counter++
	return rec, nil
}

// Generate produces n records.
func (g *Generator) Generate(n int) ([]encryptor.Record, error) {
	out := make([]encryptor.Record, 0, n)
	for i := 0; i < n; i++ {
		r, err := g.Next()
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}
