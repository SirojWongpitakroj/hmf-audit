package cassandradht

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"dhtbacked/internal/hashing"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

// OpenPersistent connects to a Cassandra cluster. Initialization creates only
// missing schema; it never truncates proof history or changes existing RF.
func OpenPersistent(ctx context.Context, d Deployment, initialize bool) (*Cassandra, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	cluster := gocql.NewCluster(d.Hosts...)
	cluster.Port = d.Port
	cluster.Timeout = time.Duration(d.TimeoutSeconds) * time.Second
	cluster.ConnectTimeout = 5 * time.Second
	cluster.Consistency = gocql.LocalQuorum
	cluster.SerialConsistency = gocql.LocalSerial
	cluster.PageSize = 128
	if d.LocalDC != "" {
		cluster.PoolConfig.HostSelectionPolicy = gocql.TokenAwareHostPolicy(gocql.DCAwareRoundRobinPolicy(d.LocalDC))
	}
	session, err := cluster.CreateSession()
	if err != nil {
		return nil, fmt.Errorf("cassandradht: connect cluster: %w", err)
	}
	c := &Cassandra{session: session, keyspace: d.Keyspace, maxValueBytes: d.Limits.MaxValueBytes}
	if initialize {
		if err = c.ensureSchema(ctx, d); err != nil {
			c.Close()
			return nil, err
		}
	}
	return c, nil
}
func (c *Cassandra) table(name string) string {
	if c.keyspace == "" {
		return name
	}
	return c.keyspace + "." + name
}
func (c *Cassandra) ensureSchema(ctx context.Context, d Deployment) error {
	dc := d.LocalDC
	if dc == "" {
		if err := c.session.Query(`SELECT data_center FROM system.local`).ScanContext(ctx, &dc); err != nil {
			return err
		}
	}
	dc = strings.ReplaceAll(dc, "'", "''")
	statement := fmt.Sprintf("CREATE KEYSPACE IF NOT EXISTS %s WITH replication = {'class':'NetworkTopologyStrategy','%s':%d} AND durable_writes=true", d.Keyspace, dc, d.ReplicationFactor)
	if err := c.session.Query(statement).ExecContext(ctx); err != nil {
		return err
	}
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS ` + c.table("proof_entries") + ` (proof_key blob PRIMARY KEY,payload blob)`,
		`CREATE TABLE IF NOT EXISTS ` + c.table("routing_table") + ` (peer int,level int,next_peer int,PRIMARY KEY(peer,level))`,
		`CREATE TABLE IF NOT EXISTS ` + c.table("store_identity") + ` (singleton text PRIMARY KEY,identity blob)`,
	} {
		if err := c.session.Query(q).ExecContext(ctx); err != nil {
			return err
		}
	}
	return nil
}

// ReplicationFactor reads the live keyspace replication, summed over data
// centers. deployment.json only applies when the keyspace is first created.
func (c *Cassandra) ReplicationFactor(ctx context.Context) (int, error) {
	var replication map[string]string
	if err := c.session.Query(`SELECT replication FROM system_schema.keyspaces WHERE keyspace_name=?`, c.keyspace).ScanContext(ctx, &replication); err != nil {
		return 0, fmt.Errorf("cassandradht: keyspace replication: %w", err)
	}
	return replicationFactor(replication)
}

func replicationFactor(replication map[string]string) (int, error) {
	total := 0
	for key, value := range replication {
		if key == "class" {
			continue
		}
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("cassandradht: invalid replication %s=%q", key, value)
		}
		total += n
	}
	if total < 1 {
		return 0, errors.New("cassandradht: keyspace has no replicas")
	}
	return total, nil
}

// Bind is append-only identity initialization. A nonempty relational cursor
// requires an existing identity; missing Cassandra state is not silently reset.
func (c *Cassandra) Bind(ctx context.Context, identity []byte, create bool) error {
	if len(identity) != 32 {
		return errors.New("cassandradht: 32-byte durable identity required")
	}
	if create {
		var existing []byte
		err := c.session.Query(`SELECT identity FROM `+c.table("store_identity")+` WHERE singleton='active'`).ScanContext(ctx, &existing)
		if errors.Is(err, gocql.ErrNotFound) {
			var key []byte
			probe := c.session.Query(`SELECT proof_key FROM `+c.table("proof_entries")+` LIMIT 1`).ScanContext(ctx, &key)
			if probe == nil {
				return errors.New("cassandradht: refusing to adopt unbound existing proof history; explicit migration required")
			}
			if !errors.Is(probe, gocql.ErrNotFound) {
				return probe
			}
		} else if err != nil {
			return err
		}
		_, err = c.session.Query(`INSERT INTO `+c.table("store_identity")+`(singleton,identity) VALUES('active',?) IF NOT EXISTS`, identity).MapScanCASContext(ctx, map[string]interface{}{})
		if err != nil {
			return err
		}
	}
	var stored []byte
	if err := c.session.Query(`SELECT identity FROM `+c.table("store_identity")+` WHERE singleton='active'`).ScanContext(ctx, &stored); err != nil {
		return fmt.Errorf("cassandradht: persistent identity unavailable; restore the matching proof store: %w", err)
	}
	if !bytes.Equal(stored, identity) {
		return errors.New("cassandradht: proof store belongs to another native run/configuration/canonical stream")
	}
	return nil
}
func (c *Cassandra) EnsureRoutes(ctx context.Context, peers int) error {
	return walkRoutes(ctx, peers, 1, c.ensureRoute)
}
func (c *Cassandra) CheckRoutes(ctx context.Context, peers int) error {
	return walkRoutes(ctx, peers, 1, func(ctx context.Context, peer, level int) error {
		got, err := c.Route(ctx, peer, level)
		if err != nil {
			return err
		}
		if got != RouteNext(peer, level) {
			return errors.New("cassandradht: invalid persistent logical route")
		}
		return nil
	})
}

// PutProof acknowledges an immutable sealed-epoch proof object only after its
// Cassandra LWT succeeds. Identical replay is allowed; conflicting bytes fail.
func (c *Cassandra) PutProof(ctx context.Context, key hashing.Digest, value []byte) error {
	if c.maxValueBytes < 1 || len(value) == 0 || len(value) > c.maxValueBytes {
		return errors.New("cassandradht: immutable proof value bound exceeded")
	}
	applied, err := c.session.Query(`INSERT INTO `+c.table("proof_entries")+`(proof_key,payload) VALUES(?,?) IF NOT EXISTS`, key[:], value).MapScanCASContext(ctx, map[string]interface{}{})
	if err != nil {
		return err
	}
	if applied {
		return nil
	}
	old, err := c.Proof(ctx, key)
	if err != nil {
		return err
	}
	if !bytes.Equal(old, value) {
		return errors.New("cassandradht: conflicting sealed proof replay")
	}
	return nil
}
