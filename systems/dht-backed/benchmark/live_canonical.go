package benchmark

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"dhtbacked/auth"
	"dhtbacked/blockchain"
	"dhtbacked/encryptor"
	"dhtbacked/ingestverifier"
	"dhtbacked/internal/cassandradht"
	"dhtbacked/internal/hashing"
	"dhtbacked/mktree"
	"dhtbacked/producer"
	"dhtbacked/storage"
)

type CanonicalObservation struct {
	Dataset   string    `json:"dataset"`
	Site      string    `json:"site"`
	Device    string    `json:"device"`
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Value     string    `json:"value"`
	Label     string    `json:"label"`
}

func (r CanonicalObservation) validate() error {
	if r.Dataset == "" || r.Site == "" || r.Device == "" || r.Type == "" || r.Timestamp.IsZero() || r.Value == "" || r.Label == "" {
		return errors.New("dht live: incomplete canonical observation")
	}
	return nil
}

type LiveOptions struct {
	StartEpoch    uint64
	EpochCapacity uint64
	Peers         int
	SeedWriters   int
	WorkDir       string
	KeyDir        string
}

type LiveResult struct{ Records, Epochs uint64 }

func loadOrCreateAEADKey(path string) ([]byte, error) {
	key, err := os.ReadFile(path)
	if err == nil {
		if len(key) != encryptor.KeyLen {
			return nil, errors.New("dht live: persisted AEAD key has wrong length")
		}
		return key, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	key = make([]byte, encryptor.KeyLen)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	if _, err := f.Write(key); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	return key, nil
}

// IngestCanonical reads MVA-PHV canonical JSONL, seals fixed-count epochs,
// and uses only bounded PostgreSQL batches plus temporary disk digest spools.
func IngestCanonical(ctx context.Context, input io.Reader, store *storage.Postgres, proofs *cassandradht.Cassandra, anchor *blockchain.PostgresAnchor, o LiveOptions) (LiveResult, error) {
	if o.StartEpoch == 0 || o.StartEpoch > 1000000 || o.EpochCapacity == 0 || o.EpochCapacity > 1000000 || o.Peers < 1 || o.SeedWriters < 1 || o.WorkDir == "" || o.KeyDir == "" || store == nil || proofs == nil || anchor == nil {
		return LiveResult{}, errors.New("dht live: invalid epoch, capacity, peers, paths or stores")
	}
	if err := os.MkdirAll(o.WorkDir, 0o700); err != nil {
		return LiveResult{}, err
	}
	if err := os.MkdirAll(o.KeyDir, 0o700); err != nil {
		return LiveResult{}, err
	}
	if _, _, err := auth.LoadOrCreateCAKeys(filepath.Join(o.KeyDir, "ca.sk"), filepath.Join(o.KeyDir, "ca.pk")); err != nil {
		return LiveResult{}, err
	}
	key, err := loadOrCreateAEADKey(filepath.Join(o.KeyDir, "aead.key"))
	if err != nil {
		return LiveResult{}, err
	}
	enc, err := encryptor.New(key)
	if err != nil {
		return LiveResult{}, err
	}
	keys, err := producer.LoadOrCreateKeys(filepath.Join(o.KeyDir, "producers"))
	if err != nil {
		return LiveResult{}, err
	}
	private := make(map[string]ed25519.PrivateKey, len(keys))
	public := make(map[string]ed25519.PublicKey, len(keys))
	for _, k := range keys {
		private[k.ID] = k.Private
		public[k.ID] = k.Public
	}
	gate := ingestverifier.New(ingestverifier.NewRegistry(public))
	if err := store.Migrate(ctx); err != nil {
		return LiveResult{}, err
	}
	if err := proofs.SeedRoutes(ctx, o.Peers); err != nil {
		return LiveResult{}, err
	}
	var result LiveResult
	epoch := o.StartEpoch
	var count uint64
	var work string
	var spool *fairDigestSpool
	var counts map[string]uint64
	batch := make([]storage.Record, 0, fairDHTCopyBatchSize)
	flush := func() error {
		if err := store.CopyRecords(ctx, batch); err != nil {
			return err
		}
		clear(batch)
		batch = batch[:0]
		return nil
	}
	start := func() error {
		if epoch > 1000000 {
			return errors.New("dht live: epoch exceeds 12-digit record ID range")
		}
		if _, err := anchor.GetRoot(ctx, epoch); err == nil {
			return fmt.Errorf("dht live: epoch %d already anchored", epoch)
		} else if !errors.Is(err, blockchain.ErrNotAnchored) {
			return err
		}
		work, err = os.MkdirTemp(o.WorkDir, "dht-epoch-")
		if err != nil {
			return err
		}
		spool, err = newFairDigestSpool(work, producer.LogTypes)
		if err != nil {
			return err
		}
		counts = make(map[string]uint64, len(producer.LogTypes))
		return nil
	}
	seal := func() error {
		if count == 0 {
			return nil
		}
		if err := flush(); err != nil {
			return err
		}
		if err := spool.Close(); err != nil {
			return err
		}
		cfg := mktree.DefaultConfig()
		cfg.EpochCapacity = count
		if cfg.EpochCapacity < 2 {
			cfg.EpochCapacity = 2
		}
		cfg.Stride = fairTreeLevels(cfg.EpochCapacity)
		if cfg.Stride > mktree.DefaultStride {
			cfg.Stride = mktree.DefaultStride
		}
		seeder := proofs.NewSeeder(ctx, o.SeedWriters)
		roots := make([]hashing.Digest, 0, len(cfg.LogTypes))
		for _, logType := range cfg.LogTypes {
			root, buildErr := buildFairSparseTree(work, spool.paths[logType], counts[logType], epoch, logType, cfg, count, seeder)
			if buildErr != nil {
				_ = seeder.Flush()
				return buildErr
			}
			roots = append(roots, root)
		}
		if err := seeder.Flush(); err != nil {
			return err
		}
		if err := anchor.CommitWithCounts(ctx, epoch, mktree.RootOfRoots(roots), cfg.EpochCapacity, count); err != nil {
			return err
		}
		if err := os.RemoveAll(work); err != nil {
			return err
		}
		result.Epochs++
		epoch++
		count = 0
		return nil
	}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), fairDHTScannerMax)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		line := bytes.TrimSpace(scanner.Bytes())
		var item CanonicalObservation
		if err := json.Unmarshal(line, &item); err != nil {
			return result, fmt.Errorf("dht live: decode record %d: %w", result.Records+1, err)
		}
		if err := item.validate(); err != nil {
			return result, err
		}
		if count == 0 {
			if err := start(); err != nil {
				return result, err
			}
		}
		// Stable source-to-type mapping; the original dataset type remains in M.
		source := sha256.Sum256([]byte(item.Dataset + "\x00" + item.Site + "\x00" + item.Device))
		logType := producer.LogTypes[int(source[0])%len(producer.LogTypes)]
		producerID := producer.IDForLogType(logType)
		id := encryptor.FormatRecordID((epoch-1)*1000000 + count)
		rec := encryptor.Record{RecordID: id, ProducerID: producerID, LogType: logType,
			TimestampNanos: item.Timestamp.UnixNano(), Message: string(line),
			Attributes: encryptor.Attributes{Host: item.Site, RequestID: item.Device, SourceIP: item.Dataset, Status: item.Label}}
		emitted, err := enc.ProcessRecord(rec, private[producerID])
		if err != nil {
			return result, err
		}
		if err := gate.Validate(emitted); err != nil {
			return result, err
		}
		leaf := counts[logType]
		counts[logType]++
		batch = append(batch, storage.Record{RecordID: id, Epoch: epoch, LogType: logType, LeafIndex: leaf, ProducerID: producerID,
			TimestampNanos: rec.TimestampNanos, C: emitted.C, T: emitted.T, AD: emitted.AD, D: emitted.D, Sigma: emitted.Sigma, ProducerPublicKey: emitted.ProducerPublicKey})
		if err := spool.Append(logType, emitted.D); err != nil {
			return result, err
		}
		count++
		result.Records++
		if len(batch) == cap(batch) {
			if err := flush(); err != nil {
				return result, err
			}
		}
		if count == o.EpochCapacity {
			if err := seal(); err != nil {
				return result, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return result, err
	}
	if result.Records == 0 {
		return result, errors.New("dht live: empty input")
	}
	if err := seal(); err != nil {
		return result, err
	}
	return result, nil
}
