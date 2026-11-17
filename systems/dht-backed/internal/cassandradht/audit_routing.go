package cassandradht

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	contract "canonicalingestion"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

// AuditRoutingIdentity binds a routing experiment to the existing immutable
// proof namespace, logical node count, owner rule and routing algorithm. It does
// not replace store_identity/active or the native ingestion configuration.
func AuditRoutingIdentity(proofIdentity []byte, nodes int) ([32]byte, error) {
	if len(proofIdentity) != 32 {
		return [32]byte{}, errors.New("cassandradht: 32-byte proof identity required")
	}
	if _, err := contract.LogicalDHTHops(nodes); err != nil {
		return [32]byte{}, err
	}
	var count [8]byte
	binary.BigEndian.PutUint64(count[:], uint64(nodes))
	h := sha256.New()
	h.Write([]byte("logical-cassandra-audit-routing-v1/sha256-owner-modulo/xor-msb/"))
	h.Write(proofIdentity)
	h.Write(count[:])
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result, nil
}

type auditRoutingStore interface {
	checkProofIdentity(context.Context, []byte) error
	loadRoutingIdentity(context.Context, string) ([]byte, error)
	bindRoutingIdentity(context.Context, string, []byte) error
	ensureRoute(context.Context, int, int) error
	Route(context.Context, int, int) (int, error)
}

func (c *Cassandra) checkProofIdentity(ctx context.Context, identity []byte) error {
	return c.Bind(ctx, identity, false)
}

func (c *Cassandra) loadRoutingIdentity(ctx context.Context, name string) ([]byte, error) {
	var identity []byte
	err := c.session.Query(`SELECT identity FROM `+c.table("store_identity")+` WHERE singleton=?`, name).ScanContext(ctx, &identity)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, ErrNotFound
	}
	return identity, err
}

func (c *Cassandra) bindRoutingIdentity(ctx context.Context, name string, identity []byte) error {
	return ensureRoutingMetadata(ctx, "model "+name, func(ctx context.Context, serial bool) error {
		var stored []byte
		query := c.session.Query(`SELECT identity FROM `+c.table("store_identity")+` WHERE singleton=?`, name)
		if serial {
			query.Consistency(gocql.LocalSerial)
		}
		if err := query.ScanContext(ctx, &stored); err != nil {
			return err
		}
		if !bytes.Equal(stored, identity) {
			return errors.New("cassandradht: conflicting audit routing identity")
		}
		return nil
	}, func(ctx context.Context) error {
		_, err := c.session.Query(`INSERT INTO `+c.table("store_identity")+`(singleton,identity) VALUES(?,?) IF NOT EXISTS`, name, identity).MapScanCASContext(ctx, map[string]interface{}{})
		return err
	})
}

func (c *Cassandra) ensureRoute(ctx context.Context, peer, level int) error {
	want := RouteNext(peer, level)
	return ensureRoutingMetadata(ctx, fmt.Sprintf("route (%d,%d)", peer, level), func(ctx context.Context, serial bool) error {
		var got int
		query := c.session.Query(`SELECT next_peer FROM `+c.table("routing_table")+` WHERE peer=? AND level=?`, peer, level)
		if serial {
			query.Consistency(gocql.LocalSerial)
		}
		if err := query.ScanContext(ctx, &got); err != nil {
			return err
		}
		if got != want {
			return errors.New("cassandradht: conflicting persistent routing metadata")
		}
		return nil
	}, func(ctx context.Context) error {
		_, err := c.session.Query(`INSERT INTO `+c.table("routing_table")+`(peer,level,next_peer) VALUES(?,?,?) IF NOT EXISTS`, peer, level, want).MapScanCASContext(ctx, map[string]interface{}{})
		return err
	})
}

// Only deterministic, immutable routing metadata uses this recovery policy.
// A normal quorum read skips already committed rows. An uncertain CAS must be
// resolved by a LOCAL_SERIAL read, which finishes pending Paxos work; a normal
// read alone is not enough to decide the CAS outcome. Never lower consistency
// or overwrite a conflicting value to make initialization succeed.
func ensureRoutingMetadata(ctx context.Context, label string, check func(context.Context, bool) error, insert func(context.Context) error) error {
	const attempts = 5
	serial := false
	var err error
	for attempt := 0; attempt < attempts; attempt++ {
		if err = ctx.Err(); err != nil {
			return err
		}
		err = check(ctx, serial)
		if err == nil {
			return nil
		}
		if routingMetadataMissing(err) {
			writeErr := insert(ctx)
			if writeErr != nil && !retryableRoutingError(writeErr) {
				return fmt.Errorf("cassandradht: initialize %s: %w", label, writeErr)
			}
			serial = writeErr != nil
			err = check(ctx, serial)
			if err == nil {
				return nil
			}
			if routingMetadataMissing(err) && writeErr != nil {
				err = writeErr
			}
		}
		if !routingMetadataMissing(err) && !retryableRoutingError(err) {
			return fmt.Errorf("cassandradht: validate %s: %w", label, err)
		}
		if attempt+1 < attempts {
			if waitErr := contract.Wait(ctx, (100*time.Millisecond)<<attempt); waitErr != nil {
				return waitErr
			}
		}
	}
	return fmt.Errorf("cassandradht: routing setup %s unresolved after %d attempts; check Cassandra replica health, connectivity and local-DC replication, then rerun to resume setup: %w", label, attempts, err)
}

