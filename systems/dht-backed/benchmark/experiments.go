package benchmark

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"dhtbacked/blockchain"
	"dhtbacked/encryptor"
	"dhtbacked/internal/clock"
	"dhtbacked/mktree"
	"dhtbacked/producer"
	"dhtbacked/storage"
	"dhtbacked/verifier"
)

// Batch sizes fixed by Q30. The paper does not specify any of them; every
// reported comparison must say so.
const (
	// CommitBatch is B = 1000 for the default commit/epoch benchmark.
	CommitBatch = 1000
	// SingleRecordBatch is n = 1 for the single-record experiment.
	SingleRecordBatch = 1
	// ThroughputRequests is the headline throughput workload: 1,024
	// verification requests.
	ThroughputRequests = 1024
	// StrideWindowBatch is n = 1024 for the stride/window experiment.
	StrideWindowBatch = 1024
)

// ScalingBatchSizes is n in {64, 128, 256, 512, 1024, 2048, 4096} for the
// locality/batch-scaling experiment (Q30).
var ScalingBatchSizes = []int{64, 128, 256, 512, 1024, 2048, 4096}

// LargeBatchEpochCapacity is the epoch that hosts the locality/batch-scaling
// and stride experiments (GAP-7 decision, user, 2026-09-11: option B).
//
// A batch must lie within one epoch, and a locality comparison within one
// tree. Q30's n = 1024 exceeds the whole default 1,000-record epoch and its
// n = 4096 exceeds any tree in a 10,000-record epoch, so both experiments run
// in Q23's 100,000-record evaluation epoch, where every Q30 size fits in one
// tree. Its trees are padded to 131,072 leaves, so L = 17 there rather than the
// default L = 10; the report states this.
const LargeBatchEpochCapacity = uint64(100000)

// LargeBatchConfig returns base with its epoch set to LargeBatchEpochCapacity.
// Stride, window and log types are unchanged.
func LargeBatchConfig(base mktree.Config) mktree.Config {
	base.EpochCapacity = LargeBatchEpochCapacity
	return base
}

// StrideValues is s in {1, 2, 4, 8} (Q25, R-EVAL-06).
var StrideValues = []uint64{1, 2, 4, 8}

// WindowValues is W in {256, 512, 1024, 2048} (Q21, R-EVAL-06).
var WindowValues = []uint64{256, 512, 1024, 2048}

// EpochSizes are the evaluated epoch sizes (Table 5, Q23).
var EpochSizes = []uint64{1000, 10000, 100000}

// Distributions are the three input distributions (Table 5, Q29).
var Distributions = []producer.Distribution{producer.Uniform, producer.ZipfHeavy, producer.Bursty}

// Env is everything an experiment needs that lives outside the Go process.
type Env struct {
	// WorkDir holds generated key material and the producer registry.
	WorkDir string
	// AEADKey is the system-wide AEAD key.
	AEADKey []byte
	// Chain is the anchoring chain. In the fidelity reproduction this is the
	// external Hardhat process (Q38).
	Chain Chain
	// NewStore constructs a fresh data lake for one experiment run.
	NewStore func() storage.Store
	// NextEpochBase supplies a starting epoch number that has not been
	// anchored yet, so repeated runs against a live chain do not collide with
	// the contract's append-only rule.
	NextEpochBase func() uint64
}

// ingested is one prepared, anchored epoch ready to be audited.
type ingested struct {
	sys     *System
	records []encryptor.Record
	epoch   uint64
	byType  map[string][]string
}

// prepare builds a system, ingests n records under the given configuration and
// finalizes and anchors the epoch.
func prepare(env Env, cfg mktree.Config, dist producer.Distribution, n int) (*ingested, error) {
	g := producer.NewGenerator(dist)
	recs, err := g.Generate(n)
	if err != nil {
		return nil, err
	}
	return prepareRecords(env, cfg, recs)
}

