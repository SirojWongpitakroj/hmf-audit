package canonicalingestion

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// AuditRequest is experiment-neutral. The experiment owns its audit size,
// selection seed, repeated-run policy and artifact paths.
type AuditRequest struct {
	Version        int     `json:"version"`
	StreamID       string  `json:"stream_id"`
	Fingerprint    string  `json:"fingerprint"`
	ExpectedN      uint64  `json:"expected_n"`
	PrefixSHA256   string  `json:"prefix_sha256"`
	ArtifactSHA256 string  `json:"artifact_sha256,omitempty"`
	Entries        []Entry `json:"entries"`
}

func (r AuditRequest) Validate(l Limits) error {
	if err := l.Validate(); err != nil {
		return err
	}
	if r.Version != 1 || r.StreamID == "" || r.Fingerprint == "" || r.ExpectedN == 0 || len(r.Entries) == 0 || len(r.Entries) > l.MaxRecords {
		return errors.New("invalid bounded audit workload")
	}
	if digest, err := hex.DecodeString(r.PrefixSHA256); err != nil || len(digest) != 32 {
		return errors.New("audit requires the exact committed dataset prefix digest")
	}
	size := 0
	sources := map[string]bool{}
	ordinals := map[uint64]bool{}
	for _, e := range r.Entries {
		if err := e.Validate(l.MaxRecordBytes); err != nil {
			return err
		}
		if e.Ordinal > r.ExpectedN || sources[e.SourceID] || ordinals[e.Ordinal] {
			return errors.New("audit workload has duplicate/out-of-range identity")
		}
		size += len(e.Record) + len(e.SourceID) + 64
		if size > l.MaxBatchBytes {
			return errors.New("audit workload exceeds byte budget")
		}
		sources[e.SourceID] = true
		ordinals[e.Ordinal] = true
	}
	return nil
}
func (r AuditRequest) Digest() string {
	b, _ := json.Marshal(struct {
		Version               int
		StreamID, Fingerprint string
		Entries               []Entry
	}{r.Version, r.StreamID, r.Fingerprint, r.Entries})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

type Resolved struct {
	Entry    Entry
	NativeID string
}

// ResolveMappings seeks only the requested identities. Its storage read belongs
// inside the scheme's query-resolution/end-to-end measurement boundary.
func (c Consumer) ResolveMappings(ctx context.Context, r AuditRequest) ([]Resolved, error) {
	if r.StreamID != c.StreamID || r.Fingerprint != c.Fingerprint {
		return nil, errors.New("audit stream differs from baseline dataset")
	}
	ids := make([]string, len(r.Entries))
	positions := make(map[string]int, len(ids))
	for i, e := range r.Entries {
		ids[i] = e.SourceID
		positions[e.SourceID] = i
	}
	rows, err := c.Store.DB.QueryContext(ctx, `SELECT ordinal,source_id,CASE WHEN octet_length(native_id)<=256 THEN native_id ELSE NULL END,payload_hash FROM canonical_ingestion.mappings WHERE consumer_id=$1 AND source_id=ANY($2::text[])`, c.ID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Resolved, len(ids))
	found := make([]bool, len(ids))
	for rows.Next() {
		var n uint64
		var source, native string
		var hash []byte
		if err = rows.Scan(&n, &source, &native, &hash); err != nil {
			return nil, err
		}
		i, ok := positions[source]
		if !ok || found[i] || r.Entries[i].Ordinal != n {
			return nil, errors.New("native source mapping differs")
		}
		expected := sha256.Sum256(r.Entries[i].Record)
		if !bytes.Equal(hash, expected[:]) {
			return nil, errors.New("canonical payload digest differs across schemes")
		}
		out[i] = Resolved{Entry: r.Entries[i], NativeID: native}
		found[i] = true
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for i, ok := range found {
		if !ok {
			return nil, fmt.Errorf("canonical observation %d missing from baseline", r.Entries[i].Ordinal)
		}
	}
	return out, nil
}

type AuditMetrics struct {
	ApplicationCacheResetBefore, ApplicationCacheResetAfter                                                        bool
	ProofCacheHits, ProofCacheMisses                                                                               uint64
	ApplicationCacheHits, ApplicationCacheMisses                                                                   uint64
	LocatorPageReferences, UniqueLocatorPages, HMFNodes                                                            uint64
	UniqueBlocksLoadedExact                                                                                        bool
	PhysicalRowsRead, PhysicalReadBytes                                                                            uint64
	TransactionQueries, TransactionRowsLoaded, TransactionLogicalVisits, UniqueBlocksLoaded                        uint64
	RequestCacheHits, RequestCacheMisses, RequestCacheBytes, RequestCachePeakBytes                                 uint64
	HistoryStateRowsRead, HistoryRecordRowsRead, UniquePhysicalHistoryRows, SignatureChecks, LogicalHistoryRecords uint64
	SerializationTime                                                                                              time.Duration
	EvidenceSizeAccountingTime                                                                                     time.Duration
	PeakConcurrentAuditJobs                                                                                        int
	DHTLookupMode                                                                                                  string
	LogicalRoutingSteps, CassandraRoutingReads, CassandraProofReads, CassandraReadBytes                            uint64
	DHTOwnerLookups                                                                                                []uint64
	DHTLogicalNodes, DHTRoutingHopsPerLookup                                                                       int
	DHTRoutingModelIdentity                                                                                        string
	DHTPeerRTT, DHTModeledPeerDelay, DHTObservedPeerDelay                                                          time.Duration
	DHTDelayedHops                                                                                                 uint64
	AncestorGroups, AncestorPathRecomputations, AncestorReuseHits                                                  uint64
	EndToEnd, QueryResolution, ProofConstruction, Retrieval, Verification                                          time.Duration
	EvidenceBytes, BitmapBytes, ProofBytes                                                                         uint64
	StorageReads, StorageWrites, StorageReadBytes, StorageWriteBytes                                               uint64
	CacheEnabled                                                                                                   bool
	CacheMaxBytes, CacheMaxEntries, CacheBytes, CacheEntries, CacheHits, CacheMisses                               uint64
	HistoryStates, HistoryBatches, HistoryRecords, HistoryBytes                                                    uint64
	DHTLookups, DHTHops, AncestorRecords, PostgresRecordRetrievals                                                 uint64
	NativeQueries, RequestedRecords, ReturnedRecords                                                               uint64
	// AuthHashInvocations is filled only by a counting pass (see WithAuthHashCount).
	AuthHashInvocations uint64
	EvidenceEncoding    string
}
type AuditSample struct {
	AllocatedBytes, PeakHeapBytes uint64
	Metrics                       AuditMetrics
	MemoryBefore, MemoryAfter     MemorySnapshot
	PeakRSSBytes                  uint64
	MemorySampleInterval          time.Duration
	ExternalProcesses             []ExternalProcess
}
type ExternalProcess struct {
	ID, Role                                                 string
	Memory                                                   MemorySnapshot
	CacheEnabled                                             bool
	CacheBytes, CacheEntries, CacheMaxBytes, CacheMaxEntries uint64
}
type AuditReport struct {
	ApplicationCachePolicy                           string
	ReadPolicy                                       *AuditReadOptions `json:",omitempty"`
	Execution                                        AuditExecution
	Scheme, Consumer, WorkloadSHA256, ArtifactSHA256 string
	N                                                uint64
	AuditSize, Warmups, Runs                         int
	Samples                                          []AuditSample
	Notes                                            []string
	Settings                                         json.RawMessage
	DatasetPrefixSHA256, DurationUnit                string
}
type AuditFunc func(context.Context, AuditRequest) (AuditMetrics, error)

func RunAudits(ctx context.Context, c Consumer, request AuditRequest, limits Limits, scheme string, warmups, runs int, interval time.Duration, measure AuditFunc) (AuditReport, error) {
	return RunAuditsObserved(ctx, c, request, limits, scheme, warmups, runs, interval, measure, nil)
}

type AuditObserver func(context.Context, *AuditMetrics) ([]ExternalProcess, error)

func RunAuditsObserved(ctx context.Context, c Consumer, request AuditRequest, limits Limits, scheme string, warmups, runs int, interval time.Duration, measure AuditFunc, observer AuditObserver) (AuditReport, error) {
	var report AuditReport
	if err := request.Validate(limits); err != nil {
		return report, err
	}
	if warmups < 0 || runs < 1 || interval <= 0 || measure == nil {
		return report, errors.New("valid repeated measurement policy required")
	}
	lease, err := c.AcquireRead(ctx)
	if err != nil {
		return report, err
	}
	defer lease.Close()
	if err = c.RequireMilestone(ctx, request.ExpectedN); err != nil {
		return report, err
	}
	report = AuditReport{ApplicationCachePolicy: ColdApplicationCachePolicy, Scheme: scheme, Consumer: c.ID, WorkloadSHA256: request.Digest(), ArtifactSHA256: request.ArtifactSHA256, N: request.ExpectedN, AuditSize: len(request.Entries), Warmups: warmups, Runs: runs, Notes: []string{"Dataset lease excludes incremental ingestion for validation, warmups and every measured request.", "Storage counters are application SQL/KV operations and bytes, not physical disk I/O.", "RSS peaks are sampled; database/server memory is separate from client process memory.", ColdApplicationCachePolicy, "Application caches are emptied before and after every warmup and measured request. Reuse within one request remains allowed."}}
	report.Execution = AuditExecutionFrom(ctx)
	report.Notes = append(report.Notes, "Independent audit jobs may overlap. Component durations are aggregate work and are not additive wall-clock stages; EndToEnd is request wall time. Repetitions and schemes run sequentially.")
	for i := 0; i < warmups+runs; i++ {
		if i == 0 {
			progress, e := c.Progress(ctx)
			if e != nil {
				return report, e
			}
			report.DatasetPrefixSHA256 = hex.EncodeToString(progress.PrefixHash[:])
			report.DurationUnit = "nanoseconds"
			if report.DatasetPrefixSHA256 != request.PrefixSHA256 {
				return report, errors.New("baseline dataset differs from published canonical prefix")
			}
		}
		if err = lease.Check(ctx); err != nil {
			return report, err
		}
		var before, after MemorySnapshot
		var monitor *MemoryMonitor
		var peak uint64
		requestCtx, activity := TrackAuditActivity(ctx)
		metrics, e := AuditWithColdCaches(requestCtx, func(ctx context.Context) (AuditMetrics, error) {
			before = ReadMemory()
			monitor = StartMemoryMonitor(interval)
			m, e := measure(ctx, request)
			peak = monitor.Stop()
			after = ReadMemory()
			return m, e
		})
		metrics.ApplicationCacheHits = metrics.CacheHits + metrics.RequestCacheHits
		metrics.ApplicationCacheMisses = metrics.CacheMisses + metrics.RequestCacheMisses
		metrics.PeakConcurrentAuditJobs = activity.Peak()
		if e != nil {
			return report, e
		}
		var external []ExternalProcess
		if observer != nil {
			external, e = observer(ctx, &metrics)
			if e != nil {
				return report, e
			}
		}
		if err = lease.Check(ctx); err != nil {
			return report, err
		}
		if err = c.RequireMilestone(ctx, request.ExpectedN); err != nil {
			return report, err
		}
		if metrics.RequestedRecords != uint64(len(request.Entries)) {
			return report, errors.New("audit did not verify the complete fixed workload")
		}
		if metrics.CacheBytes > metrics.CacheMaxBytes || metrics.CacheEntries > metrics.CacheMaxEntries {
			return report, errors.New("baseline cache exceeded configured limit")
		}
		if i >= warmups {
			report.Samples = append(report.Samples, AuditSample{AllocatedBytes: after.TotalAllocBytes - before.TotalAllocBytes, PeakHeapBytes: max(monitor.PeakHeap(), before.HeapAllocBytes, after.HeapAllocBytes), Metrics: metrics, MemoryBefore: before, MemoryAfter: after, PeakRSSBytes: max(peak, before.RSSBytes, after.RSSBytes), MemorySampleInterval: interval, ExternalProcesses: external})
			if err = writeAuditProgress(ctx, report, metrics); err != nil {
				return report, err
			}
		}
	}
	return report, nil
}
