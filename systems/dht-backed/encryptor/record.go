// Package encryptor is the producer-side, in-process canonicalization,
// encryption, digest and attestation module (Q19). It is a library invoked by
// the producer, not a network service.
package encryptor

import "time"

// TimestampLayout is the canonical string form of the timestamp field inside M
// (Q51): RFC 3339, UTC, Z suffix, fixed 9-digit fractional second with
// trailing zeros preserved. Always exactly 30 characters.
//
// Go's time.RFC3339Nano suppresses trailing zeros and must not be used.
const TimestampLayout = "2006-01-02T15:04:05.000000000Z"

// TimestampWidth is the fixed character width of a canonical timestamp (Q51).
const TimestampWidth = 30

// RecordIDWidth is the fixed character width of record_id: a zero-padded
// 12-digit decimal string (Q50).
const RecordIDWidth = 12

// Attributes is the fixed flat attributes object. It has exactly these five
// keys and no others (Q48).
//
// The SRS fixes the key set but not the JSON value types; all five are carried
// as JSON strings so that the Q48 padding-termination argument (one appended
// ASCII character adds exactly one canonical byte) holds unchanged.
type Attributes struct {
	Host      string `json:"host"`
	Padding   string `json:"padding"`
	RequestID string `json:"request_id"`
	SourceIP  string `json:"source_ip"`
	Status    string `json:"status"`
}

// Record is the fixed logical record schema: exactly the six fields named by
// Q6. TimestampNanos is the single canonical int64 nanosecond value from which
// all three timestamp representations are derived (Q51).
type Record struct {
	RecordID       string
	ProducerID     string
	LogType        string
	TimestampNanos int64
	Message        string
	Attributes     Attributes
}

// TimestampString renders the canonical timestamp form used inside M (Q51).
func (r Record) TimestampString() string {
	return time.Unix(0, r.TimestampNanos).UTC().Format(TimestampLayout)
}
