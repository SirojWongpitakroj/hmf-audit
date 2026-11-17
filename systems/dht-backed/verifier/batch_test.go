package verifier_test

import (
	"context"
	"errors"
	"testing"

	"dhtbacked/auth"
	"dhtbacked/dht"
	"dhtbacked/internal/hashing"
	"dhtbacked/storage"
	"dhtbacked/verifier"
)

// batchSource serves the in-process DHT through the BatchProofSource contract.
// It answers a batch in reverse request order, so a verifier that depended on
// the order records come back in would fail. It counts single lookups so a test
// can confirm the fold never fell back to them.
type batchSource struct {
	dht        *dht.DHT
	singles    int
	batches    int
	requested  int
	failBatch  error
	lastKeySet map[hashing.Digest]struct{}
}

func (s *batchSource) GetParent(tok *auth.Token, req dht.ParentRequest) ([]byte, error) {
	s.singles++
	return s.dht.GetParent(tok, req)
}

func (s *batchSource) GetCert(tok *auth.Token, req dht.CertRequest) ([]byte, error) {
	s.singles++
	return s.dht.GetCert(tok, req)
}

func (s *batchSource) GetRoot(tok *auth.Token, req dht.RootRequest) ([]byte, error) {
	s.singles++
	return s.dht.GetRoot(tok, req)
}

func (s *batchSource) GetMicroRoot(tok *auth.Token, req dht.MicroRootRequest) ([]byte, error) {
	s.singles++
	return s.dht.GetMicroRoot(tok, req)
}

func (s *batchSource) GetMany(tok *auth.Token, reqs []verifier.ProofRequest) (map[hashing.Digest][]byte, error) {
	s.batches++
	s.requested += len(reqs)
	if s.failBatch != nil {
		return nil, s.failBatch
	}
	s.lastKeySet = make(map[hashing.Digest]struct{}, len(reqs))
	out := make(map[hashing.Digest][]byte, len(reqs))
	for i := len(reqs) - 1; i >= 0; i-- {
		r := reqs[i]
		if _, dup := s.lastKeySet[r.Key]; dup {
			return nil, errors.New("verifier requested the same key twice in one batch")
		}
		s.lastKeySet[r.Key] = struct{}{}
		var (
			raw []byte
			err error
		)
		switch r.Kind {
		case verifier.ProofParent:
			raw, err = s.dht.GetParent(tok, dht.ParentRequest{Epoch: r.Epoch, LogType: r.LogType, J: r.A, K: r.B, Key: r.Key})
		case verifier.ProofCert:
			raw, err = s.dht.GetCert(tok, dht.CertRequest{Epoch: r.Epoch, LogType: r.LogType, S: r.A, K: r.B, Key: r.Key})
		case verifier.ProofRoot:
			raw, err = s.dht.GetRoot(tok, dht.RootRequest{Epoch: r.Epoch, LogType: r.LogType, Key: r.Key})
		}
		if err != nil {
			return nil, err
		}
		out[r.Key] = raw
	}
	return out, nil
}

// batchStore adds the one-call read to the in-memory store.
type batchStore struct {
	*storage.Memory
	calls int
}

