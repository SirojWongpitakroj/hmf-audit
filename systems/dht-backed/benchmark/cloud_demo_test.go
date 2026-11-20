package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	"dhtbacked/auth"
	"dhtbacked/blockchain"
	"dhtbacked/encryptor"
	"dhtbacked/internal/cassandradht"
	"dhtbacked/internal/clock"
	"dhtbacked/mktree"
	"dhtbacked/storage"
	"dhtbacked/verifier"
)

func TestCloudDemoSharedBatch(t *testing.T) {
	path := os.Getenv("DEMO_BATCH_IN")
	if path == "" || os.Getenv("DEMO_TRUST_STATE") == "" {
		t.Skip("run from the cloud dashboard")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var ordinals []int
	if err := json.Unmarshal(data, &ordinals); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(os.Getenv("DEMO_TRUST_STATE"))
	if err != nil {
		t.Fatal(err)
	}
	var trust cloudDHTTrust
	if err := json.Unmarshal(data, &trust); err != nil {
		t.Fatal("invalid DHT-backed trusted state")
	}
	ca, err := auth.NewCA(trust.PrivateKey, clock.System{})
	if err != nil {
		t.Fatal("invalid DHT-backed CA key")
	}
	repeats, err := strconv.Atoi(os.Getenv("DEMO_QUERY_REPEATS"))
	if err != nil || repeats < 1 || repeats > 20 {
		t.Fatal("invalid repeat count")
	}
	config := mktree.DefaultConfig()
	config.EpochCapacity = 10_000_000
	sortTypes := append([]string(nil), config.LogTypes...)
	slices.Sort(sortTypes)
	config.LogTypes = sortTypes
	ctx := context.Background()
	chain := blockchain.NewMemory()
	if err := chain.Commit(ctx, fairDHTEpoch, trust.Root); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(ctx, os.Getenv("FAIR_DHT_POSTGRES_DSN")+"&default_transaction_read_only=on")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(ctx)
	proofStore := openFairDHTCassandra(t)
	defer proofStore.Close()
	limits, err := fairDHTBenchmarkLimits(8)
	if err != nil {
		t.Fatal(err)
	}
	proofs, err := cassandradht.New(ctx, proofStore, ca.PublicKey(), cassandradht.Config{Peers: 16, Concurrency: 8, Limits: &limits})
	if err != nil {
		t.Fatal(err)
	}
	v := verifier.New(store, proofs, chain, config)
	ids := make([]string, len(ordinals))
	for i, ordinal := range ordinals {
		if ordinal < 0 || ordinal >= 10_000_000 || (i > 0 && ordinal <= ordinals[i-1]) {
			t.Fatal("invalid shared cloud batch")
		}
		ids[i] = encryptor.FormatRecordID(uint64(ordinal))
	}
	var suspects []string
	var rejected bool
	var check func([]string)
	check = func(batch []string) {
		token := ca.Issue("dashboard-auditor", fairDHTEpoch, config.LogTypes)
		result, err := v.VerifyBatch(ctx, &token, batch)
		if err == nil && result.Accepted {
			return
		}
		if err != nil && !errors.Is(err, verifier.ErrRejected) {
			t.Fatal(err)
		}
		rejected = true
		if len(batch) == 1 {
			suspects = append(suspects, batch[0])
			return
		}
		mid := len(batch) / 2
		check(batch[:mid])
		check(batch[mid:])
	}
	timings := make([]float64, 0, repeats)
	for run := 0; run <= repeats; run++ {
		start := time.Now()
		suspects, rejected = suspects[:0], false
		for batch := range slices.Chunk(ids, 256) {
			check(batch)
		}
		if run > 0 {
			timings = append(timings, float64(time.Since(start))/float64(time.Millisecond))
		}
	}
	slices.Sort(timings)
	status, message := "VALID", fmt.Sprintf("Verified %d shared records", len(ids))
	if rejected {
		status, message = "REJECTED", "The batch proof failed without an isolated record"
		if len(suspects) > 0 {
			message = fmt.Sprintf("%d record proofs failed when checked individually. First IDs: %v", len(suspects), suspects[:min(len(suspects), 10)])
		}
	}
	payload, _ := json.Marshal(map[string]any{"type": "result", "scheme": "DHT-backed", "status": status,
		"message": message, "milliseconds": (timings[(repeats-1)/2] + timings[repeats/2]) / 2})
	fmt.Printf("DEMO_EVENT %s\n", payload)
}
