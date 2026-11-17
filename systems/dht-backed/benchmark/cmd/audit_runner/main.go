package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	contract "canonicalingestion"
	"dhtbacked/benchmark"
	"dhtbacked/internal/cassandradht"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	env := flag.String("env-file", "", "deployment environment profile")
	runID := flag.String("run", "", "persistent DHT run identity")
	keys := flag.String("key-dir", "./dht-live-keys", "persistent producer/encryption/CA directory")
	proofConfig := flag.String("dht-config", "internal/cassandradht/deployment.json", "native proof-store Cassandra deployment JSON; preserve for existing datasets")
	logicalNodes := flag.Int("logical-nodes", 1024, "fixed audit routing-model node count, independent of log count and physical Cassandra hosts")
	peerRTT := flag.Duration("peer-rtt", contract.DefaultDHTPeerRTT, "additional simulated RTT per logical routing hop, on top of Cassandra latency (0 disables)")
	setupWorkers := flag.Int("routing-setup-workers", 1, "bounded routing-metadata initialization workers (1-64), separate from measured audit I/O")
	capacity := flag.Uint64("epoch-capacity", 1000, "unchanged native ingestion geometry")
	expectRF := flag.Int("expect-replication-factor", 0, "refuse to audit unless the live proof keyspace has exactly this replication factor (0 skips the check)")
	audit := contract.BindAuditFlags(flag.CommandLine)
	flag.Parse()
	if err := contract.ValidateDHTPeerRTT(*peerRTT); err != nil {
		return err
	}
	if *setupWorkers < 1 || *setupWorkers > 64 {
		return fmt.Errorf("routing-setup-workers must be between 1 and 64")
	}
	routingHops, err := contract.LogicalDHTHops(*logicalNodes)
	if err != nil {
		return err
	}
	execution := audit.Execution()
	if err := execution.SetRuntime(); err != nil {
		return err
	}
	if err := contract.LoadEnv(*env); err != nil {
		return err
	}
	request, err := contract.ReadAuditRequest(*audit.Request, audit.Limits())
	if err != nil {
		return err
	}
	deployment, logical, err := cassandradht.LoadDeployment(*proofConfig)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx = contract.WithAuditExecution(ctx, execution)
	ctx = audit.WithProgress(ctx)
	db, err := contract.OpenEnvPostgres(ctx, "BASELINES_POSTGRES", "", execution.SQLPoolSize)
	if err != nil {
		return err
	}
	defer db.Close()
	if err = benchmark.RequireBaselineDatabase(ctx, db); err != nil {
		return err
	}
	a, err := benchmark.OpenIncrementalDHT(db, benchmark.IncrementalOptions{RunID: *runID, KeyDir: *keys, WorkDir: "unused-during-audit", EpochCapacity: *capacity, MaxRecordBytes: *audit.RecordBytes, Cassandra: &logical})
	if err != nil {
		return err
	}
	c := contract.Consumer{Store: contract.Store{DB: db}, ID: "dht/" + *runID, StreamID: request.StreamID, Fingerprint: request.Fingerprint, ConfigHash: a.ConfigHash(), DeferCompletion: true}
	// Validate and hold the immutable dataset before provisioning audit-only
	// routing metadata. Neither a native identity nor proof history is adopted.
	lease, err := c.AcquireRead(ctx)
	if err != nil {
		return err
	}
	defer lease.Close()
	if err = c.RequireMilestone(ctx, request.ExpectedN); err != nil {
		return err
	}
	proofStore, err := cassandradht.OpenPersistent(ctx, deployment, false)
	if err != nil {
		return err
	}
	defer proofStore.Close()
	replicationFactor, err := proofStore.ReplicationFactor(ctx)
	if err != nil {
		return err
	}
	if *expectRF != 0 && replicationFactor != *expectRF {
		return fmt.Errorf("DHT proof keyspace replication factor is %d, comparison requires %d; follow EXP1B_FAIRNESS_CORRECTIONS.md before auditing", replicationFactor, *expectRF)
	}
	identity := a.ProofIdentity(c.StreamID, c.Fingerprint)
	setupStarted := time.Now()
	fmt.Fprintf(os.Stderr, "DHT routing model: logical_nodes=%d hops_per_lookup=%d Cassandra_contact_points=%d setup_workers=%d; validating/provisioning routing metadata before measurements\n", *logicalNodes, routingHops, len(deployment.Hosts), *setupWorkers)
	modelIdentity, err := proofStore.PrepareAuditRouting(ctx, identity[:], *logicalNodes, *setupWorkers)
	if err != nil {
		return fmt.Errorf("DHT routing setup for %d logical nodes with %d workers (before measurements): %w", *logicalNodes, *setupWorkers, err)
	}
	setupMillis := float64(time.Since(setupStarted)) / 1e6
	fmt.Fprintf(os.Stderr, "DHT routing metadata ready: logical_nodes=%d setup_ms=%.3f (outside audit timing)\n", *logicalNodes, setupMillis)
	if err = lease.Check(ctx); err != nil {
		return err
	}
	auditLogical := logical
	auditLogical.PeerRTT = *peerRTT
	auditLogical.Peers = *logicalNodes
	auditLogical.RoutingModelIdentity = hex.EncodeToString(modelIdentity[:])
	a.AuditLogicalNodes = *logicalNodes
	a.AuditRoutingModelIdentity = auditLogical.RoutingModelIdentity
	auditLimits := *logical.Limits
	auditLimits.Workers = execution.IOWorkers
	auditLogical.Limits = &auditLimits
	auditLogical.Concurrency = execution.IOWorkers
	client, err := cassandradht.New(ctx, proofStore, logical.CA, auditLogical)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "DHT simulated peer RTT: %s/hop, %s/lookup requested in addition to Cassandra queries; timer overshoot reported separately\n", *peerRTT, time.Duration(routingHops)*(*peerRTT))
	ctx = contract.WithAuditCacheReset(ctx, client.ClearAuditCache)
	report, err := contract.RunAudits(ctx, c, request, audit.Limits(), "DHT-backed", *audit.Warmups, *audit.Runs, *audit.Interval, func(ctx context.Context, r contract.AuditRequest) (contract.AuditMetrics, error) {
		return a.Audit(ctx, c, r, client, *audit.MaxRecords)
	})
	if err != nil {
		return err
	}
	report.Settings, _ = json.Marshal(struct {
		EpochCapacity                                                                  uint64
		LogicalNodes, NativeLogicalNodes, RoutingHopsPerLookup, CassandraContactPoints int
		RoutingSetupWorkers, ReplicationFactor                                         int
		ReadConsistency                                                                string
		RoutingModelIdentity                                                           string
		RoutingSetupMillis                                                             float64
		SimulatedPeerRTT, ModeledPeerDelayPerLookup                                    time.Duration
		Limits                                                                         cassandradht.Limits
		CassandraHosts                                                                 []string
		Database, ProofBackend                                                         string
	}{*capacity, *logicalNodes, logical.Peers, routingHops, len(deployment.Hosts), *setupWorkers, replicationFactor, "LOCAL_QUORUM", a.AuditRoutingModelIdentity, setupMillis, *peerRTT, time.Duration(routingHops) * (*peerRTT), auditLimits, deployment.Hosts, "baselines_db", "Cassandra"})
	report.Notes = append(report.Notes, fmt.Sprintf("Additional simulated peer RTT is %s per logical hop, calibrated from the supplied ICMP mean; this is not a measured peer-to-peer overlay RTT. Cassandra latency remains included, so this intentionally adds to existing network/storage cost. DHTObservedPeerDelay includes timer/scheduler overshoot and sums waits across workers; it is not additive wall-clock time.", *peerRTT))
	report.Notes = append(report.Notes, fmt.Sprintf("Fixed %d-node logical DHT routing model (%d routing operations per proof-key lookup) backed by %d configured Cassandra contact points; this is not a physical %d-node deployment or a reproduction of its network latency.", *logicalNodes, routingHops, len(deployment.Hosts), *logicalNodes), "Existing native ingestion/proof identity, stored records, keys, roots and payloads remain unchanged; audit routing has a separate durable identity.", "Every proof key follows deterministic modeled ownership and persisted logical routing before cache/storage lookup, including cache hits. Route setup/validation is outside timing; lookup routing reads are inside end-to-end.", "The same native ancestor-assisted verification, keys, roots and proof contents are retained. Evidence preparation and relational/Cassandra retrieval are inside end-to-end.", "RSS is the benchmark application process; Cassandra and PostgreSQL server RSS are separate. The bounded proof cache is emptied before and after every warmup and measured audit; only within-audit reuse is allowed.", "Storage byte counters include successful decoded mapping, record, aggregate, anchor and Cassandra proof/routing payloads; protocol framing is excluded.", "The root anchor remains the existing PostgreSQL append-only ledger emulator, not an independently operated blockchain.")
	return audit.Finish(report)
}