// prepareRecords builds and anchors an epoch from caller-supplied records.
// It is used by the shared real-dataset benchmark; the paper reproduction
// continues to call prepare and its original synthetic generator.
func prepareRecords(env Env, cfg mktree.Config, recs []encryptor.Record) (*ingested, error) {
	sys, err := NewSystem(Options{
		Config:       cfg,
		AEADKey:      env.AEADKey,
		KeyDir:       filepath.Join(env.WorkDir, "producers"),
		RegistryPath: filepath.Join(env.WorkDir, "registry.json"),
		CADir:        env.WorkDir,
		Store:        env.NewStore(),
		Chain:        env.Chain,
		Clock:        clock.System{},
	})
	if err != nil {
		return nil, err
	}
	if env.NextEpochBase != nil {
		if err := sys.Pipeline.Partitioner().SetEpoch(env.NextEpochBase()); err != nil {
			return nil, err
		}
	}
	ctx := context.Background()
	for _, r := range recs {
		if _, err := sys.Pipeline.Ingest(ctx, r); err != nil {
			return nil, err
		}
	}
	epoch, _, err := sys.Pipeline.FinalizeEpoch(ctx)
	if err != nil {
		return nil, err
	}
	byType := map[string][]string{}
	for _, r := range recs {
		byType[r.LogType] = append(byType[r.LogType], r.RecordID)
	}
	return &ingested{sys: sys, records: recs, epoch: epoch, byType: byType}, nil
}

// SingleRecordLatency measures R-EVAL-03: single-record verification latency,
// n = 1, over exactly 50 independent repetitions.
func SingleRecordLatency(env Env, cfg mktree.Config) (Summary, error) {
	run, err := prepare(env, cfg, producer.Uniform, CommitBatch)
	if err != nil {
		return Summary{}, err
	}
	tok := run.sys.Token("auditor-1", run.epoch)
	ctx := context.Background()
	samples := make([]time.Duration, 0, Repetitions)
	for i := 0; i < Repetitions; i++ {
		id := run.records[i%len(run.records)].RecordID
		start := time.Now()
		res, err := run.sys.Verifier.VerifyInclusion(ctx, &tok, id)
		elapsed := time.Since(start)
		if err != nil || !res.Accepted {
			return Summary{}, fmt.Errorf("single-record verification failed: %v", err)
		}
		samples = append(samples, elapsed)
	}
	return SummarizeDurations(samples), nil
}

// VerificationThroughput measures R-EVAL-04 in verifications per second, using
// the Q30 headline workload of 1,024 verification requests, over exactly 50
// repetitions.
func VerificationThroughput(env Env, cfg mktree.Config) (Summary, error) {
	run, err := prepare(env, cfg, producer.Uniform, CommitBatch)
	if err != nil {
		return Summary{}, err
	}
	tok := run.sys.Token("auditor-1", run.epoch)
	ctx := context.Background()
	requests := make([]string, 0, ThroughputRequests)
	for i := 0; i < ThroughputRequests; i++ {
		requests = append(requests, run.records[i%len(run.records)].RecordID)
	}
	rates := make([]float64, 0, Repetitions)
	for i := 0; i < Repetitions; i++ {
		start := time.Now()
		for _, id := range requests {
			res, err := run.sys.Verifier.VerifyInclusion(ctx, &tok, id)
			if err != nil || !res.Accepted {
				return Summary{}, fmt.Errorf("throughput verification failed: %v", err)
			}
		}
		elapsed := time.Since(start).Seconds()
		rates = append(rates, float64(ThroughputRequests)/elapsed)
	}
	return Summarize(rates), nil
}

// CommitTime measures R-EVAL-05: the time to construct the ADS for a new batch
// of B = 1000 records and anchor the integrity state on-chain, over exactly 50
// repetitions.
func CommitTime(env Env, cfg mktree.Config) (Summary, error) {
	samples := make([]time.Duration, 0, Repetitions)
	ctx := context.Background()
	g := producer.NewGenerator(producer.Uniform)
	recs, err := g.Generate(CommitBatch)
	if err != nil {
		return Summary{}, err
	}
	for i := 0; i < Repetitions; i++ {
		sys, err := NewSystem(Options{
			Config:       cfg,
			AEADKey:      env.AEADKey,
			KeyDir:       filepath.Join(env.WorkDir, "producers"),
			RegistryPath: filepath.Join(env.WorkDir, "registry.json"),
			CADir:        env.WorkDir,
			Store:        env.NewStore(),
			Chain:        env.Chain,
			Clock:        clock.System{},
		})
		if err != nil {
			return Summary{}, err
		}
		if env.NextEpochBase != nil {
			if err := sys.Pipeline.Partitioner().SetEpoch(env.NextEpochBase()); err != nil {
				return Summary{}, err
			}
		}
		start := time.Now()
		for _, r := range recs {
			if _, err := sys.Pipeline.Ingest(ctx, r); err != nil {
				return Summary{}, err
			}
		}
		if _, _, err := sys.Pipeline.FinalizeEpoch(ctx); err != nil {
			return Summary{}, err
		}
		samples = append(samples, time.Since(start))
	}
	return SummarizeDurations(samples), nil
}

