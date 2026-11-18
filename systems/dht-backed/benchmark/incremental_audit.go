package benchmark

import (
	"context"

	"fmt"

	"sort"
	"strconv"
	"time"

	contract "canonicalingestion"

	"dhtbacked/blockchain"

	"dhtbacked/storage"
	"dhtbacked/verifier"
)

func (a *IncrementalDHT) Audit(ctx context.Context, c contract.Consumer, request contract.AuditRequest, client verifier.ProofSource, maxRecords int) (metrics contract.AuditMetrics, err error) {
	started := time.Now()
	defer func() { metrics.EndToEnd = time.Since(started) }()
	lookup := time.Now()
	resolved, err := c.ResolveMappings(ctx, request)
	metrics.QueryResolution = time.Since(lookup)
	metrics.Retrieval = metrics.QueryResolution
	metrics.StorageReads = 1
	if err != nil {
		return metrics, err
	}
	metrics.PhysicalRowsRead = uint64(len(resolved))
	for _, r := range resolved {
		metrics.PhysicalReadBytes += uint64(len(r.Entry.SourceID) + len(r.NativeID) + 32 + 8)
	}
	groups := map[uint64][]string{}
	for _, r := range resolved {
		n, e := strconv.ParseUint(r.NativeID, 10, 64)
		if e != nil {
			return metrics, e
		}
		epoch := n/1000000 + 1
		groups[epoch] = append(groups[epoch], r.NativeID)
	}
	epochs := make([]uint64, 0, len(groups))
	for epoch := range groups {
		epochs = append(epochs, epoch)
	}
	sort.Slice(epochs, func(i, j int) bool { return epochs[i] < epochs[j] })
	ca := a.authority
	l := a.Options.proofLimits()
	store := &storage.SQLStore{DB: a.DB, MaxRecords: maxRecords, BatchRows: l.RecordFetchBatch, MaxBytes: l.RecordBytes}
	anchor := &blockchain.SQLAnchor{DB: a.DB}
	before, err := a.snapshotProof(client)
	if err != nil {
		return metrics, err
	}
	for _, epoch := range epochs {
		fetch := time.Now()
		capacity, _, e := anchor.Geometry(ctx, epoch)
		metrics.Retrieval += time.Since(fetch)
		if e != nil {
			return metrics, e
		}
		cfg := epochConfig(capacity)
		v, e := verifier.NewBounded(store, client, anchor, cfg, verifier.RequestBounds{Records: maxRecords, ProofRequests: l.MaxRequests, MapBytes: l.AncestorMapBytes, ProofBytes: l.ProofBytes})
		if e != nil {
			return metrics, e
		}
		token := ca.Issue("canonical-audit", epoch, cfg.LogTypes)
		// Preparation is inside end-to-end timing and includes all PostgreSQL/DHT
		// retrieval. Verification consumes exactly those untrusted native records.
		fetch = time.Now()
		prepared, e := v.PrepareBatch(ctx, &token, groups[epoch])
		metrics.Retrieval += time.Since(fetch)
		if e != nil {
			return metrics, e
		}
		verifyStart := time.Now()
		result, e := v.VerifyPreparedBatchConcurrent(ctx, prepared, contract.AuditExecutionFrom(ctx).CPUWorkers)
		metrics.Verification += time.Since(verifyStart)
		if e != nil {
			return metrics, e
		}
		if !result.Accepted {
			return metrics, fmt.Errorf("DHT native verification rejected: %w", verifier.ErrRejected)
		}
		metrics.AncestorGroups += uint64(result.Ancestors)
		metrics.AncestorPathRecomputations += uint64(result.AncestorPathRecomputations)
		metrics.AncestorReuseHits += uint64(len(groups[epoch]) - result.Ancestors)
		metrics.RequestedRecords += uint64(len(groups[epoch]))
		metrics.ReturnedRecords += uint64(len(groups[epoch]))
		metrics.NativeQueries++
	}
	after, e := a.snapshotProof(client)
	if e != nil {
		return metrics, e
	}
	metrics.DHTLookups = after.lookups - before.lookups
	metrics.DHTHops = after.hops - before.hops // Reference peer backend only.
	metrics.AncestorRecords = metrics.AncestorGroups
	metrics.CacheEnabled = l.CacheEntries > 0 && l.CacheBytes > 0
	metrics.CacheMaxBytes = uint64(l.CacheBytes)
	metrics.CacheMaxEntries = uint64(l.CacheEntries)
	if !after.logical {
		metrics.CacheMaxBytes *= 4
		metrics.CacheMaxEntries *= 4
	}
	metrics.CacheBytes, metrics.CacheEntries = after.cacheBytes, after.cacheEntries
	metrics.CacheHits = after.hits - before.hits
	metrics.CacheMisses = after.misses - before.misses
	metrics.ProofCacheHits, metrics.ProofCacheMisses = metrics.CacheHits, metrics.CacheMisses
	metrics.StorageReads += store.Reads + anchor.Reads + after.reads - before.reads + after.routes - before.routes
	metrics.PostgresRecordRetrievals = store.Reads
	metrics.ProofBytes = after.bytes - before.bytes
	metrics.EvidenceBytes = metrics.ProofBytes + store.ReadBytes
	metrics.StorageReadBytes = store.ReadBytes
	metrics.PhysicalRowsRead += store.Rows + anchor.Rows + after.reads - before.reads + after.routes - before.routes
	metrics.PhysicalReadBytes += store.ReadBytes + store.ControlReadBytes + anchor.ReadBytes
	if after.logical {
		metrics.DHTLookupMode = "four-node-logical-cassandra"
		if after.logicalNodes != 4 || after.routingIdentity != "" {
			metrics.DHTLookupMode = "logical-cassandra-routing-model-v1"
		}
		metrics.DHTLogicalNodes = after.logicalNodes
		metrics.DHTRoutingHopsPerLookup = after.routingHops
		metrics.DHTRoutingModelIdentity = after.routingIdentity
		metrics.LogicalRoutingSteps = after.routes - before.routes
		metrics.CassandraRoutingReads = metrics.LogicalRoutingSteps
		metrics.CassandraProofReads = after.reads - before.reads
		metrics.CassandraReadBytes = after.readBytes - before.readBytes + 4*metrics.CassandraRoutingReads // CQL next_peer is a 32-bit int.
		metrics.StorageReadBytes += metrics.CassandraReadBytes
		metrics.PhysicalReadBytes += metrics.CassandraReadBytes
		metrics.DHTOwnerLookups = make([]uint64, len(after.owners))
		for i := range metrics.DHTOwnerLookups {
			metrics.DHTOwnerLookups[i] = after.owners[i] - before.owners[i]
		}
	} else {
		metrics.DHTLookupMode = "reference-http-peers"
	}
	metrics.StorageReadBytes = metrics.PhysicalReadBytes
	metrics.EvidenceEncoding = "native serialized parent/certificate/root records plus raw encrypted tuple field bytes; transport framing, authorization and routing overhead excluded"
	return metrics, nil
}
