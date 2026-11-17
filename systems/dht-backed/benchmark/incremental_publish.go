package benchmark

import (
	contract "canonicalingestion"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"dhtbacked/internal/hashing"
	"dhtbacked/mktree"
	"dhtbacked/producer"
)

type publicationSink interface {
	fairProofSink
	Flush() error
}

// A single bounded publisher owns the proof spool. Tree preparations can run
// independently, while this guard serializes Put/Flush and retains the first
// failure so another producer cannot silently retry a failed flush.
type guardedPublicationSink struct {
	mu   sync.Mutex
	base publicationSink
	err  error
}

func (s *guardedPublicationSink) Put(k hashing.Digest, b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.err = s.base.Put(k, b)
	return s.err
}
func (s *guardedPublicationSink) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.err = s.base.Flush()
	return s.err
}

type contextProofSink struct {
	ctx  context.Context
	base fairProofSink
}

func (s contextProofSink) Put(k hashing.Digest, b []byte) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	return s.base.Put(k, b)
}

// PublishPending recovers only sealed, unpublished native epochs from their
// committed encrypted leaf digests. It never rereads raw datasets or rebuilds a
// published epoch. Replayed proof writes are byte-identical.
func (a *IncrementalDHT) PublishPending(ctx context.Context, sink publicationSink) (resultErr error) {
	if sink == nil {
		return errors.New("persistent proof publisher required")
	}
	defer func() {
		if resultErr != nil {
			if closer, ok := sink.(io.Closer); ok {
				resultErr = errors.Join(resultErr, closer.Close())
			}
		}
	}()
	if err := os.MkdirAll(a.Options.WorkDir, 0700); err != nil {
		return err
	}
	for {
		var epoch, count, capacity uint64
		var data []byte
		err := a.DB.QueryRowContext(ctx, `SELECT epoch,record_count,capacity,counts FROM dhtbacked.dht_ingestion_epochs WHERE run_id=$1 AND published_at IS NULL ORDER BY epoch LIMIT 1`, a.Options.RunID).Scan(&epoch, &count, &capacity, &data)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		var counts [5]uint64
		if err = json.Unmarshal(data, &counts); err != nil {
			return err
		}
		if err = a.publishEpoch(ctx, sink, epoch, count, capacity, counts); err != nil {
			return err
		}
	}
}
func (a *IncrementalDHT) publishEpoch(ctx context.Context, sink publicationSink, epoch, count, capacity uint64, counts [5]uint64) error {
	if count <= bufferedEpochRecords {
		return a.publishBufferedEpoch(ctx, sink, epoch, count, capacity, counts)
	}
	work, err := os.MkdirTemp(a.Options.WorkDir, "publication-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	spool, err := newFairDigestSpool(work, producer.LogTypes)
	if err != nil {
		return err
	}
	defer spool.Close()
	var total uint64
	for i, typ := range producer.LogTypes {
		var next uint64
		for next < counts[i] {
			rows, e := a.DB.QueryContext(ctx, `SELECT leaf_index,d FROM dhtbacked.records WHERE epoch=$1 AND log_type=$2 AND leaf_index >= $3 ORDER BY leaf_index LIMIT $4`, int64(epoch), typ, int64(next), a.Options.proofLimits().RecordFetchBatch)
			if e != nil {
				return e
			}
			read := 0
			for rows.Next() {
				var index uint64
				var raw []byte
				if e = rows.Scan(&index, &raw); e != nil {
					break
				}
				if index != next || len(raw) != 32 || next >= counts[i] {
					e = errors.New("dht publication: persisted digest prefix differs")
					break
				}
				var digest hashing.Digest
				copy(digest[:], raw)
				if e = spool.Append(typ, digest); e != nil {
					break
				}
				next++
				read++
			}
			readErr := rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
			if readErr != nil {
				return readErr
			}
			if read == 0 {
				return errors.New("dht publication: missing persisted digest")
			}
		}
		total += next
	}
	if total != count {
		return errors.New("dht publication: per-type counts differ")
	}
	if err = spool.Close(); err != nil {
		return err
	}
	cfg := epochConfig(capacity)
	roots := make([]hashing.Digest, len(cfg.LogTypes))
	if a.Execution == nil {
		for i, typ := range cfg.LogTypes {
			root, e := buildFairSparseTree(work, spool.paths[typ], counts[i], epoch, typ, cfg, count, sink)
			if e != nil {
				return e
			}
			roots[i] = root
		}
	} else {
		guard := &guardedPublicationSink{base: sink}
		indices := make([]int, len(cfg.LogTypes))
		for i := range indices {
			indices[i] = i
		}
		// Sparse reduction holds a 1MiB reader and writer; upper/micro trees
		// are bounded by unchanged stride/window geometry. Reserve scratch too.
		workingBytes := 3<<20 + int((uint64(1)<<(cfg.Stride+1))*32)
		err = contract.Ordered(ctx, *a.Execution, a.Metrics, indices, func(int) int { return workingBytes }, func(ctx context.Context, i int) (hashing.Digest, int, error) {
			root, err := buildFairSparseTree(work, spool.paths[cfg.LogTypes[i]], counts[i], epoch, cfg.LogTypes[i], cfg, count, contextProofSink{ctx, guard})
			return root, 32, err
		}, func(i int, root hashing.Digest) error { roots[i] = root; return nil })
		if err != nil {
			return err
		}
		sink = guard
	}
	if err = sink.Flush(); err != nil {
		return err
	}
	root := mktree.RootOfRoots(roots)
	return a.commitPublishedEpoch(ctx, epoch, count, capacity, root)
}

func (a *IncrementalDHT) commitPublishedEpoch(ctx context.Context, epoch, count, capacity uint64, root hashing.Digest) error {
	// All immutable proof objects have been durably acknowledged. Publish the
	// anchor and outbox completion atomically, never before proof-store flush.
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO dhtbacked.dht_epoch_anchors(epoch,root,capacity,record_count) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, int64(epoch), root[:], int64(capacity), int64(count))
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		var existing []byte
		if err = tx.QueryRowContext(ctx, `SELECT root FROM dhtbacked.dht_epoch_anchors WHERE epoch=$1`, int64(epoch)).Scan(&existing); err != nil {
			return err
		}
		if len(existing) != 32 || string(existing) != string(root[:]) {
			return fmt.Errorf("dht publication: conflicting anchor at epoch %d", epoch)
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE dhtbacked.dht_ingestion_epochs SET root=$3,published_at=now() WHERE run_id=$1 AND epoch=$2 AND published_at IS NULL`, a.Options.RunID, int64(epoch), root[:])
	if err != nil {
		return err
	}
	return tx.Commit()
}
