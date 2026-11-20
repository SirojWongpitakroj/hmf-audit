package integrity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestCloudDemoSharedBatch(t *testing.T) {
	path := os.Getenv("DEMO_BATCH_IN")
	if path == "" || os.Getenv("DEMO_TRUST_STATE") == "" {
		t.Skip("run from the cloud dashboard")
	}
	input, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var ordinals []int
	if err := json.Unmarshal(input, &ordinals); err != nil {
		t.Fatal(err)
	}
	anchor, err := os.ReadFile(os.Getenv("DEMO_TRUST_STATE"))
	if err != nil || len(anchor) != fairEVBESDigest {
		t.Fatal("invalid EVSEB trusted root")
	}
	repeats, err := strconv.Atoi(os.Getenv("DEMO_QUERY_REPEATS"))
	if err != nil || repeats < 1 || repeats > 20 {
		t.Fatal("invalid repeat count")
	}
	ctx := context.Background()
	config, err := pgx.ParseConfig(os.Getenv("FAIR_POSTGRES_DSN"))
	if err != nil {
		t.Fatal("invalid EVSEB PostgreSQL configuration")
	}
	config.RuntimeParams["default_transaction_read_only"] = "on"
	database, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close(ctx)
	dht, err := newFairEVBESCassandraDHT(os.Getenv("FAIR_CASSANDRA_HOST"), os.Getenv("FAIR_CASSANDRA_PORT"), 8, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer dht.Close()
	candidates := make([]string, len(ordinals))
	for i, ordinal := range ordinals {
		if ordinal < 0 || ordinal >= 10_000_000 || (i > 0 && ordinal <= ordinals[i-1]) {
			t.Fatal("invalid shared cloud batch")
		}
		candidates[i] = fairEVBESLID(ordinal)
	}
	timings := make([]float64, 0, repeats)
	var suspects []string
	var rejected bool
	for run := 0; run <= repeats; run++ {
		start := time.Now()
		suspects, rejected = suspects[:0], false
		for batch := range slices.Chunk(candidates, 1024) {
			stored, _, err := loadFairEVBESStoredTree(ctx, database, dht, batch)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(stored.Root, anchor) {
				t.Fatal("cloud EVSEB root differs from trusted ingest root")
			}
			prepared, err := loadFairEVBESPrepared(ctx, database, batch, stored)
			if err != nil {
				t.Fatal(err)
			}
			_, computed, _ := RecomputeMerkleRootPrepared(prepared, stored)
			verified, intact, err := LocalizeComputed(prepared, computed, anchor)
			if err != nil {
				t.Fatal(err)
			}
			if !intact || len(verified) != len(batch) {
				rejected = true
				passed := make(map[string]bool, len(verified))
				for _, id := range verified {
					passed[id] = true
				}
				for _, id := range batch {
					if !passed[id] {
						suspects = append(suspects, id)
					}
				}
			}
		}
		if run > 0 {
			timings = append(timings, float64(time.Since(start))/float64(time.Millisecond))
		}
	}
	slices.Sort(timings)
	status, message := "VALID", fmt.Sprintf("Verified %d shared records", len(candidates))
	if rejected {
		status, message = "REJECTED", "The batch proof failed without an isolated record"
		if len(suspects) > 0 {
			message = fmt.Sprintf("%d suspect records localized. First IDs: %v", len(suspects), suspects[:min(len(suspects), 10)])
		}
	}
	payload, _ := json.Marshal(map[string]any{"type": "result", "scheme": "EVSEB", "status": status,
		"message": message, "milliseconds": (timings[(repeats-1)/2] + timings[repeats/2]) / 2})
	fmt.Printf("DEMO_EVENT %s\n", payload)
}

func saveCloudDemoEVSEBTrust(anchor []byte, count int) error {
	if count != 10_000_000 || os.Getenv("FAIR_CLOUD") != "1" {
		return nil
	}
	dir := os.Getenv("FAIR_DASHBOARD_TRUST_DIR")
	if dir == "" {
		systems := filepath.Dir(filepath.Dir(os.Getenv("FAIR_DATASET_PATH")))
		dir = filepath.Join(systems, "demo-scripts-v2", "site", ".dashboard", "trust")
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return nil
		}
	}
	tmp, err := os.CreateTemp(dir, "evseb-anchor-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, writeErr := tmp.Write(anchor)
	closeErr := tmp.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, "evseb.anchor"))
}