// BatchPoint is one locality/batch-scaling measurement.
type BatchPoint struct {
	N         int
	Local     Summary
	Scattered Summary
	// LocalAncestors and ScatteredAncestors are g for each layout.
	LocalAncestors     int
	ScatteredAncestors int
	// Identical records whether both layouts produced the same accept/reject
	// outcome (R-VER-10 acceptance).
	Identical bool
	// ScatteredIsLocal is true when the pool held fewer than 2n leaves, so the
	// scattering step rounded down to 1 and the "scattered" batch is exactly
	// the local one. Such a row is not a locality comparison and the report
	// says so rather than presenting it as one.
	ScatteredIsLocal bool
}

// BatchScaling measures R-EVAL-06's subtree-local versus randomly scattered
// batches at the Q30 batch sizes.
func BatchScaling(env Env, cfg mktree.Config, dist producer.Distribution, datasetSize int) ([]BatchPoint, error) {
	run, err := prepare(env, cfg, dist, datasetSize)
	if err != nil {
		return nil, err
	}
	tok := run.sys.Token("auditor-1", run.epoch)
	ctx := context.Background()

	// Use the log type with the most records so large batches stay inside one
	// tree.
	var pool []string
	for _, lt := range cfg.LogTypes {
		if len(run.byType[lt]) > len(pool) {
			pool = run.byType[lt]
		}
	}
	var points []BatchPoint
	for _, n := range ScalingBatchSizes {
		if n > len(pool) {
			continue
		}
		local := pool[:n]
		scattered := make([]string, 0, n)
		step := len(pool) / n
		if step < 1 {
			step = 1
		}
		for i := 0; i < n && i*step < len(pool); i++ {
			scattered = append(scattered, pool[i*step])
		}
		localTimes := make([]time.Duration, 0, Repetitions)
		scatterTimes := make([]time.Duration, 0, Repetitions)
		var localRes, scatterRes verifier.Result
		for i := 0; i < Repetitions; i++ {
			start := time.Now()
			localRes, err = run.sys.Verifier.VerifyBatch(ctx, &tok, local)
			localTimes = append(localTimes, time.Since(start))
			if err != nil || !localRes.Accepted {
				return nil, fmt.Errorf("local batch n=%d failed: %v", n, err)
			}
			start = time.Now()
			scatterRes, err = run.sys.Verifier.VerifyBatch(ctx, &tok, scattered)
			scatterTimes = append(scatterTimes, time.Since(start))
			if err != nil || !scatterRes.Accepted {
				return nil, fmt.Errorf("scattered batch n=%d failed: %v", n, err)
			}
		}
		points = append(points, BatchPoint{
			N:                  n,
			Local:              SummarizeDurations(localTimes),
			Scattered:          SummarizeDurations(scatterTimes),
			LocalAncestors:     localRes.Ancestors,
			ScatteredAncestors: scatterRes.Ancestors,
			Identical:          localRes.Accepted == scatterRes.Accepted,
			ScatteredIsLocal:   step == 1,
		})
	}
	return points, nil
}

// StridePoint is one stride-sensitivity measurement.
type StridePoint struct {
	Distribution producer.Distribution
	Stride       uint64
	// N is the batch size actually measured. Q30 fixes n = 1024, but
	// StrideWindowBatch is only an upper bound here: the batch is every record
	// of the largest log type, capped at 1024, so N can be smaller. The report
	// prints N rather than the constant (GAP-7 in TRACKER.md).
	N         int
	Batch     Summary
	Ancestors int
	HashCount uint64
	// Bound is Theorem G.1's n(L - s) + gs for the measured batch.
	Bound uint64
}