func (s *batchStore) GetMany(ctx context.Context, ids []string) ([]storage.Record, error) {
	s.calls++
	out := make([]storage.Record, 0, len(ids))
	for _, id := range ids {
		rec, err := s.Memory.Get(ctx, id)
		if errors.Is(err, storage.ErrNotFound) {
			return nil, &storage.MissingError{RecordID: id}
		}
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

func batchVerifier(f *fixture) (*verifier.Verifier, *batchSource, *batchStore) {
	src := &batchSource{dht: f.sys.DHT}
	store := &batchStore{Memory: f.store}
	return verifier.New(store, src, f.chain, f.sys.Options.Config), src, store
}

// A batch source must change how records are fetched and nothing else: the
// verdict, the hash count and the ancestor accounting are identical.
func TestBatchSourceMatchesSequentialVerification(t *testing.T) {
	f := build(t, 400, defaultConfig(), true)
	ctx := context.Background()
	for _, subset := range [][]int{{0}, {0, 1, 2, 3}, {5, 200, 399}} {
		var recordIDs []string
		for _, i := range subset {
			recordIDs = append(recordIDs, f.records[i].RecordID)
		}
		want, err := f.sys.Verifier.VerifyBatch(ctx, &f.token, recordIDs)
		if err != nil {
			t.Fatal(err)
		}
		v, src, store := batchVerifier(f)
		got, err := v.VerifyBatch(ctx, &f.token, recordIDs)
		if err != nil {
			t.Fatalf("subset %v: %v", subset, err)
		}
		if got != want {
			t.Fatalf("subset %v: batch result %+v, sequential %+v", subset, got, want)
		}
		if src.batches != 1 || src.singles != 0 {
			t.Fatalf("subset %v: %d batches and %d single lookups, want 1 and 0", subset, src.batches, src.singles)
		}
		if store.calls != 1 {
			t.Fatalf("subset %v: %d record batches, want 1", subset, store.calls)
		}
	}
}

// Records shared by several leaves are requested once: a batch naming the same
// record twice asks for exactly what the single record asks for.
func TestBatchSourceRequestsSharedRecordsOnce(t *testing.T) {
	f := build(t, 400, defaultConfig(), true)
	ctx := context.Background()
	id := f.records[5].RecordID

	v, single, _ := batchVerifier(f)
	if _, err := v.VerifyBatch(ctx, &f.token, []string{id}); err != nil {
		t.Fatal(err)
	}
	v, twice, _ := batchVerifier(f)
	if _, err := v.VerifyBatch(ctx, &f.token, []string{id, id}); err != nil {
		t.Fatal(err)
	}
	if twice.requested != single.requested {
		t.Fatalf("a repeated record requested %d proof records, the single record %d",
			twice.requested, single.requested)
	}
}

// The verifier recomputes every hash itself, so a record forged in the DHT is
// still caught when it arrives through a batch.
func TestBatchSourceStillRejectsForgedProofRecords(t *testing.T) {
	f := build(t, 300, defaultConfig(), true)
	ctx := context.Background()
	rec, err := f.store.Get(ctx, f.records[5].RecordID)
	if err != nil {
		t.Fatal(err)
	}
	levels, _ := geometry(f.sys.Options.Config)
	key := dht.KeyP(dht.TreeID(rec.Epoch, rec.LogType), levels, rec.LeafIndex^1)
	original, err := f.sys.DHT.GetRaw(key)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := dht.ParseParentNode(original)
	if err != nil {
		t.Fatal(err)
	}
	parent.Node[0] ^= 0xFF
	f.sys.DHT.PutRaw(key, parent.Serialize())
	defer f.sys.DHT.PutRaw(key, original)

	v, _, _ := batchVerifier(f)
	if _, err := v.VerifyInclusion(ctx, &f.token, rec.RecordID); !errors.Is(err, verifier.ErrRejected) {
		t.Fatalf("forged P_j,k returned %v, want a rejection", err)
	}
}

func TestBatchSourceFailureIsProofUnavailable(t *testing.T) {
	f := build(t, 200, defaultConfig(), true)
	v, src, _ := batchVerifier(f)
	src.failBatch = errors.New("cassandra down")
	_, err := v.VerifyInclusion(context.Background(), &f.token, f.records[0].RecordID)
	if !errors.Is(err, verifier.ErrProofUnavailable) || !errors.Is(err, verifier.ErrRejected) {
		t.Fatalf("batch failure returned %v, want ErrProofUnavailable", err)
	}
}

func TestBatchStoreMissingRecordIsRejected(t *testing.T) {
	f := build(t, 200, defaultConfig(), true)
	v, _, _ := batchVerifier(f)
	_, err := v.VerifyBatch(context.Background(), &f.token, []string{f.records[0].RecordID, "no-such-record"})
	if !errors.Is(err, verifier.ErrRecordNotFound) {
		t.Fatalf("missing record returned %v, want ErrRecordNotFound", err)
	}
}
