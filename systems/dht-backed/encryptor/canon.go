package encryptor

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// rawRecord mirrors the fixed Q6 schema for strict decoding of raw JSON input.
type rawRecord struct {
	RecordID   string     `json:"record_id"`
	ProducerID string     `json:"producer_id"`
	LogType    string     `json:"log_type"`
	Timestamp  string     `json:"timestamp"`
	Message    string     `json:"message"`
	Attributes Attributes `json:"attributes"`
}

// ParseRaw decodes a raw JSON log record into the fixed logical schema.
// Unknown fields are rejected; the timestamp is normalized to UTC and reduced
// to the canonical int64 nanosecond value (Q6, Q51).
func ParseRaw(raw []byte) (Record, error) {
	var rr rawRecord
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rr); err != nil {
		return Record{}, fmt.Errorf("encryptor: raw record: %w", err)
	}
	if dec.More() {
		return Record{}, errors.New("encryptor: raw record: trailing JSON content")
	}
	if rr.RecordID == "" || rr.ProducerID == "" || rr.LogType == "" || rr.Timestamp == "" {
		return Record{}, errors.New("encryptor: raw record: missing required field")
	}
	if len(rr.RecordID) != RecordIDWidth {
		return Record{}, fmt.Errorf("encryptor: record_id must be exactly %d characters", RecordIDWidth)
	}
	ts, err := time.Parse(time.RFC3339Nano, rr.Timestamp)
	if err != nil {
		return Record{}, fmt.Errorf("encryptor: raw record: timestamp: %w", err)
	}
	return Record{
		RecordID:       rr.RecordID,
		ProducerID:     rr.ProducerID,
		LogType:        rr.LogType,
		TimestampNanos: ts.UTC().UnixNano(),
		Message:        rr.Message,
		Attributes:     rr.Attributes,
	}, nil
}

// Canon implements Canon(.) of Eq. (13): it normalizes field order, encoding
// and structure, producing the canonical plaintext M.
//
// Output is RFC 8785-style canonical JSON: UTF-8, no insignificant whitespace,
// object keys ordered, timestamps normalized to UTC (Q6).
func Canon(raw []byte) ([]byte, error) {
	r, err := ParseRaw(raw)
	if err != nil {
		return nil, err
	}
	return CanonicalBytes(r), nil
}

// CanonicalBytes serializes an already-parsed record to canonical JSON.
//
// Key ordering is the RFC 8785 ordering of these fixed key sets and is written
// literally here, in that order; no Go map is iterated or serialized (Q12).
//
//	top level: attributes, log_type, message, producer_id, record_id, timestamp
//	attributes: host, padding, request_id, source_ip, status
func CanonicalBytes(r Record) []byte {
	var b bytes.Buffer
	b.WriteByte('{')

	b.WriteString(`"attributes":`)
	b.WriteByte('{')
	writeMember(&b, "host", r.Attributes.Host, true)
	writeMember(&b, "padding", r.Attributes.Padding, false)
	writeMember(&b, "request_id", r.Attributes.RequestID, false)
	writeMember(&b, "source_ip", r.Attributes.SourceIP, false)
	writeMember(&b, "status", r.Attributes.Status, false)
	b.WriteByte('}')

	writeMember(&b, "log_type", r.LogType, false)
	writeMember(&b, "message", r.Message, false)
	writeMember(&b, "producer_id", r.ProducerID, false)
	writeMember(&b, "record_id", r.RecordID, false)
	writeMember(&b, "timestamp", r.TimestampString(), false)

	b.WriteByte('}')
	return b.Bytes()
}

func writeMember(b *bytes.Buffer, key, value string, first bool) {
	if !first {
		b.WriteByte(',')
	}
	writeJSONString(b, key)
	b.WriteByte(':')
	writeJSONString(b, value)
}

// writeJSONString emits a JSON string with RFC 8785 minimal escaping: only the
// two mandatory escapes, the five short control escapes, and \u00xx (lowercase
// hex) for the remaining control characters. All other code points are emitted
// as raw UTF-8.
func writeJSONString(b *bytes.Buffer, s string) {
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if c < 0x20 {
				b.WriteString(`\u00`)
				const hexDigits = "0123456789abcdef"
				b.WriteByte(hexDigits[c>>4])
				b.WriteByte(hexDigits[c&0x0f])
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
}

// FormatRecordID renders a zero-based counter as the fixed 12-digit,
// zero-padded decimal record_id of Q50.
func FormatRecordID(counter uint64) string {
	s := strconv.FormatUint(counter, 10)
	if len(s) > RecordIDWidth {
		return s
	}
	return zeroPad[:RecordIDWidth-len(s)] + s
}

const zeroPad = "000000000000"
