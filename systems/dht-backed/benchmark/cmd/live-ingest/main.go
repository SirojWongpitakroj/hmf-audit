package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"dhtbacked/auth"
	"dhtbacked/benchmark"
	"dhtbacked/blockchain"
	"dhtbacked/encryptor"
	"dhtbacked/internal/cassandradht"
	"dhtbacked/internal/clock"
	"dhtbacked/mktree"
	"dhtbacked/storage"
	"dhtbacked/verifier"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	mode := flag.String("mode", "ingest", "ingest or verify")
	input := flag.String("input", "-", "canonical JSONL file or - for stdin")
	dsn := flag.String("pg-dsn", os.Getenv("DHT_POSTGRES_DSN"), "PostgreSQL DSN")
	host := flag.String("cassandra-host", os.Getenv("DHT_CASSANDRA_HOST"), "Cassandra host")
	port := flag.Int("cassandra-port", 9042, "Cassandra port")
	work := flag.String("work-dir", "./dht-live-work", "temporary digest spool directory")
	keys := flag.String("key-dir", "./dht-live-keys", "persistent encryption and signing keys")
	epoch := flag.Uint64("epoch", 1, "first epoch to ingest or epoch to verify")
	capacity := flag.Uint64("epoch-capacity", 1000, "maximum records per epoch")
	peers := flag.Int("peers", 16, "modelled DHT peers")
	writers := flag.Int("seed-writers", 8, "bounded concurrent Cassandra proof writes")
	recordID := flag.String("record-id", "", "record to verify; default is first record in epoch")
	flag.Parse()
	if *dsn == "" || *host == "" {
		return fmt.Errorf("PostgreSQL DSN and Cassandra host are required")
	}
	ctx := context.Background()
	store, err := storage.Open(ctx, *dsn)
	if err != nil {
		return err
	}
	defer store.Close(ctx)
	proofs, err := cassandradht.Open(*host, *port)
	if err != nil {
		return err
	}
	defer proofs.Close()
	anchor, err := blockchain.OpenPostgresAnchor(ctx, *dsn)
	if err != nil {
		return err
	}
	defer anchor.Close(ctx)
	switch *mode {
	case "ingest":
		f := os.Stdin
		if *input != "-" {
			f, err = os.Open(*input)
			if err != nil {
				return err
			}
			defer f.Close()
		}
		r, err := benchmark.IngestCanonical(ctx, f, store, proofs, anchor, benchmark.LiveOptions{
			StartEpoch: *epoch, EpochCapacity: *capacity, Peers: *peers, SeedWriters: *writers, WorkDir: *work, KeyDir: *keys})
		if err != nil {
			return err
		}
		fmt.Printf("DHT-backed ingested records=%d sealed_epochs=%d\n", r.Records, r.Epochs)
		return nil
	case "verify":
		actual, _, err := anchor.Geometry(ctx, *epoch)
		if err != nil {
			return err
		}
		cfg := mktree.DefaultConfig()
		cfg.EpochCapacity = actual
		cfg.Stride = 0
		for w := mktree.PaddedWidth(actual); w > 1; w >>= 1 {
			cfg.Stride++
		}
		if cfg.Stride > mktree.DefaultStride {
			cfg.Stride = mktree.DefaultStride
		}
		priv, err := os.ReadFile(filepath.Join(*keys, "ca.sk"))
		if err != nil {
			return err
		}
		trusted, err := auth.LoadCAPublicKey(filepath.Join(*keys, "ca.pk"))
		if err != nil {
			return err
		}
		if len(priv) != ed25519.PrivateKeySize || !bytes.Equal(ed25519.PrivateKey(priv).Public().(ed25519.PublicKey), trusted) {
			return fmt.Errorf("invalid CA key pair")
		}
		ca, err := auth.NewCA(ed25519.PrivateKey(priv), clock.System{})
		if err != nil {
			return err
		}
		dht, err := cassandradht.New(ctx, proofs, ca.PublicKey(), cassandradht.Config{Peers: *peers, Concurrency: 1})
		if err != nil {
			return err
		}
		token := ca.Issue("live-auditor", *epoch, cfg.LogTypes)
		id := *recordID
		if id == "" {
			id = encryptor.FormatRecordID((*epoch - 1) * 1000000)
		}
		r, err := verifier.New(store, dht, anchor, cfg).VerifyInclusion(ctx, &token, id)
		if err != nil {
			return err
		}
		if !r.Accepted {
			return fmt.Errorf("DHT-backed verification rejected %s", id)
		}
		fmt.Printf("DHT-backed verified epoch=%d record_id=%s accepted=true\n", *epoch, id)
		return nil
	default:
		return fmt.Errorf("unknown mode %q", *mode)
	}
}
