package benchmark

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	contract "canonicalingestion"
	"dhtbacked/auth"
	"dhtbacked/encryptor"
	"dhtbacked/ingestverifier"
	"dhtbacked/internal/cassandradht"
	"dhtbacked/internal/clock"
	"dhtbacked/internal/hashing"
	"dhtbacked/internal/peerdht"
	"dhtbacked/mktree"
	"dhtbacked/producer"
	"dhtbacked/storage"
)

type IncrementalOptions struct {
	// Cassandra selects the active logical backend. Peers remains an explicit
	// alternative/reference for existing networked-peer tests and deployments.
	Cassandra              *cassandradht.Config
	RunID, KeyDir, WorkDir string
	EpochCapacity          uint64
	MaxRecordBytes         int
	Peers                  peerdht.Config
}
type IncrementalDHT struct {
	// Audit-only logical routing model. Zero retains the native deployment.
	// These fields never participate in ingestion/proof fingerprints.
	AuditLogicalNodes         int
	AuditRoutingModelIdentity string
	DB                        *sql.DB
	Options                   IncrementalOptions
	enc                       interface {
		ProcessRecord(encryptor.Record, ed25519.PrivateKey) (encryptor.Emitted, error)
	}
	gate      *ingestverifier.Verifier
	keys      []producer.KeyPair
	keyHash   hashing.Digest
	ca        ed25519.PublicKey
	authority *auth.CA
	Execution *contract.Execution
	Metrics   *contract.IngestMetrics
}
type DHTProgress struct {
	Total, Epoch, InEpoch uint64
	Counts                [5]uint64
}

