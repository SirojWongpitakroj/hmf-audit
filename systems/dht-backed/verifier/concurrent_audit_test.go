package verifier_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"dhtbacked/dht"
)

func TestConcurrentPreparedAuditMatchesSerialAndRejectsTampering(t *testing.T) {
	f := build(t, 128, defaultConfig(), true)
	ctx := context.Background()
	v, source, _ := batchVerifier(f)
	recordIDs := ids(f.records[:64])
	check := func(accepted bool) {
		t.Helper()
		prepared, err := v.PrepareBatch(ctx, &f.token, recordIDs)
		if err != nil {
			t.Fatal(err)
		}
		before := source.requested
		serial, serialErr := v.VerifyPreparedBatch(prepared)
		if (serialErr == nil && serial.Accepted) != accepted {
			t.Fatal("unexpected reference verdict", serial, serialErr)
		}
		for _, workers := range []int{1, 2, 8, 2} {
			got, err := v.VerifyPreparedBatchConcurrent(ctx, prepared, workers)
			if (err == nil) != (serialErr == nil) || !reflect.DeepEqual(got, serial) {
				t.Fatal("parallel proof/hash/ancestor result differs", workers, got, serial, err, serialErr)
			}
			if err != nil && err.Error() != serialErr.Error() {
				t.Fatal("rejection changed", err, serialErr)
			}
		}
		if source.requested != before {
			t.Fatal("verification performed extra retrieval")
		}
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := v.VerifyPreparedBatchConcurrent(canceled, prepared, 2); !errors.Is(err, context.Canceled) {
			t.Fatal("cancellation ignored", err)
		}
	}
	check(true)
	record, err := f.store.Get(ctx, recordIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	levels, stride := geometry(f.sys.Options.Config)
	tree := dht.TreeID(record.Epoch, record.LogType)
	parentKey := dht.KeyP(tree, levels, record.LeafIndex^1)
	parentRaw, err := f.sys.DHT.GetRaw(parentKey)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := dht.ParseParentNode(parentRaw)
	if err != nil {
		t.Fatal(err)
	}
	parent.Node[0] ^= 0xff
	f.sys.DHT.PutRaw(parentKey, parent.Serialize())
	check(false)
	f.sys.DHT.PutRaw(parentKey, parentRaw)
	ancestor := record.LeafIndex >> (levels - stride)
	certKey := dht.KeyC(tree, stride, ancestor)
	certRaw, err := f.sys.DHT.GetRaw(certKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := dht.ParseAncestorCert(certRaw)
	if err != nil {
		t.Fatal(err)
	}
	cert.AncestorHash[0] ^= 0xff
	f.sys.DHT.PutRaw(certKey, cert.Serialize())
	check(false)
	f.sys.DHT.PutRaw(certKey, certRaw)
	check(true)
}
