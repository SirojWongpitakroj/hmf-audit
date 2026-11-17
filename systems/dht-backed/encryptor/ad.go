package encryptor

import "dhtbacked/internal/serial"

// BuildAD serializes AD = (producerID, logType, timestamp, recordID).
// The record ID binds a lookup request to the anchored ciphertext; without
// it an untrusted data lake can return a different valid record for an ID.
// in exactly that order (Q7): length-prefixed UTF-8 producerID, length-prefixed
// UTF-8 logType, then the big-endian 64-bit Unix timestamp in nanoseconds.
//
// The length prefix is a 4-byte unsigned big-endian integer. Q7 says
// "length-prefixed" without naming a width; the width was fixed by the user's
// GAP-3 decision (2026-09-11). It matches the Q12 string framing used for DHT
// records.
//
// This identical byte representation is used both as AEAD associated data
// (Eq. (14)) and inside the leaf digest (Eq. (15)).
func BuildAD(producerID, logType string, timestampNanos int64, recordID string) []byte {
	w := serial.NewRawWriter()
	w.String(producerID)
	w.String(logType)
	w.Uint64(uint64(timestampNanos))
	w.String(recordID)
	return w.Result()
}

// ParseAD decodes an AD byte string back into its three fields.
func ParseAD(ad []byte) (producerID, logType string, timestampNanos int64, recordID string, err error) {
	r := serial.NewRawReader(ad)
	if producerID, err = r.String(); err != nil {
		return "", "", 0, "", err
	}
	if logType, err = r.String(); err != nil {
		return "", "", 0, "", err
	}
	var ns uint64
	if ns, err = r.Uint64(); err != nil {
		return "", "", 0, "", err
	}
	if recordID, err = r.String(); err != nil {
		return "", "", 0, "", err
	}
	if err = r.End(); err != nil {
		return "", "", 0, "", err
	}
	return producerID, logType, int64(ns), recordID, nil
}

// ADOf builds the AD byte string for a record.
func ADOf(r Record) []byte {
	return BuildAD(r.ProducerID, r.LogType, r.TimestampNanos, r.RecordID)
}
