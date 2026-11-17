package benchmark_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dhtbacked/benchmark"
	"dhtbacked/blockchain"
	"dhtbacked/encryptor"
	"dhtbacked/internal/clock"
	"dhtbacked/internal/config"
	"dhtbacked/internal/hashing"
	"dhtbacked/mktree"
	"dhtbacked/producer"
	"dhtbacked/storage"
)

// Q39: the mean and the 95% confidence interval are computed in ordinary Go
// code from the Student-t critical value for 49 degrees of freedom.
func TestSummarizeMeanAndConfidenceInterval(t *testing.T) {
	samples := make([]float64, benchmark.Repetitions)
	for i := range samples {
		samples[i] = float64(i)
	}
	s := benchmark.Summarize(samples)
	if s.N != 50 {
		t.Fatalf("N = %d, want 50", s.N)
	}
	if math.Abs(s.Mean-24.5) > 1e-9 {
		t.Fatalf("mean = %v, want 24.5", s.Mean)
	}
	// Population of 0..49: sample standard deviation is sqrt(sum/49).
	var ss float64
	for _, v := range samples {
		d := v - 24.5
		ss += d * d
	}
	wantSD := math.Sqrt(ss / 49)
	if math.Abs(s.StdDev-wantSD) > 1e-9 {
		t.Fatalf("stddev = %v, want %v", s.StdDev, wantSD)
	}
	wantCI := 2.009575 * wantSD / math.Sqrt(50)
	if math.Abs(s.CI95-wantCI) > 1e-6 {
		t.Fatalf("CI half-width = %v, want %v", s.CI95, wantCI)
	}
	if s.Min != 0 || s.Max != 49 {
		t.Fatalf("min/max = %v/%v", s.Min, s.Max)
	}
	// A single sample has no interval.
	if one := benchmark.Summarize([]float64{7}); one.CI95 != 0 || one.Mean != 7 {
		t.Fatalf("single-sample summary = %+v", one)
	}
}

// Q30: the batch sizes are exactly those the decision fixes.
func TestBatchSizesMatchQ30(t *testing.T) {
	if benchmark.CommitBatch != 1000 || benchmark.SingleRecordBatch != 1 ||
		benchmark.ThroughputRequests != 1024 || benchmark.StrideWindowBatch != 1024 {
		t.Fatal("a Q30 batch size changed")
	}
	want := []int{64, 128, 256, 512, 1024, 2048, 4096}
	if len(benchmark.ScalingBatchSizes) != len(want) {
		t.Fatalf("scaling sizes = %v", benchmark.ScalingBatchSizes)
	}
	for i := range want {
		if benchmark.ScalingBatchSizes[i] != want[i] {
			t.Fatalf("scaling sizes = %v, want %v", benchmark.ScalingBatchSizes, want)
		}
	}
	if len(benchmark.StrideValues) != 4 || benchmark.StrideValues[3] != 8 {
		t.Fatalf("stride values = %v", benchmark.StrideValues)
	}
	if len(benchmark.WindowValues) != 4 || benchmark.WindowValues[3] != 2048 {
		t.Fatalf("window values = %v", benchmark.WindowValues)
	}
	if len(benchmark.EpochSizes) != 3 || benchmark.EpochSizes[2] != 100000 {
		t.Fatalf("epoch sizes = %v", benchmark.EpochSizes)
	}
}

// R-EVAL-09/R-EVAL-10/R-EVAL-11: the report reproduces the functional
// comparison and labels baseline values as author-reported, not reproduced.
func TestReportContent(t *testing.T) {
	r := benchmark.NewReport(1000, 8, 1024)
	r.Note("example note")
	md := r.Markdown()
	for _, want := range []string{
		"reported, not asserted",
		"MK-Tree + ancestor certificates + DHT",
		"Blockchain anchor + CA-issued tokens",
		"NOT implemented and NOT reproduced",
		"reported by the original authors; NOT reproduced",
		"exactly 50 independent repetitions",
		"The paper does not specify any of these batch sizes",
		"no operation in Sec. III uses a GPU",
		"example note",
	} {
		if !strings.Contains(md, want) && !strings.Contains(strings.ToLower(md), strings.ToLower(want)) {
			t.Errorf("report is missing %q", want)
		}
	}
	if !strings.Contains(md, "`"+r.GOOS+"`") || !strings.Contains(md, "`"+r.GOARCH+"`") {
		t.Error("report does not record the actual OS and architecture")
	}
}

