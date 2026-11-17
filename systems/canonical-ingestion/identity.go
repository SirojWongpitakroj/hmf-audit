// Package canonicalingestion defines the storage-neutral identity and durable
// consumption contract shared by the four systems. It contains no experiment
// configuration and no authentication algorithm.
package canonicalingestion

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// SourceObservation is deliberately byte-for-byte compatible with MVA-PHV's
// version-1 identity. File is the source-relative path, Column the physical
// zero-based source column; neither the measurement value nor timestamp is an ID.
type SourceObservation struct {
	Dataset string `json:"dataset"`
	File    string `json:"file"`
	Sheet   string `json:"sheet,omitempty"`
	Row     uint64 `json:"row"`
	Column  int    `json:"column"`
}

func (s SourceObservation) ID() string {
	b, _ := json.Marshal(struct {
		Version int
		SourceObservation
	}{1, s})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

type Entry struct {
	Ordinal  uint64          `json:"ordinal"`
	SourceID string          `json:"source_id"`
	Record   json.RawMessage `json:"record"`
}

func (e Entry) PayloadHash() [32]byte { return sha256.Sum256(e.Record) }

func (e Entry) Validate(maxBytes int) error {
	if e.Ordinal == 0 || maxBytes <= 0 || len(e.Record) > maxBytes || !json.Valid(e.Record) {
		return errors.New("invalid canonical entry size, JSON, or ordinal")
	}
	var r struct {
		Dataset   string             `json:"dataset"`
		Source    *SourceObservation `json:"source"`
		Site      string             `json:"site"`
		Device    string             `json:"device"`
		Type      string             `json:"type"`
		Timestamp time.Time          `json:"timestamp"`
		Value     string             `json:"value"`
		Label     string             `json:"label"`
	}
	if err := json.Unmarshal(e.Record, &r); err != nil {
		return err
	}
	value := strings.TrimSpace(r.Value)
	if r.Site == "" || r.Device == "" || r.Type == "" || r.Timestamp.IsZero() || r.Label == "" || value == "" || strings.EqualFold(value, "nan") || strings.EqualFold(value, "null") {
		return errors.New("invalid canonical observation")
	}
	if r.Source == nil || r.Source.Dataset != r.Dataset || r.Source.File == "" || r.Source.Row == 0 || r.Source.Column < 0 || e.SourceID != r.Source.ID() {
		return fmt.Errorf("canonical source identity mismatch at ordinal %d", e.Ordinal)
	}
	switch r.Dataset {
	case "WADI", "HAI", "SWAT":
	default:
		return errors.New("unknown canonical dataset")
	}
	return nil
}

var milestones = [...]uint64{1000000, 10000000, 50000000, 100000000, 250000000}

func Target(mode string) (uint64, error) {
	switch mode {
	case "1M":
		return milestones[0], nil
	case "10M":
		return milestones[1], nil
	case "50M":
		return milestones[2], nil
	case "100M":
		return milestones[3], nil
	case "250M":
		return milestones[4], nil
	}
	return 0, fmt.Errorf("unknown incremental mode %q", mode)
}

func predecessor(target uint64) (uint64, error) {
	var prior uint64
	for _, n := range milestones {
		if n == target {
			return prior, nil
		}
		prior = n
	}
	return 0, fmt.Errorf("unsupported cumulative target %d", target)
}
