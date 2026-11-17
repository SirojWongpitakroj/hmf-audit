package durable

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"blas/alis"
	"blas/bhash"
	"blas/internal/diskindex"
	"blas/iomf"
	"blas/live"
	"blas/query"
	"blas/verify"
	contract "canonicalingestion"
)

// Audit authenticates native attribute/time queries for the fixed logical
// observations. Transaction/file evidence is streamed one block/file at a time;
// a large candidate history never becomes one retained Stage2Result in RAM.
func (a Adapter) Audit(ctx context.Context, c contract.Consumer, request contract.AuditRequest, cache *diskindex.Cache, scan bool) (metrics contract.AuditMetrics, err error) {
	started := time.Now()
	defer func() { metrics.EndToEnd = time.Since(started) }()
	policyReads := contract.AuditReadsFrom(ctx)
	if err := policyReads.Validate(); err != nil {
		return metrics, err
	}
	var blockCache *contract.AuditReadCache[uint64, [][]byte]
	if !policyReads.Legacy {
		blockCache = contract.NewAuditReadCache[uint64, [][]byte](policyReads)
		ctx = context.WithValue(ctx, blockInputsKey{}, blockCache)
	}
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
		metrics.PhysicalReadBytes += uint64(8 + len(r.Entry.SourceID) + len(r.NativeID) + 32)
	}
	if cache == nil {
		cache, err = diskindex.NewCache(a.Options.Index)
		if err != nil {
			return metrics, err
		}
	}
	before := cache.Stats()
	headerSource := a.NewReader(ctx, cache)
	count, err := headerSource.BlockCount()
	if err != nil {
		return metrics, err
	}
	metrics.Retrieval += headerSource.IO.Read
	addAuditReader(&metrics, headerSource)
	parts := make([]contract.AuditMetrics, len(resolved))
	// Point audits are I/O-bound, as MVA CLS/CP and DHT retrieval are, so they
	// share the same I/O ceiling. Readers hold no transaction, so the equal SQL
	// pool bounds concurrent statements and GOMAXPROCS bounds CPU.
	policy := contract.AuditExecutionFrom(ctx)
	workers := policy.IOWorkers
	if contract.AuthHashCountFrom(ctx) {
		// The hash counter is process-wide, so the counting pass verifies one
		// target at a time. Scheduling changes; what is verified does not.
		workers = 1
	}
	err = contract.AuditParallel(ctx, len(resolved), workers, func(i int) error {
		var e error
		parts[i], e = a.auditTarget(ctx, resolved[i], cache, scan, count)
		return e
	})
	if err != nil {
		return metrics, err
	}
	for _, part := range parts {
		metrics.AddIndependent(part)
	}
	after := cache.Stats()
	metrics.CacheEnabled = a.Options.Index.CacheBytes > 0 && a.Options.Index.CacheEntries > 0
	metrics.CacheMaxBytes = uint64(a.Options.Index.CacheBytes)
	metrics.CacheMaxEntries = uint64(a.Options.Index.CacheEntries)
	metrics.CacheBytes = after.CacheBytes
	metrics.CacheEntries = after.CacheEntries
	metrics.CacheHits = after.CacheHits - before.CacheHits
	metrics.CacheMisses = after.CacheMisses - before.CacheMisses
	if blockCache != nil {
		st := blockCache.Stats()
		metrics.RequestCacheHits = st.Hits
		metrics.RequestCacheMisses = st.Misses
		metrics.RequestCacheBytes = st.Bytes
		metrics.RequestCachePeakBytes = st.PeakBytes
		metrics.UniqueBlocksLoaded = st.Misses
		metrics.UniqueBlocksLoadedExact = st.Evictions == 0 && st.Bypasses == 0
	}
	metrics.ProofBytes = metrics.EvidenceBytes
	metrics.StorageReadBytes = metrics.PhysicalReadBytes
	metrics.EvidenceEncoding = "streamed gob native ALIS messages with Bitmap.GobEncode; one stage-1 answer, each block/file VO, and one aggregation binding per segment"
	return metrics, nil
}

