package benchmark

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	contract "canonicalingestion"
	"canonicalingestion/testfixture"
	"dhtbacked/encryptor"
	"dhtbacked/internal/cassandradht"
	"dhtbacked/producer"
)

// TEST ONLY: per-record fixed entropy lets independent executions compare exact
// proof bytes. Production keeps crypto/rand nonces; no production flag exists.
type fixedFixtureEncryption struct{ aead cipher.AEAD }

func (f fixedFixtureEncryption) ProcessRecord(r encryptor.Record, key ed25519.PrivateKey) (encryptor.Emitted, error) {
	n := sha256.Sum256([]byte("fixture-only/" + r.RecordID))
	ad := encryptor.ADOf(r)
	sealed := f.aead.Seal(nil, n[:12], encryptor.CanonicalBytes(r), ad)
	c := append(append([]byte{}, n[:12]...), sealed[:len(sealed)-16]...)
	tag := sealed[len(sealed)-16:]
	d := encryptor.LeafDigest(c, tag, ad)
	return encryptor.Emitted{C: c, T: tag, AD: ad, D: d, Sigma: encryptor.Attest(key, d), ProducerPublicKey: key.Public().(ed25519.PublicKey)}, nil
}

func TestConcurrentDHTOldStateEquivalenceAndPublicationRecovery(t *testing.T) {
	keys := t.TempDir()
	if err := PrepareDHTKeys(keys); err != nil {
		t.Fatal(err)
	}
	block, _ := aes.NewCipher(make([]byte, 32))
	aead, _ := cipher.NewGCM(block)
	var snapshots, proofSnapshots [2]string
	var checkpoints [2][]byte
	var nativeIDs [2][]string
	for mode := 0; mode < 2; mode++ {
		db, dsn := testfixture.Database(t, "DHT_TEST_POSTGRES_DSN")
		a, cfg := logicalOptions(t, db, keys, t.TempDir())
		a.Execution = nil
		a.Options.EpochCapacity = 32
		a.enc = fixedFixtureEncryption{aead}
		if err := a.Migrate(context.Background()); err != nil {
			t.Fatal(err)
		}
		proofDir := t.TempDir()
		proof, closeProof := fixtureProof(t, cfg, a.ProofIdentity("fixture", "v1"), proofDir, "")
		defer closeProof()
		limits := *cfg.Limits
		limits.Workers = 4
		publisher, err := cassandradht.NewPublisher(context.Background(), proof, limits)
		if err != nil {
			t.Fatal(err)
		}
		p := contract.Progress{Target: 100}
		fingerprint := a.ConfigHash()
		apply := func(fn contract.Apply, n int, commit bool) {
			t.Helper()
			tx, err := a.DB.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			ids, checkpoint, err := fn(context.Background(), tx, p, testfixture.Entries(p.Committed+1, n))
			if err != nil {
				tx.Rollback()
				t.Fatal(err)
			}
			if len(ids) != n {
				t.Fatal("lost native IDs")
			}
			if !commit {
				tx.Rollback()
				return
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			p.Committed += uint64(n)
			p.Checkpoint = checkpoint
			nativeIDs[mode] = append(nativeIDs[mode], ids...)
		}
		apply(a.applySequentialReference, 100, true)
		p.Target = 257
		if err := a.PublishPending(context.Background(), publisher); err != nil {
			t.Fatal(err)
		}
		prefix := testfixture.Snapshot(t, a.DB, `SELECT record_id,c,t,ad,d,sigma FROM dhtbacked.records WHERE epoch<=4 ORDER BY record_id`, `SELECT epoch,root FROM dhtbacked.dht_epoch_anchors WHERE epoch<=4 ORDER BY epoch`)
		if mode == 1 {
			e := contract.DefaultExecution()
			e.CPUWorkers = 8
			a.Execution = &e
			if a.ConfigHash() != fingerprint {
				t.Fatal("execution changed native fingerprint")
			}
			apply(a.Apply, 23, false)
			var n int
			if err := db.QueryRow(`SELECT count(*) FROM dhtbacked.records`).Scan(&n); err != nil || n != 100 {
				t.Fatal("rolled-back records survived", n, err)
			}
			db.Close()
			reopened, err := contract.OpenPostgres(context.Background(), dsn, 16)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			a.DB = reopened
		}
		injected := false
		for p.Committed < 257 {
			fn := a.applySequentialReference
			if mode == 1 {
				fn = a.Apply
			}
			apply(fn, min(23, int(257-p.Committed)), true)
			if mode == 1 && !injected && p.Committed > 128 {
				if err := a.PublishPending(context.Background(), failAfterProofFlush{publisher}); err == nil {
					t.Fatal("publication interruption not detected")
				}
				tx, _ := a.DB.BeginTx(context.Background(), nil)
				if err := a.PublicationReady(context.Background(), tx, p); err == nil {
					t.Fatal("unpublished epoch accepted")
				}
				tx.Rollback()
				injected = true
			}
			if err := a.PublishPending(context.Background(), publisher); err != nil {
				t.Fatal(err)
			}
		}
		tx, _ := a.DB.BeginTx(context.Background(), nil)
		err = a.PublicationReady(context.Background(), tx, p)
		tx.Rollback()
		if err != nil {
			t.Fatal(err)
		}
		if prefix != testfixture.Snapshot(t, a.DB, `SELECT record_id,c,t,ad,d,sigma FROM dhtbacked.records WHERE epoch<=4 ORDER BY record_id`, `SELECT epoch,root FROM dhtbacked.dht_epoch_anchors WHERE epoch<=4 ORDER BY epoch`) {
			t.Fatal("upgrade modified committed encrypted state/anchors")
		}
		snapshots[mode] = testfixture.Snapshot(t, a.DB, `SELECT record_id,epoch,log_type,leaf_index,producer_id,timestamp_ns,c,t,ad,d,sigma,producer_public_key FROM dhtbacked.records ORDER BY record_id`, `SELECT epoch,record_count,capacity,counts,source_highwater,root FROM dhtbacked.dht_ingestion_epochs ORDER BY epoch`, `SELECT epoch,root,capacity,record_count FROM dhtbacked.dht_epoch_anchors ORDER BY epoch`)
		h := sha256.New()
		files, err := os.ReadDir(proofDir)
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			b, err := os.ReadFile(filepath.Join(proofDir, file.Name()))
			if err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(h, "%s:%x\n", file.Name(), b)
		}
		proofSnapshots[mode] = fmt.Sprintf("%x", h.Sum(nil))
		checkpoints[mode] = p.Checkpoint
		var total uint64
		for _, typ := range producer.LogTypes {
			var n uint64
			if err := a.DB.QueryRow(`SELECT count(*) FROM dhtbacked.records WHERE log_type=$1`, typ).Scan(&n); err != nil {
				t.Fatal(err)
			}
			total += n
		}
		if total != 257 {
			t.Fatal("record gap", total)
		}
	}
	if snapshots[0] != snapshots[1] || proofSnapshots[0] != proofSnapshots[1] || !bytes.Equal(checkpoints[0], checkpoints[1]) || !slices.Equal(nativeIDs[0], nativeIDs[1]) {
		t.Fatal("DHT sequential/concurrent artifacts differ", snapshots, proofSnapshots)
	}
}
