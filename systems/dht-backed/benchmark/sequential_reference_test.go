// Pre-optimization Apply retained only as a compatibility/performance oracle.
package benchmark

import (
	contract "canonicalingestion"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"dhtbacked/encryptor"
	"dhtbacked/producer"
	"dhtbacked/storage"
	"encoding/json"
	"errors"
)

func (a *IncrementalDHT) applySequentialReference(ctx context.Context, tx *sql.Tx, p contract.Progress, entries []contract.Entry) ([]string, []byte, error) {
	st := DHTProgress{Epoch: 1}
	if len(p.Checkpoint) > 0 {
		if err := json.Unmarshal(p.Checkpoint, &st); err != nil {
			return nil, nil, err
		}
	}
	if st.Total != p.Committed || st.Epoch < 1 || st.InEpoch >= a.Options.EpochCapacity {
		return nil, nil, errors.New("dht incremental: checkpoint/cursor mismatch")
	}
	if p.Committed == 0 {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM dhtbacked.records) OR EXISTS(SELECT 1 FROM dhtbacked.dht_epoch_anchors)`).Scan(&exists); err != nil {
			return nil, nil, err
		}
		if exists {
			return nil, nil, errors.New("dht incremental: existing native dataset has no matching provenance; refusing implicit adoption")
		}
	}
	seal := func() error {
		if st.InEpoch == 0 {
			return nil
		}
		counts, err := json.Marshal(st.Counts)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO dhtbacked.dht_ingestion_epochs(run_id,epoch,record_count,capacity,counts,source_highwater) VALUES($1,$2,$3,$4,$5,$6)`, a.Options.RunID, int64(st.Epoch), int64(st.InEpoch), int64(max(uint64(2), st.InEpoch)), counts, int64(st.Total))
		if err != nil {
			return err
		}
		st.Epoch++
		st.InEpoch = 0
		st.Counts = [5]uint64{}
		return nil
	}
	ids := make([]string, len(entries))
	for i, e := range entries {
		if len(e.Record) > a.Options.MaxRecordBytes || st.Epoch > 1000000 {
			return nil, nil, errors.New("dht incremental: record/ID capacity limit exceeded")
		}
		var item CanonicalObservation
		if err := json.Unmarshal(e.Record, &item); err != nil {
			return nil, nil, err
		}
		if err := item.validate(); err != nil {
			return nil, nil, err
		}
		source := sha256.Sum256([]byte(item.Dataset + "\x00" + item.Site + "\x00" + item.Device))
		typeIndex := int(source[0]) % len(producer.LogTypes)
		logType := producer.LogTypes[typeIndex]
		producerID := producer.IDForLogType(logType)
		var key ed25519.PrivateKey
		for _, k := range a.keys {
			if k.ID == producerID {
				key = k.Private
				break
			}
		}
		if key == nil {
			return nil, nil, errors.New("missing producer key")
		}
		id := encryptor.FormatRecordID((st.Epoch-1)*1000000 + st.InEpoch)
		rec := encryptor.Record{RecordID: id, ProducerID: producerID, LogType: logType, TimestampNanos: item.Timestamp.UnixNano(), Message: string(e.Record), Attributes: encryptor.Attributes{Host: item.Site, RequestID: item.Device, SourceIP: item.Dataset, Status: item.Label}}
		emitted, err := a.enc.ProcessRecord(rec, key)
		if err != nil {
			return nil, nil, err
		}
		if err = a.gate.Validate(emitted); err != nil {
			return nil, nil, err
		}
		record := storage.Record{RecordID: id, Epoch: st.Epoch, LogType: logType, LeafIndex: st.Counts[typeIndex], ProducerID: producerID, TimestampNanos: rec.TimestampNanos, C: emitted.C, T: emitted.T, AD: emitted.AD, D: emitted.D, Sigma: emitted.Sigma, ProducerPublicKey: emitted.ProducerPublicKey}
		if err = storage.InsertTransaction(ctx, tx, record); err != nil {
			return nil, nil, err
		}
		ids[i] = id
		st.Counts[typeIndex]++
		st.InEpoch++
		st.Total++
		if st.InEpoch == a.Options.EpochCapacity {
			if err = seal(); err != nil {
				return nil, nil, err
			}
		}
	}
	if st.Total == p.Target {
		if err := seal(); err != nil {
			return nil, nil, err
		}
	}
	checkpoint, err := json.Marshal(st)
	return ids, checkpoint, err
}