// auditTarget owns every mutable reader and auditor; only the bounded,
// synchronized page cache and immutable native dataset are shared.
func (a Adapter) auditTarget(ctx context.Context, target contract.Resolved, cache *diskindex.Cache, scan bool, count uint64) (metrics contract.AuditMetrics, err error) {
	headerSource := a.NewReader(ctx, cache)
	trusted, err := verify.NewStoredAuditor(int(count), a.Options.SegmentSize, headerSource.Header)
	if err != nil {
		return metrics, err
	}
	addEvidence := func(v any) error {
		n, e := evidenceSize(v)
		if e != nil {
			return e
		}
		metrics.EvidenceBytes += n
		metrics.BitmapBytes += bitmapVolume(v)
		return nil
	}
	countAuth := contract.AuthHashCountFrom(ctx)
	authStart := func() {
		if countAuth {
			bhash.StartCount()
		}
	}
	authStop := func() {
		if countAuth {
			metrics.AuthHashInvocations += bhash.StopCount()
		}
	}
	var o live.Observation
	if err = json.Unmarshal(target.Entry.Record, &o); err != nil {
		return metrics, err
	}
	var lid, fid, esn uint64
	expectedLID := a.lid()
	if a.Options.GeneratorMode != "" {
		_, expectedLID = generatorIdentity(o)
	}
	if n, _ := fmt.Sscanf(target.NativeID, "%d/%d/%d", &lid, &fid, &esn); n != 3 || lid != expectedLID {
		return metrics, errors.New("BLAS native source mapping invalid")
	}
	words := []string{"dataset:" + o.Dataset, "site:" + o.Site, "device:" + o.Device, "type:" + o.Type}
	expression := iomf.Keyword(words[0])
	for _, w := range words[1:] {
		expression = iomf.And(expression, iomf.Keyword(w))
	}
	ts := o.Timestamp.UnixMilli()
	q := query.QOn{LG: lid, FK: expression, HasRange: true, TS: ts, TE: ts}
	r := a.NewReader(ctx, cache)
	r.FixedBlockCount = &count
	construct := time.Now()
	s1, e := r.Stage1(q, scan)
	span := time.Since(construct)
	metrics.QueryResolution += span
	metrics.ProofConstruction += span - r.IO.Read
	metrics.Retrieval += r.IO.Read
	addAuditReader(&metrics, r)
	if e != nil {
		return metrics, e
	}
	verification := time.Now()
	ioBefore := headerSource.IO.Read
	authStart()
	candidates, ok := trusted.VerifyStage1(q, s1, scan)
	authStop()
	metrics.Verification += time.Since(verification) - (headerSource.IO.Read - ioBefore)
	if !ok {
		return metrics, errors.New("BLAS stage-1 verification rejected")
	}
	if err = addEvidence(s1); err != nil {
		return metrics, err
	}
	var kids [4]uint64
	var known [4]bool
	registry := a.NewReader(ctx, cache)
	for i, w := range words {
		if a.Options.GeneratorMode != "" {
			kids[i], known[i], e = registry.GeneratorKeywordID(lid, w)
		} else {
			kids[i], known[i], e = registry.KeywordID(w)
		}
		if e != nil {
			return metrics, e
		}
	}
	metrics.Retrieval += registry.IO.Read
	addAuditReader(&metrics, registry)
	found := false
	lastBinding := uint64(^uint64(0))
	for _, height := range candidates {
		blockReader := a.NewReader(ctx, cache)
		blockReader.FixedBlockCount = &count
		construct = time.Now()
		var s2 query.Stage2Result
		var bindings map[uint64]query.AggregatedLGBinding
		if a.Options.Aggregation {
			idx := height / a.Options.SegmentSize
			var segment query.SegmentBlockResult
			present := false
			for _, s := range s1.Segments {
				if s.SegmentBase == idx*a.Options.SegmentSize {
					segment = s
					present = true
					break
				}
			}
			if !present {
				return metrics, errors.New("BLAS candidate has no authenticated segment")
			}
			s2, bindings, e = blockReader.Stage2Aggregated(q, query.Stage1Result{Segments: []query.SegmentBlockResult{segment}, CandidateHeights: []uint64{height}})
		} else {
			s2, e = blockReader.Stage2(q, []uint64{height})
		}
		span = time.Since(construct)
		metrics.QueryResolution += span
		metrics.ProofConstruction += span - blockReader.IO.Read
		if e != nil {
			return metrics, e
		}
		verification = time.Now()
		ioBefore = headerSource.IO.Read
		var txs []alis.Transaction
		authStart()
		if a.Options.Aggregation {
			txs, ok = trusted.VerifyStage2Aggregated(q, []uint64{height}, s2, bindings)
		} else {
			txs, ok = trusted.VerifyStage2(q, []uint64{height}, s2)
		}
		authStop()
		metrics.Verification += time.Since(verification) - (headerSource.IO.Read - ioBefore)
		if !ok {
			return metrics, errors.New("BLAS stage-2 verification rejected")
		}
		if err = addEvidence(s2); err != nil {
			return metrics, err
		}
		if a.Options.Aggregation && lastBinding != height/a.Options.SegmentSize {
			if err = addEvidence(bindings); err != nil {
				return metrics, err
			}
			lastBinding = height / a.Options.SegmentSize
		}
		for _, tx := range txs {
			keep := true
			for i, kid := range kids {
				if !known[i] || kid >= tx.BmEW.Len() || !tx.BmEW.Get(kid) {
					keep = false
					break
				}
			}
			if !keep {
				continue
			}
			fileReader := a.NewReader(ctx, cache)
			eq := alis.EntryQuery{EK: expression, HasRange: true, TS: ts, TE: ts}
			construct = time.Now()
			vo, e := fileReader.Entry(tx.FID, eq)
			span = time.Since(construct)
			metrics.ProofConstruction += span - fileReader.IO.Read
			metrics.Retrieval += fileReader.IO.Read
			addAuditReader(&metrics, fileReader)
			if e != nil {
				return metrics, e
			}
			verification = time.Now()
			authStart()
			valid := alis.VerifyEntryQuery(tx.RHIIMT, tx.RHOMT, eq, vo)
			authStop()
			metrics.Verification += time.Since(verification)
			if !valid {
				return metrics, errors.New("BLAS entry verification rejected")
			}
			if err = addEvidence(vo); err != nil {
				return metrics, err
			}
			for _, proof := range vo.ResultProofs {
				entry, e := alis.DecodeLogEntry(proof.Object)
				if e != nil {
					return metrics, e
				}
				metrics.ReturnedRecords++
				if tx.FID == fid && entry.ESN == esn && bytes.Equal(entry.V, target.Entry.Record) {
					found = true
				}
			}
		}
		metrics.Retrieval += blockReader.IO.Read
		addAuditReader(&metrics, blockReader)
	}
	if !found {
		return metrics, errors.New("BLAS native queries did not authenticate requested canonical observation")
	}
	metrics.RequestedRecords++
	metrics.NativeQueries++
	metrics.Retrieval += headerSource.IO.Read
	addAuditReader(&metrics, headerSource)
	return metrics, nil
}

type blockInputsKey struct{}

func addAuditReader(m *contract.AuditMetrics, r *Reader) {
	m.StorageReads += r.IO.Statements
	m.PhysicalRowsRead += r.IO.Rows
	m.PhysicalReadBytes += r.IO.Bytes
	m.TransactionQueries += r.TransactionQueries
	m.TransactionRowsLoaded += r.TransactionRowsLoaded
	m.TransactionLogicalVisits += r.TransactionLogicalVisits
}