func PrepareDHTKeys(dir string) error {
	if dir == "" {
		return errors.New("DHT persistent key directory required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if _, _, err := auth.LoadOrCreateCAKeys(filepath.Join(dir, "ca.sk"), filepath.Join(dir, "ca.pk")); err != nil {
		return err
	}
	if _, err := loadOrCreateAEADKey(filepath.Join(dir, "aead.key")); err != nil {
		return err
	}
	_, err := producer.LoadOrCreateKeys(filepath.Join(dir, "producers"))
	return err
}
func OpenIncrementalDHT(db *sql.DB, o IncrementalOptions) (*IncrementalDHT, error) {
	if db == nil || o.RunID == "" || o.EpochCapacity < 2 || o.EpochCapacity > 1000000 || o.KeyDir == "" || o.WorkDir == "" || o.MaxRecordBytes < 1 {
		return nil, errors.New("dht incremental: invalid explicit native geometry/paths/budgets")
	}
	if o.Cassandra != nil {
		if err := o.Cassandra.ValidateActive(); err != nil {
			return nil, err
		}
	} else {
		if err := o.Peers.Validate(); err != nil {
			return nil, err
		}
	}
	// Ingestion restores pre-provisioned keys. It never replaces a missing key
	// during a restart; provisioning is a separate explicit operation.
	key, err := os.ReadFile(filepath.Join(o.KeyDir, "aead.key"))
	if err != nil {
		return nil, err
	}
	enc, err := encryptor.New(key)
	if err != nil {
		return nil, err
	}
	keys, err := producer.LoadKeys(filepath.Join(o.KeyDir, "producers"))
	if err != nil {
		return nil, err
	}
	pub, err := auth.LoadCAPublicKey(filepath.Join(o.KeyDir, "ca.pk"))
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(pub, o.proofAuthority()) {
		return nil, errors.New("dht incremental: proof-store and ingestion authorities differ")
	}
	privateCA, err := os.ReadFile(filepath.Join(o.KeyDir, "ca.sk"))
	if err != nil {
		return nil, err
	}
	authority, err := auth.NewCA(ed25519.PrivateKey(privateCA), clock.System{})
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(authority.PublicKey(), pub) {
		return nil, errors.New("dht incremental: CA key pair differs")
	}
	registry := map[string]ed25519.PublicKey{}
	for _, k := range keys {
		registry[k.ID] = k.Public
	}
	return &IncrementalDHT{DB: db, Options: o, enc: enc, gate: ingestverifier.New(ingestverifier.NewRegistry(registry)), keys: keys, keyHash: hashing.H(key), ca: pub, authority: authority}, nil
}
func (a *IncrementalDHT) ConfigHash() string {
	var pub []byte
	for _, k := range a.keys {
		pub = append(pub, k.Public...)
	}
	if a.Options.Cassandra != nil {
		b, _ := json.Marshal(struct {
			Version                 int
			Backend, RunID, Overlay string
			LogicalNodes            int
			Capacity                uint64
			KeyHash                 hashing.Digest
			Public, CA              []byte
		}{2, "logical-cassandra-v1", a.Options.RunID, a.Options.overlay(), a.Options.Cassandra.Peers, a.Options.EpochCapacity, a.keyHash, pub, a.ca})
		h := sha256.Sum256(b)
		return hex.EncodeToString(h[:])
	}
	b, _ := json.Marshal(struct {
		Version        int
		RunID, Overlay string
		Capacity       uint64
		KeyHash        hashing.Digest
		Public, CA     []byte
	}{1, a.Options.RunID, a.Options.Peers.Overlay, a.Options.EpochCapacity, a.keyHash, pub, a.ca})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func (a *IncrementalDHT) Migrate(ctx context.Context) error {
	if err := (&storage.SQLStore{DB: a.DB}).Migrate(ctx); err != nil {
		return err
	}
	_, err := a.DB.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS dhtbacked.dht_ingestion_epochs(run_id text NOT NULL,epoch bigint PRIMARY KEY,record_count bigint NOT NULL,capacity bigint NOT NULL,counts bytea NOT NULL,source_highwater bigint NOT NULL,root bytea,published_at timestamptz);
 CREATE TABLE IF NOT EXISTS dhtbacked.dht_epoch_anchors(epoch bigint PRIMARY KEY,root bytea NOT NULL CHECK(octet_length(root)=32),capacity bigint NOT NULL,record_count bigint NOT NULL,committed_at timestamptz NOT NULL DEFAULT now());
 CREATE INDEX IF NOT EXISTS dht_pending_epochs ON dhtbacked.dht_ingestion_epochs(run_id,epoch) WHERE published_at IS NULL;
 CREATE INDEX IF NOT EXISTS dht_completed_epochs ON dhtbacked.dht_ingestion_epochs(run_id,epoch DESC) INCLUDE(source_highwater) WHERE published_at IS NOT NULL;`)
	return err
}
func (a *IncrementalDHT) Apply(ctx context.Context, tx *sql.Tx, p contract.Progress, entries []contract.Entry) ([]string, []byte, error) {
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
	type decodedRecord struct {
		Item      CanonicalObservation
		TypeIndex int
	}
	decoded, err := contract.PrepareSlice(ctx, a.Execution, a.Metrics, entries, func(e contract.Entry) int { return len(e.Record)*4 + 2048 }, func(ctx context.Context, e contract.Entry) (decodedRecord, int, error) {
		if len(e.Record) > a.Options.MaxRecordBytes {
			return decodedRecord{}, 0, errors.New("dht incremental: record byte limit exceeded")
		}
		var item CanonicalObservation
		if err := json.Unmarshal(e.Record, &item); err != nil {
			return decodedRecord{}, 0, err
		}
		if err := item.validate(); err != nil {
			return decodedRecord{}, 0, err
		}
		source := sha256.Sum256([]byte(item.Dataset + "\x00" + item.Site + "\x00" + item.Device))
		return decodedRecord{item, int(source[0]) % len(producer.LogTypes)}, len(e.Record) + 512, nil
	})
	if err != nil {
		return nil, nil, err
	}
	type encryptionTask struct {
		Record encryptor.Record
		Native storage.Record
		Key    ed25519.PrivateKey
	}
	tasks := make([]encryptionTask, 0, len(entries))
	for i, e := range entries {
		if len(e.Record) > a.Options.MaxRecordBytes || st.Epoch > 1000000 {
			return nil, nil, errors.New("dht incremental: record/ID capacity limit exceeded")
		}
		item, typeIndex := decoded[i].Item, decoded[i].TypeIndex
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
		tasks = append(tasks, encryptionTask{rec, storage.Record{RecordID: id, Epoch: st.Epoch, LogType: logType, LeafIndex: st.Counts[typeIndex], ProducerID: producerID, TimestampNanos: rec.TimestampNanos}, key})
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
	records, err := contract.PrepareSlice(ctx, a.Execution, a.Metrics, tasks, func(t encryptionTask) int { return len(t.Record.Message)*6 + 4096 }, func(ctx context.Context, t encryptionTask) (storage.Record, int, error) {
		emitted, err := a.enc.ProcessRecord(t.Record, t.Key)
		if err != nil {
			return storage.Record{}, 0, err
		}
		if err = a.gate.Validate(emitted); err != nil {
			return storage.Record{}, 0, err
		}
		r := t.Native
		r.C, r.T, r.AD, r.D, r.Sigma, r.ProducerPublicKey = emitted.C, emitted.T, emitted.AD, emitted.D, emitted.Sigma, emitted.ProducerPublicKey
		return r, len(r.C) + len(r.T) + len(r.AD) + len(r.Sigma) + len(r.ProducerPublicKey) + 512, nil
	})
	if err != nil {
		return nil, nil, err
	}
	rows := make([][]any, 0, len(records))
	for _, r := range records {
		rows = append(rows, []any{r.RecordID, int64(r.Epoch), r.LogType, int64(r.LeafIndex), r.ProducerID, r.TimestampNanos, r.C, r.T, r.AD, r.D[:], r.Sigma, []byte(r.ProducerPublicKey)})
	}
	maxRows, maxBytes := 1000, 8<<20
	if a.Execution != nil {
		maxRows = a.Execution.BatchMaxRecords
		maxBytes = a.Execution.BatchMaxBytes
	}
	if err = contract.InsertRows(ctx, tx, `INSERT INTO dhtbacked.records(record_id,epoch,log_type,leaf_index,producer_id,timestamp_ns,c,t,ad,d,sigma,producer_public_key) VALUES `, "", rows, maxRows, maxBytes, a.Metrics); err != nil {
		return nil, nil, err
	}
	checkpoint, err := json.Marshal(st)
	return ids, checkpoint, err
}

// PublicationReady is executed in the same transaction that marks the manual
// stage complete. Unpublished outbox epochs never qualify as a benchmark state.
func (a *IncrementalDHT) PublicationReady(ctx context.Context, tx *sql.Tx, p contract.Progress) error {
	var pending bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM dhtbacked.dht_ingestion_epochs WHERE run_id=$1 AND published_at IS NULL)`, a.Options.RunID).Scan(&pending); err != nil {
		return err
	}
	if pending {
		return errors.New("dht incremental: authentication publication is pending")
	}
	var total uint64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(sum(record_count),0) FROM dhtbacked.dht_ingestion_epochs WHERE run_id=$1 AND published_at IS NOT NULL`, a.Options.RunID).Scan(&total); err != nil {
		return err
	}
	if total != p.Committed {
		return fmt.Errorf("dht incremental: published %d records, logical cursor %d", total, p.Committed)
	}
	return nil
}

func epochConfig(capacity uint64) mktree.Config {
	cfg := mktree.DefaultConfig()
	cfg.EpochCapacity = capacity
	cfg.Stride = min(fairTreeLevels(capacity), mktree.DefaultStride)
	return cfg
}
