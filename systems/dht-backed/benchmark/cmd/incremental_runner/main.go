package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
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
	execution := contract.DefaultExecution()
	execution.Flags(flag.CommandLine)
	follow := flag.Bool("follow", false, "consume live finalized prefixes")
	managed := flag.Bool("managed", false, "stop when supervisor stdin closes")
	poll := flag.Duration("poll-interval", 500*time.Millisecond, "published prefix polling interval")
	envFile := flag.String("env-file", "", "optional local deployment environment profile")
	prepare := flag.Bool("prepare-keys", false, "provision persistent keys only; does not ingest")
	mode := flag.String("mode", "", "one cumulative target: 1M, 10M, 50M, 100M, 250M")
	runID := flag.String("run", "", "persistent DHT ingestion identity")
	stream := flag.String("stream", "canonical-v1", "published canonical stream ID")
	proofConfig := flag.String("dht-config", "internal/cassandradht/deployment.json", "four-logical-node Cassandra deployment JSON")
	keys := flag.String("key-dir", "./dht-live-keys", "existing persistent encryption/producer/CA keys")
	work := flag.String("work-dir", "./dht-live-work", "bounded pending-epoch construction spool")
	capacity := flag.Uint64("epoch-capacity", 1000, "native DHT epoch capacity, unchanged across stages")
	rows := flag.Int("batch-records", 1000, "canonical transaction row limit")
	bytes := flag.Int("batch-bytes", 8<<20, "canonical transaction byte limit")
	recordBytes := flag.Int("record-bytes", 1<<20, "maximum canonical observation bytes")
	maxUnpublished := flag.Uint64("max-unpublished-epochs", 0, "follow only: publish proofs in the background, letting at most this many sealed epochs wait; 0 publishes after every step")
	flag.Parse()
	if err := execution.ConfigureRuntime(); err != nil {
		return err
	}
	if err := contract.LoadEnv(*envFile); err != nil {
		return err
	}
	if *prepare {
		if *mode != "" {
			return fmt.Errorf("choose key provisioning or one ingestion mode")
		}
		return benchmark.PrepareDHTKeys(*keys)
	}
	target, err := contract.Target(*mode)
	if err != nil {
		return err
	}
	ctx, stop := contract.ManagedContext(*managed)
	defer stop()
	metrics := &contract.IngestMetrics{}
	ctx = contract.WithIngestMetrics(ctx, metrics)
	sourceDB, db, closeDatabases, err := contract.OpenIngestionDatabases(ctx, execution.SQLPoolSize)
	if err != nil {
		return err
	}
	defer closeDatabases()
	stopMetrics := metrics.Reporter(ctx, os.Stderr, execution, map[string]*sql.DB{"source": sourceDB, "native": db})
	defer stopMetrics()
	if err = benchmark.RequireBaselineDatabase(ctx, db); err != nil {
		return err
	}
	if *follow {
		if err = (contract.Store{DB: db}).Migrate(ctx); err != nil {
			return err
		}
		var exists bool
		if err = db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM canonical_ingestion.consumers WHERE consumer_id=$1)", "dht/"+*runID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			if err = benchmark.PrepareDHTKeys(*keys); err != nil {
				return err
			}
		}
	}
	deployment, proofOptions, err := cassandradht.LoadDeployment(*proofConfig)
	if err != nil {
		return err
	}
	source := contract.Store{DB: sourceDB}
	if err = source.Migrate(ctx); err != nil {
		return err
	}
	var manifest contract.Manifest
	if *follow {
		manifest, err = source.WaitManifest(ctx, *stream, *poll)
	} else {
		manifest, err = source.Manifest(ctx, *stream)
	}
	if err != nil {
		return err
	}
	a, err := benchmark.OpenIncrementalDHT(db, benchmark.IncrementalOptions{RunID: *runID, KeyDir: *keys, WorkDir: *work, EpochCapacity: *capacity, MaxRecordBytes: *recordBytes, Cassandra: &proofOptions})
	if err != nil {
		return err
	}
	store := contract.Store{DB: db}
	a.Execution, a.Metrics = &execution, metrics
	if err = store.Migrate(ctx); err != nil {
		return err
	}
	c := contract.Consumer{Store: store, ID: "dht/" + *runID, StreamID: *stream, Fingerprint: manifest.Fingerprint, ConfigHash: a.ConfigHash(), DeferCompletion: true}
	c.Execution, c.Metrics = &execution, metrics
	lease, err := c.Acquire(ctx)
	if err != nil {
		return err
	}
	defer lease.Close()
	if err = a.Migrate(ctx); err != nil {
		return err
	}
	if err = c.Open(ctx); err != nil {
		return fmt.Errorf("DHT durable configuration: %w; peer-backed state requires explicit proof migration, never automatic reset or re-ingestion", err)
	}
	current, err := c.Progress(ctx)
	if err != nil {
		return err
	}
	if current.Target > current.Committed {
		metrics.Recoveries.Add(1)
	}
	if !*follow && current.Committed < target && manifest.Committed < target {
		return fmt.Errorf("publish the complete canonical target %d first (available prefix %d)", target, manifest.Committed)
	}
	p := current
	if !*follow {
		p, err = c.Begin(ctx, target)
	}
	if err != nil {
		return err
	}
	if p.Committed > target {
		fmt.Printf("current=%d requested=%d; requested target has been passed; no ingestion\n", p.Committed, target)
		return nil
	}
	proofStore, err := cassandradht.OpenPersistent(ctx, deployment, true)
	if err != nil {
		return err
	}
	defer proofStore.Close()
	identity := a.ProofIdentity(c.StreamID, c.Fingerprint)
	if err = proofStore.Bind(ctx, identity[:], p.Committed == 0); err != nil {
		return err
	}
	if p.Committed == 0 {
		err = proofStore.EnsureRoutes(ctx, proofOptions.Peers)
	} else {
		err = proofStore.CheckRoutes(ctx, proofOptions.Peers)
	}
	if err != nil {
		return err
	}
	if p.Committed == target && c.RequireMilestone(ctx, target) == nil {
		fmt.Printf("current=%d; target already durably published; no ingestion\n", p.Committed)
		return nil
	}
	publicationLimits := deployment.Limits
	publicationLimits.Workers = execution.IOWorkers
	publicationLimits.QueueBytes = execution.StorageQueueBytes()
	publicationLimits.MaxRequests = min(publicationLimits.MaxRequests, execution.BatchMaxRecords)
	publisher, err := cassandradht.NewPublisher(ctx, proofStore, publicationLimits)
	if err != nil {
		return err
	}
	defer publisher.Close()
	publisher.Metrics = metrics
	if err = a.PublishPending(ctx, publisher); err != nil {
		return err
	}
	limits := contract.Limits{MaxRecords: *rows, MaxBatchBytes: *bytes, MaxRecordBytes: *recordBytes}
	if *follow && *maxUnpublished > 0 {
		// One background publisher; stages still complete only after Drain and
		// the unchanged PublicationReady check.
		followCtx, stopFollow := context.WithCancel(ctx)
		defer stopFollow()
		pipeline := benchmark.StartPipelinedPublication(ctx, func(ctx context.Context) error { return a.PublishPending(ctx, publisher) }, a.UnpublishedEpochs, *maxUnpublished, stopFollow)
		p, err = c.FollowMilestones(followCtx, source, target, limits, a.Apply, *poll, pipeline.AfterStep,
			func(ctx context.Context) error {
				if err := pipeline.Drain(ctx); err != nil {
					return err
				}
				return c.CompleteStage(ctx, a.PublicationReady)
			})
		if closeErr := pipeline.Close(); closeErr != nil {
			return errors.Join(closeErr, err)
		}
		if err != nil {
			return err
		}
	} else if *follow {
		p, err = c.FollowMilestones(ctx, source, target, limits, a.Apply, *poll,
			func(ctx context.Context) error { return a.PublishPending(ctx, publisher) },
			func(ctx context.Context) error { return c.CompleteStage(ctx, a.PublicationReady) })
		if err != nil {
			return err
		}
	} else {
		for p.Committed < target {
			p, err = c.Step(ctx, source, limits, a.Apply)
			if err != nil {
				return err
			}
			if err = a.PublishPending(ctx, publisher); err != nil {
				return err
			}
		}
		if err = c.CompleteStage(ctx, a.PublicationReady); err != nil {
			return err
		}
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		Consumer                         string
		Committed, Target                uint64
		Stopped, AuthenticationPublished bool
	}{c.ID, p.Committed, p.Target, true, true})
}
