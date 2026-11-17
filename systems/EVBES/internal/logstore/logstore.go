// Package logstore is Phase 3 Step 2: LID assignment, log encryption (Eq. 14),
// metadata encryption (Eq. 15), and the Cloud Storage Provider that holds the
// ciphertexts (Sec. IV.A.1).
package logstore

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"

	"evseb/internal/crypto"
)

// Log is one log entry Ln with its metadata {Ti, Si, LType}.
type Log struct {
	LID     string
	Content string // Ci
	Ti      int64  // timestamp, Unix seconds
	Si      string // source
	LType   string
}

// EncryptedLog is what the Cloud Storage Provider stores for one entry.
type EncryptedLog struct {
	LID        string
	CTLog      []byte // EncAES(Ln, Kenc), Eq. 14
	CTMetadata []byte // EncAES({Ti, Si, LType}, Kenc), Eq. 15
}

// LIDAssigner hands out unique LIDs (Phase 3 Step 2). An LID is a run-wide
// zero-based counter rendered as a zero-padded 12-digit decimal string, so
// lexical order equals ingestion order. The paper only requires uniqueness.
type LIDAssigner struct{ next uint64 }

// Next returns the next unused LID.
func (a *LIDAssigner) Next() string {
	lid := fmt.Sprintf("%012d", a.next)
	a.next++
	return lid
}

// EncodeMetadata is the pre-encryption form of Metadata = {Ti, Si, LType}:
// Ti as int64 big-endian, then Si and LType each as a 4-byte big-endian
// length followed by the bytes.
func EncodeMetadata(ti int64, si, lType string) []byte {
	b := binary.BigEndian.AppendUint64(nil, uint64(ti))
	b = binary.BigEndian.AppendUint32(b, uint32(len(si)))
	b = append(b, si...)
	b = binary.BigEndian.AppendUint32(b, uint32(len(lType)))
	return append(b, lType...)
}

// DecodeMetadata reverses EncodeMetadata.
func DecodeMetadata(b []byte) (ti int64, si, lType string, err error) {
	bad := errors.New("logstore: bad metadata encoding")
	if len(b) < 12 {
		return 0, "", "", bad
	}
	ti = int64(binary.BigEndian.Uint64(b))
	b = b[8:]
	n := binary.BigEndian.Uint32(b)
	if uint64(n)+4 > uint64(len(b)) {
		return 0, "", "", bad
	}
	si, b = string(b[4:4+n]), b[4+n:]
	if len(b) < 4 {
		return 0, "", "", bad
	}
	n = binary.BigEndian.Uint32(b)
	if uint64(n)+4 != uint64(len(b)) {
		return 0, "", "", bad
	}
	return ti, si, string(b[4:]), nil
}

// EncryptLog is Eq. 14 and Eq. 15. lg.LID must already be assigned.
func EncryptLog(lg Log, kenc [crypto.KeySize]byte) (EncryptedLog, error) {
	if lg.LID == "" {
		return EncryptedLog{}, errors.New("logstore: log has no LID")
	}
	ctLog, err := crypto.EncAES([]byte(lg.Content), kenc)
	if err != nil {
		return EncryptedLog{}, err
	}
	ctMeta, err := crypto.EncAES(EncodeMetadata(lg.Ti, lg.Si, lg.LType), kenc)
	if err != nil {
		return EncryptedLog{}, err
	}
	return EncryptedLog{LID: lg.LID, CTLog: ctLog, CTMetadata: ctMeta}, nil
}

// DecryptLog recovers Ln and its metadata with Kenc (Data User side, B18).
func DecryptLog(e EncryptedLog, kenc [crypto.KeySize]byte) (Log, error) {
	content, err := crypto.DecAES(e.CTLog, kenc)
	if err != nil {
		return Log{}, err
	}
	meta, err := crypto.DecAES(e.CTMetadata, kenc)
	if err != nil {
		return Log{}, err
	}
	ti, si, lt, err := DecodeMetadata(meta)
	if err != nil {
		return Log{}, err
	}
	return Log{LID: e.LID, Content: string(content), Ti: ti, Si: si, LType: lt}, nil
}

// Storage is the Cloud Storage Provider's store of encrypted logs, keyed by
// LID (Section 11). It is in memory; the paper names no storage format.
type Storage struct {
	records map[string]EncryptedLog
}

// NewStorage returns an empty store.
func NewStorage() *Storage { return &Storage{records: map[string]EncryptedLog{}} }

// Put stores (or overwrites) a record.
func (s *Storage) Put(e EncryptedLog) {
	s.records[e.LID] = EncryptedLog{LID: e.LID, CTLog: bytes.Clone(e.CTLog), CTMetadata: bytes.Clone(e.CTMetadata)}
}

// Get returns a copy of the record for lid.
func (s *Storage) Get(lid string) (EncryptedLog, bool) {
	e, ok := s.records[lid]
	if !ok {
		return EncryptedLog{}, false
	}
	return EncryptedLog{LID: e.LID, CTLog: bytes.Clone(e.CTLog), CTMetadata: bytes.Clone(e.CTMetadata)}, true
}

// Delete removes a record (used to simulate a dropped file).
func (s *Storage) Delete(lid string) { delete(s.records, lid) }

// LIDs returns every stored LID in ascending order.
func (s *Storage) LIDs() []string {
	out := make([]string, 0, len(s.records))
	for lid := range s.records {
		out = append(out, lid)
	}
	slices.Sort(out)
	return out
}

// Len is the number of stored records.
func (s *Storage) Len() int { return len(s.records) }