// StrideSensitivity measures R-EVAL-06 over s in {1, 2, 4, 8} at n = 1024 for
// one input distribution.
func StrideSensitivity(env Env, base mktree.Config, dist producer.Distribution) ([]StridePoint, error) {
	var out []StridePoint
	for _, s := range StrideValues {
		cfg := base
		cfg.Stride = s
		run, err := prepare(env, cfg, dist, int(cfg.EpochCapacity))
		if err != nil {
			return nil, err
		}
		tok := run.sys.Token("auditor-1", run.epoch)
		ctx := context.Background()
		var pool []string
		for _, lt := range cfg.LogTypes {
			if len(run.byType[lt]) > len(pool) {
				pool = run.byType[lt]
			}
		}
		batch := pool
		if len(batch) > StrideWindowBatch {
			batch = batch[:StrideWindowBatch]
		}
		times := make([]time.Duration, 0, Repetitions)
		var res verifier.Result
		for i := 0; i < Repetitions; i++ {
			start := time.Now()
			res, err = run.sys.Verifier.VerifyBatch(ctx, &tok, batch)
			times = append(times, time.Since(start))
			if err != nil || !res.Accepted {
				return nil, fmt.Errorf("stride %d batch failed: %v", s, err)
			}
		}
		levels := levelsFor(cfg.EpochCapacity)
		out = append(out, StridePoint{
			Distribution: dist,
			Stride:       s,
			N:            len(batch),
			Batch:        SummarizeDurations(times),
			Ancestors:    res.Ancestors,
			HashCount:    res.HashCount,
			Bound:        uint64(len(batch))*(levels-s) + uint64(res.Ancestors)*s,
		})
	}
	return out, nil
}

// WindowPoint is one window-sensitivity measurement.
type WindowPoint struct {
	Distribution producer.Distribution
	Window       uint64
	Windows      int
	MicroRoot    Summary
}

// WindowSensitivity measures R-EVAL-06 over W in {256, 512, 1024, 2048} for
// one input distribution, timing the provisional micro-root check.
func WindowSensitivity(env Env, base mktree.Config, dist producer.Distribution) ([]WindowPoint, error) {
	var out []WindowPoint
	for _, w := range WindowValues {
		cfg := base
		cfg.Window = w
		run, err := prepare(env, cfg, dist, int(cfg.EpochCapacity))
		if err != nil {
			return nil, err
		}
		tok := run.sys.Token("auditor-1", run.epoch)
		ctx := context.Background()
		var pool []string
		for _, lt := range cfg.LogTypes {
			if len(run.byType[lt]) > len(pool) {
				pool = run.byType[lt]
			}
		}
		times := make([]time.Duration, 0, Repetitions)
		for i := 0; i < Repetitions; i++ {
			id := pool[i%len(pool)]
			start := time.Now()
			res, err := run.sys.Verifier.VerifyMicroRoot(ctx, &tok, id)
			times = append(times, time.Since(start))
			if err != nil || !res.Accepted {
				return nil, fmt.Errorf("window %d micro-root check failed: %v", w, err)
			}
		}
		windows := (len(pool) + int(w) - 1) / int(w)
		out = append(out, WindowPoint{
			Distribution: dist,
			Window:       w,
			Windows:      windows,
			MicroRoot:    SummarizeDurations(times),
		})
	}
	return out, nil
}

// OnChainPoint records the on-chain bytes written for one epoch size.
type OnChainPoint struct {
	EpochSize    uint64
	BytesOnChain int
	Epoch        uint64
}

// OnChainStorage measures R-EVAL-07: on-chain bytes written per epoch across
// epoch sizes. The payload is (epoch, R_agg) only (Q15), so the storage
// written is one bytes32 regardless of the number of records.
func OnChainStorage(env Env, base mktree.Config, sizes []uint64) ([]OnChainPoint, error) {
	var out []OnChainPoint
	for _, size := range sizes {
		cfg := base
		cfg.EpochCapacity = size
		run, err := prepare(env, cfg, producer.Uniform, int(size))
		if err != nil {
			return nil, err
		}
		out = append(out, OnChainPoint{
			EpochSize:    size,
			BytesOnChain: blockchain.OnChainPayloadBytes,
			Epoch:        run.epoch,
		})
	}
	return out, nil
}

func levelsFor(capacity uint64) uint64 {
	var levels uint64
	for w := mktree.PaddedWidth(capacity); w > 1; w >>= 1 {
		levels++
	}
	return levels
}