// The pipeline is Producer -> Encryptor/Attestation -> Ingestion Verifier ->
// accepted storage + Partitioner (Q20), and a discarded record contributes
// nothing downstream (R-ING-03).
func TestPipelineDiscardsUnverifiedRecords(t *testing.T) {
	dir := t.TempDir()
	key := make([]byte, 32)
	store := storage.NewMemory()
	sys, err := benchmark.NewSystem(benchmark.Options{
		Config:       mktree.DefaultConfig(),
		AEADKey:      key,
		KeyDir:       filepath.Join(dir, "producers"),
		RegistryPath: filepath.Join(dir, "registry.json"),
		CADir:        dir,
		Store:        store,
		Chain:        blockchain.NewMemory(),
		Clock:        &clock.Manual{T: time.Now().UTC()},
	})
	if err != nil {
		t.Fatal(err)
	}
	g := producer.NewGenerator(producer.Uniform)
	recs, err := g.Generate(20)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, r := range recs {
		if _, err := sys.Pipeline.Ingest(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	before := sys.DHT.Count()
	if store.Len() != 20 {
		t.Fatalf("%d records stored, want 20", store.Len())
	}

	// A record from an unregistered producer never reaches storage, the tree
	// or the DHT.
	rogue := recs[0]
	rogue.RecordID = "999999999999"
	rogue.ProducerID = "producer-rogue"
	if _, err := sys.Pipeline.Ingest(ctx, rogue); err == nil {
		t.Fatal("a record from an unregistered producer was ingested")
	}
	if store.Len() != 20 || sys.DHT.Count() != before {
		t.Fatal("a discarded record changed storage or the DHT")
	}
	if sys.Pipeline.Partitioner().Accepted() != 20 {
		t.Fatalf("partitioner accepted %d records", sys.Pipeline.Partitioner().Accepted())
	}
}

// RunEvaluation produces the full report. It is skipped unless
// DHTBACKED_RUN_BENCHMARKS=1, because it takes minutes and needs the external
// Hardhat chain when one is configured.
func TestRunEvaluation(t *testing.T) {
	if os.Getenv("DHTBACKED_RUN_BENCHMARKS") != "1" {
		t.Skip("set DHTBACKED_RUN_BENCHMARKS=1 to run the evaluation")
	}
	dir := t.TempDir()
	key := fidelityAEADKey(t)

	report := benchmark.NewReport(mktree.DefaultEpochCapacity, mktree.DefaultStride, mktree.DefaultWindow)
	a, rpc := fidelityChain(t)
	var chain benchmark.Chain = a
	report.Note("Anchoring ran against the external Hardhat v2.20 chain at %s, chain id %d.", rpc, a.ChainID())
	// GAP-2 decision (user, 2026-09-11): keep the in-memory store for these
	// benchmarks, labelled clearly as excluding PostgreSQL I/O.
	report.Note("Records were held in the in-memory store (storage.Memory), not PostgreSQL. " +
		"Latency and throughput therefore exclude any database round trip for record retrieval. " +
		"Per the GAP-2 decision the figures are labelled as excluding PostgreSQL I/O.")

	// The anchoring contract is append-only (Q14), so each run must start above
	// every epoch already anchored on the chain.
	base := uint64(time.Now().UTC().Unix()) * 1000
	env := benchmark.Env{
		WorkDir:  dir,
		AEADKey:  key,
		Chain:    chain,
		NewStore: func() storage.Store { return storage.NewMemory() },
		NextEpochBase: func() uint64 {
			base++
			return base
		},
	}
	cfg := mktree.DefaultConfig()

	if s, err := benchmark.SingleRecordLatency(env, cfg); err != nil {
		report.Note("single-record latency failed: %v", err)
	} else {
		report.SingleRecordLatency = &s
	}
	if s, err := benchmark.VerificationThroughput(env, cfg); err != nil {
		report.Note("throughput failed: %v", err)
	} else {
		report.Throughput = &s
	}
	if s, err := benchmark.CommitTime(env, cfg); err != nil {
		report.Note("commit time failed: %v", err)
	} else {
		report.CommitTime = &s
	}
	for _, dist := range benchmark.Distributions {
		// GAP-7 decision: scaling and stride run in the 100,000-record epoch,
		// the only Q23 epoch in which every Q30 batch size fits in one tree.
		large := benchmark.LargeBatchConfig(cfg)
		if pts, err := benchmark.BatchScaling(env, large, dist, int(large.EpochCapacity)); err != nil {
			report.Note("batch scaling (%s) failed: %v", dist, err)
		} else {
			report.BatchScaling[string(dist)] = pts
		}
		if pts, err := benchmark.StrideSensitivity(env, large, dist); err != nil {
			report.Note("stride sensitivity (%s) failed: %v", dist, err)
		} else {
			report.Stride_[string(dist)] = pts
		}
		if pts, err := benchmark.WindowSensitivity(env, cfg, dist); err != nil {
			report.Note("window sensitivity (%s) failed: %v", dist, err)
		} else {
			report.WindowSweep[string(dist)] = pts
		}
	}
	sizes := benchmark.EpochSizes
	report.Note("On-chain storage was measured at epoch sizes %v (R-EPO-05 acceptance).", sizes)
	if pts, err := benchmark.OnChainStorage(env, cfg, sizes); err != nil {
		report.Note("on-chain storage failed: %v", err)
	} else {
		report.OnChain = pts
	}

	out := filepath.Join("..", "reports")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(out, "evaluation.md")
	if err := os.WriteFile(path, []byte(report.Markdown()), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s", path)
}

// R-ING-06: no plaintext M appears in any MK-Tree node, DHT record, or
// on-chain payload. R-VER-09: no AEAD decryption occurs on any audit path.
func TestNoPlaintextInIntegrityStructures(t *testing.T) {
	dir := t.TempDir()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 5)
	}
	store := storage.NewMemory()
	chain := blockchain.NewMemory()
	sys, err := benchmark.NewSystem(benchmark.Options{
		Config:       mktree.DefaultConfig(),
		AEADKey:      key,
		KeyDir:       filepath.Join(dir, "producers"),
		RegistryPath: filepath.Join(dir, "registry.json"),
		CADir:        dir,
		Store:        store,
		Chain:        chain,
		Clock:        &clock.Manual{T: time.Now().UTC()},
	})
	if err != nil {
		t.Fatal(err)
	}
	g := producer.NewGenerator(producer.Uniform)
	recs, err := g.Generate(200)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, r := range recs {
		if _, err := sys.Pipeline.Ingest(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	epoch, agg, err := sys.Pipeline.FinalizeEpoch(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Distinctive plaintext fragments that must not appear anywhere.
	var fragments [][]byte
	for _, r := range recs[:20] {
		m := encryptor.CanonicalBytes(r)
		fragments = append(fragments, m)
		if len(r.Attributes.Padding) >= 32 {
			fragments = append(fragments, []byte(r.Attributes.Padding[:32]))
		}
		fragments = append(fragments, []byte(r.Attributes.RequestID))
	}
	sys.DHT.ForEach(func(_ hashing.Digest, value []byte) {
		for _, frag := range fragments {
			if len(frag) >= 8 && bytes.Contains(value, frag) {
				t.Fatalf("a DHT record contains plaintext %q", frag)
			}
		}
	})
	// The on-chain payload is a single 32-byte root.
	onChain, err := chain.GetRoot(ctx, epoch)
	if err != nil {
		t.Fatal(err)
	}
	if onChain != agg || len(onChain) != 32 {
		t.Fatal("the on-chain payload is not the 32-byte root-of-roots")
	}
	for _, frag := range fragments {
		if len(frag) >= 8 && bytes.Contains(onChain[:], frag) {
			t.Fatal("the on-chain payload contains plaintext")
		}
	}
	// Every MK-Tree node is a 32-byte digest, never a plaintext.
	for _, lt := range mktree.DefaultConfig().LogTypes {
		tree, err := sys.Pipeline.Partitioner().Tree(lt)
		if err != nil {
			t.Fatal(err)
		}
		for j := uint64(0); j <= tree.Levels(); j++ {
			for k := uint64(0); k < uint64(1)<<j; k++ {
				n, err := tree.Node(j, k)
				if err != nil {
					t.Fatal(err)
				}
				if len(n) != 32 {
					t.Fatalf("node (%d,%d) is %d bytes", j, k, len(n))
				}
			}
		}
	}
}

// R-VER-09: the verifier package contains no decryption entry point.
func TestVerifierNeverDecrypts(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "verifier", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no verifier sources found")
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		for _, tok := range []string{".Open(", "Decrypt", "cipher.", "crypto/aes"} {
			if strings.Contains(src, tok) {
				t.Errorf("%s: references %q on a verification path", f, tok)
			}
		}
	}
}

// R-EVAL-07 / R-EPO-05: on-chain bytes written per epoch are identical at
// epoch sizes 1,000 / 10,000 / 100,000. Gated because the 100,000-record epoch
// takes several minutes.
func TestConstantOnChainStorageAcrossEpochSizes(t *testing.T) {
	if os.Getenv("DHTBACKED_RUN_BENCHMARKS") != "1" {
		t.Skip("set DHTBACKED_RUN_BENCHMARKS=1 to run the epoch-size sweep")
	}
	dir := t.TempDir()
	key := fidelityAEADKey(t)
	// This test measures on-chain bytes written per epoch (R-EPO-05). Against
	// the in-memory test double there are no on-chain bytes at all, so it must
	// run against the real contract or not at all.
	a, _ := fidelityChain(t)
	var chain benchmark.Chain = a
	base := uint64(time.Now().UTC().Unix()) * 1000
	env := benchmark.Env{
		WorkDir:  dir,
		AEADKey:  key,
		Chain:    chain,
		NewStore: func() storage.Store { return storage.NewMemory() },
		NextEpochBase: func() uint64 {
			base++
			return base
		},
	}
	pts, err := benchmark.OnChainStorage(env, mktree.DefaultConfig(), benchmark.EpochSizes)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 3 {
		t.Fatalf("%d measurements, want 3", len(pts))
	}
	for _, p := range pts {
		t.Logf("epoch size %d -> %d on-chain bytes (epoch %d)", p.EpochSize, p.BytesOnChain, p.Epoch)
		if p.BytesOnChain != pts[0].BytesOnChain {
			t.Fatalf("epoch size %d wrote %d bytes, want %d", p.EpochSize, p.BytesOnChain, pts[0].BytesOnChain)
		}
	}
}

// fidelityChain opens the external Hardhat chain for an evaluation run.
//
// The fidelity reproduction anchors on the real Hardhat v2.20 chain and never on
// an in-memory Go map (Q38), and an unavailable chain is a fail-fast startup
// error (Q46). An evaluation without a configured chain therefore refuses to run
// instead of substituting blockchain.Memory. That type remains available to the
// pure-logic tests, which never report a measured figure.
func fidelityChain(t *testing.T) (*blockchain.Anchor, string) {
	t.Helper()
	addr := os.Getenv(config.EnvContractAddress)
	pk := os.Getenv(config.EnvEVMPrivateKeyHex)
	if addr == "" || pk == "" {
		t.Fatalf("the evaluation anchors on the external Hardhat chain, never on the in-memory "+
			"test double (Q38); set %s and %s", config.EnvContractAddress, config.EnvEVMPrivateKeyHex)
	}
	rpc := os.Getenv(config.EnvEthRPCURL)
	if rpc == "" {
		rpc = config.DefaultEthRPCURL
	}
	a, err := blockchain.Open(context.Background(), rpc, config.DefaultChainID, addr, pk)
	if err != nil {
		t.Fatalf("Hardhat chain: %v", err)
	}
	t.Cleanup(a.Close)
	return a, rpc
}

// fidelityAEADKey loads the one system-wide AEAD key from DHT_AEAD_KEY_HEX
// (Q2, Q46).
//
// Q2 permits fixed test keys only in unit tests, for deterministic vectors. The
// evaluation is not a unit test, so it uses the configured key and refuses to
// run without one rather than hard-coding a substitute.
func fidelityAEADKey(t *testing.T) []byte {
	t.Helper()
	s := strings.TrimPrefix(os.Getenv(config.EnvAEADKeyHex), "0x")
	if len(s) != 2*config.AEADKeyLen {
		t.Fatalf("the evaluation uses the configured AEAD key (Q2); set %s to exactly %d hex characters",
			config.EnvAEADKeyHex, 2*config.AEADKeyLen)
	}
	key, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("%s is not valid hexadecimal", config.EnvAEADKeyHex)
	}
	return key
}

// DEF-4: the report must state the batch sizes it actually measured. It must
// not print Q30's n = 1024 for a stride run that measured fewer, must not
// present a "scattered" batch that collapsed to the local one as a locality
// comparison, and must list every Q30 scaling size it could not run.
func TestReportStatesMeasuredSizesNotQ30Sizes(t *testing.T) {
	r := benchmark.NewReport(1000, 8, 1024)
	sum := benchmark.Summary{N: 50, Mean: 0.01}
	r.BatchScaling["uniform"] = []benchmark.BatchPoint{
		{N: 64, Local: sum, Scattered: sum, LocalAncestors: 16, ScatteredAncestors: 48, Identical: true},
		{N: 128, Local: sum, Scattered: sum, LocalAncestors: 32, ScatteredAncestors: 32, Identical: true, ScatteredIsLocal: true},
	}
	r.Stride_["uniform"] = []benchmark.StridePoint{
		{Distribution: producer.Uniform, Stride: 1, N: 214, Batch: sum, Ancestors: 1, HashCount: 1927, Bound: 1927},
	}
	md := r.Markdown()

	for _, want := range []string{
		"If any section still measured a different size, it says so", // Q30 table no longer claims to be the workload
		"| uniform | 1 | 214 |",                                                                                                                                                // stride row shows the real n
		"n differs from Q30's n = 1024",                                                                                                                                        // and says why
		"— identical to local †",                                                                                                                                               // degenerate scattered row is flagged
		"not locality comparisons",                                                                                                                                             // with its footnote
		"Q30 sizes not measured",                                                                                                                                               // skipped scaling sizes are listed
		"uniform: n = 256, 512, 1024, 2048, 4096",                                                                                                                              // exactly the missing ones
		"One JSON-RPC `getRoot` call", "Ed25519 signature check on every DHT proof request", "makes 8 such requests", "PostgreSQL round trip", "100000-record epoch", "L = 17", // the latency figures are explained
	} {
		if !strings.Contains(md, want) {
			t.Errorf("report is missing %q", want)
		}
	}
	// The degenerate row must not print a scattered timing as if it were one.
	if strings.Contains(md, "| uniform | 128 | 0.010000 ± 0.000000 | 0.010000") {
		t.Error("a scattered batch identical to the local one is still reported as a locality comparison")
	}

	// A run where every size fit produces none of the caveats.
	full := benchmark.NewReport(100000, 8, 1024)
	for _, n := range benchmark.ScalingBatchSizes {
		full.BatchScaling["uniform"] = append(full.BatchScaling["uniform"],
			benchmark.BatchPoint{N: n, Local: sum, Scattered: sum, Identical: true})
	}
	full.Stride_["uniform"] = []benchmark.StridePoint{
		{Distribution: producer.Uniform, Stride: 8, N: benchmark.StrideWindowBatch, Batch: sum},
	}
	fmd := full.Markdown()
	for _, unwanted := range []string{"Q30 sizes not measured", "n differs from Q30", "identical to local"} {
		if strings.Contains(fmd, unwanted) {
			t.Errorf("a complete run's report wrongly contains %q", unwanted)
		}
	}
}
