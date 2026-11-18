package benchmark

import (
	"context"
	"testing"

	"dhtbacked/internal/cassandradht"
	"dhtbacked/internal/hashing"
)

type identityRouteKV struct{}

func (identityRouteKV) Proof(context.Context, hashing.Digest) ([]byte, error) {
	return []byte{1}, nil
}
func (identityRouteKV) Route(_ context.Context, peer, level int) (int, error) {
	return cassandradht.RouteNext(peer, level), nil
}

// The 1,024-node audit routing model must not change the persisted dataset
// binding, and must be reported as 1,024 logical nodes with 10 hops.
func TestAuditLogicalNodesAreAuditOnlyAndReported(t *testing.T) {
	limits := cassandradht.DefaultLimits()
	native := cassandradht.Config{Peers: 4, Concurrency: limits.Workers, Limits: &limits, Overlay: "native"}
	a := &IncrementalDHT{Options: IncrementalOptions{Cassandra: &native, RunID: "existing", EpochCapacity: 1000}}
	before := a.ConfigHash()
	identity := a.ProofIdentity("stream", "fingerprint")
	a.AuditLogicalNodes = 1024
	if a.ConfigHash() != before || a.ProofIdentity("stream", "fingerprint") != identity {
		t.Fatal("audit logical nodes changed the persisted dataset binding")
	}
	model := native
	model.Peers = a.AuditLogicalNodes
	client, err := cassandradht.New(context.Background(), identityRouteKV{}, nil, model)
	if err != nil {
		t.Fatal(err)
	}
	stats, err := a.snapshotProof(client)
	if err != nil || stats.logicalNodes != 1024 || stats.routingHops != 10 {
		t.Fatal("audit routing metadata not propagated", stats, err)
	}
}