func routingMetadataMissing(err error) bool {
	return errors.Is(err, ErrNotFound) || errors.Is(err, gocql.ErrNotFound)
}

func retryableRoutingError(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var request gocql.RequestError
	if errors.As(err, &request) {
		switch request.Code() {
		case gocql.ErrCodeCASWriteUnknown, gocql.ErrCodeWriteTimeout, gocql.ErrCodeReadTimeout,
			gocql.ErrCodeUnavailable, gocql.ErrCodeOverloaded, gocql.ErrCodeBootstrapping,
			gocql.ErrCodeReadFailure, gocql.ErrCodeWriteFailure:
			return true
		}
	}
	var network net.Error
	return errors.Is(err, gocql.ErrTimeoutNoResponse) || errors.Is(err, gocql.ErrConnectionClosed) ||
		errors.Is(err, gocql.ErrNoConnections) || errors.As(err, &network)
}

// PrepareAuditRouting provisions only missing, deterministic routing rows and
// a separate model marker, outside measurement. Existing proofs, native identity
// and routing rows are never overwritten. Interrupted setup is idempotent.
func (c *Cassandra) PrepareAuditRouting(ctx context.Context, proofIdentity []byte, nodes, workers int) ([32]byte, error) {
	return prepareAuditRouting(ctx, c, proofIdentity, nodes, workers)
}

func prepareAuditRouting(ctx context.Context, store auditRoutingStore, proofIdentity []byte, nodes, workers int) ([32]byte, error) {
	identity, err := AuditRoutingIdentity(proofIdentity, nodes)
	if err != nil {
		return identity, err
	}
	if workers < 1 || workers > 64 {
		return identity, errors.New("cassandradht: routing setup workers must be between 1 and 64")
	}
	if err = store.checkProofIdentity(ctx, proofIdentity); err != nil {
		return identity, err
	}
	name := fmt.Sprintf("audit-routing/xor-v1/%d", nodes)
	stored, err := store.loadRoutingIdentity(ctx, name)
	if err == nil {
		if !bytes.Equal(stored, identity[:]) {
			return identity, errors.New("cassandradht: audit routing model belongs to another proof namespace/configuration")
		}
		err = walkRoutes(ctx, nodes, workers, func(ctx context.Context, peer, level int) error {
			got, err := store.Route(ctx, peer, level)
			if err != nil {
				return err
			}
			if got != RouteNext(peer, level) {
				return errors.New("cassandradht: invalid persistent logical route")
			}
			return nil
		})
		return identity, err
	}
	if !errors.Is(err, ErrNotFound) {
		return identity, err
	}
	if err = walkRoutes(ctx, nodes, workers, store.ensureRoute); err != nil {
		return identity, err
	}
	// Publish the model marker only after every routing row is acknowledged.
	return identity, store.bindRoutingIdentity(ctx, name, identity[:])
}

// walkRoutes streams bounded setup work; it never queues a whole logical DHT.
func walkRoutes(ctx context.Context, nodes, workers int, visit func(context.Context, int, int) error) error {
	hops, err := contract.LogicalDHTHops(nodes)
	if err != nil {
		return err
	}
	if workers < 1 || workers > 64 || visit == nil {
		return errors.New("cassandradht: invalid bounded routing setup")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type route struct{ peer, level int }
	jobs := make(chan route, workers)
	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error
	for range min(workers, nodes*hops) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				if ctx.Err() != nil {
					return
				}
				if err := visit(ctx, job.peer, job.level); err != nil {
					once.Do(func() { firstErr = err; cancel() })
					return
				}
			}
		}()
	}
dispatch:
	for peer := 0; peer < nodes; peer++ {
		for level := 0; level < hops; level++ {
			select {
			case jobs <- route{peer, level}:
			case <-ctx.Done():
				break dispatch
			}
		}
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}
